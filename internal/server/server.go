package server

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/lucasfd3/Airlink/internal/engine"
	"github.com/lucasfd3/Airlink/internal/model"
	"github.com/lucasfd3/Airlink/internal/shares"
)

//go:embed assets/*
var assets embed.FS

type Options struct {
	DataDir, ReceiveDir string
	Port                int
	NoDiscovery         bool
}
type pendingPair struct {
	model.PairRequest
	PollHash     string
	State, Token string
	Device       model.Device
	Expires      time.Time
}
type Server struct {
	engine                               *engine.Engine
	shares                               *shares.Store
	clipboardMu                          sync.Mutex
	clipboardMessages                    []model.ClipboardMessage
	store                                *stateStore
	opts                                 Options
	fingerprint                          string
	certificate                          []byte
	urls, setupURLs                      []string
	lock                                 *flock.Flock
	mu                                   sync.Mutex
	pending                              map[string]*pendingPair
	discovered                           map[string]model.Device
	rate                                 map[string][]time.Time
	senders                              map[string]bool
	senderCancels                        map[string]context.CancelFunc
	active                               map[string]map[string]context.CancelFunc
	chunkSlots                           chan struct{}
	ctx                                  context.Context
	cancel                               context.CancelFunc
	httpServer, adminServer, setupServer *http.Server
	adminURL                             string
	wg                                   sync.WaitGroup
	stopDiscovery                        func()
}

func New(opts Options) (*Server, error) {
	if opts.DataDir == "" {
		dir, e := os.UserConfigDir()
		if e != nil {
			return nil, e
		}
		opts.DataDir = filepath.Join(dir, "AirLink")
	}
	if err := os.MkdirAll(opts.DataDir, 0700); err != nil {
		return nil, err
	}
	_ = os.Chmod(opts.DataDir, 0700)
	lock := flock.New(filepath.Join(opts.DataDir, "daemon.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("AirLink is already running with this data directory")
	}
	success := false
	defer func() {
		if !success {
			_ = lock.Unlock()
		}
	}()
	store, err := openStore(opts.DataDir, opts.ReceiveDir)
	if err != nil {
		return nil, err
	}
	p := store.snapshot()
	cert, fp, ca, err := identity(opts.DataDir, p.Settings.Name)
	if err != nil {
		return nil, err
	}
	eng, err := engine.New(opts.DataDir, p.Settings.ReceiveDirectory)
	if err != nil {
		return nil, err
	}
	shareStore, err := shares.New(opts.DataDir)
	if err != nil {
		eng.Close()
		return nil, err
	}
	for _, t := range eng.Transfers("") {
		if t.Direction == "receive" && t.State == "reconnecting" {
			_ = eng.Resume(t.ID)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{engine: eng, shares: shareStore, store: store, opts: opts, fingerprint: fp, certificate: ca, pending: map[string]*pendingPair{}, discovered: map[string]model.Device{}, rate: map[string][]time.Time{}, senders: map[string]bool{}, senderCancels: map[string]context.CancelFunc{}, active: map[string]map[string]context.CancelFunc{}, chunkSlots: make(chan struct{}, 2), ctx: ctx, cancel: cancel, lock: lock}
	port := opts.Port
	if port == 0 {
		port = 43871
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil && opts.Port == 0 {
		listener, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		eng.Close()
		shareStore.Close()
		cancel()
		return nil, err
	}
	port = listener.Addr().(*net.TCPAddr).Port
	for _, ip := range localIPs() {
		if !ip.IsLoopback() {
			s.urls = append(s.urls, "https://"+net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		}
	}
	s.urls = append(s.urls, "https://localhost:"+strconv.Itoa(port))
	var certMu sync.Mutex
	currentCert := cert
	ips := fmt.Sprint(localIPs())
	s.httpServer = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		certMu.Lock()
		defer certMu.Unlock()
		nextIPs := fmt.Sprint(localIPs())
		if nextIPs != ips {
			renewed, _, _, e := identity(opts.DataDir, p.Settings.Name)
			if e != nil {
				return nil, e
			}
			currentCert = renewed
			ips = nextIPs
		}
		return &currentCert, nil
	}}}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if e := s.httpServer.Serve(tls.NewListener(listener, s.httpServer.TLSConfig)); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Print(e)
		}
	}()
	if err = s.startAdmin(); err != nil {
		s.Close()
		return nil, err
	}
	if err = s.startSetup(); err != nil {
		s.Close()
		return nil, err
	}
	if !opts.NoDiscovery {
		s.discovery(port)
	}
	s.wg.Add(1)
	go s.outgoingLoop()
	s.wg.Add(1)
	go s.confirmationLoop()
	s.wg.Add(1)
	go s.heartbeatLoop()
	s.wg.Add(1)
	go s.clipboardLoop()
	success = true
	return s, nil
}
func (s *Server) Close() error {
	s.cancel()
	if s.stopDiscovery != nil {
		s.stopDiscovery()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(ctx)
		_ = s.httpServer.Close()
	}
	if s.adminServer != nil {
		_ = s.adminServer.Shutdown(ctx)
	}
	if s.setupServer != nil {
		_ = s.setupServer.Shutdown(ctx)
	}
	s.wg.Wait()
	e := s.engine.Close()
	if s.shares != nil {
		if shareErr := s.shares.Close(); e == nil {
			e = shareErr
		}
	}
	if s.lock != nil {
		_ = s.lock.Unlock()
	}
	return e
}
func (s *Server) AdminURL() string { return s.adminURL }
func (s *Server) Status() model.Status {
	p := s.store.snapshot()
	ds := []model.Device{}
	known := map[string]bool{}
	for _, r := range p.Devices {
		d := r.Device
		t, _ := time.Parse(time.RFC3339, d.LastSeen)
		d.Online = time.Since(t) < 45*time.Second
		ds = append(ds, d)
		known[d.ID] = true
	}
	s.mu.Lock()
	for id, d := range s.discovered {
		if !known[id] {
			ds = append(ds, d)
		}
	}
	requests := []model.PairRequest{}
	for _, r := range s.pending {
		if r.State == "pending" && time.Now().Before(r.Expires) {
			requests = append(requests, r.PairRequest)
		}
	}
	s.mu.Unlock()
	return model.Status{Version: model.Version, ID: p.ID, Name: p.Settings.Name, URLs: s.currentURLs(s.urls), SetupURLs: s.currentURLs(s.setupURLs), Fingerprint: s.fingerprint, CertificatePath: filepath.Join(s.opts.DataDir, "certificate.pem"), ChunkSize: model.ChunkSize, Devices: ds, Transfers: s.engine.Transfers(""), PairRequests: requests, Settings: p.Settings, Shares: s.shareSummaries(""), Clipboard: s.clipboardSnapshot(""), ClipboardAllowed: true}
}
func (s *Server) guestStatus(d model.Device) model.Status {
	p := s.store.snapshot()
	host := model.Device{ID: p.ID, Name: p.Settings.Name, Kind: "engine", Paired: true, Online: true, CanClipboard: d.CanClipboard}
	ts := s.engine.Transfers(d.ID)
	for i := range ts {
		ts[i].Destination = ""
		if ts[i].Error != "" {
			ts[i].Error = "Transfer needs attention. Check the source and destination on the host."
		}
	}
	messages := []model.ClipboardMessage{}
	if d.CanClipboard {
		messages = s.clipboardSnapshot(d.ID)
	}
	return model.Status{Version: model.Version, ID: p.ID, Name: p.Settings.Name, URLs: s.currentURLs(s.urls), Fingerprint: s.fingerprint, ChunkSize: model.ChunkSize, Devices: []model.Device{host}, Transfers: ts, PairRequests: []model.PairRequest{}, Shares: s.shareSummaries(d.ID), Clipboard: messages, ClipboardAllowed: d.CanClipboard}
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, e error) {
	respond(w, status, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		fail(w, 400, err)
		return false
	}
	return true
}
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	s.registerShareRoutes(mux)
	mux.HandleFunc("GET /certificate.cer", func(w http.ResponseWriter, r *http.Request) { s.serveCertificate(w, r) })
	mux.HandleFunc("GET /certificate.mobileconfig", func(w http.ResponseWriter, r *http.Request) { s.serveCertificate(w, r) })
	mux.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		p := s.store.snapshot()
		respond(w, 200, map[string]any{"version": model.Version, "id": p.ID, "name": p.Settings.Name, "chunkSize": model.ChunkSize, "fingerprint": s.fingerprint, "urls": s.currentURLs(s.urls)})
	})
	mux.HandleFunc("GET /certificate.pem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `attachment; filename="AirLink-certificate.pem"`)
		_, _ = w.Write(s.certificate)
	})
	mux.HandleFunc("GET /setup", s.setup)
	mux.HandleFunc("POST /api/v1/pair/request", s.pairRequest)
	mux.HandleFunc("POST /api/v1/pair/poll", s.pairPoll)
	mux.HandleFunc("/api/v1/", s.authenticated)
	sub, _ := fs.Sub(assets, "assets")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/s/") && len(strings.Split(strings.Trim(r.URL.Path, "/"), "/")) == 2 {
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
			return
		}
		if r.URL.Path != "/" {
			if _, e := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); e != nil {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'none'")
		if !s.validHost(r.Host) {
			fail(w, 403, errors.New("unrecognized host"))
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "https://"+r.Host {
			fail(w, 403, errors.New("cross-origin request denied"))
			return
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" && r.Method != "GET" {
			fail(w, 403, errors.New("cross-site request denied"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) validHost(h string) bool {
	host, _, e := net.SplitHostPort(h)
	if e != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	for _, ip := range localIPs() {
		if strings.EqualFold(host, ip.String()) {
			return true
		}
	}
	name, _ := os.Hostname()
	return strings.EqualFold(host, name) || strings.EqualFold(host, strings.TrimSuffix(name, ".local")+".local")
}
func (s *Server) auth(r *http.Request) (model.Device, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		if c, e := r.Cookie("airlink_session"); e == nil {
			token = c.Value
		}
	}
	return s.store.authenticate(token)
}
func (s *Server) authenticated(w http.ResponseWriter, r *http.Request) {
	d, ok := s.auth(r)
	if !ok {
		fail(w, 401, errors.New("pair this device first"))
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	rid := randomID()
	s.mu.Lock()
	if s.active[d.ID] == nil {
		s.active[d.ID] = map[string]context.CancelFunc{}
	}
	s.active[d.ID][rid] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.active[d.ID], rid)
		if len(s.active[d.ID]) == 0 {
			delete(s.active, d.ID)
		}
		s.mu.Unlock()
	}()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if s.handleShareAPI(w, r, d) {
		return
	}
	if s.handleClipboardAPI(w, r, d) {
		return
	}
	if path == "status" && r.Method == "GET" {
		respond(w, 200, s.guestStatus(d))
		return
	}
	if path == "events" && r.Method == "GET" {
		s.events(w, r, d)
		return
	}
	if path == "peer/confirm" && r.Method == "POST" {
		s.peerConfirm(w, r, d)
		return
	}
	if path == "transfers" && r.Method == "POST" {
		if !d.CanSend {
			fail(w, 403, errors.New("sending not allowed"))
			return
		}
		var offer model.Offer
		if !decode(w, r, &offer) {
			return
		}
		t, err := s.engine.CreateIncoming(d, offer.Files)
		if err != nil {
			s.engineError(w, err)
			return
		}
		t = publicTransfer(t)
		respond(w, 201, t)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "transfers" {
		http.NotFound(w, r)
		return
	}
	t, err := s.engine.Get(parts[1])
	if err != nil {
		s.engineError(w, err)
		return
	}
	if t.DeviceID != d.ID {
		fail(w, 404, errors.New("transfer not found"))
		return
	}
	if t.Direction == "receive" && !d.CanSend || t.Direction == "send" && !d.CanReceive {
		fail(w, 403, errors.New("device permission revoked"))
		return
	}
	if len(parts) == 2 && r.Method == "GET" {
		t = publicTransfer(t)
		respond(w, 200, t)
		return
	}
	if len(parts) == 3 && r.Method == "POST" {
		switch parts[2] {
		case "pause":
			err = s.engine.Pause(t.ID)
		case "resume":
			err = s.engine.Resume(t.ID)
		case "cancel":
			err = s.engine.Cancel(t.ID)
		case "downloaded":
			err = s.engine.Downloaded(t.ID)
		default:
			fail(w, 403, errors.New("action requires local approval"))
			return
		}
		if err != nil {
			s.engineError(w, err)
			return
		}
		fresh, _ := s.engine.Get(t.ID)
		fresh = publicTransfer(fresh)
		respond(w, 200, fresh)
		return
	}
	if len(parts) == 3 && parts[2] == "archive" && r.Method == "GET" {
		s.archive(w, r, t)
		return
	}
	if len(parts) >= 5 && parts[2] == "files" {
		fid := parts[3]
		if len(parts) == 6 && parts[4] == "chunks" && r.Method == "PUT" {
			select {
			case s.chunkSlots <- struct{}{}:
				defer func() { <-s.chunkSlots }()
			case <-r.Context().Done():
				return
			}
			index, e := strconv.ParseInt(parts[5], 10, 64)
			if e != nil {
				fail(w, 400, e)
				return
			}
			n, e := s.engine.WriteChunk(r.Context(), t.ID, fid, index, r.Header.Get("X-Chunk-SHA256"), http.MaxBytesReader(w, r.Body, model.ChunkSize+1))
			if e != nil {
				s.engineError(w, e)
				return
			}
			respond(w, 200, map[string]int64{"received": n})
			return
		}
		if len(parts) == 5 && parts[4] == "complete" && r.Method == "POST" {
			var v struct {
				SHA string `json:"sha256"`
			}
			if !decode(w, r, &v) {
				return
			}
			if e := s.engine.CompleteFile(r.Context(), t.ID, fid, v.SHA); e != nil {
				s.engineError(w, e)
				return
			}
			fresh, _ := s.engine.Get(t.ID)
			fresh = publicTransfer(fresh)
			respond(w, 200, fresh)
			return
		}
		if len(parts) == 5 && parts[4] == "download" && r.Method == "GET" {
			f, m, e := s.engine.OpenDownload(t.ID, fid)
			if e != nil {
				s.engineError(w, e)
				return
			}
			defer f.Close()
			fi, e := f.Stat()
			if e != nil {
				s.engineError(w, e)
				return
			}
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(m.Path)))
			w.Header().Set("ETag", fmt.Sprintf(`"%s-%d-%d"`, m.ID, fi.Size(), fi.ModTime().UnixNano()))
			http.ServeContent(w, r, m.Path, fi.ModTime(), &cancelReader{Context: r.Context(), File: f})
			return
		}
	}
	http.NotFound(w, r)
}
func (s *Server) engineError(w http.ResponseWriter, e error) {
	var pathError *os.PathError
	var linkError *os.LinkError
	if errors.As(e, &pathError) || errors.As(e, &linkError) {
		log.Printf("Transfer filesystem error: %v", e)
		fail(w, 409, errors.New("source or destination is unavailable; check it on the host"))
		return
	}
	code := 500
	if errors.Is(e, engine.ErrNotFound) {
		code = 404
	} else if errors.Is(e, engine.ErrConflict) {
		code = 409
	} else if errors.Is(e, engine.ErrInvalid) || errors.Is(e, engine.ErrIntegrity) {
		code = 400
	}
	fail(w, code, e)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request, d model.Device) {
	f, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, errors.New("streaming unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		fresh, valid := s.auth(r)
		if !valid {
			return
		}
		b, _ := json.Marshal(s.guestStatus(fresh))
		_, err := fmt.Fprintf(w, "event: status\ndata: %s\n\n", b)
		if err != nil {
			return
		}
		f.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><meta name="viewport" content="width=device-width"><title>AirLink · Verbindung</title><style>body{font:17px system-ui;max-width:660px;margin:60px auto;padding:24px;line-height:1.6;color-scheme:light dark}a{color:#3485ed}code{overflow-wrap:anywhere}</style><h1>Sicher lokal verbinden</h1><p>Vergleiche den Fingerabdruck dieses AirLink-Gastgebers mit der Anzeige in seiner App oder lokalen Konsole, bevor du seinem Zertifikat vertraust.</p><p><code>`+s.fingerprint+`</code></p><p><a href="/certificate.pem">Öffentliches Zertifikat laden</a></p><p>Installiere das Zertifikat auf diesem Gerät als vertrauenswürdig. Unter iOS: Einstellungen → Allgemein → VPN und Geräteverwaltung zum Installieren, danach Allgemein → Info → Zertifikatsvertrauenseinstellungen → volles Vertrauen aktivieren.</p><p>Windows: Zertifikat für den aktuellen Benutzer unter Vertrauenswürdige Stammzertifizierungsstellen importieren. macOS: im Schlüsselbund importieren und nach Fingerabdruckprüfung SSL-Vertrauen setzen. Linux: Zertifikat im Browser oder lokalen Zertifikatsspeicher importieren.</p><p>Das Zertifikat gilt nur für diesen Gastgeber. Widerrufe es bei Bedarf in den Systemeinstellungen. Private Schlüssel werden niemals verteilt.</p><p><a href="/">Zurück zu AirLink</a></p>`)
}

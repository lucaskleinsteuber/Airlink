package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/peer"
	"github.com/lucaskleinsteuber/Airlink/internal/traffic"
	"github.com/lucaskleinsteuber/Airlink/internal/transfer"
)

type Options struct {
	DataDir, ReceiveDir string
	Port                int
	NoDiscovery         bool
}

type Server struct {
	engine            *transfer.Engine
	peers             *peer.Manager
	traffic           *traffic.Meter
	clipboardMu       sync.Mutex
	clipboardMessages []model.ClipboardMessage
	clipboardWaits    map[string]chan string
	store             *stateStore
	settingsMu        sync.Mutex
	opts              Options
	fingerprint       string
	certificate       []byte
	certMu            sync.Mutex
	currentCert       tls.Certificate
	certIPs           string
	port              int
	boot              string
	urls              []string
	lock              *flock.Flock
	mu                sync.Mutex
	discovered        map[string]model.Device
	rate              map[string][]time.Time
	discoveryNames    chan struct{}
	preparations      map[string]*preparationJob
	pairingWorkers    map[string]bool
	pairingNext       map[string]time.Time
	pairingOpenUntil  time.Time
	pairingSlots      chan struct{}
	dials             map[string]*dialState
	dialWake          chan struct{}
	changes           chan struct{}
	ctx               context.Context
	cancel            context.CancelFunc
	httpServer        *http.Server
	wg                sync.WaitGroup
	stopDiscovery     func()
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
	meter, err := traffic.New(opts.DataDir)
	if err != nil {
		return nil, err
	}
	if err = meter.SetLimits(p.Settings.SendLimitBytesPerSecond, p.Settings.ReceiveLimitBytesPerSecond); err != nil {
		meter.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{traffic: meter, store: store, opts: opts, fingerprint: fp, certificate: ca, currentCert: cert, certIPs: fmt.Sprint(localIPs()), boot: randomID()[:16],
		clipboardWaits: map[string]chan string{}, discovered: map[string]model.Device{}, rate: map[string][]time.Time{}, discoveryNames: make(chan struct{}, 1),
		dials: map[string]*dialState{}, dialWake: make(chan struct{}, 1), changes: make(chan struct{}, 1), ctx: ctx, cancel: cancel, lock: lock}
	s.pairingWorkers, s.pairingSlots = map[string]bool{}, make(chan struct{}, 2)
	s.peers = peer.NewManager(peer.Config{Certificate: s.certificateNow, Local: s.localHello, Trusted: s.trustedDevice, Handler: s, Opened: s.sessionOpened})
	s.engine, err = transfer.New(opts.DataDir, p.Settings.ReceiveDirectory, transfer.Options{
		Session: func(id string) transfer.Conn {
			if session := s.peers.Get(id); session != nil {
				return session
			}
			return nil
		},
		Connect: s.requestConnection,
		Device:  s.pairedDevice,
		Wait:    meter.Wait,
		Add:     meter.Add,
		Changed: s.changed,
	})
	if err != nil {
		cancel()
		meter.Close()
		return nil, err
	}
	port := opts.Port
	if port == 0 {
		port = 43871
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil && opts.Port == 0 {
		listener, err = net.Listen("tcp", ":0")
	}
	if err != nil {
		s.engine.Close()
		meter.Close()
		cancel()
		return nil, err
	}
	s.port = listener.Addr().(*net.TCPAddr).Port
	for _, ip := range localIPs() {
		if !ip.IsLoopback() {
			s.urls = append(s.urls, "https://"+net.JoinHostPort(ip.String(), strconv.Itoa(s.port)))
		}
	}
	s.urls = append(s.urls, "https://localhost:"+strconv.Itoa(s.port))
	// One TLS port: protocol 3 sessions (ALPN airlink/3, client certificate
	// required by the session layer) and HTTP/1.1 for device info and pairing.
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequestClientCert, NextProtos: []string{peer.ALPN, "http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return s.certificateNow(), nil }}
	s.httpServer = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10, TLSConfig: tlsConfig,
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){peer.ALPN: func(_ *http.Server, c *tls.Conn, _ http.Handler) { s.peers.ServeConn(c) }},
		ErrorLog:     log.New(discard{}, "", 0)}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		local := localOnlyListener{Listener: listener, allowed: localNetworkAddress}
		if e := s.httpServer.Serve(tls.NewListener(local, tlsConfig)); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Print(e)
		}
	}()
	if !opts.NoDiscovery {
		s.discovery(s.port)
	}
	s.wg.Add(3)
	go s.connectLoop()
	go s.clipboardLoop()
	go s.pairingLoop()
	success = true
	return s, nil
}

// changed tells the app (over stdio) that the visible state changed. Signals
// coalesce; the app then reads status.summary once.
func (s *Server) changed() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

type discard struct{}

func (discard) Write(b []byte) (int, error) { return len(b), nil }

// certificateNow returns the TLS certificate, renewing the leaf when the
// local addresses changed. The identity root stays the same.
func (s *Server) certificateNow() *tls.Certificate {
	s.certMu.Lock()
	defer s.certMu.Unlock()
	if ips := fmt.Sprint(localIPs()); ips != s.certIPs {
		if renewed, _, _, err := identity(s.opts.DataDir, s.store.snapshot().Settings.Name); err == nil {
			s.currentCert, s.certIPs = renewed, ips
		}
	}
	cert := s.currentCert
	return &cert
}

func (s *Server) localHello() peer.Hello {
	p := s.store.snapshot()
	platform, form := localDevice()
	return peer.Hello{ID: p.ID, Name: p.Settings.Name, Version: model.Version, Boot: s.boot, Port: s.port, Platform: platform, Form: form}
}

func (s *Server) trustedDevice(fingerprint string) (string, bool) {
	for id, r := range s.store.snapshot().Devices {
		if r.Device.Paired && r.Device.Kind == "engine" && r.Device.Fingerprint == fingerprint {
			return id, true
		}
	}
	return "", false
}

func (s *Server) pairedDevice(id string) (model.Device, bool) {
	r, ok := s.store.snapshot().Devices[id]
	return r.Device, ok && r.Device.Paired
}

func (s *Server) Close() error {
	s.cancel()
	if s.stopDiscovery != nil {
		s.stopDiscovery()
	}
	s.peers.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(ctx)
		_ = s.httpServer.Close()
	}
	s.wg.Wait()
	e := s.engine.Close()
	if s.traffic != nil {
		if closeErr := s.traffic.Close(); e == nil {
			e = closeErr
		}
	}
	if s.lock != nil {
		_ = s.lock.Unlock()
	}
	return e
}

func (s *Server) basicStatus() model.Status {
	p := s.store.snapshot()
	ds := []model.Device{}
	known := map[string]bool{}
	connected := s.peers.Connected()
	for _, r := range p.Devices {
		d := r.Device
		d.Online = connected[d.ID] != nil
		ds = append(ds, d)
		known[d.ID] = true
	}
	s.mu.Lock()
	ds = append(ds, s.discoveredDevicesLocked(known, time.Now())...)
	s.mu.Unlock()
	return model.Status{Version: model.Version, ID: p.ID, Name: p.Settings.Name, URLs: s.currentURLs(s.urls), Fingerprint: s.fingerprint, ChunkSize: model.ChunkSize, Devices: ds, Settings: p.Settings, ClipboardAllowed: true, CurrentDeviceID: p.ID, Capabilities: []string{"status-summary-v1", "preparation-v1", "history-page-v1", "protocol-3"}}
}

func (s *Server) Status() model.Status {
	v := s.basicStatus()
	v.Transfers, v.Clipboard = s.engine.Transfers(""), s.clipboardSnapshot("")
	v.Statistics = s.statistics("")
	return v
}

func (s *Server) summaryStatus() model.Status {
	v := s.basicStatus()
	v.Summary = true
	v.Statistics.Days = []model.DayStatistics{}
	v.Transfers, v.ActiveTransferCount = s.engine.StatusSummaries()
	v.Clipboard = s.clipboardSnapshot("")
	for i := range v.Clipboard {
		v.Clipboard[i].Text = ""
	}
	v.Preparations = s.preparationSnapshots()
	v.PairingSessions = s.pairingSnapshots()
	v.Capabilities = append(v.Capabilities, "pairing-v2")
	return v
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

// handler serves only device information and comparison-code pairing.
// Transfers and clipboard text use authenticated protocol 3 sessions.
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		p := s.store.snapshot()
		platform, form := localDevice()
		respond(w, 200, map[string]any{"version": model.Version, "id": p.ID, "name": p.Settings.Name, "fingerprint": s.fingerprint, "urls": s.currentURLs(s.urls), "capabilities": []string{"pairing-v2", "protocol-3"}, "platform": platform, "form": form})
	})
	mux.HandleFunc("POST /api/v1/pair/v2/start", s.pairingBegin)
	for _, action := range []string{"poll", "confirm", "ack", "cancel"} {
		mux.HandleFunc("POST /api/v1/pair/v2/"+action, s.pairingRemote)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if !s.validHost(r.Host) {
			fail(w, 403, errors.New("unrecognized host"))
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

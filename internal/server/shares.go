package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/shares"
)

func (s *Server) registerShareRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/public/shares/", s.publicShareAPI)
}
func (s *Server) shareSummaries(owner string) []model.Share {
	out := s.shares.List(owner)
	base := s.currentURLs(s.urls)[0]
	for i := range out {
		out[i].URL = base + out[i].URL
	}
	return out
}
func (s *Server) shareControl(method string, params json.RawMessage) (any, error) {
	return s.shareControlContext(s.ctx, method, params)
}
func (s *Server) shareControlContext(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "share.list":
		return s.shareSummaries(""), nil
	case "share.create":
		var v model.ShareCreate
		if err := json.Unmarshal(params, &v); err != nil {
			return nil, err
		}
		return s.createShareContext(ctx, "local", v)
	case "share.revoke":
		var v struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params, &v); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, s.shares.Revoke(v.ID, "local")
	default:
		return nil, shares.ErrInvalid
	}
}
func (s *Server) createShare(owner string, input model.ShareCreate) (model.Share, error) {
	return s.createShareContext(s.ctx, owner, input)
}
func (s *Server) createShareContext(ctx context.Context, owner string, input model.ShareCreate) (model.Share, error) {
	if err := ctx.Err(); err != nil {
		return model.Share{}, err
	}
	if (len(input.Paths) > 0) == (input.TransferID != "") {
		return model.Share{}, shares.ErrInvalid
	}
	var sources []shares.Source
	var err error
	if len(input.Paths) > 0 {
		if owner != "local" {
			return model.Share{}, shares.ErrInvalid
		}
		sources, err = shares.CapturePathsContext(ctx, input.Paths)
	} else {
		t, engineErr := s.engine.Get(input.TransferID)
		if engineErr != nil {
			return model.Share{}, shares.ErrNotFound
		}
		if owner != "local" && (t.DeviceID != owner || t.Direction != "receive" || t.State != "completed") {
			return model.Share{}, shares.ErrNotFound
		}
		if t.Direction == "receive" && t.State != "completed" {
			return model.Share{}, shares.ErrInvalid
		}
		if t.Direction != "receive" && t.Direction != "send" {
			return model.Share{}, shares.ErrInvalid
		}
		if t.State == "cancelled" || t.State == "failed" {
			return model.Share{}, shares.ErrUnavailable
		}
		mapped := make([]shares.Source, 0, len(t.Files))
		for _, file := range t.Files {
			if err := ctx.Err(); err != nil {
				return model.Share{}, err
			}
			if file.Skipped {
				continue
			}
			ref := shares.Source{File: publicFile(file)}
			if t.Direction == "receive" {
				ref.Root = t.Destination
				path := file.DestinationPath
				if path == "" {
					path = file.Path
				}
				ref.Path = filepath.Join(t.Destination, filepath.FromSlash(path))
				// Transfer timestamps describe the sender. Received files have a
				// local creation time, which becomes the share's immutable snapshot.
				ref.File.Modified = 0
			} else if !file.Directory {
				opened, _, openErr := s.engine.OpenDownloadContext(ctx, t.ID, file.ID)
				if openErr != nil {
					return model.Share{}, shares.ErrUnavailable
				}
				ref.Path = opened.Name()
				opened.Close()
			}
			mapped = append(mapped, ref)
		}
		sources, err = shares.CaptureFilesContext(ctx, mapped)
	}
	if err != nil {
		return model.Share{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.Share{}, err
	}
	created, err := s.shares.Create(owner, input, sources)
	if err != nil {
		return model.Share{}, err
	}
	if owner != "local" {
		if _, paired := s.store.snapshot().Devices[owner]; !paired {
			_ = s.shares.Revoke(created.ID, "local")
			return model.Share{}, shares.ErrUnauthorized
		}
	}
	created.URL = s.currentURLs(s.urls)[0] + created.URL
	return created, nil
}
func (s *Server) handleShareAPI(w http.ResponseWriter, r *http.Request, d model.Device) bool {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if p != "shares" && !strings.HasPrefix(p, "shares/") {
		return false
	}
	if p == "shares" && r.Method == "POST" {
		var v model.ShareCreate
		if !decode(w, r, &v) {
			return true
		}
		if len(v.Paths) > 0 {
			shareFailure(w, shares.ErrInvalid)
			return true
		}
		created, err := s.createShare(d.ID, v)
		if err != nil {
			shareFailure(w, err)
			return true
		}
		respond(w, 201, created)
		return true
	}
	parts := strings.Split(p, "/")
	if len(parts) == 3 && parts[2] == "revoke" && r.Method == "POST" {
		if err := s.shares.Revoke(parts[1], d.ID); err != nil {
			shareFailure(w, err)
		} else {
			respond(w, 200, map[string]bool{"ok": true})
		}
		return true
	}
	http.NotFound(w, r)
	return true
}
func shareCode(err error) int {
	switch {
	case errors.Is(err, shares.ErrNotFound):
		return 404
	case errors.Is(err, shares.ErrUnauthorized):
		return 401
	case errors.Is(err, shares.ErrGone):
		return 410
	case errors.Is(err, shares.ErrQuota) || errors.Is(err, shares.ErrUnavailable):
		return 409
	case errors.Is(err, shares.ErrInvalid):
		return 400
	case errors.Is(err, shares.ErrRateLimit):
		return 429
	default:
		return 500
	}
}
func shareFailure(w http.ResponseWriter, err error) {
	code := shareCode(err)
	message := "Share unavailable. Try again later."
	if code != 500 {
		message = err.Error()
	}
	respond(w, code, map[string]string{"error": message})
}
func shareCookieName(id string) string { return "airlink_share_" + id }
func shareCookie(r *http.Request, id string) string {
	c, err := r.Cookie(shareCookieName(id))
	if err != nil || len(c.Value) > 128 {
		return ""
	}
	return c.Value
}
func (s *Server) shareUnlockAllowed(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := "share-unlock:" + ip
	cutoff := time.Now().Add(-time.Minute)
	s.mu.Lock()
	defer s.mu.Unlock()
	recent := []time.Time{}
	for _, tick := range s.rate[key] {
		if tick.After(cutoff) {
			recent = append(recent, tick)
		}
	}
	if len(recent) >= 20 {
		return false
	}
	s.rate[key] = append(recent, time.Now())
	return true
}
func (s *Server) publicShareAPI(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/public/shares/")
	parts := strings.Split(p, "/")
	if len(parts) == 0 || len(parts[0]) != 32 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	token := shareCookie(r, id)
	if len(parts) == 2 && parts[1] == "unlock" && r.Method == "POST" {
		if !s.shareUnlockAllowed(r) {
			shareFailure(w, shares.ErrRateLimit)
			return
		}
		var v struct {
			Secret   string `json:"secret"`
			Password string `json:"password,omitempty"`
		}
		if !decode(w, r, &v) {
			return
		}
		summary, newToken, err := s.shares.Unlock(r.Context(), id, v.Secret, v.Password, token)
		if err != nil {
			shareFailure(w, err)
			return
		}
		cookieExpiry := time.Now().Add(shares.SessionLifetime)
		if summary.ExpiresAt != "" {
			if expiry, parseErr := time.Parse(time.RFC3339Nano, summary.ExpiresAt); parseErr == nil && expiry.Before(cookieExpiry) {
				cookieExpiry = expiry
			}
		}
		maxAge := int(time.Until(cookieExpiry).Seconds())
		if maxAge < 0 {
			maxAge = -1
		}
		http.SetCookie(w, &http.Cookie{Name: shareCookieName(id), Value: newToken, Path: "/api/v1/public/shares/" + id, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: cookieExpiry, MaxAge: maxAge})
		respond(w, 200, map[string]any{"share": summary})
		return
	}
	summary, err := s.shares.Session(id, token)
	if err != nil {
		shareFailure(w, err)
		return
	}
	if len(parts) == 1 && r.Method == "GET" {
		respond(w, 200, summary)
		return
	}
	if len(parts) == 3 && parts[1] == "files" && (r.Method == "GET" || r.Method == "HEAD") {
		file, m, err := s.shares.Open(id, parts[2])
		if err != nil {
			shareFailure(w, err)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			shareFailure(w, shares.ErrUnavailable)
			return
		}
		// ServeContent intentionally ignores Range for an empty resource. For
		// share quotas an unsatisfiable explicit range must remain a 416.
		if info.Size() == 0 && r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes */0")
			http.Error(w, "Requested range is not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(filepath.Base(m.Path)))
		w.Header().Set("ETag", fmt.Sprintf(`"%s-%d-%d"`, m.ID, info.Size(), info.ModTime().UnixNano()))
		writer := &shareDownloadWriter{ResponseWriter: w, ctx: r.Context(), store: s.shares, id: id, token: token, method: r.Method, emptyBodyAllowed: m.Size == 0, validate: func() error { return s.shares.ValidateOpen(id, m.ID, file) }}
		http.ServeContent(s.traffic.WrapWriter(r.Context(), writer, "send"), r, m.Path, info.ModTime(), file)
		writer.finish()
		return
	}
	if len(parts) == 2 && parts[1] == "archive" && r.Method == "GET" {
		s.publicShareArchive(w, r, id, token, summary)
		return
	}
	http.NotFound(w, r)
}

// Headers remain buffered until a valid body write reserves the session's
// allowance. HEAD, conditional 304s and invalid ranges never reserve a download.
type shareDownloadWriter struct {
	http.ResponseWriter
	ctx                        context.Context
	store                      *shares.Store
	id, token, method          string
	status                     int
	started, blocked, reserved bool
	emptyBodyAllowed           bool
	validate                   func() error
}

func (w *shareDownloadWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *shareDownloadWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *shareDownloadWriter) Write(p []byte) (int, error) {
	if w.blocked {
		return 0, io.ErrClosedPipe
	}
	if w.status == 0 {
		w.status = 200
	}
	success := w.method == "GET" && (w.status == 200 || w.status == 206) && len(p) > 0
	if success {
		err := w.ctx.Err()
		if err == nil {
			err = w.store.Check(w.id, w.token)
		}
		if err == nil && w.validate != nil {
			err = w.validate()
		}
		if err == nil && !w.reserved {
			err = w.store.BeginDownload(w.id, w.token)
			if err == nil {
				w.reserved = true
			}
		}
		if err != nil {
			w.blocked = true
			if !w.started {
				for _, header := range []string{"Content-Length", "Content-Disposition", "Content-Range", "Accept-Ranges", "ETag"} {
					w.Header().Del(header)
				}
				shareFailure(w.ResponseWriter, err)
				w.started = true
			}
			return 0, err
		}
	}
	if !w.started {
		w.ResponseWriter.WriteHeader(w.status)
		w.started = true
	}
	return w.ResponseWriter.Write(p)
}
func (w *shareDownloadWriter) finish() {
	if !w.started && !w.blocked {
		if w.status == 0 {
			w.status = 200
		}
		if w.method == "GET" && (w.status == 200 || w.status == 206) && !w.reserved {
			err := w.ctx.Err()
			if err == nil && !w.emptyBodyAllowed {
				err = shares.ErrUnavailable
			}
			if err == nil && w.validate != nil {
				err = w.validate()
			}
			if err == nil {
				err = w.store.BeginDownload(w.id, w.token)
			}
			if err != nil {
				for _, header := range []string{"Content-Length", "Content-Disposition", "Content-Range", "Accept-Ranges", "ETag"} {
					w.Header().Del(header)
				}
				shareFailure(w.ResponseWriter, err)
				w.blocked = true
				w.started = true
				return
			}
			w.reserved = true
		}
		w.ResponseWriter.WriteHeader(w.status)
		w.started = true
	}
}
func (s *Server) publicShareArchive(w http.ResponseWriter, r *http.Request, id, token string, summary model.Share) {
	if r.Header.Get("Range") != "" {
		http.Error(w, "Archive ranges are not supported", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	for _, m := range summary.Files {
		if m.Directory {
			continue
		}
		f, _, err := s.shares.Open(id, m.ID)
		if err != nil {
			shareFailure(w, err)
			return
		}
		f.Close()
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="AirLink.zip"`)
	writer := &shareDownloadWriter{ResponseWriter: w, ctx: r.Context(), store: s.shares, id: id, token: token, method: r.Method}
	archive := zip.NewWriter(s.traffic.WrapWriter(r.Context(), writer, "send"))
	for _, m := range summary.Files {
		if err := r.Context().Err(); err != nil {
			return
		}
		if err := s.shares.Check(id, token); err != nil {
			return
		}
		if m.Directory {
			if _, err := archive.Create(strings.TrimSuffix(m.Path, "/") + "/"); err != nil {
				return
			}
			continue
		}
		f, _, err := s.shares.Open(id, m.ID)
		if err != nil {
			return
		}
		writer.validate = func() error { return s.shares.ValidateOpen(id, m.ID, f) }
		header := &zip.FileHeader{Name: m.Path, Method: zip.Store}
		header.SetModTime(time.UnixMilli(m.Modified))
		entry, err := archive.CreateHeader(header)
		if err == nil {
			_, err = io.CopyBuffer(entry, f, make([]byte, 256*1024))
		}
		f.Close()
		writer.validate = nil
		if err != nil {
			return
		}
	}
	_ = archive.Close()
	writer.finish()
}

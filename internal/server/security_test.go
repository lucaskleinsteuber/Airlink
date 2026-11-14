package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func newSecurityServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Options{DataDir: filepath.Join(dir, "data"), ReceiveDir: filepath.Join(dir, "receive"), NoDiscovery: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func securityURL(s *Server) string { return s.urls[len(s.urls)-1] }
func securityRequest(s *Server, method, path, token, origin string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, securityURL(s)+path, bytes.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}
func securityDevice(t *testing.T, s *Server, id string) (model.Device, string) {
	t.Helper()
	token := randomID()
	d := model.Device{ID: id, Name: "Security " + id, Kind: "browser", CanSend: true, CanReceive: true, Paired: true}
	if err := s.store.put(deviceRecord{Device: d, TokenHash: digest([]byte(token))}); err != nil {
		t.Fatal(err)
	}
	return d, token
}

func TestSecurityPinnedTLSRejectsWrongFingerprint(t *testing.T) {
	s := newSecurityServer(t)
	var info map[string]any
	if err := requestJSON(context.Background(), pinnedClient(s.fingerprint), "GET", securityURL(s)+"/api/v1/info", "", nil, &info); err != nil {
		t.Fatal("correct pin rejected", err)
	}
	if info["version"] != model.Version {
		t.Fatal("unexpected peer info", info)
	}
	if err := requestJSON(context.Background(), pinnedClient(strings.Repeat("0", 64)), "GET", securityURL(s)+"/api/v1/info", "", nil, &info); err == nil {
		t.Fatal("untrusted TLS identity accepted")
	}
}

func TestSecurityGuestIsolationAndLocalPathRedaction(t *testing.T) {
	s := newSecurityServer(t)
	a, tokenA := securityDevice(t, s, "security-a")
	_, tokenB := securityDevice(t, s, "security-b")
	body := []byte(`{"files":[{"id":"file-a","path":"private-name.txt","size":7}]}`)
	w := securityRequest(s, "POST", "/api/v1/transfers", tokenA, "", body)
	if w.Code != 201 {
		t.Fatalf("create %d: %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), s.opts.DataDir) || strings.Contains(w.Body.String(), s.store.snapshot().Settings.ReceiveDirectory) {
		t.Fatal("create exposed local absolute path", w.Body.String())
	}
	var created model.Transfer
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Destination != "" {
		t.Fatal("initial transfer exposed destination", created.Destination)
	}
	w = securityRequest(s, "GET", "/api/v1/transfers/"+created.ID, tokenB, "", nil)
	if w.Code != 404 {
		t.Fatalf("other guest read transfer: %d", w.Code)
	}
	w = securityRequest(s, "POST", "/api/v1/transfers/"+created.ID+"/cancel", tokenB, "", []byte(`{}`))
	if w.Code != 404 {
		t.Fatalf("other guest changed transfer: %d", w.Code)
	}
	w = securityRequest(s, "GET", "/api/v1/transfers/"+created.ID, tokenA, "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "Destination") {
		t.Fatal("owner retrieval failed", w.Code, w.Body)
	}
	w = securityRequest(s, "POST", "/api/v1/transfers/"+created.ID+"/accept", tokenA, "", []byte(`{}`))
	if w.Code != 403 {
		t.Fatal("guest approved own transfer", w.Code)
	}
	local, _ := s.engine.Get(created.ID)
	if local.DeviceID != a.ID || local.Destination == "" {
		t.Fatal("local administration lost destination")
	}
}

func TestSecurityDataDirectoryLock(t *testing.T) {
	s := newSecurityServer(t)
	second, err := New(s.opts)
	if err == nil {
		second.Close()
		t.Fatal("concurrent daemon acquired same transfer database")
	}
}

func TestSecurityFilesystemErrorsDoNotExposeSourcePath(t *testing.T) {
	s := newSecurityServer(t)
	d, token := securityDevice(t, s, "security-download")
	source := filepath.Join(t.TempDir(), "secret-source.txt")
	if err := os.WriteFile(source, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	tr, err := s.engine.Offer(d, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	w := securityRequest(s, "GET", "/api/v1/transfers/"+tr.ID+"/files/"+tr.Files[0].ID+"/download", token, "", nil)
	if w.Code < 400 {
		t.Fatal("deleted file served", w.Code)
	}
	if strings.Contains(w.Body.String(), filepath.Dir(source)) || strings.Contains(w.Body.String(), s.opts.DataDir) {
		t.Fatal("filesystem error exposed absolute local path", w.Body.String())
	}
}

func TestSecurityHostAuthenticationAndRemovedBrowserSurface(t *testing.T) {
	s := newSecurityServer(t)
	if w := securityRequest(s, "GET", "/api/v1/transfers/x", "", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated peer API %d: %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "https://attacker.invalid:43871/api/v1/info", nil)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DNS rebinding host accepted: %d", w.Code)
	}
	// 0.13 has no browser interface: no web app, setup page, certificate
	// download, legacy pairing, guest status or event stream.
	for _, route := range []struct{ method, path string }{{"GET", "/"}, {"GET", "/setup"}, {"GET", "/certificate.pem"}, {"GET", "/certificate.mobileconfig"}, {"POST", "/api/v1/pair/request"}, {"POST", "/api/v1/pair/poll"}, {"GET", "/api/v1/status"}, {"GET", "/api/v1/events"}, {"GET", "/api/v1/public/shares/x"}} {
		if w := securityRequest(s, route.method, route.path, "", "", nil); w.Code < 400 {
			t.Fatalf("removed route %s %s answered %d", route.method, route.path, w.Code)
		}
	}
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestSecurityOriginHostAndAuthenticationGuards(t *testing.T) {
	s := newSecurityServer(t)
	if w := securityRequest(s, "GET", "/api/v1/status", "", "", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status %d: %s", w.Code, w.Body)
	}
	if w := securityRequest(s, "POST", "/api/v1/pair/request", "", "https://attacker.invalid", []byte(`{"name":"Attacker","kind":"browser"}`)); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin pairing %d", w.Code)
	}
	r := httptest.NewRequest("GET", "https://attacker.invalid:43871/api/v1/info", nil)
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("DNS rebinding host accepted: %d", w.Code)
	}
	r = httptest.NewRequest("POST", securityURL(s)+"/api/v1/pair/request", strings.NewReader(`{"name":"Attacker","kind":"browser"}`))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("cross-site request without Origin accepted")
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
	w = securityRequest(s, "GET", "/api/v1/status", tokenB, "", nil)
	if strings.Contains(w.Body.String(), created.ID) || strings.Contains(w.Body.String(), "private-name.txt") {
		t.Fatal("guest status exposed another guest transfer")
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

func TestSecurityPublicBootstrapContainsOnlyTrustMaterial(t *testing.T) {
	s := newSecurityServer(t)
	base := strings.TrimSuffix(s.setupURLs[len(s.setupURLs)-1], "/setup")
	client := &http.Client{}
	for _, path := range []string{"/setup", "/certificate.pem"} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatal("bootstrap unavailable", err, resp.StatusCode)
		}
		admin, _ := url.Parse(s.adminURL)
		secret := admin.Query().Get("token")
		for _, forbidden := range []string{s.opts.DataDir, secret, "identity-key.pem", "BEGIN PRIVATE KEY"} {
			if forbidden != "" && bytes.Contains(b, []byte(forbidden)) {
				t.Fatalf("bootstrap exposed %q", forbidden)
			}
		}
	}
	for _, path := range []string{"/control", "/api/v1/status", "/api/v1/transfers"} {
		resp, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("bootstrap exposed administrative path %s: %d", path, resp.StatusCode)
		}
	}
}

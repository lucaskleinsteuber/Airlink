package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/transfer"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func integrationHost(t *testing.T) *Server {
	t.Helper()
	base := t.TempDir()
	s, err := New(Options{DataDir: filepath.Join(base, "state"), ReceiveDir: filepath.Join(base, "received"), NoDiscovery: true, Port: freePort(t)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func hostURL(s *Server) string { return s.urls[len(s.urls)-1] }

func hostID(s *Server) string { return s.store.snapshot().ID }

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// pairedHosts returns two connected services that trust each other and
// receive automatically from each other.
func pairedHosts(t *testing.T) (*Server, *Server) {
	t.Helper()
	a, b := integrationHost(t), integrationHost(t)
	pairForTest(t, a, b)
	for _, pair := range [][2]*Server{{a, b}, {b, a}} {
		if err := pair[0].store.edit(hostID(pair[1]), func(r *deviceRecord) { r.Device.AutoAccept = true; r.Device.CanClipboard = true }); err != nil {
			t.Fatal(err)
		}
	}
	a.requestConnection(hostID(b))
	waitFor(t, 10*time.Second, "session", func() bool { return a.peers.Get(hostID(b)) != nil && b.peers.Get(hostID(a)) != nil })
	return a, b
}

func offer(t *testing.T, s *Server, to string, paths ...string) model.Transfer {
	t.Helper()
	params, _ := json.Marshal(preparationInput{DeviceID: to, Paths: paths})
	v, err := s.Control("offer", params)
	if err != nil {
		t.Fatal(err)
	}
	return v.(model.Transfer)
}

func waitTransfer(t *testing.T, s *Server, id, state string) model.Transfer {
	t.Helper()
	var last model.Transfer
	waitFor(t, 30*time.Second, "transfer "+state, func() bool {
		last, _ = s.engine.Header(id)
		return last.State == state
	})
	return last
}

func randomFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	_, _ = rand.Read(data)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPairedServicesTransferBothDirectionsOverOneSession(t *testing.T) {
	a, b := pairedHosts(t)
	folder := filepath.Join(t.TempDir(), "Projekt")
	want := map[string][]byte{}
	for i, name := range []string{"a.txt", "sub/b.bin", "sub/deeper/c.dat", "größe.txt"} {
		want[name] = randomFile(t, filepath.Join(folder, filepath.FromSlash(name)), 1000*(i+1))
	}
	out := offer(t, a, hostID(b), folder)
	waitTransfer(t, a, out.ID, "completed")
	waitTransfer(t, b, out.ID, "completed")
	for name, data := range want {
		got, err := os.ReadFile(filepath.Join(b.opts.ReceiveDir, "Projekt", filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatal(name, err)
		}
	}
	single := filepath.Join(t.TempDir(), "reply.pdf")
	data := randomFile(t, single, 70_000)
	back := offer(t, b, hostID(a), single)
	waitTransfer(t, a, back.ID, "completed")
	waitTransfer(t, b, back.ID, "completed")
	if got, err := os.ReadFile(filepath.Join(a.opts.ReceiveDir, "reply.pdf")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("reverse direction", err)
	}
	if !a.basicStatus().Devices[0].Online {
		t.Fatal("connected device not shown online")
	}
	result, err := b.Control("transfer.received", json.RawMessage(`{"id":"`+out.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	items := result.(transfer.ReceivedResult).Items
	if len(items) != 1 || filepath.Base(items[0].Path) != "Projekt" || !items[0].Directory {
		t.Fatal("received items", items)
	}
}

func TestOneWayReachabilityStillDeliversToTheUnreachableSide(t *testing.T) {
	a, b := integrationHost(t), integrationHost(t)
	pairForTest(t, a, b)
	// a cannot reach b (as if a firewall blocked b's port); b can reach a.
	if err := a.store.edit(hostID(b), func(r *deviceRecord) { r.Device.Address = "https://127.0.0.1:1" }); err != nil {
		t.Fatal(err)
	}
	if err := b.store.edit(hostID(a), func(r *deviceRecord) { r.Device.AutoAccept = true }); err != nil {
		t.Fatal(err)
	}
	if err := a.store.edit(hostID(b), func(r *deviceRecord) { r.Device.AutoAccept = true }); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "to-unreachable.bin")
	data := randomFile(t, source, 50_000)
	out := offer(t, a, hostID(b), source)
	b.requestConnection(hostID(a))
	waitTransfer(t, b, out.ID, "completed")
	waitTransfer(t, a, out.ID, "completed")
	if got, err := os.ReadFile(filepath.Join(b.opts.ReceiveDir, "to-unreachable.bin")); err != nil || !bytes.Equal(got, data) {
		t.Fatal(err)
	}
	if s := a.peers.Get(hostID(b)); s == nil || s.Initiator {
		t.Fatal("the session should have been opened by the reachable side")
	}
}

func TestClipboardOverSessionRequiresBothPermissions(t *testing.T) {
	a, b := pairedHosts(t)
	if err := b.store.edit(hostID(a), func(r *deviceRecord) { r.Device.CanClipboard = false }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.sendClipboard(hostID(b), "blocked on the other side"); err == nil {
		t.Fatal("receiver permission skipped")
	}
	if err := b.store.edit(hostID(a), func(r *deviceRecord) { r.Device.CanClipboard = true }); err != nil {
		t.Fatal(err)
	}
	sent, err := a.sendClipboard(hostID(b), "Hallo Welt 🌍\nzweite Zeile")
	if err != nil {
		t.Fatal(err)
	}
	got := b.clipboardSnapshot(hostID(a))
	if len(got) != 1 || got[0].Text != sent.Text || got[0].Direction != "receive" || sent.Direction != "send" {
		t.Fatal(got, sent)
	}
	if err := a.store.edit(hostID(b), func(r *deviceRecord) { r.Device.CanClipboard = false }); err != nil {
		t.Fatal(err)
	}
	if _, err = a.sendClipboard(hostID(b), "no local permission"); err == nil {
		t.Fatal("sender permission skipped")
	}
}

func TestRevokingTrustCancelsAndDisconnects(t *testing.T) {
	a, b := pairedHosts(t)
	if err := b.store.edit(hostID(a), func(r *deviceRecord) { r.Device.AutoAccept = false }); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "pending.bin")
	randomFile(t, source, 1000)
	out := offer(t, a, hostID(b), source)
	waitTransfer(t, b, out.ID, "offered")
	if _, err := a.Control("device.revoke", json.RawMessage(`{"id":"`+hostID(b)+`"}`)); err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, a, out.ID, "cancelled")
	waitTransfer(t, b, out.ID, "cancelled")
	waitFor(t, 5*time.Second, "disconnect", func() bool { return a.peers.Get(hostID(b)) == nil })
	// b still trusts a, but a no longer accepts b's identity.
	b.requestConnection(hostID(a))
	time.Sleep(300 * time.Millisecond)
	if a.peers.Get(hostID(b)) != nil {
		t.Fatal("revoked device reconnected")
	}
}

func TestOnlyInfoAndPairingAreServedOverHTTP(t *testing.T) {
	s := integrationHost(t)
	client := pinnedClient(s.fingerprint)
	for _, route := range []struct {
		method, path string
		ok           bool
	}{{"GET", "/api/v1/info", true}, {"GET", "/", false}, {"GET", "/setup", false}, {"GET", "/certificate.pem", false}, {"POST", "/api/v1/transfers", false}, {"POST", "/api/v1/clipboard", false}, {"GET", "/api/v1/status", false}, {"POST", "/api/v1/pair/request", false}, {"POST", "/api/v1/diagnostics.export", false}} {
		req, _ := http.NewRequest(route.method, hostURL(s)+route.path, strings.NewReader("{}"))
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if (res.StatusCode == 200) != route.ok {
			t.Fatalf("%s %s answered %d", route.method, route.path, res.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", "https://attacker.invalid/api/v1/info", nil)
	req.Host = "attacker.invalid:1"
	req.URL.Host = strings.TrimPrefix(hostURL(s), "https://")
	res, err := client.Do(req)
	if err == nil {
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatal("DNS rebinding host accepted", res.StatusCode)
		}
	}
}

func TestStdioControlAnswersAndStops(t *testing.T) {
	s := integrationHost(t)
	var out bytes.Buffer
	if err := s.Stdio(strings.NewReader("{\"id\":7,\"method\":\"status.summary\"}\n{\"id\":8,\"method\":\"share.create\"}\n{\"id\":9,\"method\":\"shutdown\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if !strings.HasPrefix(line, `{"event":`) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 3 || !strings.Contains(lines[0], `"id":7`) || !strings.Contains(lines[1], "unknown control method") {
		t.Fatal(out.String())
	}
}

// Changes are pushed to the app as coalesced "status" events; reading the
// status does not cause one.
func TestStdioPushesStatusEvents(t *testing.T) {
	s := integrationHost(t)
	in, feed := io.Pipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- s.Stdio(in, &out) }()
	send := func(line string) { _, _ = feed.Write([]byte(line + "\n")) }
	events := func() int { return strings.Count(out.String(), `{"event":"status"}`) }
	send(`{"id":1,"method":"status.summary"}`)
	waitFor(t, 2*time.Second, "summary reply", func() bool { return strings.Contains(out.String(), `"id":1`) })
	time.Sleep(300 * time.Millisecond)
	if n := events(); n != 0 {
		t.Fatalf("reading status produced %d events", n)
	}
	settings := s.store.snapshot().Settings
	settings.Name = "Renamed"
	body, _ := json.Marshal(map[string]any{"id": 2, "method": "settings.update", "params": settings})
	send(string(body))
	waitFor(t, 2*time.Second, "status event", func() bool { return events() == 1 })
	for range 20 {
		s.changed()
	}
	waitFor(t, 2*time.Second, "coalesced event", func() bool { return events() >= 2 })
	time.Sleep(300 * time.Millisecond)
	if n := events(); n > 3 {
		t.Fatalf("20 signals produced %d events", n)
	}
	send(`{"id":3,"method":"shutdown"}`)
	_ = feed.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestRestartKeepsIdentityAndContinuesWithPeer(t *testing.T) {
	a, b := pairedHosts(t)
	opts := b.opts
	id := hostID(b)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "disconnect after restart", func() bool { return a.peers.Get(id) == nil })
	restarted, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	if hostID(restarted) != id {
		t.Fatal("identity changed")
	}
	restarted.requestConnection(hostID(a))
	waitFor(t, 10*time.Second, "reconnect", func() bool { return a.peers.Get(id) != nil && restarted.peers.Get(hostID(a)) != nil })
	source := filepath.Join(t.TempDir(), "after-restart.txt")
	data := randomFile(t, source, 123)
	out := offer(t, a, id, source)
	waitTransfer(t, restarted, out.ID, "completed")
	if got, err := os.ReadFile(filepath.Join(opts.ReceiveDir, "after-restart.txt")); err != nil || !bytes.Equal(got, data) {
		t.Fatal(errors.Join(err))
	}
}

// integrationDevice stores a paired device record that is not running.
func integrationDevice(t *testing.T, s *Server, auto bool) (model.Device, string) {
	t.Helper()
	token := randomID()
	d := model.Device{ID: randomID(), Name: "Integration peer", Kind: "engine", Fingerprint: randomID() + randomID()[:16], Paired: true, CanSend: true, CanReceive: true, AutoAccept: auto}
	if err := s.store.put(deviceRecord{Device: d, TokenHash: digest([]byte(token))}); err != nil {
		t.Fatal(err)
	}
	return d, token
}

func TestOnlyLocalNetworkAddressesAreAccepted(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "192.168.178.22", "10.1.2.3", "172.20.0.1", "fe80::1", "fd5c:84d4:aa63::1"} {
		if !localNetworkAddress(net.ParseIP(ip)) {
			t.Errorf("%s rejected", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"} {
		if localNetworkAddress(net.ParseIP(ip)) {
			t.Errorf("%s accepted", ip)
		}
	}
}

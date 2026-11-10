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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func pairingCall(t *testing.T, s *Server, method string, input any) model.PairingSession {
	t.Helper()
	b, _ := json.Marshal(input)
	v, err := s.Control(method, b)
	if err != nil {
		t.Fatal(method, err)
	}
	return v.(model.PairingSession)
}
func awaitPairing(t *testing.T, s *Server, id, state string) model.PairingSession {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range s.pairingSnapshots() {
			if (id == "" || p.ID == id) && p.State == state {
				return p
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pairing did not reach %s: %+v", state, s.pairingSnapshots())
	return model.PairingSession{}
}

func TestMutualPairingTLSCodeAndDirectionalTrust(t *testing.T) {
	a, b := integrationHost(t), integrationHost(t)
	started := pairingCall(t, a, "pairing.start", map[string]string{"url": hostURL(b), "fingerprint": b.fingerprint})
	ap := awaitPairing(t, a, started.ID, "comparing")
	bp := awaitPairing(t, b, "", "comparing")
	if len(ap.ComparisonCode) != 8 || ap.ComparisonCode != bp.ComparisonCode {
		t.Fatal("TLS-bound comparison differs", ap, bp)
	}
	if len(a.store.snapshot().Devices) != 0 || len(b.store.snapshot().Devices) != 0 {
		t.Fatal("comparison granted credentials")
	}
	publicRequest := a.store.snapshot().Pairings[ap.ID]
	var wire json.RawMessage
	if err := requestJSON(context.Background(), pinnedClient(b.fingerprint), "POST", hostURL(b)+"/api/v1/pair/v2/poll", "", pairingRemoteInput{ID: bp.ID, Secret: publicRequest.Secret}, &wire); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wire, []byte(ap.ComparisonCode)) || bytes.Contains(wire, []byte(`"token"`)) {
		t.Fatal("comparison code/credentials exposed before approval", string(wire))
	}
	pairingCall(t, a, "pairing.confirm", map[string]any{"id": ap.ID, "automaticReceive": true})
	time.Sleep(500 * time.Millisecond)
	if len(a.store.snapshot().Devices) != 0 || len(b.store.snapshot().Devices) != 0 {
		t.Fatal("one approval granted trust")
	}
	pairingCall(t, b, "pairing.confirm", map[string]any{"id": bp.ID, "automaticReceive": false})
	awaitPairing(t, a, ap.ID, "paired")
	awaitPairing(t, b, bp.ID, "paired")
	ar, br := a.store.snapshot().Devices[b.store.snapshot().ID], b.store.snapshot().Devices[a.store.snapshot().ID]
	if !ar.Device.AutoAccept || br.Device.AutoAccept || !ar.Device.Paired || !br.Device.Paired {
		t.Fatal("directional explicit trust lost", ar.Device, br.Device)
	}
	var remote model.Status
	if err := requestJSON(context.Background(), pinnedClient(b.fingerprint), "GET", hostURL(b)+"/api/v1/status", ar.OutboundToken, nil, &remote); err != nil {
		t.Fatal("credential unusable", err)
	}
	if err := requestJSON(context.Background(), pinnedClient(a.fingerprint), "GET", hostURL(a)+"/api/v1/status", br.OutboundToken, nil, &remote); err != nil {
		t.Fatal("reverse credential unusable", err)
	}
	if _, err := a.Control("device.revoke", json.RawMessage(`{"id":"`+ar.Device.ID+`"}`)); err != nil {
		t.Fatal(err)
	}
	if len(a.store.snapshot().Pairings) != 0 {
		t.Fatal("revoked pairing receipt could re-grant device")
	}
}

func TestPairingReceiptSurvivesHelperRestartAndLostReplies(t *testing.T) {
	a, b := integrationHost(t), integrationHost(t)
	started := pairingCall(t, a, "pairing.start", map[string]string{"url": hostURL(b), "fingerprint": b.fingerprint})
	ap := awaitPairing(t, a, started.ID, "comparing")
	bp := awaitPairing(t, b, "", "comparing")
	options := b.opts
	u, _ := url.Parse(hostURL(b))
	options.Port, _ = strconv.Atoi(u.Port())
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	b, err = New(options)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := pairingCall(t, b, "pairing.status", map[string]string{"id": bp.ID}); got.ComparisonCode != ap.ComparisonCode {
		t.Fatal("restart changed original TLS comparison", got)
	}
	pairingCall(t, a, "pairing.confirm", map[string]any{"id": ap.ID})
	pairingCall(t, b, "pairing.confirm", map[string]any{"id": bp.ID})
	awaitPairing(t, a, ap.ID, "paired")
	awaitPairing(t, b, bp.ID, "paired")
	// Repeated authenticated polling and acknowledgment return the same grant.
	r := a.store.snapshot().Pairings[ap.ID]
	var one, two pairingRemoteReply
	client := pinnedClient(b.fingerprint)
	for _, out := range []*pairingRemoteReply{&one, &two} {
		if err = requestJSON(context.Background(), client, "POST", hostURL(b)+"/api/v1/pair/v2/ack", "", pairingRemoteInput{ID: r.RemoteID, Secret: r.Secret}, out); err != nil {
			t.Fatal(err)
		}
	}
	if one.Token == "" || one.Token != two.Token || len(b.store.snapshot().Devices) != 1 {
		t.Fatal("repeated acknowledgment duplicated grant")
	}
}

func TestPairingRejectsReplayExpiryAndFailedCommit(t *testing.T) {
	a, b := integrationHost(t), integrationHost(t)
	started := pairingCall(t, a, "pairing.start", map[string]string{"url": hostURL(b), "fingerprint": b.fingerprint})
	awaitPairing(t, a, started.ID, "comparing")
	bp := awaitPairing(t, b, "", "comparing")
	r := a.store.snapshot().Pairings[started.ID]
	var reply pairingRemoteReply
	if err := requestJSON(context.Background(), pinnedClient(b.fingerprint), "POST", hostURL(b)+"/api/v1/pair/v2/confirm", "", pairingRemoteInput{ID: bp.ID, Secret: randomID(), ReturnToken: r.ReturnToken}, &reply); err == nil {
		t.Fatal("foreign session secret accepted")
	}
	failedPath := filepath.Join(t.TempDir(), "not-a-file")
	if err := os.Mkdir(failedPath, 0700); err != nil {
		t.Fatal(err)
	}
	b.store.Lock()
	original := b.store.path
	b.store.path = failedPath
	b.store.Unlock()
	_, err := b.Control("pairing.confirm", json.RawMessage(`{"id":"`+bp.ID+`"}`))
	b.store.Lock()
	b.store.path = original
	confirmed := b.store.data.Pairings[bp.ID].LocalConfirmed
	b.store.Unlock()
	if err == nil || confirmed || len(b.store.snapshot().Devices) != 0 {
		t.Fatal("failed commit mutated trust", err)
	}
	if err = b.editPairings(func(p *persisted) error {
		x := p.Pairings[bp.ID]
		x.Expires = time.Now().Add(-time.Second)
		p.Pairings[bp.ID] = x
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Control("pairing.confirm", json.RawMessage(`{"id":"`+bp.ID+`"}`)); err == nil {
		t.Fatal("expired comparison accepted")
	}
	if err = requestJSON(context.Background(), pinnedClient(b.fingerprint), "POST", hostURL(b)+"/api/v1/pair/v2/poll", "", pairingRemoteInput{ID: bp.ID, Secret: r.Secret}, &reply); err == nil {
		t.Fatal("expired pairing polled")
	}
}

func TestPairingTLSIntermediaryCannotSupplyMatchingCode(t *testing.T) {
	a, b, identityHost := integrationHost(t), integrationHost(t), integrationHost(t)
	forward := pinnedClient(b.fingerprint)
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body.Close()
		if r.URL.Path == "/api/v1/pair/v2/start" {
			var input pairingBeginInput
			_ = json.Unmarshal(body, &input)
			input.TargetFingerprint = b.fingerprint
			body, _ = json.Marshal(input)
		}
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, hostURL(b)+r.URL.Path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := forward.Do(req)
		if err != nil {
			http.Error(w, "forward failed", 502)
			return
		}
		defer res.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	proxy.TLS = identityHost.httpServer.TLSConfig.Clone()
	proxy.StartTLS()
	defer proxy.Close()
	// The provisional discovery hint points at the intermediary identity.
	started := pairingCall(t, a, "pairing.start", map[string]string{"url": strings.Replace(proxy.URL, "127.0.0.1", "localhost", 1), "fingerprint": identityHost.fingerprint})
	ap := awaitPairing(t, a, started.ID, "comparing")
	bp := awaitPairing(t, b, "", "comparing")
	if ap.ComparisonCode == bp.ComparisonCode {
		t.Fatal("separate TLS connections produced a matching comparison")
	}
	if len(a.store.snapshot().Devices) != 0 || len(b.store.snapshot().Devices) != 0 {
		t.Fatal("intermediary granted trust")
	}
}

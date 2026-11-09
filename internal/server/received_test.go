package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func receivedControlParams(id string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"id": id})
	return b
}

func persistedDevices(t *testing.T, s *Server) map[string]deviceRecord {
	t.Helper()
	b, err := os.ReadFile(s.store.path)
	if err != nil {
		t.Fatal(err)
	}
	var p persisted
	if err = json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p.Devices
}

func TestAcceptTrustedExplicitlyEnablesOnlyCurrentSender(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, false)
	other, _ := integrationDevice(t, s, false)
	d.CanReceive = false
	d.CanClipboard = true
	if err := s.store.updateDevice(d); err != nil {
		t.Fatal(err)
	}
	previous := s.store.snapshot().Devices[d.ID]
	tr, err := s.engine.CreateIncoming(d, []model.File{{ID: "new", Path: "new.txt", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := s.Control("transfer.acceptTrusted", receivedControlParams(tr.ID))
	if err != nil || value.(model.Transfer).State != "running" {
		t.Fatal(value, err)
	}
	got := s.store.snapshot().Devices[d.ID]
	expected := previous
	expected.Device.AutoAccept = true
	if got != expected || s.store.snapshot().Devices[other.ID].Device.AutoAccept || !persistedDevices(t, s)[d.ID].Device.AutoAccept {
		t.Fatal("trusted acceptance changed unrelated grants or did not persist", got)
	}
	future, err := s.engine.CreateIncoming(got.Device, []model.File{{ID: "future", Path: "future.txt", Size: 1}})
	if err != nil || future.State != "running" {
		t.Fatal("future offer still needs confirmation", future, err)
	}
}

func TestAcceptTrustedRejectsRevokedGrantAndFailedAcceptance(t *testing.T) {
	for _, mode := range []string{"unpaired", "permission", "cancelled", "already-accepted", "destination"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationHost(t)
			d, _ := integrationDevice(t, s, false)
			tr, err := s.engine.CreateIncoming(d, []model.File{{ID: "new", Path: "new.txt", Size: 1}})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "unpaired":
				err = s.store.revoke(d.ID)
			case "permission":
				d.CanSend = false
				err = s.store.updateDevice(d)
			case "cancelled":
				err = s.engine.Cancel(tr.ID)
			case "already-accepted":
				err = s.engine.Accept(tr.ID)
			case "destination":
				err = os.WriteFile(filepath.Join(tr.Destination, ".airlink-partials"), []byte("private stage collision"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Control("transfer.acceptTrusted", receivedControlParams(tr.ID)); err == nil {
				t.Fatal("ineligible transfer/device accepted as trusted")
			}
			if s.store.snapshot().Devices[d.ID].Device.AutoAccept || persistedDevices(t, s)[d.ID].Device.AutoAccept {
				t.Fatal("failed acceptance silently enabled future receipt")
			}
		})
	}
}

func TestAcceptTrustedSettingsFailureReportsActualAcceptanceWithoutTrust(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, false)
	tr, err := s.engine.CreateIncoming(d, []model.File{{ID: "new", Path: "new.txt", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	s.store.Lock()
	originalPath := s.store.path
	s.store.path = filepath.Join(t.TempDir(), "missing", "settings.json")
	s.store.Unlock()
	_, err = s.Control("transfer.acceptTrusted", receivedControlParams(tr.ID))
	s.store.Lock()
	s.store.path = originalPath
	s.store.Unlock()
	if err == nil || !strings.Contains(err.Error(), "transfer accepted; automatic receipt could not be saved") {
		t.Fatal("settings failure hid the actual state", err)
	}
	current, _ := s.engine.Get(tr.ID)
	if current.State != "running" || s.store.snapshot().Devices[d.ID].Device.AutoAccept || persistedDevices(t, s)[d.ID].Device.AutoAccept {
		t.Fatal("failed setting save invented durable trust", current)
	}
}

func TestReceivedAndTrustedControlsArePrivateOnly(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, false)
	tr := historyReceived(t, s, d, "received.txt", []byte("received"))
	value, err := s.Control("transfer.received", receivedControlParams(tr.ID))
	if err != nil || len(value.(engine.ReceivedResult).Items) != 1 {
		t.Fatal(value, err)
	}
	for _, action := range []string{"received", "acceptTrusted"} {
		w := securityRequest(s, "POST", "/api/v1/transfers/"+tr.ID+"/"+action, token, "", []byte(`{}`))
		if w.Code != 403 || bytes.Contains(w.Body.Bytes(), []byte(tr.Destination)) {
			t.Fatal("private control exposed to guest", action, w.Code, w.Body.String())
		}
	}
	if s.store.snapshot().Devices[d.ID].Device.AutoAccept {
		t.Fatal("guest changed its own receive permission")
	}
}

func browserPairRequest(t *testing.T, s *Server) (string, string) {
	t.Helper()
	w := securityRequest(s, "POST", "/api/v1/pair/request", "", "", []byte(`{"name":"Pair test browser","kind":"browser"}`))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p struct {
		RequestID string `json:"requestId"`
		PollToken string `json:"pollToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p.RequestID, p.PollToken
}

func TestTrustedPairApprovalPersistsGrantBeforeIssuingCredential(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		name := "default"
		if trusted {
			name = "explicit trusted"
		}
		t.Run(name, func(t *testing.T) {
			s := integrationHost(t)
			id, poll := browserPairRequest(t, s)
			method := "pair.approve"
			if trusted {
				method = "pair.approveTrusted"
			}
			if _, err := s.Control(method, receivedControlParams(id)); err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			deviceID := s.pending[id].Device.ID
			s.mu.Unlock()
			device := persistedDevices(t, s)[deviceID].Device
			if device.AutoAccept != trusted || !device.CanSend || !device.CanReceive || device.CanClipboard || device.CanRelay {
				t.Fatal("pair approval changed unrelated grants", device)
			}
			body, _ := json.Marshal(map[string]string{"requestId": id, "pollToken": poll})
			w := securityRequest(s, "POST", "/api/v1/pair/poll", "", "", body)
			if w.Code != 200 || len(w.Result().Cookies()) != 1 {
				t.Fatal("credential unavailable after durable choice", w.Code, w.Body.String())
			}
		})
	}
}

func TestTrustedPairApprovalFailureDenialAndExpiryNeverGrantAccess(t *testing.T) {
	for _, mode := range []string{"settings-failure", "denied", "expired"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationHost(t)
			id, _ := browserPairRequest(t, s)
			s.mu.Lock()
			deviceID := s.pending[id].Device.ID
			if mode == "expired" {
				s.pending[id].Expires = time.Now().Add(-time.Minute)
			}
			s.mu.Unlock()
			originalPath := s.store.path
			if mode == "settings-failure" {
				s.store.Lock()
				s.store.path = filepath.Join(t.TempDir(), "missing", "settings.json")
				s.store.Unlock()
			}
			if mode == "denied" {
				if err := s.approvePair(id, false); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Control("pair.approveTrusted", receivedControlParams(id)); err == nil {
				t.Fatal("failed/expired/denied approval succeeded")
			}
			s.store.Lock()
			s.store.path = originalPath
			s.store.Unlock()
			if _, exists := s.store.snapshot().Devices[deviceID]; exists {
				t.Fatal("unsaved/denied trust visible in memory")
			}
			if _, exists := persistedDevices(t, s)[deviceID]; exists {
				t.Fatal("unsaved/denied trust was persisted")
			}
			s.mu.Lock()
			credential := s.pending[id].Token
			s.mu.Unlock()
			if credential != "" {
				t.Fatal("failed approval issued a credential")
			}
		})
	}
}

func TestPeerPairTrustIsDirectionalAndExplicit(t *testing.T) {
	for _, policy := range []struct {
		name      string
		initiator bool
		approver  bool
	}{{"default", false, false}, {"trusted initiator", true, false}, {"trusted approver", false, true}, {"both explicit", true, true}} {
		t.Run(policy.name, func(t *testing.T) {
			source, target := integrationHost(t), integrationHost(t)
			method := "peer.connect"
			if policy.initiator {
				method = "peer.connectTrusted"
			}
			body, _ := json.Marshal(map[string]string{"url": hostURL(target), "fingerprint": target.fingerprint})
			value, err := source.Control(method, body)
			if err != nil {
				t.Fatal(err)
			}
			id := value.(map[string]string)["requestId"]
			approve := "pair.approve"
			if policy.approver {
				approve = "pair.approveTrusted"
			}
			if _, err := target.Control(approve, receivedControlParams(id)); err != nil {
				t.Fatal(err)
			}
			sourceID, targetID := source.store.snapshot().ID, target.store.snapshot().ID
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				local := source.store.snapshot().Devices[targetID]
				remote := target.store.snapshot().Devices[sourceID]
				if local.OutboundToken != "" && remote.OutboundToken != "" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			local := source.store.snapshot().Devices[targetID]
			remote := target.store.snapshot().Devices[sourceID]
			if local.OutboundToken == "" || remote.OutboundToken == "" || local.Device.AutoAccept != policy.initiator || remote.Device.AutoAccept != policy.approver {
				t.Fatal("one side's choice implicitly trusted the reverse direction", local.Device, remote.Device)
			}
			if persistedDevices(t, source)[targetID].Device.AutoAccept != policy.initiator || persistedDevices(t, target)[sourceID].Device.AutoAccept != policy.approver {
				t.Fatal("explicit trust did not persist")
			}
		})
	}
}

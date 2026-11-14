package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

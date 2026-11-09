package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func clipboardDevice(t *testing.T, s *Server) (model.Device, string) {
	t.Helper()
	d, token := integrationDevice(t, s, false)
	d.CanClipboard = true
	if err := s.store.updateDevice(d); err != nil {
		t.Fatal(err)
	}
	return d, token
}
func TestClipboardPermissionAndGuestIsolation(t *testing.T) {
	s := integrationHost(t)
	blocked, blockedToken := integrationDevice(t, s, false)
	allowed, token := clipboardDevice(t, s)
	other, _ := clipboardDevice(t, s)
	payload := []byte(`{"text":"explicit clipboard text"}`)
	if w := securityRequest(s, http.MethodPost, "/api/v1/clipboard", blockedToken, "", payload); w.Code != 403 {
		t.Fatalf("disabled clipboard accepted: %d", w.Code)
	}
	if len(s.clipboardSnapshot(blocked.ID)) != 0 {
		t.Fatal("blocked message stored")
	}
	w := securityRequest(s, http.MethodPost, "/api/v1/clipboard", token, "", payload)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var incoming model.ClipboardMessage
	_ = json.Unmarshal(w.Body.Bytes(), &incoming)
	if incoming.Direction != "receive" || incoming.DeviceID != allowed.ID {
		t.Fatal(incoming)
	}
	if len(s.guestStatus(other).Clipboard) != 0 || len(s.guestStatus(allowed).Clipboard) != 1 {
		t.Fatal("clipboard crossed guest boundary")
	}
	if !s.guestStatus(allowed).ClipboardAllowed || s.guestStatus(blocked).ClipboardAllowed {
		t.Fatal("permission status incorrect")
	}
	if w := securityRequest(s, http.MethodDelete, "/api/v1/clipboard/"+incoming.ID, otherToken(t, s, other.ID), "", nil); w.Code != 404 {
		t.Fatal("another device dismissed text", w.Code)
	}
	if w := securityRequest(s, http.MethodDelete, "/api/v1/clipboard/"+incoming.ID, token, "", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if len(s.clipboardSnapshot(allowed.ID)) != 0 {
		t.Fatal("dismiss failed")
	}
}
func otherToken(t *testing.T, s *Server, id string) string {
	t.Helper()
	token := randomID()
	if err := s.store.edit(id, func(r *deviceRecord) { r.TokenHash = digest([]byte(token)) }); err != nil {
		t.Fatal(err)
	}
	return token
}
func TestClipboardBoundsDeduplicationAndExpiry(t *testing.T) {
	s := integrationHost(t)
	d, _ := clipboardDevice(t, s)
	for _, text := range []string{"", strings.Repeat("a", clipboardMaxBytes+1), strings.Repeat("🍎", clipboardMaxBytes/4+1)} {
		if _, err := s.recordClipboard(d, "receive", "", text); err == nil {
			t.Fatal("invalid text accepted")
		}
	}
	text := strings.Repeat("🍎", clipboardMaxBytes/4)
	first, err := s.recordClipboard(d, "receive", "stable-id", text)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.recordClipboard(d, "receive", "stable-id", text)
	if err != nil || first.ID != second.ID || len(s.clipboardSnapshot(d.ID)) != 1 {
		t.Fatal("retry duplicated text")
	}
	if _, err = s.recordClipboard(d, "receive", "stable-id", "changed"); err == nil {
		t.Fatal("changed duplicate accepted")
	}
	s.clipboardMu.Lock()
	s.clipboardMessages[0].CreatedAt = time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	s.clipboardMu.Unlock()
	if len(s.clipboardSnapshot(d.ID)) != 0 {
		t.Fatal("expired text retained")
	}
	for i := 0; i < 105; i++ {
		if _, err = s.recordClipboard(d, "receive", "", "bounded history"); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.clipboardSnapshot("")) != 100 {
		t.Fatal("history not bounded")
	}
}
func TestClipboardHostToBrowserAndRevoke(t *testing.T) {
	s := integrationHost(t)
	d, _ := clipboardDevice(t, s)
	message, err := s.sendClipboard(d.ID, "host text")
	if err != nil {
		t.Fatal(err)
	}
	if message.Direction != "send" || len(s.guestStatus(d).Clipboard) != 1 {
		t.Fatal("browser did not receive host offer")
	}
	params, _ := json.Marshal(map[string]string{"id": d.ID})
	if _, err = s.Control("device.revoke", params); err != nil {
		t.Fatal(err)
	}
	if len(s.clipboardSnapshot("")) != 0 {
		t.Fatal("revocation retained device clipboard")
	}
}

func TestClipboardDismissStillAllowedAfterPermissionDisabled(t *testing.T) {
	s := integrationHost(t)
	d, token := clipboardDevice(t, s)
	message, err := s.sendClipboard(d.ID, "already received")
	if err != nil {
		t.Fatal(err)
	}
	d.CanClipboard = false
	if err = s.store.updateDevice(d); err != nil {
		t.Fatal(err)
	}
	w := securityRequest(s, http.MethodDelete, "/api/v1/clipboard/"+message.ID, token, "", nil)
	if w.Code != 200 || len(s.clipboardSnapshot(d.ID)) != 0 {
		t.Fatal("cannot dismiss own prior message", w.Code)
	}
}

func TestClipboardIsNeverPersistedAndOldPairingSurvivesUpgrade(t *testing.T) {
	source := integrationHost(t)
	d, token := clipboardDevice(t, source)
	opts := source.opts
	id := source.store.snapshot().ID
	fingerprint := source.fingerprint
	marker := "clipboard-private-ephemeral-marker"
	if _, err := source.recordClipboard(d, "receive", "", marker); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.fingerprint != fingerprint || resumed.Status().ID != id {
		t.Fatal("upgrade replaced identity")
	}
	if len(resumed.Status().Clipboard) != 0 {
		t.Fatal("clipboard text persisted across restart")
	}
	if _, ok := resumed.store.authenticate(token); !ok {
		t.Fatal("pairing credential lost across upgrade")
	}
	p := resumed.store.snapshot()
	if !p.Devices[d.ID].Device.CanClipboard {
		t.Fatal("explicit clipboard grant lost")
	}
}
func TestClipboardNativeDeliveryUsesPinnedPairAndExplicitPermission(t *testing.T) {
	source, target := integrationHost(t), integrationHost(t)
	rid, err := source.connectPeer(hostURL(target), target.fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.approvePair(rid, true); err != nil {
		t.Fatal(err)
	}
	sourceID, targetID := source.store.snapshot().ID, target.store.snapshot().ID
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		rec, ok := source.store.snapshot().Devices[targetID]
		if ok && rec.PendingReturnToken == "" && target.store.snapshot().Devices[sourceID].OutboundToken != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err = source.sendClipboard(targetID, "not allowed"); err == nil {
		t.Fatal("permission skipped")
	}
	if err = source.store.edit(targetID, func(r *deviceRecord) { r.Device.CanClipboard = true }); err != nil {
		t.Fatal(err)
	}
	if _, err = source.sendClipboard(targetID, "still blocked on target"); err == nil {
		t.Fatal("target permission skipped")
	}
	if err = target.store.edit(sourceID, func(r *deviceRecord) { r.Device.CanClipboard = true }); err != nil {
		t.Fatal(err)
	}
	sent, err := source.sendClipboard(targetID, "Hello, Welt 🌍\nsecond line")
	if err != nil {
		t.Fatal(err)
	}
	messages := target.clipboardSnapshot(sourceID)
	if len(messages) != 1 || messages[0].ID != sent.ID || messages[0].Text != sent.Text || messages[0].Direction != "receive" {
		t.Fatal("peer text mismatch", messages)
	}
	if sent.Direction != "send" {
		t.Fatal("incorrect source direction")
	}
	if err = requestJSON(context.Background(), pinnedClient(target.fingerprint), http.MethodPost, hostURL(target)+"/api/v1/clipboard", source.store.snapshot().Devices[targetID].OutboundToken, map[string]string{"id": sent.ID, "text": sent.Text}, nil); err != nil {
		t.Fatal(err)
	}
	if len(target.clipboardSnapshot(sourceID)) != 1 {
		t.Fatal("native retry duplicated incoming text")
	}
}

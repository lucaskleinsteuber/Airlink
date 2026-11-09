package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/engine"
	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func historyReceived(t *testing.T, s *Server, d model.Device, path string, data []byte) model.Transfer {
	t.Helper()
	d.AutoAccept = true
	tr, e := s.engine.CreateIncoming(d, []model.File{{ID: "history-file", Path: path, Size: int64(len(data))}})
	if e != nil {
		t.Fatal(e)
	}
	if len(data) > 0 {
		if _, e = s.engine.WriteChunk(context.Background(), tr.ID, "history-file", 0, digest(data), bytes.NewReader(data)); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.engine.CompleteFile(context.Background(), tr.ID, "history-file", digest(data)); e != nil {
		t.Fatal(e)
	}
	tr, _ = s.engine.Get(tr.ID)
	return tr
}

func TestRemoveHistoryHTTPPreservesUnselectedDataSharesAndRelays(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, true)
	other, _ := integrationDevice(t, s, true)
	data := []byte("A shared received file outlives its list entry.")
	own := historyReceived(t, s, d, "keep.bin", data)
	unselected := historyReceived(t, s, d, "unselected.bin", []byte("other own entry"))
	foreign := historyReceived(t, s, other, "foreign.bin", []byte("private"))
	active, err := s.engine.CreateIncoming(d, []model.File{{ID: "active", Path: "pending.bin", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	share, err := s.createShare("local", model.ShareCreate{TransferID: own.ID})
	if err != nil {
		t.Fatal(err)
	}
	d.CanRelay = true
	other.CanRelay = true
	relay, err := s.relays.Create(d, other, []model.File{{ID: "relay-file", Path: "relay.txt", Size: 0, SHA256: digest(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.relays.Action(relay.ID, d.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(s.traffic.WrapReader(context.Background(), bytes.NewReader(data), "receive"))
	var result map[string]bool
	if err = requestJSON(context.Background(), pinnedClient(s.fingerprint), http.MethodDelete, hostURL(s)+"/api/v1/history/"+own.ID, token, nil, &result); err != nil || !result["ok"] {
		t.Fatal("owned entry could not be removed", result, err)
	}
	if _, err = s.engine.Get(own.ID); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal("selected entry retained", err)
	}
	for _, id := range []string{unselected.ID, foreign.ID, active.ID} {
		if _, err = s.engine.Get(id); err != nil {
			t.Fatal("unselected entry removed", id, err)
		}
	}
	if _, err = s.relays.Get(relay.ID, d.ID); err != nil {
		t.Fatal("file removal affected relay history", err)
	}
	if got := s.statistics("").TrafficReceivedBytes; got != int64(len(data)) {
		t.Fatal("lifetime traffic counter changed", got)
	}
	if got, err := os.ReadFile(filepath.Join(own.Destination, "keep.bin")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("received payload changed", err)
	}
	f, _, err := s.shares.Open(share.ID, share.Files[0].ID)
	if err != nil {
		t.Fatal("independent share source lost", err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("share bytes changed", err)
	}
	opts := s.opts
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = next.engine.Get(own.ID); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal("entry returned after server restart", err)
	}
	if _, err = next.relays.Get(relay.ID, d.ID); err != nil {
		t.Fatal("relay history lost across restart", err)
	}
	f, _, err = next.shares.Open(share.ID, share.Files[0].ID)
	if err != nil {
		t.Fatal("share lost across restart", err)
	}
	f.Close()
}

func TestRemoveHistoryHTTPEnforcesOwnerStateAuthenticationAndMethods(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, true)
	other, _ := integrationDevice(t, s, true)
	foreign := historyReceived(t, s, other, "private.bin", []byte("other"))
	active, err := s.engine.CreateIncoming(d, []model.File{{ID: "active", Path: "active.bin", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, bearer string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequest(method, hostURL(s)+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := pinnedClient(s.fingerprint).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, string(body), res.Header
	}
	foreignCode, foreignBody, _ := request(http.MethodDelete, "/api/v1/history/"+foreign.ID, token)
	missingCode, missingBody, _ := request(http.MethodDelete, "/api/v1/history/missing-entry", token)
	if foreignCode != 404 || missingCode != 404 || foreignBody != missingBody || foreignBody != "{\"error\":\"history entry not found\"}\n" {
		t.Fatal("foreign entry existence disclosed", foreignCode, foreignBody, missingCode, missingBody)
	}
	if code, _, _ := request(http.MethodDelete, "/api/v1/history/"+active.ID, token); code != 409 {
		t.Fatal("active entry was removable", code)
	}
	if code, _, _ := request(http.MethodDelete, "/api/v1/history/", token); code != 400 {
		t.Fatal("missing ID accepted", code)
	}
	if code, _, _ := request(http.MethodDelete, "/api/v1/history/invalid/path", token); code != 400 {
		t.Fatal("invalid ID accepted", code)
	}
	if code, _, headers := request(http.MethodGet, "/api/v1/history/"+foreign.ID, token); code != 405 || headers.Get("Allow") != http.MethodDelete {
		t.Fatal("wrong method accepted", code, headers)
	}
	if code, _, _ := request(http.MethodDelete, "/api/v1/history/"+foreign.ID, ""); code != 401 {
		t.Fatal("unauthenticated deletion accepted", code)
	}
	for _, id := range []string{foreign.ID, active.ID} {
		if _, err = s.engine.Get(id); err != nil {
			t.Fatal("rejected request removed entry", id, err)
		}
	}
}

func TestRemoveHistoryPrivateControlValidatesInputAndCanRemoveSentEntry(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, true)
	source := filepath.Join(t.TempDir(), "original.txt")
	data := []byte("Host source remains available.")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	tr, err := s.engine.Offer(d, []string{source})
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]string{"id": tr.ID})
	if _, err = s.Control("history.remove", params); !errors.Is(err, engine.ErrConflict) {
		t.Fatal("active local entry removed", err)
	}
	if err = s.engine.Cancel(tr.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.Control("history.remove", params)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if string(encoded) != `{"ok":true}` {
		t.Fatal("unexpected removal response", string(encoded))
	}
	if _, err = s.engine.Get(tr.ID); !errors.Is(err, engine.ErrNotFound) {
		t.Fatal("host removal retained entry", err)
	}
	if got, err := os.ReadFile(source); err != nil || !bytes.Equal(got, data) {
		t.Fatal("host removal changed source", err)
	}
	for _, input := range []string{"", "null", "{}", `{"id":""}`, `{"id":"/invalid"}`, `{"id":12}`, "{"} {
		if _, err = s.Control("history.remove", json.RawMessage(input)); !errors.Is(err, engine.ErrInvalid) {
			t.Fatal("invalid input accepted", input, err)
		}
	}
}
func TestHistoryFiltersPrivacyAndClearPreservesSharedData(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, true)
	other, _ := integrationDevice(t, s, true)
	data := []byte("history remains a share source")
	own := historyReceived(t, s, d, "Holiday.mov", data)
	foreign := historyReceived(t, s, other, "private.bin", []byte("other"))
	active, e := s.engine.CreateIncoming(d, []model.File{{ID: "waiting", Path: "active.bin", Size: 1}})
	if e != nil {
		t.Fatal(e)
	}
	share, e := s.createShare("local", model.ShareCreate{TransferID: own.ID})
	if e != nil {
		t.Fatal(e)
	}
	h := s.history(d.ID, historyFilter{Query: "HOLIDAY", State: "completed", Direction: "receive", Limit: 1})
	if len(h.Transfers) != 1 || h.Transfers[0].ID != own.ID || h.Transfers[0].Destination != "" {
		t.Fatal(h)
	}
	var remote model.History
	if e = requestJSON(context.Background(), pinnedClient(s.fingerprint), http.MethodGet, hostURL(s)+"/api/v1/history?deviceId="+other.ID, token, nil, &remote); e != nil {
		t.Fatal(e)
	}
	if len(remote.Transfers) > 0 || len(remote.Relays) > 0 {
		t.Fatal("history crossed device boundary")
	}
	if e = requestJSON(context.Background(), pinnedClient(s.fingerprint), http.MethodDelete, hostURL(s)+"/api/v1/history", token, nil, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.engine.Get(active.ID); e != nil {
		t.Fatal("active removed")
	}
	if _, e = s.engine.Get(foreign.ID); e != nil {
		t.Fatal("foreign removed")
	}
	if b, e := os.ReadFile(filepath.Join(own.Destination, "Holiday.mov")); e != nil || !bytes.Equal(b, data) {
		t.Fatal("received file removed", e)
	}
	f, _, e := s.shares.Open(share.ID, share.Files[0].ID)
	if e != nil {
		t.Fatal("share broken by history clear", e)
	}
	defer f.Close()
	if b, _ := io.ReadAll(f); !bytes.Equal(b, data) {
		t.Fatal("share source changed")
	}
}
func TestStatisticsConfirmedBytesAndPrivateLifetimeCounters(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, true)
	other, _ := integrationDevice(t, s, true)
	historyReceived(t, s, d, "one.txt", []byte("abc"))
	historyReceived(t, s, other, "two.txt", []byte("12345"))
	_, _ = io.ReadAll(s.traffic.WrapReader(context.Background(), bytes.NewReader([]byte("payload")), "receive"))
	private := s.statistics(d.ID)
	host := s.statistics("")
	if private.ReceivedBytes != 3 || private.CompletedTransfers != 1 || private.TrafficReceivedBytes != 0 || host.ReceivedBytes != 8 || host.CompletedTransfers != 2 || host.TrafficReceivedBytes != 7 {
		t.Fatal(private, host)
	}
	if len(private.Days) != 1 || private.Days[0].Date != time.Now().UTC().Format("2006-01-02") {
		t.Fatal(private.Days)
	}
	if e := s.clearHistory(d.ID); e != nil {
		t.Fatal(e)
	}
	if s.statistics(d.ID).ReceivedBytes != 0 || s.statistics("").TrafficReceivedBytes != 7 {
		t.Fatal("clear reset traffic or retained cleared totals")
	}
	if plus(math.MaxInt64, 1) != math.MaxInt64 {
		t.Fatal("stats overflow")
	}
}
func TestSettingsMigrationDefaultsPreservesDisabledShortcutAndOldClientUpdates(t *testing.T) {
	s := integrationHost(t)
	opts := s.opts
	fp := s.fingerprint
	original := s.store.snapshot()
	if e := s.Close(); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(opts.DataDir, "settings.json")
	b, _ := os.ReadFile(path)
	var legacy map[string]any
	_ = json.Unmarshal(b, &legacy)
	settings := legacy["settings"].(map[string]any)
	delete(settings, "shortcut")
	delete(settings, "sendLimitBytesPerSecond")
	delete(settings, "receiveLimitBytesPerSecond")
	b, _ = json.Marshal(legacy)
	_ = os.WriteFile(path, b, 0600)
	up, e := New(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer up.Close()
	got := up.store.snapshot()
	if got.ID != original.ID || up.fingerprint != fp || got.Settings.Shortcut != (model.Shortcut{Enabled: true, KeyCode: 37, Modifiers: 768}) {
		t.Fatal("migration replaced identity/defaults")
	}
	got.Settings.Shortcut.Enabled = false
	got.Settings.SendLimitBytesPerSecond = 32768
	payload, _ := json.Marshal(got.Settings)
	if _, e = up.Control("settings.update", payload); e != nil {
		t.Fatal(e)
	}
	if _, e = up.Control("settings.update", json.RawMessage(`{"name":"Old client rename"}`)); e != nil {
		t.Fatal(e)
	}
	current := up.store.snapshot().Settings
	if current.Shortcut.Enabled || current.SendLimitBytesPerSecond != 32768 || current.Name != "Old client rename" {
		t.Fatal("old client reset new settings", current)
	}
	if e = up.Close(); e != nil {
		t.Fatal(e)
	}
	again, e := New(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	if again.store.snapshot().Settings.Shortcut.Enabled {
		t.Fatal("explicit disabled shortcut re-enabled")
	}
	for _, shortcut := range []model.Shortcut{{Enabled: true, KeyCode: 37}, {Enabled: true, KeyCode: 200, Modifiers: 256}, {Enabled: true, KeyCode: 37, Modifiers: 1 << 20}} {
		if validateShortcut(shortcut) == nil {
			t.Fatal("invalid shortcut accepted")
		}
	}
}
func TestPublicSharePayloadRespectsLiveGlobalSendLimit(t *testing.T) {
	s := integrationHost(t)
	file := filepath.Join(t.TempDir(), "throttle.bin")
	_ = os.WriteFile(file, make([]byte, 8192), 0600)
	share, e := s.createShare("local", model.ShareCreate{Paths: []string{file}})
	if e != nil {
		t.Fatal(e)
	}
	_, token, e := s.shares.Unlock(context.Background(), share.ID, share.URL[len(share.URL)-64:], "", "")
	if e != nil {
		t.Fatal(e)
	}
	_ = s.traffic.SetLimits(32768, 0)
	req, _ := http.NewRequest(http.MethodGet, hostURL(s)+"/api/v1/public/shares/"+share.ID+"/files/"+share.Files[0].ID, nil)
	req.AddCookie(&http.Cookie{Name: shareCookieName(share.ID), Value: token})
	start := time.Now()
	res, e := pinnedClient(s.fingerprint).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(res.Body)
	res.Body.Close()
	if e != nil || len(b) != 8192 || res.StatusCode != 200 {
		t.Fatal(res.Status, e)
	}
	if time.Since(start) < 210*time.Millisecond {
		t.Fatal("share bypassed shared cap")
	}
	if s.traffic.Snapshot().SentBytes != 8192 {
		t.Fatal("payload counter incorrect")
	}
}

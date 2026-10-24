package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
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

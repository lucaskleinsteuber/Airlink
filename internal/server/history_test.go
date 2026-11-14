package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
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
func TestHistoryFiltersAndClearPreserveActiveForeignAndReceivedData(t *testing.T) {
	s := integrationHost(t)
	d, _ := integrationDevice(t, s, true)
	other, _ := integrationDevice(t, s, true)
	data := []byte("received files outlive their history entry")
	own := historyReceived(t, s, d, "Holiday.mov", data)
	foreign := historyReceived(t, s, other, "private.bin", []byte("other"))
	active, e := s.engine.CreateIncoming(d, []model.File{{ID: "waiting", Path: "active.bin", Size: 1}})
	if e != nil {
		t.Fatal(e)
	}
	h := s.history("", historyFilter{Query: "HOLIDAY", State: "completed", Direction: "receive", Limit: 1})
	if len(h.Transfers) != 1 || h.Transfers[0].ID != own.ID {
		t.Fatal(h)
	}
	if e = s.clearHistory(d.ID); e != nil {
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
}

package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestClearHistoryKeepsPayloadsSourcesAndOtherDevices(t *testing.T) {
	e, _, _ := testEngine(t)
	data := []byte("Keep the received file after clearing history.")
	done := incoming(t, e, "kept.txt", data)
	putAll(t, e, done, data)
	if err := e.CompleteFile(context.Background(), done.ID, "file-a", digest(data)); err != nil {
		t.Fatal(err)
	}
	active := incoming(t, e, "active.txt", []byte("waiting"))
	other := testDevice()
	other.ID = "device-b"
	foreign, err := e.CreateIncoming(other, []model.File{{ID: "foreign", Path: "foreign", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Cancel(foreign.ID)
	source := filepath.Join(t.TempDir(), "original.txt")
	_ = os.WriteFile(source, data, 0600)
	out, err := e.Offer(testDevice(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Cancel(out.ID)
	count, err := e.ClearHistory(testDevice().ID)
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if _, err = e.Get(done.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("terminal metadata retained")
	}
	if _, err = e.Get(active.ID); err != nil {
		t.Fatal("active removed")
	}
	if _, err = e.Get(foreign.ID); err != nil {
		t.Fatal("other device removed")
	}
	received, err := os.ReadFile(filepath.Join(done.Destination, "kept.txt"))
	if err != nil || string(received) != string(data) {
		t.Fatal("payload removed", err)
	}
	original, err := os.ReadFile(source)
	if err != nil || string(original) != string(data) {
		t.Fatal("source modified", err)
	}
}

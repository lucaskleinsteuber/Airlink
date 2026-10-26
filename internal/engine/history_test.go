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

func TestRemoveHistoryPreservesFilesOtherEntriesAndRestart(t *testing.T) {
	e, dataDir, receiveDir := testEngine(t)
	data := []byte("Verified received bytes remain outside history metadata.")
	done := incoming(t, e, "keep.txt", data)
	putAll(t, e, done, data)
	if err := e.CompleteFile(context.Background(), done.ID, "file-a", digest(data)); err != nil {
		t.Fatal(err)
	}
	done, _ = e.Get(done.ID)
	active := incoming(t, e, "pending.txt", []byte("active"))
	other := incoming(t, e, "other.txt", []byte("other"))
	if err := e.Cancel(other.ID); err != nil {
		t.Fatal(err)
	}
	source := offerSource(t)
	out, err := e.Offer(testDevice(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Cancel(out.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.RemoveHistory(testDevice().ID, done.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{active.ID, other.ID, out.ID} {
		if _, err = e.Get(id); err != nil {
			t.Fatal("unselected entry removed", id, err)
		}
	}
	var chunks int
	if err = e.db.QueryRow("SELECT count(*) FROM chunks WHERE transfer_id=?", done.ID).Scan(&chunks); err != nil || chunks != 0 {
		t.Fatal("removed metadata retained chunks", chunks, err)
	}
	// Late completion/transport callbacks must not recreate a removed record.
	if err = e.CompleteFile(context.Background(), done.ID, "file-a", digest(data)); !errors.Is(err, ErrNotFound) {
		t.Fatal("late completion did not reject removed ID", err)
	}
	if err = e.MarkReconnecting(done.ID, "late worker"); !errors.Is(err, ErrNotFound) {
		t.Fatal("late worker revived history", err)
	}
	if err = e.RemoveHistory("", out.ID); err != nil {
		t.Fatal("private host could not remove outgoing entry", err)
	}
	if err = e.MarkOutgoingProgress(out.ID, out.Files[0].ID, 0, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("late outgoing ACK recreated entry", err)
	}
	if err = e.CompleteOutgoingFile(out.ID, out.Files[0].ID, digest([]byte("anything"))); !errors.Is(err, ErrNotFound) {
		t.Fatal("late final ACK recreated entry", err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(dataDir, receiveDir)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	for _, id := range []string{done.ID, out.ID} {
		if _, err = next.Get(id); !errors.Is(err, ErrNotFound) {
			t.Fatal("entry returned after restart", id, err)
		}
	}
	for _, id := range []string{active.ID, other.ID} {
		if _, err = next.Get(id); err != nil {
			t.Fatal("unselected entry lost after restart", id, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(done.Destination, "keep.txt")); err != nil || string(got) != string(data) {
		t.Fatal("published bytes changed", err)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != "Original source remains in place." {
		t.Fatal("outgoing source changed", err)
	}
}

func TestRemoveHistoryChecksOwnerBeforeStateAndRejectsEveryActiveState(t *testing.T) {
	for _, state := range []string{"offered", "queued", "running", "paused", "reconnecting", "verifying", "checkingDuplicate", "waitingDuplicate", "future-state"} {
		t.Run(state, func(t *testing.T) {
			e, _, _ := testEngine(t)
			tr := incoming(t, e, "pending.txt", []byte("x"))
			e.mu.Lock()
			e.records[tr.ID].Transfer.State = state
			err := e.saveLocked(e.records[tr.ID])
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if err = e.RemoveHistory("another-device", tr.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("foreign state or existence disclosed", err)
			}
			if err = e.RemoveHistory(testDevice().ID, tr.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("active/unknown state removed", err)
			}
			if got, err := e.Get(tr.ID); err != nil || got.State != state {
				t.Fatal("rejected removal changed entry", got.State, err)
			}
		})
	}
}

func TestRemoveHistoryAllowsOnlyTerminalEntriesAndValidID(t *testing.T) {
	for _, state := range []string{"completed", "failed", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			e, _, _ := testEngine(t)
			tr := incoming(t, e, "terminal.txt", []byte("x"))
			e.mu.Lock()
			e.records[tr.ID].Transfer.State = state
			err := e.saveLocked(e.records[tr.ID])
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if err = e.RemoveHistory("another-device", tr.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("foreign entry could be removed", err)
			}
			if err = e.RemoveHistory(testDevice().ID, tr.ID); err != nil {
				t.Fatal(err)
			}
			if err = e.RemoveHistory(testDevice().ID, tr.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("missing entry did not report not found", err)
			}
		})
	}
	e, _, _ := testEngine(t)
	for _, id := range []string{"", " \t", "invalid/path"} {
		if err := e.RemoveHistory("", id); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid ID accepted", err)
		}
	}
}

func TestRemoveHistoryRollsBackFailedDurableDeletion(t *testing.T) {
	e, _, _ := testEngine(t)
	data := []byte("durable")
	done := incoming(t, e, "durable.txt", data)
	putAll(t, e, done, data)
	if err := e.CompleteFile(context.Background(), done.ID, "file-a", digest(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec("CREATE TRIGGER reject_history_delete BEFORE DELETE ON transfers BEGIN SELECT RAISE(ABORT,'injected deletion failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveHistory("", done.ID); err == nil {
		t.Fatal("failed SQLite transaction reported success")
	}
	if got, err := e.Get(done.ID); err != nil || got.State != "completed" {
		t.Fatal("failed removal changed memory", err)
	}
	var count int
	if err := e.db.QueryRow("SELECT count(*) FROM chunks WHERE transfer_id=?", done.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed removal lost persisted checkpoints", count, err)
	}
	if _, err := e.db.Exec("DROP TRIGGER reject_history_delete"); err != nil {
		t.Fatal(err)
	}
	if err := e.RemoveHistory("", done.ID); err != nil {
		t.Fatal("corrected failure could not be retried", err)
	}
}

func TestRemoveHistoryExplicitlyEndsRetainedOfferReplayWithoutStartingWork(t *testing.T) {
	e, _, _ := testEngine(t)
	source := offerSource(t)
	const key = "cccccccccccccccccccccccccccccccc"
	tr, err := e.OfferWithRequestID(testDevice(), []string{source}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Cancel(tr.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.RemoveHistory("", tr.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.Transfers("")) != 0 {
		t.Fatal("removal started replacement work")
	}
	// As with existing history.clear, only a later explicit offer creates work.
	next, err := e.OfferWithRequestID(testDevice(), []string{source}, key)
	if err != nil || next.ID == tr.ID || len(e.Transfers("")) != 1 {
		t.Fatal("explicit new offer did not follow retained-record replay lifetime", next.ID, err)
	}
}

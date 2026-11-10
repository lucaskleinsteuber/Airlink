package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHealthyReceiverOfferClearsDurableReconnectError(t *testing.T) {
	e, state, received := testEngine(t)
	path := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(path, []byte("original content"), 0600); err != nil {
		t.Fatal(err)
	}
	offer, err := e.Offer(testDevice(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.MarkReconnecting(offer.ID, "connection timeout"); err != nil {
		t.Fatal(err)
	}
	// A failed status save must leave the old status/error in memory as well.
	if _, err = e.db.Exec("CREATE TRIGGER reject_wait BEFORE UPDATE ON transfers BEGIN SELECT RAISE(FAIL,'simulated write failure'); END;"); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingWaiting(offer.ID, "offered"); err == nil {
		t.Fatal("failed status save accepted")
	}
	before, _ := e.Get(offer.ID)
	if before.State != "reconnecting" || before.Error != "connection timeout" {
		t.Fatal("failed save changed visible state")
	}
	if _, err = e.db.Exec("DROP TRIGGER reject_wait"); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingWaiting(offer.ID, "offered"); err != nil {
		t.Fatal(err)
	}
	after, _ := e.Get(offer.ID)
	if after.State != "offered" || after.Error != "" || after.Problem != nil || after.BytesPerSecond != 0 || after.CompletedBytes != 0 || after.Files[0].ID != offer.Files[0].ID {
		t.Fatal("healthy waiting offer retained stale error or changed progress", after)
	}
	if err = e.MarkOutgoingWaiting(offer.ID, "offered"); err != nil {
		t.Fatal(err)
	}
	again, _ := e.Get(offer.ID)
	if again.UpdatedAt != after.UpdatedAt {
		t.Fatal("unchanged peer polling rewrote activity")
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(state, received)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	kept, _ := reopened.Get(offer.ID)
	if kept.State != "offered" || kept.Error != "" || kept.Files[0].ID != offer.Files[0].ID {
		t.Fatal("restart lost waiting state")
	}
}

func TestLateReceiverWaitCannotResumePausedOrTerminalSender(t *testing.T) {
	e, _, _ := testEngine(t)
	path := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(path, []byte("original content"), 0600); err != nil {
		t.Fatal(err)
	}
	offer, err := e.Offer(testDevice(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Resume(offer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.Pause(offer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingWaiting(offer.ID, "offered"); !errors.Is(err, ErrConflict) {
		t.Fatal("late wait revived paused sender", err)
	}
	if err = e.Cancel(offer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingWaiting(offer.ID, "offered"); !errors.Is(err, ErrConflict) {
		t.Fatal("late wait revived cancelled sender", err)
	}
	receiver, err := e.CreateIncoming(testDevice(), offer.Files)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingWaiting(receiver.ID, "offered"); !errors.Is(err, ErrConflict) {
		t.Fatal("sender status changed incoming offer", err)
	}
}

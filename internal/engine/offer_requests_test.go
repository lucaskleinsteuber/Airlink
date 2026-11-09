package engine

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func offerSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(path, []byte("Original source remains in place."), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOfferReceiptSurvivesLostReplyAndServiceRestart(t *testing.T) {
	e, data, receive := testEngine(t)
	source := offerSource(t)
	const key = "0123456789abcdef0123456789abcdef"
	first, err := e.OfferWithRequestID(testDevice(), []string{source}, key)
	if err != nil {
		t.Fatal(err)
	}
	// The caller never receives this successful commit. A completed/cancelled
	// transaction must remain the same operation, never a new restarted send.
	if err = e.Cancel(first.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	next, err := New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	replayed, err := next.OfferWithRequestID(testDevice(), []string{source}, key)
	if err != nil || replayed.ID != first.ID || replayed.State != "cancelled" || len(next.Transfers("")) != 1 {
		t.Fatal("lost reply duplicated or revived work", replayed, err)
	}
	if replayed.Files[0].ID != first.Files[0].ID {
		t.Fatal("retry replaced manifest identities")
	}
}

func TestConcurrentOfferRetriesCommitOneTransfer(t *testing.T) {
	e, _, _ := testEngine(t)
	source := offerSource(t)
	var wg sync.WaitGroup
	ids := make(chan string, 24)
	for range 24 {
		wg.Go(func() {
			got, err := e.OfferWithRequestID(testDevice(), []string{source}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- got.ID
		})
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 || len(e.Transfers("")) != 1 {
		t.Fatal("concurrent requests created duplicate jobs", seen)
	}
	contents, err := os.ReadFile(source)
	if err != nil || string(contents) != "Original source remains in place." {
		t.Fatal("source changed", err)
	}
}

func TestOfferRequestCannotChangeRecipientOrPaths(t *testing.T) {
	e, _, _ := testEngine(t)
	first, second := offerSource(t), offerSource(t)
	const key = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := e.OfferWithRequestID(testDevice(), []string{first}, key); err != nil {
		t.Fatal(err)
	}
	other := testDevice()
	other.ID = "different-recipient"
	for _, item := range []struct {
		device model.Device
		paths  []string
	}{{testDevice(), []string{second}}, {other, []string{first}}} {
		if _, err := e.OfferWithRequestID(item.device, item.paths, key); !errors.Is(err, ErrConflict) {
			t.Fatal("same request changed meaning", err)
		}
	}
	for _, invalid := range []string{"short", "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", "../../../../../../../../../../.."} {
		if _, err := e.OfferWithRequestID(testDevice(), []string{first}, invalid); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid request accepted", err)
		}
	}
	if len(e.Transfers("")) != 1 {
		t.Fatal("rejected changes created work")
	}
}

func TestFailedOfferCommitDoesNotReserveItsRequestID(t *testing.T) {
	e, _, _ := testEngine(t)
	source := offerSource(t)
	const key = "cccccccccccccccccccccccccccccccc"
	if _, err := e.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.OfferWithRequestID(testDevice(), []string{source}, key); err == nil {
		t.Fatal("read-only write claimed success")
	}
	if len(e.Transfers("")) != 0 {
		t.Fatal("failed commit survived in memory")
	}
	if _, err := e.db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.OfferWithRequestID(testDevice(), []string{source}, key); err != nil {
		t.Fatal("failed request could not retry", err)
	}
	if len(e.Transfers("")) != 1 {
		t.Fatal("retry did not commit exactly once")
	}
	// Old clients retain their distinct-submission behavior without a key.
	first, err := e.OfferWithRequestID(testDevice(), []string{source}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.OfferWithRequestID(testDevice(), []string{source}, "")
	if err != nil || first.ID == second.ID {
		t.Fatal("legacy offers collapsed", err)
	}
}

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestSummaryBoundsTenThousandHistoryRecordsAndManifestEntries(t *testing.T) {
	e, _, _ := testEngine(t)
	files := make([]model.File, 10000)
	for i := range files {
		files[i] = model.File{ID: fmt.Sprint(i), Path: fmt.Sprintf("folder/file-%05d", i), Completed: true, DuplicateCandidates: []model.DuplicateCandidate{{ID: "private", Path: "private/path"}}}
	}
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("t-%05d", i)
		stamp := time.Date(2026, 1, 1, 0, 0, i, 0, time.UTC).Format(time.RFC3339Nano)
		e.records[id] = &record{Transfer: model.Transfer{ID: id, DeviceID: "peer", State: "completed", Direction: "receive", CreatedAt: stamp, UpdatedAt: stamp, Files: files}}
	}
	items, active := e.StatusSummaries()
	if len(items) != 3 || active != 0 {
		t.Fatal(len(items), active)
	}
	for _, item := range items {
		if !item.Summary || item.FileCount != 10000 || len(item.Files) != 3 || len(item.Files[0].DuplicateCandidates) != 0 || !item.HasContent {
			t.Fatal(item)
		}
	}
	data, _ := json.Marshal(items)
	if len(data) > 16000 {
		t.Fatal("summary copied retained manifests", len(data))
	}
	page, err := e.SummaryPage(SummaryQuery{})
	if err != nil || len(page.Transfers) != 50 || page.Next == "" {
		t.Fatal(page, err)
	}
	data, _ = json.Marshal(page)
	if len(data) > 150000 {
		t.Fatal("history page unbounded", len(data))
	}
	seen := map[string]bool{}
	for {
		for _, item := range page.Transfers {
			if seen[item.ID] {
				t.Fatal("duplicate page item", item.ID)
			}
			seen[item.ID] = true
		}
		if page.Next == "" {
			break
		}
		page, err = e.SummaryPage(SummaryQuery{Cursor: page.Next})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 10000 {
		t.Fatal("missing history records", len(seen))
	}
	if _, err = e.SummaryPage(SummaryQuery{Cursor: "invalid"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	filePage, err := e.FilePage("t-00000", 9990, 50)
	if err != nil || len(filePage.Files) != 10 || filePage.Next != -1 || filePage.Total != 10000 {
		t.Fatal(filePage, err)
	}
	filePage.Files[0].DuplicateCandidates[0].Path = "changed"
	if files[9990].DuplicateCandidates[0].Path != "private/path" {
		t.Fatal("page aliases engine metadata")
	}
}

func TestCancelledOfferDoesNotCreateReceiptOrWaitForOfferLock(t *testing.T) {
	e, _, _ := testEngine(t)
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(path, []byte("source stays intact"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.offerMu.Lock()
	_, err := e.OfferWithRequestIDContext(ctx, testDevice(), []string{path}, "0123456789abcdef0123456789abcdef")
	e.offerMu.Unlock()
	if !errors.Is(err, context.Canceled) || len(e.Transfers("")) != 0 {
		t.Fatal(err)
	}
	tr, err := e.OfferWithRequestIDContext(context.Background(), testDevice(), []string{path}, "0123456789abcdef0123456789abcdef")
	if err != nil || tr.ID == "" {
		t.Fatal(tr, err)
	}
}

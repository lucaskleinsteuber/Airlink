package engine

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestLargeManifestCheckpointWritesOnlyOneFile(t *testing.T) {
	base := t.TempDir()
	e, err := New(filepath.Join(base, "state"), filepath.Join(base, "received"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	files := make([]model.File, 10000)
	for i := range files {
		files[i] = model.File{ID: fmt.Sprintf("f%d", i), Path: fmt.Sprintf("file-%05d.bin", i), Size: 8}
	}
	d := testDevice()
	d.AutoAccept = false
	tr, err := e.CreateIncoming(d, files)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Accept(tr.ID); err != nil {
		t.Fatal(err)
	}
	// A hot path must never delete/reinsert the other 9,999 checkpoints.
	if _, err = e.db.Exec("CREATE TRIGGER forbid_bulk BEFORE DELETE ON transfer_files BEGIN SELECT RAISE(FAIL,'unexpected manifest rewrite'); END;"); err != nil {
		t.Fatal(err)
	}
	var before, after int
	e.db.QueryRow("SELECT total_changes()").Scan(&before)
	data := []byte("12345678")
	if n, err := e.WriteChunk(context.Background(), tr.ID, files[0].ID, 0, digest(data), bytes.NewReader(data)); err != nil || n != 8 {
		t.Fatal(n, err)
	}
	e.db.QueryRow("SELECT total_changes()").Scan(&after)
	if after-before != 3 {
		t.Fatalf("one receipt, file and header required; got %d writes", after-before)
	}
	var header []byte
	if err = e.db.QueryRow("SELECT document FROM transfers WHERE id=?", tr.ID).Scan(&header); err != nil {
		t.Fatal(err)
	}
	if len(header) > 4096 {
		t.Fatalf("checkpoint header grew with manifest: %d", len(header))
	}
	if err = json.Unmarshal(header, &record{}); err == nil {
		t.Fatal("old readers could silently accept a partial manifest")
	}
	if err = e.CompleteFile(context.Background(), tr.ID, files[0].ID, digest(data)); err != nil {
		t.Fatal(err)
	}
	// Reader bounds, including the last file, are independent of manifest size.
	one, err := e.FileSnapshot(tr.ID, files[9999].ID)
	if err != nil || len(one.Files) != 1 || one.Files[0].ID != files[9999].ID {
		t.Fatal(one.ID, err)
	}
}

func TestFailedCheckpointAndReplayPreserveDurableOffsets(t *testing.T) {
	base := t.TempDir()
	state := filepath.Join(base, "state")
	received := filepath.Join(base, "received")
	e, err := New(state, received)
	if err != nil {
		t.Fatal(err)
	}
	d := testDevice()
	d.AutoAccept = true
	data := []byte("one durable block")
	tr, err := e.CreateIncoming(d, []model.File{{ID: "file", Path: "result.bin", Size: int64(len(data))}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.db.Exec("CREATE TRIGGER fail_checkpoint BEFORE UPDATE ON transfer_files BEGIN SELECT RAISE(FAIL,'simulated disk full'); END;")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "file", 0, digest(data), bytes.NewReader(data)); err == nil {
		t.Fatal("failed checkpoint acknowledged")
	}
	current, _ := e.Get(tr.ID)
	if current.CompletedBytes != 0 || current.Files[0].Received != 0 {
		t.Fatal("failed transaction advanced memory")
	}
	var count int
	e.db.QueryRow("SELECT count(*) FROM chunks").Scan(&count)
	if count != 0 {
		t.Fatal("receipt survived rolled-back checkpoint")
	}
	e.db.Exec("DROP TRIGGER fail_checkpoint")
	e.Close()
	e, err = New(state, received)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if n, err := e.WriteChunk(context.Background(), tr.ID, "file", 0, digest(data), bytes.NewReader(data)); err != nil || n != int64(len(data)) {
			t.Fatal(n, err)
		}
	}
	e.db.QueryRow("SELECT count(*) FROM chunks").Scan(&count)
	if count != 1 {
		t.Fatal("replay duplicated receipts", count)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "file", digest(data)); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(received, "result.bin"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("published bytes differ", err)
	}
}

func TestLegacyMigrationIsAtomicAndPreservesFileMetadata(t *testing.T) {
	base := t.TempDir()
	state := filepath.Join(base, "state")
	received := filepath.Join(base, "received")
	e, err := New(state, received)
	if err != nil {
		t.Fatal(err)
	}
	e.Close()
	db, err := sql.Open("sqlite", filepath.Join(state, "transfers.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := record{Transfer: model.Transfer{ID: "legacy", Direction: "send", State: "paused", Files: []model.File{{ID: "first", Path: "a.bin", Size: 100, Received: 8, SHA256: digest([]byte("source"))}, {ID: "empty", Path: "folder", Directory: true, Completed: true}}, CompletedBytes: 8}, Sources: map[string]source{"first": {Path: filepath.Join(base, "selected.bin"), Size: 100, Identity: "kept-identity"}}, OfferRequestID: "0123456789abcdef0123456789abcdef", OfferInputHash: "kept-input", LegacyVerified: map[string]string{"first": "kept-verification"}}
	legacy, _ := json.Marshal(&r)
	if _, err = db.Exec("INSERT INTO transfers VALUES(?,?)", r.Transfer.ID, legacy); err != nil {
		t.Fatal(err)
	}
	db.Exec("CREATE TRIGGER refuse_migration BEFORE INSERT ON transfer_files BEGIN SELECT RAISE(FAIL,'simulated disk full'); END;")
	if bad, err := New(state, received); err == nil {
		bad.Close()
		t.Fatal("failed migration accepted")
	}
	var kept []byte
	db.QueryRow("SELECT document FROM transfers WHERE id='legacy'").Scan(&kept)
	if !bytes.Equal(kept, legacy) {
		t.Fatal("failed migration changed legacy metadata")
	}
	db.Exec("DROP TRIGGER refuse_migration")
	for run := 0; run < 2; run++ {
		e, err = New(state, received)
		if err != nil {
			t.Fatal(err)
		}
		got := e.records["legacy"]
		if len(got.Transfer.Files) != 2 || got.Transfer.Files[0].Received != 8 || got.Sources["first"] != r.Sources["first"] || got.LegacyVerified["first"] != "kept-verification" || got.OfferRequestID != r.OfferRequestID {
			t.Fatal("migration lost metadata")
		}
		if got.Transfer.UpdatedAt != r.Transfer.UpdatedAt {
			t.Fatal("opening history changed its activity timestamp")
		}
		e.Close()
	}
	db.Exec("DELETE FROM transfer_files WHERE transfer_id='legacy' AND file_id='first'")
	if bad, err := New(state, received); err == nil {
		bad.Close()
		t.Fatal("incomplete checkpoint manifest accepted")
	}
}

func TestRetryPreservesSourceIdentityAndRejectsUnsafeFailures(t *testing.T) {
	base := t.TempDir()
	e, err := New(filepath.Join(base, "state"), filepath.Join(base, "received"))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	path := filepath.Join(base, "original.bin")
	os.WriteFile(path, []byte("original bytes"), 0600)
	tr, err := e.Offer(testDevice(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	before := e.records[tr.ID].Sources[tr.Files[0].ID]
	if err = e.Fail(tr.ID, "storage_full: Receiving disk is full"); err != nil {
		t.Fatal(err)
	}
	current, _ := e.Get(tr.ID)
	if !current.Retryable {
		t.Fatal("recoverable sender not retryable")
	}
	if err = e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	if e.records[tr.ID].Sources[tr.Files[0].ID] != before {
		t.Fatal("retry changed chosen source identity")
	}
	if err = e.Fail(tr.ID, "file integrity check failed"); err != nil {
		t.Fatal(err)
	}
	if err = e.Resume(tr.ID); err == nil {
		t.Fatal("integrity failure resumed silently")
	}
}

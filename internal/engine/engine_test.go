package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func testDevice() model.Device {
	return model.Device{ID: "device-a", Name: "Test Mac", CanSend: true, CanReceive: true, AutoAccept: true}
}
func testEngine(t *testing.T) (*Engine, string, string) {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	receive := filepath.Join(dir, "receive")
	e, err := New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	return e, data, receive
}
func incoming(t *testing.T, e *Engine, name string, b []byte) model.Transfer {
	t.Helper()
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "file-a", Path: name, Size: int64(len(b))}})
	if err != nil {
		t.Fatal(err)
	}
	return tr
}
func putAll(t *testing.T, e *Engine, tr model.Transfer, b []byte) {
	t.Helper()
	for i, offset := int64(0), int64(0); offset < int64(len(b)); i++ {
		end := min(offset+model.ChunkSize, int64(len(b)))
		chunk := b[offset:end]
		n, err := e.WriteChunk(context.Background(), tr.ID, "file-a", i, digest(chunk), bytes.NewReader(chunk))
		if err != nil {
			t.Fatal(err)
		}
		if n != end {
			t.Fatalf("received %d want %d", n, end)
		}
		offset = end
	}
}

func TestDurableResumeDuplicateAndFinalize(t *testing.T) {
	e, data, _ := testEngine(t)
	b := bytes.Repeat([]byte("abcdefgh"), int(model.ChunkSize/8)+19)
	tr := incoming(t, e, "folder/video.bin", b)
	first := b[:model.ChunkSize]
	if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(first), bytes.NewReader(first)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tr.Destination, "folder/video.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified destination visible", err)
	}
	if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(first), bytes.NewReader(first)); err != nil {
		t.Fatal("duplicate", err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err := New(data, filepath.Dir(tr.Destination))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	current, _ := e.Get(tr.ID)
	if current.State != "reconnecting" || current.Files[0].Received != model.ChunkSize {
		t.Fatalf("restart lost checkpoint: %+v", current)
	}
	if err = e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	tail := b[model.ChunkSize:]
	if _, err = e.WriteChunk(context.Background(), tr.ID, "file-a", 1, digest(tail), bytes.NewReader(tail)); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "file-a", digest(b)); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "file-a", digest(b)); err != nil {
		t.Fatal("idempotent completion", err)
	}
	got, err := os.ReadFile(filepath.Join(tr.Destination, "folder/video.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, b) {
		t.Fatal("content mismatch")
	}
	current, _ = e.Get(tr.ID)
	if current.State != "completed" || current.CompletedBytes != int64(len(b)) {
		t.Fatalf("bad completed state %+v", current)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(first), bytes.NewReader(first)); err != nil {
		t.Fatal("completed duplicate must be idempotent", err)
	}
	if len(e.Transfers("other-device")) != 0 {
		t.Fatal("device history filter leaked")
	}
}

func TestCorruptionPauseAndUnconfirmedBlocks(t *testing.T) {
	e, _, _ := testEngine(t)
	b := []byte("a small but valuable original")
	tr := incoming(t, e, "file.txt", b)
	if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest([]byte("wrong")), bytes.NewReader(b)); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("expected integrity error got %v", err)
	}
	if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(b), bytes.NewReader(append(b, 'x'))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversize chunk: %v", err)
	}
	current, _ := e.Get(tr.ID)
	if current.CompletedBytes != 0 {
		t.Fatal("bad block was acknowledged")
	}
	if err := e.Pause(tr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(b), bytes.NewReader(b)); !errors.Is(err, ErrConflict) {
		t.Fatalf("paused upload accepted: %v", err)
	}
	if err := e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	putAll(t, e, tr, b)
	if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest([]byte("wrong file"))); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("bad final hash accepted %v", err)
	}
	if _, err := os.Stat(filepath.Join(tr.Destination, "file.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("corrupt final visible")
	}
	if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest(b)); err != nil {
		t.Fatal(err)
	}
}

func TestQueueHasTwoActiveFiles(t *testing.T) {
	e, _, _ := testEngine(t)
	one := incoming(t, e, "one", []byte("1"))
	two := incoming(t, e, "two", []byte("2"))
	three := incoming(t, e, "three", []byte("3"))
	if three.State != "queued" {
		t.Fatal("third transfer must queue", three.State)
	}
	if err := e.Pause(one.ID); err != nil {
		t.Fatal(err)
	}
	current, _ := e.Get(three.ID)
	if current.State != "running" {
		t.Fatal("queue did not advance")
	}
	if err := e.Resume(one.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = e.Get(one.ID)
	if current.State != "queued" {
		t.Fatal("resumed transfer bypassed slots")
	}
	if err := e.Cancel(two.ID); err != nil {
		t.Fatal(err)
	}
	current, _ = e.Get(one.ID)
	if current.State != "running" {
		t.Fatal("cancel did not release slot")
	}
}

func TestDuplexQueuesCannotDeadlockBehindReceivingSlots(t *testing.T) {
	e, _, _ := testEngine(t)
	// Model both peers accepting two incoming offers before starting their
	// corresponding outgoing streams: each side must still get send slots.
	incomingOne := incoming(t, e, "one", []byte("1"))
	incoming(t, e, "two", []byte("2"))
	incomingThree := incoming(t, e, "three", []byte("3"))
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("outgoing"), 0600); err != nil {
		t.Fatal(err)
	}
	outgoing := make([]model.Transfer, 3)
	for i := range outgoing {
		var err error
		outgoing[i], err = e.Offer(testDevice(), []string{source})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.Resume(outgoing[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	active := map[string]int{}
	for _, tr := range e.Transfers("") {
		if tr.State == "running" || tr.State == "verifying" {
			active[tr.Direction]++
		}
	}
	if active["receive"] != 2 || active["send"] != 2 {
		t.Fatalf("duplex pool blocked: %v", active)
	}
	thirdOut, _ := e.Get(outgoing[2].ID)
	if incomingThree.State != "queued" || thirdOut.State != "queued" {
		t.Fatal("per-direction limit bypassed")
	}
	if err := e.Pause(incomingOne.ID); err != nil {
		t.Fatal(err)
	}
	thirdIn, _ := e.Get(incomingThree.ID)
	thirdOut, _ = e.Get(outgoing[2].ID)
	if thirdIn.State != "running" || thirdOut.State != "queued" {
		t.Fatal("receive slot released wrong queue")
	}
	if err := e.Pause(outgoing[0].ID); err != nil {
		t.Fatal(err)
	}
	thirdOut, _ = e.Get(outgoing[2].ID)
	if thirdOut.State != "running" {
		t.Fatal("send queue did not advance independently")
	}
}

func TestRejectUnsafePathsAndNames(t *testing.T) {
	e, _, _ := testEngine(t)
	for _, p := range []string{"../escape", "/absolute", "a/../../escape", "a\\..\\escape", "C:/escape", "a//b", "a/./b", "CON.txt", "a/..", "a.", ".airlink-partials/data", "nul", "foo\x00bar"} {
		t.Run(p, func(t *testing.T) {
			if _, err := e.CreateIncoming(testDevice(), []model.File{{ID: "f", Path: p, Size: 1}}); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe path %q accepted: %v", p, err)
			}
		})
	}
	for _, files := range [][]model.File{{{ID: "a", Path: "File"}, {ID: "b", Path: "file"}}, {{ID: "a", Path: "parent"}, {ID: "b", Path: "parent/child"}}, {{ID: "a", Path: "one"}, {ID: "a", Path: "two"}}} {
		if _, err := e.CreateIncoming(testDevice(), files); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid manifest accepted", err)
		}
	}
}

func TestDestinationLinkCannotEscapeAndCollisionNeverOverwrites(t *testing.T) {
	e, _, _ := testEngine(t)
	outside := t.TempDir()
	b := []byte("payload")
	tr := incoming(t, e, "nested/file", b)
	if err := os.Remove(filepath.Join(tr.Destination, "nested")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tr.Destination, "nested")); err != nil {
		t.Skip(err)
	}
	putAll(t, e, tr, b)
	if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest(b)); err == nil {
		t.Fatal("escaped destination")
	}
	if _, err := os.Stat(filepath.Join(outside, "file")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("wrote outside destination")
	}
	other := incoming(t, e, "existing", b)
	original := []byte("user data")
	if err := os.WriteFile(filepath.Join(other.Destination, "existing"), original, 0600); err != nil {
		t.Fatal(err)
	}
	putAll(t, e, other, b)
	if err := e.CompleteFile(context.Background(), other.ID, "file-a", digest(b)); err != nil {
		t.Fatal("collision could not be renamed", err)
	}
	completed, _ := e.Get(other.ID)
	if completed.Files[0].DestinationPath != "existing (2)" {
		t.Fatal("unexpected collision name", completed.Files[0].DestinationPath)
	}
	got, _ := os.ReadFile(filepath.Join(other.Destination, "existing"))
	if !bytes.Equal(got, original) {
		t.Fatal("original changed")
	}
}

func TestOfferReferencesSourcesAndRejectsChanges(t *testing.T) {
	e, _, _ := testEngine(t)
	folder := t.TempDir()
	p := filepath.Join(folder, "original.txt")
	b := []byte("original bytes")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	tr, err := e.Offer(testDevice(), []string{p})
	if err != nil {
		t.Fatal(err)
	}
	f := tr.Files[0]
	in, _, err := e.OpenDownload(tr.ID, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(in)
	in.Close()
	if !bytes.Equal(got, b) {
		t.Fatal("offered content mismatch")
	}
	hash, err := e.SourceSHA256(context.Background(), tr.ID, f.ID)
	if err != nil || hash != digest(b) {
		t.Fatal(hash, err)
	}
	if err = e.MarkOutgoingProgress(tr.ID, f.ID, int64(len(b)), hash); err != nil {
		t.Fatal(err)
	}
	current, _ := e.Get(tr.ID)
	if current.Files[0].Completed {
		t.Fatal("progress marked verified completion")
	}
	if err = e.CompleteOutgoingFile(tr.ID, f.ID, hash); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteOutgoingFile(tr.ID, f.ID, hash); err != nil {
		t.Fatal("completion retry", err)
	}
	changed, err := e.Offer(testDevice(), []string{p})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, []byte("different source content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = e.OpenDownload(changed.ID, changed.Files[0].ID); !errors.Is(err, ErrConflict) {
		t.Fatal("modified source accepted", err)
	}
	link := filepath.Join(folder, "link")
	if err = os.Symlink(p, link); err == nil {
		if _, err = e.Offer(testDevice(), []string{link}); !errors.Is(err, ErrInvalid) {
			t.Fatal("symlink offered", err)
		}
	}
}

func TestRecoverUnacknowledgedTailAndPublishedCompletion(t *testing.T) {
	e, data, receive := testEngine(t)
	b := []byte("resilient bytes")
	tr := incoming(t, e, "file", b)
	putAll(t, e, tr, b)
	part := partialPathForTest(e, tr.ID, "file-a")
	f, err := os.OpenFile(part, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("unacknowledged"))
	f.Close()
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(part)
	if info.Size() != int64(len(b)) {
		t.Fatalf("unacknowledged tail retained: %d", info.Size())
	}
	if err = e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate the publication-to-DB crash window with the expected checksum
	// already durable but the completion record not yet committed.
	e.mu.Lock()
	r := e.records[tr.ID]
	r.Transfer.State = "verifying"
	r.Transfer.Files[0].SHA256 = digest(b)
	err = e.saveLocked(r)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Link(part, filepath.Join(tr.Destination, "file")); err != nil {
		t.Fatal(err)
	}
	os.Remove(part)
	e.Close()
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	current, _ := e.Get(tr.ID)
	if current.State != "completed" || !current.Files[0].Completed {
		t.Fatalf("published file not reconciled: %+v", current)
	}
}

func TestEmptyFileAndDirectory(t *testing.T) {
	e, _, _ := testEngine(t)
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "dir", Path: "empty", Directory: true}, {ID: "file-a", Path: "empty/zero", Size: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "file-a", digest(nil)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(tr.Destination, "empty/zero"))
	if err != nil || info.Size() != 0 {
		t.Fatal(info, err)
	}
}

func TestAtomicRenameFallbackRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.MkdirAll("staging", 0700); err != nil {
		t.Fatal(err)
	}
	if err = root.MkdirAll("final", 0700); err != nil {
		t.Fatal(err)
	}
	if err = root.WriteFile("staging/a", []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = renameNoReplace(root, "staging/a", "final/a"); err != nil {
		t.Fatal("atomic fallback", err)
	}
	if err = root.WriteFile("staging/b", []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = renameNoReplace(root, "staging/b", "final/a"); err == nil {
		t.Fatal("fallback replaced existing user file")
	}
	b, err := root.ReadFile("final/a")
	if err != nil || string(b) != "first" {
		t.Fatal("original overwritten", string(b), err)
	}
	if _, err = root.Stat("staging/b"); err != nil {
		t.Fatal("failed rename discarded source", err)
	}
}

func TestDirectoryAndMultiFileResumeOrder(t *testing.T) {
	e, data, receive := testEngine(t)
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "dir", Path: "folder", Directory: true}, {ID: "a", Path: "folder/a", Size: 1}, {ID: "b", Path: "folder/b", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "b", 0, digest([]byte("b")), bytes.NewReader([]byte("b"))); !errors.Is(err, ErrConflict) {
		t.Fatal("second active file bypassed queue", err)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "a", 0, digest([]byte("a")), bytes.NewReader([]byte("a"))); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "a", digest([]byte("a"))); err != nil {
		t.Fatal(err)
	}
	e.Close()
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "b", 0, digest([]byte("b")), bytes.NewReader([]byte("b"))); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "b", digest([]byte("b"))); err != nil {
		t.Fatal(err)
	}
	current, _ := e.Get(tr.ID)
	if current.State != "completed" || current.CompletedBytes != 2 {
		t.Fatal("multi-file completion failed", current)
	}
}

func TestPauseCancelsVerificationWithoutStalePublication(t *testing.T) {
	e, _, _ := testEngine(t)
	// Sparse content is a cancellation fixture, not a large-transfer proof.
	const size int64 = 256 * 1024 * 1024
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "large", Path: "large.bin", Size: size}})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := os.OpenFile(partialPathForTest(e, tr.ID, "large"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = partial.Truncate(size); err != nil {
		t.Fatal(err)
	}
	partial.Close()
	e.mu.Lock()
	r := e.records[tr.ID]
	r.Transfer.Files[0].Received = size
	r.Transfer.CompletedBytes = size
	err = e.saveLocked(r)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- e.CompleteFile(context.Background(), tr.ID, "large", digest([]byte("expected hash"))) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, _ := e.Get(tr.ID)
		if current.State == "verifying" {
			break
		}
		select {
		case early := <-done:
			t.Fatalf("verification ended before pause: %v", early)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("verification did not start")
		}
		time.Sleep(time.Millisecond)
	}
	e.mu.Lock()
	oldOp := e.verifications[tr.ID]
	e.mu.Unlock()
	if oldOp == nil {
		t.Fatal("verification operation missing")
	}
	if err = e.Pause(tr.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(oldOp.ctx.Err(), context.Canceled) {
		t.Fatal("pause did not cancel ongoing hash read")
	}
	if err = e.Resume(tr.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("cancelled verifier returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("paused verifier continued reading full file")
	}
	current, _ := e.Get(tr.ID)
	if current.State != "running" || current.Files[0].Completed {
		t.Fatalf("stale verifier altered resumed state: %+v", current)
	}
	if _, err = os.Stat(filepath.Join(tr.Destination, "large.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale verifier published file")
	}
	// An old operation's late error may not reset another verification's state.
	nextCtx, nextCancel := context.WithCancel(context.Background())
	defer nextCancel()
	next := &verificationOperation{ctx: nextCtx, cancel: nextCancel}
	e.mu.Lock()
	e.verifications[tr.ID] = next
	e.records[tr.ID].Transfer.State = "verifying"
	e.mu.Unlock()
	if err = e.verificationError(tr.ID, oldOp, ErrIntegrity); !errors.Is(err, ErrConflict) {
		t.Fatal("stale verification error accepted", err)
	}
	current, _ = e.Get(tr.ID)
	if current.State != "verifying" {
		t.Fatal("stale error reset current verification")
	}
}

func partialPathForTest(e *Engine, transferID, fileID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return filepath.Join(e.records[transferID].ReceiveRoot, partialName(fileID))
}

package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func awaitCancelledPartialRemoval(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cancelled partial was not reclaimed", path)
}

func TestCancelledDirectReceiveReclaimsItsKnownPartial(t *testing.T) {
	e, _, _ := testEngine(t)
	body := bytes.Repeat([]byte{0x53}, 300*1024)
	transfer := incoming(t, e, "unfinished.bin", body)
	putAll(t, e, transfer, body)
	partial := partialPathForTest(e, transfer.ID, "file-a")
	if err := e.Cancel(transfer.ID); err != nil {
		t.Fatal(err)
	}
	awaitCancelledPartialRemoval(t, partial)
	e.autoWG.Wait()
	if _, err := os.Stat(filepath.Dir(filepath.Dir(partial))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty private transfer stage remains", err)
	}
	if _, err := os.Stat(transfer.Destination); err != nil {
		t.Fatal("chosen public directory was removed", err)
	}
	current, _ := e.Get(transfer.ID)
	if current.State != "cancelled" {
		t.Fatal(current.State)
	}
}

func TestCancelledDirectReceivePreservesPublishedAndUnknownFiles(t *testing.T) {
	e, _, _ := testEngine(t)
	transfer, err := e.CreateIncoming(testDevice(), []model.File{{ID: "done", Path: "done.txt", Size: 4}, {ID: "pending", Path: "pending.txt", Size: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), transfer.ID, "done", 0, digest([]byte("done")), bytes.NewReader([]byte("done"))); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), transfer.ID, "done", digest([]byte("done"))); err != nil {
		t.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), transfer.ID, "pending", 0, digest([]byte("pending")), bytes.NewReader([]byte("pending"))); err != nil {
		t.Fatal(err)
	}
	partial := partialPathForTest(e, transfer.ID, "pending")
	unknown := filepath.Join(filepath.Dir(partial), "unknown.keep")
	if err = os.WriteFile(unknown, []byte("preserve this"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.Cancel(transfer.ID); err != nil {
		t.Fatal(err)
	}
	awaitCancelledPartialRemoval(t, partial)
	e.autoWG.Wait()
	for path, expected := range map[string]string{filepath.Join(transfer.Destination, "done.txt"): "done", unknown: "preserve this"} {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != expected {
			t.Fatal("cleanup removed published or unknown content", path, err)
		}
	}
}

type heldCancelReader struct {
	ready   chan struct{}
	release chan struct{}
	sent    bool
}

func (r *heldCancelReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	close(r.ready)
	<-r.release
	p[0] = 'x'
	return 1, nil
}

func TestCancelledCleanupWaitsForAnActiveWriterWithoutBlockingCancel(t *testing.T) {
	e, _, _ := testEngine(t)
	first := bytes.Repeat([]byte{7}, int(model.ChunkSize))
	transfer := incoming(t, e, "active.bin", append(first, 'x'))
	if _, err := e.WriteChunk(context.Background(), transfer.ID, "file-a", 0, digest(first), bytes.NewReader(first)); err != nil {
		t.Fatal(err)
	}
	partial := partialPathForTest(e, transfer.ID, "file-a")
	reader := &heldCancelReader{ready: make(chan struct{}), release: make(chan struct{})}
	writeDone := make(chan error, 1)
	go func() {
		_, err := e.WriteChunk(context.Background(), transfer.ID, "file-a", 1, digest([]byte("x")), reader)
		writeDone <- err
	}()
	select {
	case <-reader.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not enter its body read")
	}
	if err := e.Cancel(transfer.ID); err != nil {
		close(reader.release)
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(partial); err != nil {
		close(reader.release)
		t.Fatal("cleanup removed a file still owned by an active writer", err)
	}
	close(reader.release)
	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("cancelled writer claimed success")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not leave after cancellation")
	}
	awaitCancelledPartialRemoval(t, partial)
}

func TestCancelledCleanupRetriesAtStartupAndCannotHoldCloseOnAFileLock(t *testing.T) {
	e, data, receive := testEngine(t)
	body := []byte("durably confirmed bytes")
	transfer := incoming(t, e, "restart.bin", body)
	putAll(t, e, transfer, body)
	partial := partialPathForTest(e, transfer.ID, "file-a")
	lock, err := e.fileLock(transfer.ID, "file-a")
	if err != nil {
		t.Fatal(err)
	}
	lock.Lock()
	if err = e.Cancel(transfer.ID); err != nil {
		lock.Unlock()
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- e.Close() }()
	select {
	case err = <-closed:
		if err != nil {
			lock.Unlock()
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		lock.Unlock()
		t.Fatal("cleanup kept Close waiting on a held file lock")
	}
	lock.Unlock()
	if _, err = os.Stat(partial); err != nil {
		t.Fatal("fixture no longer exercises pending startup cleanup", err)
	}
	next, err := New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	awaitCancelledPartialRemoval(t, partial)
	current, _ := next.Get(transfer.ID)
	if current.State != "cancelled" {
		t.Fatal("startup revived cancelled work", current.State)
	}
}

func TestCancellationDoesNotCleanLegacyReceivesOrFollowPrivateLinks(t *testing.T) {
	e, _, receive := testEngine(t)
	device := testDevice()
	device.AutoAccept = false
	transfer, err := e.CreateIncoming(device, []model.File{{ID: "file-a", Path: "old.txt", Size: 3}})
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(receive, "legacy-transfer")
	e.mu.Lock()
	r := e.records[transfer.ID]
	r.DirectReceive = false
	r.ReceiveRoot = legacy
	r.Transfer.Destination = legacy
	err = e.saveLocked(r)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Accept(transfer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), transfer.ID, "file-a", 0, digest([]byte("old")), bytes.NewReader([]byte("old"))); err != nil {
		t.Fatal(err)
	}
	if err = e.Cancel(transfer.ID); err != nil {
		t.Fatal(err)
	}
	e.autoWG.Wait()
	if _, err = os.Stat(filepath.Join(legacy, partialName("file-a"))); err != nil {
		t.Fatal("legacy partial was cleaned implicitly", err)
	}
	other, _, _ := testEngine(t)
	linked := incoming(t, other, "link.bin", []byte("x"))
	part := partialPathForTest(other, linked.ID, "file-a")
	unknownRoot := t.TempDir()
	unknown := filepath.Join(unknownRoot, filepath.Base(part))
	if err = os.WriteFile(unknown, []byte("not transfer data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Dir(part)); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(unknownRoot, filepath.Dir(part)); err != nil {
		t.Skip(err)
	}
	if err = other.Cancel(linked.ID); err != nil {
		t.Fatal(err)
	}
	other.autoWG.Wait()
	if contents, err := os.ReadFile(unknown); err != nil || string(contents) != "not transfer data" {
		t.Fatal("cleanup followed a private directory link", err)
	}
}

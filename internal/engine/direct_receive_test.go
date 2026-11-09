package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestDirectReceivePublishesIntoChosenFolderAndCleansOnlyItsStage(t *testing.T) {
	e, _, receive := testEngine(t)
	payload := []byte("a directly visible received file")
	transfer := incoming(t, e, "photo.jpg", payload)
	if transfer.Destination != e.receiveDir {
		t.Fatal("receiver added a wrapper", transfer.Destination, receive)
	}
	partial := partialPathForTest(e, transfer.ID, "file-a")
	if filepath.Dir(filepath.Dir(partial)) == transfer.Destination {
		t.Fatal("partial stage is not isolated per transfer")
	}
	putAll(t, e, transfer, payload)
	if _, err := os.Stat(filepath.Join(transfer.Destination, "photo.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified file was visible", err)
	}
	if err := e.CompleteFile(context.Background(), transfer.ID, "file-a", digest(payload)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(transfer.Destination, "photo.jpg"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("published file missing", err)
	}
	result, err := e.ReceivedItems(transfer.ID)
	if err != nil || len(result.Items) != 1 || result.Items[0].Path != filepath.Join(transfer.Destination, "photo.jpg") {
		t.Fatal("result points to a stage or wrapper", result, err)
	}
	entries, err := os.ReadDir(transfer.Destination)
	if err != nil || len(entries) != 1 || entries[0].Name() != "photo.jpg" {
		t.Fatal("empty internal directories were left behind", entries, err)
	}
}

func TestDirectReceiveReservesCollisionNamesAcrossPendingTransfers(t *testing.T) {
	e, _, _ := testEngine(t)
	root := e.receiveDir
	original := []byte("existing user content")
	if err := os.WriteFile(filepath.Join(root, "Photo.JPG"), original, 0600); err != nil {
		t.Fatal(err)
	}
	device := testDevice()
	device.AutoAccept = false
	first, err := e.CreateIncoming(device, []model.File{{ID: "file-a", Path: "photo.jpg", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.CreateIncoming(device, []model.File{{ID: "file-a", Path: "photo.jpg", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Files[0].DestinationPath != "photo (2).jpg" || second.Files[0].DestinationPath != "photo (3).jpg" {
		t.Fatal("pending names collide", first.Files, second.Files)
	}
	for i, transfer := range []model.Transfer{first, second} {
		if err = e.Accept(transfer.ID); err != nil {
			t.Fatal(err)
		}
		payload := []byte{byte('a' + i)}
		putAll(t, e, transfer, payload)
		if err = e.CompleteFile(context.Background(), transfer.ID, "file-a", digest(payload)); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(filepath.Join(root, "Photo.JPG"))
	if !bytes.Equal(got, original) {
		t.Fatal("existing case variant was changed")
	}
	for _, name := range []string{"photo (2).jpg", "photo (3).jpg"} {
		if _, err = os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(name, err)
		}
	}
}

func TestDirectReceiveKeepsFolderHierarchyWithoutMergingExistingFolder(t *testing.T) {
	e, _, _ := testEngine(t)
	root := e.receiveDir
	if err := os.Mkdir(filepath.Join(root, "Album"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Album", "mine.txt"), []byte("mine"), 0600); err != nil {
		t.Fatal(err)
	}
	payload := []byte("incoming picture")
	transfer, err := e.CreateIncoming(testDevice(), []model.File{{ID: "folder", Path: "Album", Directory: true}, {ID: "empty", Path: "Album/Empty", Directory: true}, {ID: "file-a", Path: "Album/picture.jpg", Size: int64(len(payload))}})
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Files[0].DestinationPath != "Album (2)" {
		t.Fatal("folder was merged", transfer.Files)
	}
	putAll(t, e, transfer, payload)
	if err = e.CompleteFile(context.Background(), transfer.ID, "file-a", digest(payload)); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "Album", "mine.txt")); err != nil || string(got) != "mine" {
		t.Fatal("existing folder changed", err)
	}
	if _, err = os.Stat(filepath.Join(root, "Album (2)", "Empty")); err != nil {
		t.Fatal("empty folder was lost", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "Album (2)", "picture.jpg")); err != nil || !bytes.Equal(got, payload) {
		t.Fatal("hierarchy changed", err)
	}
	result, err := e.ReceivedItems(transfer.ID)
	if err != nil || len(result.Items) != 1 || result.Items[0].Path != filepath.Join(root, "Album (2)") {
		t.Fatal("folder result is not the published root", result, err)
	}
}

func TestDirectReceiveSameFileIDsRemainIsolated(t *testing.T) {
	e, _, _ := testEngine(t)
	first := incoming(t, e, "one.txt", []byte("first"))
	second := incoming(t, e, "two.txt", []byte("other"))
	if partialPathForTest(e, first.ID, "file-a") == partialPathForTest(e, second.ID, "file-a") {
		t.Fatal("sender-controlled IDs shared a partial file")
	}
	for i, transfer := range []model.Transfer{first, second} {
		payload := [][]byte{[]byte("first"), []byte("other")}[i]
		putAll(t, e, transfer, payload)
		if err := e.CompleteFile(context.Background(), transfer.ID, "file-a", digest(payload)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(transfer.Destination, transfer.Files[0].Path))
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatal("parallel partial content mixed", err)
		}
	}
}

func TestIndependentReceiversNeverOverwriteOneAnother(t *testing.T) {
	base := t.TempDir()
	receive := filepath.Join(base, "Downloads")
	firstEngine, err := New(filepath.Join(base, "first-state"), receive)
	if err != nil {
		t.Fatal(err)
	}
	defer firstEngine.Close()
	secondEngine, err := New(filepath.Join(base, "second-state"), receive)
	if err != nil {
		t.Fatal(err)
	}
	defer secondEngine.Close()
	first := incoming(t, firstEngine, "same.txt", []byte("first"))
	second := incoming(t, secondEngine, "same.txt", []byte("other"))
	putAll(t, firstEngine, first, []byte("first"))
	putAll(t, secondEngine, second, []byte("other"))
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := firstEngine.CompleteFile(context.Background(), first.ID, "file-a", digest([]byte("first"))); err != nil {
			t.Error(err)
		}
	})
	wg.Go(func() {
		if err := secondEngine.CompleteFile(context.Background(), second.ID, "file-a", digest([]byte("other"))); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	contents := map[string]bool{}
	for _, name := range []string{"same.txt", "same (2).txt"} {
		data, err := os.ReadFile(filepath.Join(receive, name))
		if err != nil {
			t.Fatal(err)
		}
		contents[string(data)] = true
	}
	if !contents["first"] || !contents["other"] || len(contents) != 2 {
		t.Fatal("independent services overwrote a payload", contents)
	}
}

func TestDirectReceiveRecoveryPreservesExternalCollisionAndConfirmedBytes(t *testing.T) {
	e, data, receive := testEngine(t)
	payload := []byte("confirmed transfer content")
	transfer := incoming(t, e, "file.txt", payload)
	putAll(t, e, transfer, payload)
	if err := os.WriteFile(filepath.Join(transfer.Destination, "file.txt"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	current, _ := next.Get(transfer.ID)
	if current.State != "reconnecting" || current.CompletedBytes != int64(len(payload)) || current.Files[0].DestinationPath != "file (2).txt" {
		t.Fatal("recovery lost confirmed bytes or misidentified an external file", current)
	}
	if err = next.Resume(transfer.ID); err != nil {
		t.Fatal(err)
	}
	if err = next.CompleteFile(context.Background(), transfer.ID, "file-a", digest(payload)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(current.Destination, "file.txt")); string(got) != "external" {
		t.Fatal("external collision overwritten")
	}
	if got, _ := os.ReadFile(filepath.Join(current.Destination, "file (2).txt")); !bytes.Equal(got, payload) {
		t.Fatal("resumed file missing")
	}
}

func TestDirectReceiveRejectsReplacedOwnedFolderAndPartialRootLink(t *testing.T) {
	e, _, _ := testEngine(t)
	transfer := incoming(t, e, "Folder/item.txt", []byte("payload"))
	folder := filepath.Join(transfer.Destination, "Folder")
	if err := os.Rename(folder, folder+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	putAll(t, e, transfer, []byte("payload"))
	if err := e.CompleteFile(context.Background(), transfer.ID, "file-a", digest([]byte("payload"))); err == nil {
		t.Fatal("replacement folder was accepted as owned")
	}
	if _, err := os.Stat(filepath.Join(folder, "item.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published into a replaced folder", err)
	}
	other, _, _ := testEngine(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(other.receiveDir, ".airlink-partials")); err != nil {
		t.Skip(err)
	}
	if _, err := other.CreateIncoming(testDevice(), []model.File{{ID: "file-a", Path: "safe.txt", Size: 1}}); err == nil {
		t.Fatal("private stage followed a link")
	}
}

func TestLegacyReceiveRecordKeepsItsHistoricalFolderAndContents(t *testing.T) {
	e, data, receive := testEngine(t)
	device := testDevice()
	device.AutoAccept = false
	transfer, err := e.CreateIncoming(device, []model.File{{ID: "file-a", Path: "legacy.txt", Size: 6}})
	if err != nil {
		t.Fatal(err)
	}
	legacyFolder := filepath.Join(receive, "AirLink-old-transfer")
	e.mu.Lock()
	record := e.records[transfer.ID]
	record.DirectReceive = false
	record.DirectDirectories = nil
	record.ReceiveRoot = legacyFolder
	record.Transfer.Destination = legacyFolder
	err = e.saveLocked(record)
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Accept(transfer.ID); err != nil {
		t.Fatal(err)
	}
	putAll(t, e, transfer, []byte("legacy"))
	if err = e.CompleteFile(context.Background(), transfer.ID, "file-a", digest([]byte("legacy"))); err != nil {
		t.Fatal(err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	result, err := next.ReceivedItems(transfer.ID)
	expected, _ := filepath.EvalSymlinks(filepath.Join(legacyFolder, "legacy.txt"))
	if err != nil || len(result.Items) != 1 || result.Items[0].Path != expected {
		t.Fatal("legacy file was moved or hidden", result, err)
	}
	if _, err := os.Stat(filepath.Join(receive, "legacy.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy data was migrated implicitly", err)
	}
}

func TestIncomingConfigurationFailureKeepsPriorRootAndPolicy(t *testing.T) {
	e, _, _ := testEngine(t)
	previous := e.receiveDir
	newRoot := filepath.Join(t.TempDir(), "chosen")
	wanted := model.Organization{DetectDuplicates: true, Rules: []model.SortRule{}}
	failure := errors.New("settings disk is unavailable")
	if err := e.ConfigureIncoming(newRoot, wanted, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatal("configuration commit error lost", err)
	}
	if e.receiveDir != previous || e.organization.DetectDuplicates {
		t.Fatal("failed settings affected new receives")
	}
	transfer := incoming(t, e, "still-old.txt", []byte("a"))
	if transfer.Destination != previous {
		t.Fatal("new receive used unconfirmed folder", transfer.Destination)
	}
}

func TestDirectReceiveReusedFilenameIndexesTheNewVerifiedContent(t *testing.T) {
	e, data, receive := testEngine(t)
	old := verifiedIncoming(t, e, "photo.jpg", []byte("original old content"))
	if err := os.Remove(filepath.Join(old.Destination, "photo.jpg")); err != nil {
		t.Fatal(err)
	}
	replacement := []byte("a different newly received file")
	current := verifiedIncoming(t, e, "photo.jpg", replacement)
	if current.Files[0].DestinationPath != "photo.jpg" {
		t.Fatal("a freed filename was not reused", current.Files)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	enableDuplicates(t, next)
	third := incoming(t, next, "renamed.jpg", replacement)
	decision, err := next.PrepareFile(context.Background(), third.ID, "file-a", digest(replacement))
	if err != nil || decision != "pending" {
		t.Fatal("new content at a reused direct path was missing from duplicate index", decision, err)
	}
	got, _ := next.Get(third.ID)
	if len(got.Files[0].DuplicateCandidates) != 1 || got.Files[0].DuplicateCandidates[0].SHA256 != digest(replacement) {
		t.Fatal("the candidate retained the old file's content identity", got.Files[0].DuplicateCandidates)
	}
}

package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func completedReceivedManifest(t *testing.T, e *Engine, files []model.File, contents map[string][]byte) model.Transfer {
	t.Helper()
	tr, err := e.CreateIncoming(testDevice(), files)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Directory {
			continue
		}
		body := contents[file.ID]
		if len(body) > 0 {
			if _, err := e.WriteChunk(context.Background(), tr.ID, file.ID, 0, digest(body), bytes.NewReader(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.CompleteFile(context.Background(), tr.ID, file.ID, digest(body)); err != nil {
			t.Fatal(err)
		}
	}
	tr, _ = e.Get(tr.ID)
	if tr.State != "completed" {
		t.Fatal(tr.State)
	}
	return tr
}

func TestReceivedItemsSelectPublishedFileAndOriginalFolderRoot(t *testing.T) {
	e, _, _ := testEngine(t)
	file := verifiedIncoming(t, e, "message.txt", []byte("hello"))
	result, err := e.ReceivedItems(file.ID)
	if err != nil || result.Directory != file.Destination || len(result.Items) != 1 || result.Items[0].Directory || result.Items[0].FileID != "file-a" || result.Items[0].Path != filepath.Join(file.Destination, "message.txt") {
		t.Fatal(result, err)
	}
	folder := completedReceivedManifest(t, e, []model.File{{ID: "folder", Path: "Album", Directory: true}, {ID: "empty", Path: "Album/Empty", Directory: true}, {ID: "photo", Path: "Album/photo.jpg", Size: 1}}, map[string][]byte{"photo": []byte("x")})
	result, err = e.ReceivedItems(folder.ID)
	if err != nil || len(result.Items) != 1 || !result.Items[0].Directory || result.Items[0].FileID != "folder" || result.Items[0].Path != filepath.Join(folder.Destination, "Album") {
		t.Fatal("folder root was replaced by a hidden wrapper or child list", result, err)
	}
	empty := completedReceivedManifest(t, e, []model.File{{ID: "empty-only", Path: "Empty", Directory: true}}, nil)
	result, err = e.ReceivedItems(empty.ID)
	if err != nil || len(result.Items) != 1 || !result.Items[0].Directory {
		t.Fatal("explicit empty directory disappeared", result, err)
	}
}

func TestReceivedItemsUseHistoricalSortedMappingAndOmitMissingEntries(t *testing.T) {
	e, _, receive := testEngine(t)
	if err := e.SetOrganization(model.Organization{SortEnabled: true, Rules: []model.SortRule{{ID: "text", Extensions: []string{"txt"}, Folder: "Documents"}}}); err != nil {
		t.Fatal(err)
	}
	tr := completedReceivedManifest(t, e, []model.File{{ID: "one", Path: "one.txt", Size: 1}, {ID: "two", Path: "two.txt", Size: 1}}, map[string][]byte{"one": []byte("a"), "two": []byte("b")})
	if err := e.SetReceiveDirectory(filepath.Join(receive, "future-only")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tr.Destination, "Documents", "one.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := e.ReceivedItems(tr.ID)
	if err != nil || result.Directory != tr.Destination || len(result.Items) != 1 || result.Items[0].FileID != "two" || result.Items[0].Path != filepath.Join(tr.Destination, "Documents", "two.txt") {
		t.Fatal("reveal ignored actual/historical destination", result, err)
	}
}

func TestReceivedItemsNeverRedirectSkippedPayloadToDuplicate(t *testing.T) {
	e, _, _ := testEngine(t)
	payload := []byte("same content")
	original := verifiedIncoming(t, e, "original.txt", payload)
	enableDuplicates(t, e)
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "folder", Path: "Folder", Directory: true}, {ID: "new", Path: "Folder/renamed.txt", Size: int64(len(payload))}})
	if err != nil {
		t.Fatal(err)
	}
	tr = pendingDuplicate(t, e, tr, "new", payload)
	if _, err = e.ResolveDuplicate(context.Background(), tr.ID, "new", "skip", tr.Files[1].DuplicateCandidates[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReceivedItems(tr.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("all-skipped transfer exposed existing payload or empty structural wrapper", err)
	}
	got, err := os.ReadFile(filepath.Join(original.Destination, "original.txt"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("reveal changed the existing file", err)
	}
}

func TestReceivedItemsRejectOutgoingUnpublishedAndSymlinkTargets(t *testing.T) {
	t.Run("outgoing and incomplete", func(t *testing.T) {
		e, _, _ := testEngine(t)
		pending := incoming(t, e, "pending.txt", []byte("x"))
		if _, err := e.ReceivedItems(pending.ID); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "source.txt")
		if err := os.WriteFile(path, []byte("source"), 0600); err != nil {
			t.Fatal(err)
		}
		offer, err := e.Offer(testDevice(), []string{path})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.ReceivedItems(offer.ID); !errors.Is(err, ErrConflict) {
			t.Fatal("source file exposed as received payload", err)
		}
	})
	for _, mode := range []string{"file", "root"} {
		t.Run(mode+" symlink", func(t *testing.T) {
			e, _, _ := testEngine(t)
			tr := verifiedIncoming(t, e, "received.txt", []byte("x"))
			if mode == "file" {
				outside := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(tr.Destination, "received.txt")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(tr.Destination, "received.txt")); err != nil {
					t.Skip("symlink privilege unavailable", err)
				}
			} else {
				moved := tr.Destination + "-moved"
				if err := os.Rename(tr.Destination, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, tr.Destination); err != nil {
					t.Skip("symlink privilege unavailable", err)
				}
			}
			if _, err := e.ReceivedItems(tr.ID); !errors.Is(err, ErrConflict) {
				t.Fatal("symlink replaced an approved receive path", err)
			}
		})
	}
}

func TestCompletedReceivesRemoveOnlyTheirNewEmptyStagingDirectory(t *testing.T) {
	t.Run("normal and empty directory completion", func(t *testing.T) {
		e, _, _ := testEngine(t)
		file := verifiedIncoming(t, e, "new.txt", []byte("new"))
		empty := completedReceivedManifest(t, e, []model.File{{ID: "empty", Path: "Empty", Directory: true}}, nil)
		for _, tr := range []model.Transfer{file, empty} {
			if _, err := os.Stat(filepath.Join(tr.Destination, ".airlink-partials")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("internal empty staging directory remains in a new completed receive", tr.ID, err)
			}
		}
	})
	t.Run("explicit skip", func(t *testing.T) {
		e, _, _ := testEngine(t)
		data := []byte("duplicate")
		verifiedIncoming(t, e, "original", data)
		enableDuplicates(t, e)
		tr := incoming(t, e, "new", data)
		tr = pendingDuplicate(t, e, tr, "file-a", data)
		if _, err := e.ResolveDuplicate(context.Background(), tr.ID, "file-a", "skip", tr.Files[0].DuplicateCandidates[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(tr.Destination, ".airlink-partials")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("skipped receive retained its empty internal staging", err)
		}
	})
	t.Run("unknown nonempty contents survive", func(t *testing.T) {
		e, _, _ := testEngine(t)
		data := []byte("verified")
		tr := incoming(t, e, "new.txt", data)
		putAll(t, e, tr, data)
		unknown := filepath.Join(tr.Destination, ".airlink-partials", "unrelated-note")
		if err := os.WriteFile(unknown, []byte("preserve this"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest(data)); err != nil {
			t.Fatal("nonempty cleanup failure invalidated verified completion", err)
		}
		got, err := os.ReadFile(unknown)
		if err != nil || string(got) != "preserve this" {
			t.Fatal("cleanup deleted unknown contents", err)
		}
		current, _ := e.Get(tr.ID)
		if current.State != "completed" {
			t.Fatal(current.State)
		}
	})
}

func TestReceivedRevealAndIdempotentCompletionDoNotCleanOldFolders(t *testing.T) {
	e, dataDirectory, receiveDirectory := testEngine(t)
	data := []byte("already completed")
	tr := verifiedIncoming(t, e, "old.txt", data)
	oldEmptyStaging := filepath.Join(tr.Destination, ".airlink-partials")
	if err := os.Mkdir(oldEmptyStaging, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ReceivedItems(tr.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest(data)); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(dataDirectory, receiveDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.ReceivedItems(tr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldEmptyStaging); err != nil {
		t.Fatal("historical folder mutated during reveal/restart/idempotent completion", err)
	}
}

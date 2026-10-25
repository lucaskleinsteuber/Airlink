package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func verifiedIncoming(t *testing.T, e *Engine, name string, payload []byte) model.Transfer {
	t.Helper()
	tr := incoming(t, e, name, payload)
	putAll(t, e, tr, payload)
	if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest(payload)); err != nil {
		t.Fatal(err)
	}
	got, err := e.Get(tr.ID)
	if err != nil || got.State != "completed" {
		t.Fatal("fixture wasn't verified", got.State, err)
	}
	return got
}
func enableDuplicates(t *testing.T, e *Engine) {
	t.Helper()
	if err := e.SetOrganization(model.Organization{DetectDuplicates: true}); err != nil {
		t.Fatal(err)
	}
}
func pendingDuplicate(t *testing.T, e *Engine, tr model.Transfer, fileID string, payload []byte) model.Transfer {
	t.Helper()
	decision, err := e.PrepareFile(context.Background(), tr.ID, fileID, digest(payload))
	if err != nil || decision != "pending" {
		t.Fatal("duplicate not detected", decision, err)
	}
	got, _ := e.Get(tr.ID)
	if got.State != "waitingDuplicate" {
		t.Fatal("decision didn't release transfer", got.State)
	}
	return got
}
func waitEngineState(t *testing.T, e *Engine, id, state string) model.Transfer {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got, err := e.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == state {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("state=%s want=%s error=%s", got.State, state, got.Error)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestOrganizationDefaultsValidationAndImmutableSortedFolderSnapshot(t *testing.T) {
	e, _, _ := testEngine(t)
	if e.organization.DetectDuplicates || e.organization.SortEnabled || len(e.organization.Rules) != 0 {
		t.Fatal("organization enabled by default")
	}
	policy := model.Organization{SortEnabled: true, Rules: []model.SortRule{{ID: "photos", Extensions: []string{".JPG", "png", "JPG"}, Folder: "Photos"}, {ID: "later", Extensions: []string{"jpg"}, Folder: "Ignored"}}}
	normalized, err := ValidateOrganization(policy)
	if err != nil || len(normalized.Rules[0].Extensions) != 2 || normalized.Rules[0].Extensions[0] != "jpg" {
		t.Fatal("extension normalization", normalized, err)
	}
	if err = e.SetOrganization(policy); err != nil {
		t.Fatal(err)
	}
	policy.Rules[0].Folder = "External mutation"
	policy.Rules[0].Extensions[0] = "txt"
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "loose", Path: "PHOTO.JPG", Size: 1, DestinationPath: "../evil", Skipped: true, DuplicateState: "pending", DuplicateCandidates: []model.DuplicateCandidate{{ID: "evil", Path: "/private"}}}, {ID: "dir", Path: "Album", Directory: true}, {ID: "inner", Path: "Album/inside.JPG", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Files[0].Path != "PHOTO.JPG" || tr.Files[0].DestinationPath != "Photos/PHOTO.JPG" || tr.Files[0].Skipped || tr.Files[0].DuplicateState != "" || len(tr.Files[0].DuplicateCandidates) != 0 || tr.Files[2].DestinationPath != "Album/inside.JPG" {
		t.Fatal("wire/private mapping or folder snapshot failed", tr.Files)
	}
	if err = e.SetOrganization(model.Organization{}); err != nil {
		t.Fatal(err)
	}
	for _, part := range []struct {
		id   string
		body []byte
	}{{"loose", []byte("x")}, {"inner", []byte("y")}} {
		if _, err = e.WriteChunk(context.Background(), tr.ID, part.id, 0, digest(part.body), bytes.NewReader(part.body)); err != nil {
			t.Fatal(err)
		}
		if err = e.CompleteFile(context.Background(), tr.ID, part.id, digest(part.body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"Photos/PHOTO.JPG", "Album/inside.JPG"} {
		if _, err = os.Stat(filepath.Join(tr.Destination, filepath.FromSlash(p))); err != nil {
			t.Fatal("snapshot changed after settings edit", err)
		}
	}
	if _, err = os.Stat(filepath.Join(tr.Destination, "PHOTO.JPG")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published unsorted wire path")
	}
	for _, invalid := range []model.Organization{{SortEnabled: true}, {Rules: []model.SortRule{{ID: "x", Extensions: []string{"jpg"}, Folder: "../escape"}}}, {Rules: []model.SortRule{{ID: "x", Extensions: []string{"jpg"}, Folder: ".airlink-partials"}}}, {Rules: []model.SortRule{{ID: "x", Extensions: []string{"../jpg"}, Folder: "Images"}}}, {Rules: []model.SortRule{{ID: "x", Extensions: []string{"jpg"}, Folder: "Images"}, {ID: "y", Extensions: []string{"png"}, Folder: "images"}}}} {
		if _, err = ValidateOrganization(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe rule accepted", invalid, err)
		}
	}
}

func TestSortRejectsDestinationCollisionsAndSymlinkBuckets(t *testing.T) {
	e, _, _ := testEngine(t)
	if err := e.SetOrganization(model.Organization{SortEnabled: true, Rules: []model.SortRule{{ID: "x", Extensions: []string{"jpg"}, Folder: "Images"}}}); err != nil {
		t.Fatal(err)
	}
	for _, files := range [][]model.File{{{ID: "a", Path: "image.jpg", Size: 1}, {ID: "b", Path: "Images", Directory: true}, {ID: "c", Path: "Images/other.txt", Size: 1}}, {{ID: "a", Path: "Folder/a.txt", Size: 1}, {ID: "b", Path: "folder/b.txt", Size: 1}}} {
		if _, err := e.CreateIncoming(testDevice(), files); !errors.Is(err, ErrInvalid) {
			t.Fatal("conflicting destinations accepted", err)
		}
	}
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "a", Path: "one.jpg", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err = os.Remove(filepath.Join(tr.Destination, "Images")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(tr.Destination, "Images")); err != nil {
		t.Skip(err)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "a", 0, digest([]byte("x")), bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "a", digest([]byte("x"))); err == nil {
		t.Fatal("sorted publication followed a symlink")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote outside managed transfer")
	}
}

func TestContentDuplicatePreflightSkipHasNoPlaceholderOrInflatedBytes(t *testing.T) {
	e, _, _ := testEngine(t)
	payload := []byte("equal content different names")
	original := verifiedIncoming(t, e, "first.txt", payload)
	enableDuplicates(t, e)
	different := incoming(t, e, "first.txt", []byte("different content"))
	if decision, err := e.PrepareFile(context.Background(), different.ID, "file-a", digest([]byte("different content"))); err != nil || decision != "upload" {
		t.Fatal("name-only duplicate match", decision, err)
	}
	next := incoming(t, e, "other-name.txt", payload)
	pending := pendingDuplicate(t, e, next, "file-a", payload)
	candidate := pending.Files[0].DuplicateCandidates[0]
	if p, err := e.DuplicatePath(candidate.ID); err != nil || p != filepath.Join(original.Destination, "first.txt") {
		t.Fatal("local candidate path", p, err)
	}
	pending.Files[0].DuplicateCandidates[0].Path = "tampered"
	got, _ := e.Get(next.ID)
	if got.Files[0].DuplicateCandidates[0].Path == "tampered" {
		t.Fatal("private candidates were shallow-cloned")
	}
	done, err := e.ResolveDuplicate(context.Background(), next.ID, "file-a", "skip", candidate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.State != "completed" || !done.Files[0].Skipped || !done.Files[0].Completed || done.Files[0].Received != 0 || done.CompletedBytes != 0 || done.SkippedBytes != int64(len(payload)) {
		t.Fatal("skip invented network progress", done)
	}
	if _, err = os.Stat(filepath.Join(done.Destination, destination(done.Files[0]))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("skip created a fake payload")
	}
	if decision, err := e.PrepareFile(context.Background(), next.ID, "file-a", digest(payload)); err != nil || decision != "skip" {
		t.Fatal("repeat preparation not idempotent", decision, err)
	}
	b, _ := os.ReadFile(filepath.Join(original.Destination, "first.txt"))
	if !bytes.Equal(b, payload) {
		t.Fatal("existing candidate changed")
	}
	if len(e.managed) != 1 {
		t.Fatal("skipped file was indexed")
	}
}

func TestMixedSkipAndSentProgressAndKeepCopiesRemainSeparate(t *testing.T) {
	e, _, _ := testEngine(t)
	same := []byte("duplicate")
	verifiedIncoming(t, e, "existing", same)
	enableDuplicates(t, e)
	other := []byte("new content")
	tr, err := e.CreateIncoming(testDevice(), []model.File{{ID: "same", Path: "same", Size: int64(len(same))}, {ID: "other", Path: "other", Size: int64(len(other))}})
	if err != nil {
		t.Fatal(err)
	}
	pending := pendingDuplicate(t, e, tr, "same", same)
	if _, err = e.ResolveDuplicate(context.Background(), tr.ID, "same", "skip", pending.Files[0].DuplicateCandidates[0].ID); err != nil {
		t.Fatal(err)
	}
	if decision, err := e.PrepareFile(context.Background(), tr.ID, "other", digest(other)); err != nil || decision != "upload" {
		t.Fatal(decision, err)
	}
	if _, err = e.WriteChunk(context.Background(), tr.ID, "other", 0, digest(other), bytes.NewReader(other)); err != nil {
		t.Fatal(err)
	}
	if err = e.CompleteFile(context.Background(), tr.ID, "other", digest(other)); err != nil {
		t.Fatal(err)
	}
	done, _ := e.Get(tr.ID)
	if done.State != "completed" || done.CompletedBytes != int64(len(other)) || done.SkippedBytes != int64(len(same)) {
		t.Fatal("mixed progress incorrect", done)
	}
	keep := incoming(t, e, "new-copy", same)
	pending = pendingDuplicate(t, e, keep, "file-a", same)
	if _, err = e.ResolveDuplicate(context.Background(), keep.ID, "file-a", "keep", ""); err != nil {
		t.Fatal(err)
	}
	putAll(t, e, keep, same)
	if err = e.CompleteFile(context.Background(), keep.ID, "file-a", digest(same)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(keep.Destination, "new-copy"))
	if err != nil || !bytes.Equal(b, same) {
		t.Fatal("keep did not create separate verified copy", err)
	}
}

func TestDuplicateCandidateMissingReplacedSymlinkAndSameMetadataContent(t *testing.T) {
	for _, mutation := range []string{"missing", "inode", "symlink", "same-metadata-content"} {
		t.Run(mutation, func(t *testing.T) {
			e, _, _ := testEngine(t)
			payload := []byte("original bytes")
			original := verifiedIncoming(t, e, "original", payload)
			path := filepath.Join(original.Destination, "original")
			info, _ := os.Stat(path)
			enableDuplicates(t, e)
			next := incoming(t, e, "new", payload)
			pending := pendingDuplicate(t, e, next, "file-a", payload)
			id := pending.Files[0].DuplicateCandidates[0].ID
			switch mutation {
			case "missing":
				os.Remove(path)
			case "inode":
				replacement := path + ".new"
				os.WriteFile(replacement, payload, 0600)
				os.Chtimes(replacement, info.ModTime(), info.ModTime())
				os.Rename(replacement, path)
			case "symlink":
				external := filepath.Join(t.TempDir(), "external")
				os.WriteFile(external, payload, 0600)
				os.Remove(path)
				if err := os.Symlink(external, path); err != nil {
					t.Skip(err)
				}
			case "same-metadata-content":
				changed := append([]byte{}, payload...)
				changed[0] = 'z'
				os.WriteFile(path, changed, 0600)
				os.Chtimes(path, info.ModTime(), info.ModTime())
			}
			if _, err := e.ResolveDuplicate(context.Background(), next.ID, "file-a", "skip", id); err == nil {
				t.Fatal("stale candidate silently discarded new file")
			}
			got, _ := e.Get(next.ID)
			if got.Files[0].Skipped || got.Files[0].Completed || got.State != "waitingDuplicate" {
				t.Fatal("failed skip revived or completed transfer", got)
			}
			if _, err := e.DuplicatePath(id); err == nil {
				t.Fatal("stale candidate still exposed as validated")
			}
		})
	}
}

func TestDuplicateIndexSurvivesHistoryClearAndOldHistoryImportDoesNotHash(t *testing.T) {
	e, data, receive := testEngine(t)
	payload := []byte("persisted managed content")
	original := verifiedIncoming(t, e, "original", payload)
	if _, err := e.ClearHistory(""); err != nil {
		t.Fatal(err)
	}
	e.Close()
	var err error
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	enableDuplicates(t, e)
	next := incoming(t, e, "later", payload)
	pendingDuplicate(t, e, next, "file-a", payload)
	if _, err = os.Stat(filepath.Join(original.Destination, "original")); err != nil {
		t.Fatal("history clear deleted payload", err)
	}
	other, oldData, oldReceive := testEngine(t)
	old := verifiedIncoming(t, other, "old", payload)
	other.mu.Lock()
	_, err = other.db.Exec("DELETE FROM managed_files;DELETE FROM managed_seen")
	other.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	changed := append([]byte{}, payload...)
	changed[0] = 'z'
	if err = os.WriteFile(filepath.Join(old.Destination, "old"), changed, 0600); err != nil {
		t.Fatal(err)
	}
	other, err = New(oldData, oldReceive)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if len(other.managed) != 1 {
		t.Fatal("old completed history was not imported lazily")
	}
	enableDuplicates(t, other)
	candidate := incoming(t, other, "not-equal", payload)
	decision, err := other.PrepareFile(context.Background(), candidate.ID, "file-a", digest(payload))
	if err != nil || decision != "upload" || len(other.managed) != 0 {
		t.Fatal("lazy full validation didn't invalidate changed historical file", decision, err)
	}
}

func TestLegacySkipAndQueuedKeepPublishWithoutSourceAcrossRestart(t *testing.T) {
	e, data, receive := testEngine(t)
	payload := []byte("legacy duplicate")
	verifiedIncoming(t, e, "original", payload)
	enableDuplicates(t, e)
	legacy := incoming(t, e, "legacy-skip", payload)
	putAll(t, e, legacy, payload)
	if err := e.CompleteFile(context.Background(), legacy.ID, "file-a", digest(payload)); err != nil {
		t.Fatal(err)
	}
	pending := waitEngineState(t, e, legacy.ID, "waitingDuplicate")
	if _, err := e.ResolveDuplicate(context.Background(), legacy.ID, "file-a", "skip", pending.Files[0].DuplicateCandidates[0].ID); err != nil {
		t.Fatal(err)
	}
	done, _ := e.Get(legacy.ID)
	if done.CompletedBytes != int64(len(payload)) || done.SkippedBytes != 0 || !done.Files[0].Skipped {
		t.Fatal("legacy skip lost actual network bytes", done)
	}
	if _, err := os.Stat(partialPathForTest(e, legacy.ID, "file-a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy skip retained app-owned staging")
	}
	keep := incoming(t, e, "legacy-keep", payload)
	putAll(t, e, keep, payload)
	if err := e.CompleteFile(context.Background(), keep.ID, "file-a", digest(payload)); err != nil {
		t.Fatal(err)
	}
	waitEngineState(t, e, keep.ID, "waitingDuplicate")
	incoming(t, e, "block-a", []byte("a"))
	incoming(t, e, "block-b", []byte("b"))
	queued, err := e.ResolveDuplicate(context.Background(), keep.ID, "file-a", "keep", "")
	if err != nil || queued.State != "queued" {
		t.Fatal("legacy keep bypassed busy receive pool", queued.State, err)
	}
	e.Close()
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err = e.Resume(keep.ID); err != nil {
		t.Fatal(err)
	}
	waitEngineState(t, e, keep.ID, "completed")
	b, err := os.ReadFile(filepath.Join(keep.Destination, "legacy-keep"))
	if err != nil || !bytes.Equal(b, payload) {
		t.Fatal("kept staging didn't publish after restart without sender", err)
	}
}

func TestPartialUploadSkipCountsOnlyAvoidedRemainder(t *testing.T) {
	e, _, _ := testEngine(t)
	payload := append(bytes.Repeat([]byte("a"), int(model.ChunkSize)), []byte("last bytes")...)
	verifiedIncoming(t, e, "original", payload)
	enableDuplicates(t, e)
	next := incoming(t, e, "partial", payload)
	first := payload[:model.ChunkSize]
	if _, err := e.WriteChunk(context.Background(), next.ID, "file-a", 0, digest(first), bytes.NewReader(first)); err != nil {
		t.Fatal(err)
	}
	pending := pendingDuplicate(t, e, next, "file-a", payload)
	done, err := e.ResolveDuplicate(context.Background(), next.ID, "file-a", "skip", pending.Files[0].DuplicateCandidates[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.CompletedBytes != model.ChunkSize || done.SkippedBytes != int64(len(payload))-model.ChunkSize || done.Files[0].Received != model.ChunkSize {
		t.Fatal("partial skip inflated avoided/received counts", done)
	}
}

func TestOutgoingSkippedWaitingAndPausedStaleResults(t *testing.T) {
	e, _, _ := testEngine(t)
	path := filepath.Join(t.TempDir(), "source")
	payload := []byte("source remains untouched")
	os.WriteFile(path, payload, 0600)
	offer, err := e.Offer(testDevice(), []string{path})
	if err != nil {
		t.Fatal(err)
	}
	id := offer.Files[0].ID
	if err = e.Resume(offer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingWaiting(offer.ID, "waitingDuplicate"); err != nil {
		t.Fatal(err)
	}
	if err = e.Pause(offer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingSkipped(offer.ID, id, 0, digest(payload)); !errors.Is(err, ErrConflict) {
		t.Fatal("stale skip revived paused sender", err)
	}
	if err = e.Resume(offer.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingSkipped(offer.ID, id, 0, digest(payload)); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Get(offer.ID)
	if got.State != "completed" || got.CompletedBytes != 0 || got.SkippedBytes != int64(len(payload)) {
		t.Fatal("outgoing skip progress", got)
	}
	if _, _, err = e.OpenDownload(offer.ID, id); !errors.Is(err, ErrConflict) {
		t.Fatal("skipped export opened source payload", err)
	}
	b, _ := os.ReadFile(path)
	if !bytes.Equal(b, payload) {
		t.Fatal("outgoing skip changed source")
	}
}

func TestCancelledDuplicateHashAndQueuedSkipDoNotReviveWork(t *testing.T) {
	e, _, _ := testEngine(t)
	const size int64 = 256 * 1024 * 1024
	fixture, err := e.CreateIncoming(testDevice(), []model.File{{ID: "file-a", Path: "large", Size: size}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(fixture.Destination, "large"))
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(size)
	f.Close()
	hash := digest([]byte("sparse cancellation fixture only"))
	e.mu.Lock()
	r := e.records[fixture.ID]
	r.Transfer.Files[0].SHA256 = hash
	r.Transfer.Files[0].Completed = true
	r.Transfer.Files[0].Received = size
	r.Transfer.CompletedBytes = size
	r.Transfer.State = "completed"
	err = e.saveLocked(r)
	if err == nil {
		err = e.indexFileLocked(r, r.Transfer.Files[0])
	}
	e.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	enableDuplicates(t, e)
	next, err := e.CreateIncoming(testDevice(), []model.File{{ID: "file-a", Path: "incoming", Size: size}})
	if err != nil {
		t.Fatal(err)
	}
	checking := make(chan error, 1)
	go func() { _, err := e.PrepareFile(context.Background(), next.ID, "file-a", hash); checking <- err }()
	waitEngineState(t, e, next.ID, "checkingDuplicate")
	if err = e.Pause(next.ID); err != nil {
		t.Fatal(err)
	}
	if err = e.Resume(next.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-checking:
		if err == nil {
			t.Fatal("stale duplicate check succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("duplicate hash ignored cancellation")
	}
	got, _ := e.Get(next.ID)
	if got.State != "running" || got.Files[0].Skipped || got.Files[0].DuplicateState == "pending" {
		t.Fatal("stale checker revived decision", got)
	}
	e.Cancel(next.ID)
	payload := []byte("small duplicate")
	if err = e.SetOrganization(model.Organization{}); err != nil {
		t.Fatal(err)
	}
	verifiedIncoming(t, e, "small", payload)
	enableDuplicates(t, e)
	pending := incoming(t, e, "queued-skip", payload)
	pending = pendingDuplicate(t, e, pending, "file-a", payload)
	incoming(t, e, "block-one", []byte("1"))
	incoming(t, e, "block-two", []byte("2"))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := e.ResolveDuplicate(ctx, pending.ID, "file-a", "skip", pending.Files[0].DuplicateCandidates[0].ID)
		result <- err
	}()
	waitEngineState(t, e, pending.ID, "queued")
	cancel()
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("cancelled queued skip succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued skip ignored cancellation")
	}
	got, _ = e.Get(pending.ID)
	if got.State != "waitingDuplicate" || got.Files[0].Skipped {
		t.Fatal("cancelled decision left phantom running slot", got)
	}
}

func TestPreparedSourceHashCannotChangeAtPublicationOrOutgoingSkip(t *testing.T) {
	e, _, _ := testEngine(t)
	enableDuplicates(t, e)
	original := []byte("source A")
	changed := []byte("source B")
	tr := incoming(t, e, "immutable", original)
	if decision, err := e.PrepareFile(context.Background(), tr.ID, "file-a", digest(original)); err != nil || decision != "upload" {
		t.Fatal(decision, err)
	}
	putAll(t, e, tr, changed)
	if err := e.CompleteFile(context.Background(), tr.ID, "file-a", digest(changed)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("completion changed prepared source identity", err)
	}
	got, _ := e.Get(tr.ID)
	if got.Files[0].SHA256 != digest(original) || got.Files[0].Completed {
		t.Fatal("changed source replaced prepared manifest")
	}
	if _, err := os.Stat(filepath.Join(tr.Destination, "immutable")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("changed source was published")
	}
	source := filepath.Join(t.TempDir(), "source")
	os.WriteFile(source, original, 0600)
	outgoing, err := e.Offer(testDevice(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	id := outgoing.Files[0].ID
	if err = e.MarkOutgoingProgress(outgoing.ID, id, 0, digest(original)); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingSkipped(outgoing.ID, id, 0, digest(changed)); !errors.Is(err, ErrIntegrity) {
		t.Fatal("skip changed prepared outgoing identity", err)
	}
	out, _ := e.Get(outgoing.ID)
	if out.Files[0].Skipped || out.SkippedBytes != 0 {
		t.Fatal("rejected outgoing skip mutated progress")
	}
}

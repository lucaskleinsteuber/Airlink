package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func sourceOffer(t *testing.T, e *Engine, body []byte) (model.Transfer, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "original.bin")
	if err := os.WriteFile(p, body, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := e.Offer(testDevice(), []string{p})
	if err != nil {
		t.Fatal(err)
	}
	return v, p
}
func replaceSourcePreservingMetadata(t *testing.T, p string, body []byte) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	replacement := p + ".replacement"
	if err = os.WriteFile(replacement, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, p); err != nil {
		t.Fatal(err)
	}
}
func makeLegacySource(t *testing.T, e *Engine, v model.Transfer, knownHash string) {
	t.Helper()
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.records[v.ID]
	src := r.Sources[v.Files[0].ID]
	src.Identity = ""
	r.Sources[v.Files[0].ID] = src
	r.Transfer.Files[0].SHA256 = knownHash
	if err := e.saveLocked(r); err != nil {
		t.Fatal(err)
	}
}

func TestSourceIdentityRejectsSameMetadataReplacementAcrossRestart(t *testing.T) {
	e, data, receive := testEngine(t)
	original := []byte("original source bytes")
	changed := append([]byte{}, original...)
	changed[0] = 'z'
	offer, p := sourceOffer(t, e, original)
	id := offer.Files[0].ID
	if hash, err := e.SourceSHA256(context.Background(), offer.ID, id); err != nil || hash != digest(original) {
		t.Fatal(hash, err)
	}
	e.mu.Lock()
	identity := e.records[offer.ID].Sources[id].Identity
	e.mu.Unlock()
	if identity == "" {
		t.Fatal("offer did not capture filesystem identity")
	}
	replaceSourcePreservingMetadata(t, p, changed)
	if _, _, err := e.OpenDownloadContext(context.Background(), offer.ID, id); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("same-metadata replacement was served", err)
	}
	if _, err := e.SourceSHA256(context.Background(), offer.ID, id); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("stale cached SHA was reused for new inode", err)
	}
	if _, _, err := e.ReadSourceChunk(context.Background(), offer.ID, id, 0); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("replacement chunk was returned", err)
	}
	e.Close()
	var err error
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err = e.SourceSHA256(context.Background(), offer.ID, id); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("source identity did not persist across restart", err)
	}
	got, _ := e.Get(offer.ID)
	if got.Files[0].SHA256 != digest(original) {
		t.Fatal("failure overwrote original digest")
	}
}

func TestSourceIdentityChecksOpenedFileAndCurrentPath(t *testing.T) {
	for _, mutation := range []string{"replacement-identical-bytes", "in-place-edit"} {
		t.Run(mutation, func(t *testing.T) {
			e, _, _ := testEngine(t)
			body := []byte("original")
			offer, p := sourceOffer(t, e, body)
			id := offer.Files[0].ID
			file, _, err := e.OpenDownloadContext(context.Background(), offer.ID, id)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if mutation == "replacement-identical-bytes" {
				replaceSourcePreservingMetadata(t, p, body)
			} else {
				if err = os.WriteFile(p, []byte("modified"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = e.ValidateSource(offer.ID, id, file); !errors.Is(err, ErrSourceChanged) {
				t.Fatal("active source mutation wasn't rejected", err)
			}
		})
	}
}

func TestLegacyCachedDigestMustBeFullyValidatedBeforeIdentityAnchor(t *testing.T) {
	e, data, receive := testEngine(t)
	body := []byte("legacy original")
	offer, p := sourceOffer(t, e, body)
	id := offer.Files[0].ID
	makeLegacySource(t, e, offer, digest(body))
	e.Close()
	var err error
	e, err = New(data, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.mu.Lock()
	unanchored := e.records[offer.ID].Sources[id].Identity
	e.mu.Unlock()
	if unanchored != "" {
		t.Fatal("startup trusted legacy inode without full validation")
	}
	changed := append([]byte{}, body...)
	changed[0] = 'z'
	replaceSourcePreservingMetadata(t, p, changed)
	if _, _, err = e.OpenDownloadContext(context.Background(), offer.ID, id); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("legacy cached SHA bypassed full validation", err)
	}
	e.mu.Lock()
	unanchored = e.records[offer.ID].Sources[id].Identity
	e.mu.Unlock()
	if unanchored != "" {
		t.Fatal("failed validation anchored replacement")
	}
	replaceSourcePreservingMetadata(t, p, body)
	if hash, err := e.SourceSHA256(context.Background(), offer.ID, id); err != nil || hash != digest(body) {
		t.Fatal("valid legacy source couldn't resume", hash, err)
	}
	e.mu.Lock()
	anchored := e.records[offer.ID].Sources[id].Identity
	e.mu.Unlock()
	if anchored == "" {
		t.Fatal("validated legacy identity wasn't persisted")
	}
	replaceSourcePreservingMetadata(t, p, body)
	if _, err = e.SourceSHA256(context.Background(), offer.ID, id); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("anchored reference accepted another inode", err)
	}
}

func TestLegacyUncachedReferenceAnchorsWithoutInventingDigest(t *testing.T) {
	e, _, _ := testEngine(t)
	body := []byte("legacy uncached")
	offer, p := sourceOffer(t, e, body)
	id := offer.Files[0].ID
	makeLegacySource(t, e, offer, "")
	before, _ := os.Stat(p)
	file, _, err := e.OpenDownloadContext(context.Background(), offer.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	got, _ := e.Get(offer.ID)
	if got.Files[0].SHA256 != "" {
		t.Fatal("metadata anchoring invented a full digest")
	}
	if hash, err := e.SourceSHA256(context.Background(), offer.ID, id); err != nil || hash != digest(body) {
		t.Fatal(hash, err)
	}
	after, _ := os.Stat(p)
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("source was rewritten during migration")
	}
	got, _ = e.Get(offer.ID)
	if got.Files[0].SHA256 != digest(body) {
		t.Fatal("computed digest wasn't durably cached")
	}
}

func TestLegacySourceHashValidationIsCancelledByPauseAndClose(t *testing.T) {
	for _, action := range []string{"pause", "close"} {
		t.Run(action, func(t *testing.T) {
			e, _, _ := testEngine(t)
			p := filepath.Join(t.TempDir(), "sparse-cancellation-fixture")
			file, err := os.Create(p)
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Truncate(256 * 1024 * 1024); err != nil {
				t.Fatal(err)
			}
			file.Close()
			offer, err := e.Offer(testDevice(), []string{p})
			if err != nil {
				t.Fatal(err)
			}
			id := offer.Files[0].ID
			makeLegacySource(t, e, offer, digest([]byte("known-hash fixture, not a large-transfer proof")))
			if err = e.Resume(offer.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := e.SourceSHA256(context.Background(), offer.ID, id); done <- err }()
			deadline := time.Now().Add(time.Second)
			for {
				e.mu.Lock()
				active := len(e.sourceOperations[offer.ID]) > 0
				e.mu.Unlock()
				if active {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("source validation did not start")
				}
				time.Sleep(time.Millisecond)
			}
			if action == "pause" {
				err = e.Pause(offer.ID)
			} else {
				err = e.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("cancelled hash validation succeeded")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("source hash ignored cancellation")
			}
			e.mu.Lock()
			identity := e.records[offer.ID].Sources[id].Identity
			e.mu.Unlock()
			if identity != "" {
				t.Fatal("cancelled validation anchored an unverified identity")
			}
			if action == "pause" {
				if err = e.Resume(offer.ID); err != nil {
					t.Fatal(err)
				}
				got, _ := e.Get(offer.ID)
				if got.State != "running" {
					t.Fatal("stale hash changed resumed source state", got.State)
				}
			}
		})
	}
}

func TestOutgoingProgressCannotReplaceVerifiedSourceDigest(t *testing.T) {
	e, _, _ := testEngine(t)
	body := []byte("source anchor")
	offer, _ := sourceOffer(t, e, body)
	id := offer.Files[0].ID
	if _, err := e.SourceSHA256(context.Background(), offer.ID, id); err != nil {
		t.Fatal(err)
	}
	other := digest([]byte("different"))
	if err := e.MarkOutgoingProgress(offer.ID, id, 1, other); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("progress replaced source anchor", err)
	}
	if err := e.CompleteOutgoingFile(offer.ID, id, other); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("completion replaced source anchor", err)
	}
	got, _ := e.Get(offer.ID)
	if got.CompletedBytes != 0 || got.Files[0].Received != 0 || got.Files[0].SHA256 != digest(body) {
		t.Fatal("rejected source update mutated progress", got)
	}
	actual, _ := os.ReadFile(e.records[offer.ID].Sources[id].Path)
	if !bytes.Equal(actual, body) {
		t.Fatal("source changed")
	}
}

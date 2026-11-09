package engine

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

type irregularChunkReader struct {
	data     []byte
	step     int
	endError error
}

func (r *irregularChunkReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		if r.endError != nil {
			return 0, r.endError
		}
		return 0, io.EOF
	}
	n := min(len(p), r.step, len(r.data))
	copy(p, r.data[:n])
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, r.endError
	}
	return n, nil
}

func TestBoundedChunkReadsPreserveReaderErrorsAndExactLengths(t *testing.T) {
	body := bytes.Repeat([]byte("validated body"), 257)
	for _, step := range []int{1, 31, len(body)} {
		for _, final := range []error{nil, io.EOF} {
			r := &irregularChunkReader{data: append([]byte(nil), body...), step: step, endError: final}
			got, err := readChunkBytes(context.Background(), r, int64(len(body)))
			if err != nil || !bytes.Equal(got, body) {
				t.Fatal("fragmented body changed", step, err)
			}
			if err = requireChunkEOF(context.Background(), r); err != nil {
				t.Fatal(err)
			}
		}
	}
	broken := errors.New("fixture read failure")
	r := &irregularChunkReader{data: append([]byte(nil), body...), step: len(body), endError: broken}
	if _, err := readChunkBytes(context.Background(), r, int64(len(body))); !errors.Is(err, broken) {
		t.Fatal("error accompanying final bytes was ignored", err)
	}
	if _, err := readChunkBytes(context.Background(), bytes.NewReader(body[:len(body)-1]), int64(len(body))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("truncated body accepted", err)
	}
	for _, size := range []int64{-1, 0, model.ChunkSize + 1} {
		if _, err := readChunkBytes(context.Background(), bytes.NewReader(nil), size); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid allocation size", size, err)
		}
	}
}

func TestChunkLengthAndCancellationFailuresDoNotAcknowledgeBytes(t *testing.T) {
	e, _, _ := testEngine(t)
	body := bytes.Repeat([]byte("payload"), 100)
	tr := incoming(t, e, "kept.bin", body)
	for _, test := range []struct {
		name string
		body []byte
	}{
		{"short", body[:len(body)-1]},
		{"long", append(append([]byte(nil), body...), 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(body), bytes.NewReader(test.body)); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid body acknowledged", err)
			}
			current, _ := e.Get(tr.ID)
			if current.Files[0].Received != 0 || current.CompletedBytes != 0 {
				t.Fatal("invalid body changed progress", current)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.WriteChunk(ctx, tr.ID, "file-a", 0, digest(body), bytes.NewReader(body)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read acknowledged", err)
	}
	if _, err := e.WriteChunk(context.Background(), tr.ID, "file-a", 0, digest(body), bytes.NewReader(body)); err != nil {
		t.Fatal("failed bodies prevented retry", err)
	}
}

func TestUnchangedOutgoingProgressDoesNotRewriteDurableSourceAnchor(t *testing.T) {
	e, state, receive := testEngine(t)
	body := []byte("persistent source hash and acknowledgement")
	offer, original := sourceOffer(t, e, body)
	id := offer.Files[0].ID
	hash, err := e.SourceSHA256(context.Background(), offer.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err = e.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	prior, _ := e.Get(offer.ID)
	oldSample := time.Now().Add(-time.Hour)
	e.mu.Lock()
	e.progress[offer.ID] = progressSample{at: oldSample, bytes: 0}
	e.mu.Unlock()
	for _, value := range []string{hash, "", hash} {
		if err = e.MarkOutgoingProgress(offer.ID, id, 0, value); err != nil {
			t.Fatal(err)
		}
	}
	if err = e.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	current, _ := e.Get(offer.ID)
	if before != after || current.UpdatedAt != prior.UpdatedAt || current.Files[0].SHA256 != hash {
		t.Fatal("unchanged receipt rewrote state", before, after, current)
	}
	e.mu.Lock()
	sampled := e.progress[offer.ID]
	e.mu.Unlock()
	if !sampled.at.After(oldSample) || sampled.bytes != current.CompletedBytes {
		t.Fatal("resume kept an old offline speed sample", sampled)
	}
	if err = e.MarkOutgoingProgress(offer.ID, id, 1, hash); err != nil {
		t.Fatal(err)
	}
	if err = e.MarkOutgoingProgress(offer.ID, id, 1, digest([]byte("different"))); !errors.Is(err, ErrSourceChanged) {
		t.Fatal("no-op bypassed hash identity", err)
	}
	if err = e.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(state, receive)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err = reopened.Get(offer.ID)
	if err != nil || current.Files[0].Received != 1 || current.Files[0].SHA256 != hash {
		t.Fatal("persisted progress lost", err, current)
	}
	got, err := os.ReadFile(original)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatal("source changed", err)
	}
}

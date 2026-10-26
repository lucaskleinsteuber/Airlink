package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasfd3/Airlink/internal/model"
)

// These isolate block buffering and validation from network/filesystem rates.
// Receive replay still checks the whole body and its persisted block receipt.
func BenchmarkReadSourceBlock8MiB(b *testing.B) {
	root := b.TempDir()
	e, err := New(filepath.Join(root, "state"), filepath.Join(root, "received"))
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	body := bytes.Repeat([]byte("AirLink!"), int(model.ChunkSize/8))
	source := filepath.Join(root, "source.bin")
	if err = os.WriteFile(source, body, 0600); err != nil {
		b.Fatal(err)
	}
	offer, err := e.Offer(testDevice(), []string{source})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(model.ChunkSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, hash, err := e.ReadSourceChunk(context.Background(), offer.ID, offer.Files[0].ID, 0)
		if err != nil || int64(len(data)) != model.ChunkSize || !validHash(hash) {
			b.Fatal(err)
		}
	}
}

func BenchmarkReceiveReplayBlock8MiB(b *testing.B) {
	root := b.TempDir()
	e, err := New(filepath.Join(root, "state"), filepath.Join(root, "received"))
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()
	body := bytes.Repeat([]byte("AirLink!"), int(model.ChunkSize/8))
	hash := digest(body)
	offer, err := e.CreateIncoming(testDevice(), []model.File{{ID: "file", Path: "received.bin", Size: model.ChunkSize}})
	if err != nil {
		b.Fatal(err)
	}
	if _, err = e.WriteChunk(context.Background(), offer.ID, "file", 0, hash, bytes.NewReader(body)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(model.ChunkSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.WriteChunk(context.Background(), offer.ID, "file", 0, hash, bytes.NewReader(body)); err != nil {
			b.Fatal(err)
		}
	}
}

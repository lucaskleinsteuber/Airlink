package server

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestReconnectedSenderWaitsForAcceptanceWithoutStaleNetworkError(t *testing.T) {
	source, target := integrationHost(t), integrationHost(t)
	device, token := integrationDevice(t, target, false)
	// Directly drive one worker; the regular native worker ignores browser peers.
	rec := deviceRecord{Device: model.Device{ID: target.store.snapshot().ID, Kind: "browser", Name: "Receiver", Paired: true, CanReceive: true, Address: hostURL(target), Fingerprint: target.fingerprint}, OutboundToken: token}
	if err := source.store.put(rec); err != nil {
		t.Fatal(err)
	}
	data := []byte("original file survives reconnect and acceptance")
	path := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	offer, err := source.engine.Offer(rec.Device, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if err = source.engine.MarkReconnecting(offer.ID, "connection timeout"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); source.sendTransfer(ctx, offer, rec) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(4 * time.Second)
	var remote model.Transfer
	for {
		rows := target.engine.Transfers(device.ID)
		local, _ := source.engine.Get(offer.ID)
		if len(rows) == 1 && rows[0].State == "offered" && local.State == "offered" && local.Error == "" {
			remote = rows[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("healthy receiver did not replace stale reconnect status", local.State, local.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if source.remoteID(offer.ID) != remote.ID || remote.CompletedBytes != 0 {
		t.Fatal("waiting changed offer identity or acknowledged data")
	}
	if err = target.engine.Accept(remote.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("accepted transfer did not complete")
	}
	local, _ := source.engine.Get(offer.ID)
	if local.State != "completed" || local.Error != "" || len(target.engine.Transfers(device.ID)) != 1 {
		t.Fatal("acceptance lost the original job", local.State, local.Error)
	}
	actual, err := os.ReadFile(filepath.Join(remote.Destination, "original.txt"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("received file differs", err)
	}
	original, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, data) {
		t.Fatal("source changed", err)
	}
}

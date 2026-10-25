package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/engine"
	"github.com/lucasfd3/Airlink/internal/model"
	"github.com/lucasfd3/Airlink/internal/traffic"
)

// With polling disabled, only the actual private offer wake can start this
// worker. The TLS peer also rejects unknown/chunked lengths, so a successful
// transfer proves the real sender uses the bounded block's known byte count.
func TestPrivateOfferWakesSenderAndUsesKnownLengthTLSBlocks(t *testing.T) {
	base := t.TempDir()
	data := bytes.Repeat([]byte("AirLink block framing regression\n"), 4096)
	original := filepath.Join(base, "payload.bin")
	if err := os.WriteFile(original, data, 0600); err != nil {
		t.Fatal(err)
	}
	peerState := filepath.Join(base, "peer-state")
	if err := os.MkdirAll(peerState, 0700); err != nil {
		t.Fatal(err)
	}
	cert, fingerprint, _, err := identity(peerState, "TLS receiver")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var remote model.Transfer
	blocks := 0
	peer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-only-token" {
			fail(w, 401, io.EOF)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/transfers":
			var input model.Offer
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || len(input.Files) != 1 {
				fail(w, 400, io.ErrUnexpectedEOF)
				return
			}
			remote = model.Transfer{ID: "remote-transfer", State: "running", Files: input.Files, TotalBytes: int64(len(data))}
			respond(w, 201, remote)
		case r.Method == "GET":
			respond(w, 200, remote)
		case r.Method == "POST" && bytes.HasSuffix([]byte(r.URL.Path), []byte("/prepare")):
			respond(w, 200, map[string]any{"decision": "upload", "transfer": remote})
		case r.Method == "PUT":
			if r.ContentLength != int64(len(data)) || len(r.TransferEncoding) != 0 {
				fail(w, 400, io.ErrUnexpectedEOF)
				return
			}
			payload, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Equal(payload, data) || r.Header.Get("X-Chunk-SHA256") != digest(data) {
				fail(w, 400, io.ErrUnexpectedEOF)
				return
			}
			blocks++
			remote.Files[0].Received = int64(len(payload))
			remote.CompletedBytes = int64(len(payload))
			respond(w, 200, map[string]int64{"received": int64(len(payload))})
		case r.Method == "POST" && bytes.HasSuffix([]byte(r.URL.Path), []byte("/complete")):
			remote.Files[0].Completed = true
			remote.Files[0].SHA256 = digest(data)
			remote.State = "completed"
			respond(w, 200, remote)
		default:
			fail(w, 404, io.EOF)
		}
	}))
	peer.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
	peer.StartTLS()
	defer peer.Close()
	state := filepath.Join(base, "state")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := openStore(state, filepath.Join(base, "received"))
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(state, filepath.Join(base, "received"))
	if err != nil {
		t.Fatal(err)
	}
	meter, err := traffic.New(state)
	if err != nil {
		eng.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source := &Server{engine: eng, store: store, traffic: meter, opts: Options{DataDir: state}, ctx: ctx, cancel: cancel,
		senders: map[string]bool{}, senderCancels: map[string]context.CancelFunc{}, outgoingWake: make(chan struct{}, 1)}
	defer func() { cancel(); source.wg.Wait(); eng.Close(); meter.Close() }()
	device := model.Device{ID: "peer", Name: "Peer", Kind: "engine", Paired: true, CanReceive: true, Fingerprint: fingerprint, Address: peer.URL}
	if err := store.put(deviceRecord{Device: device, OutboundToken: "test-only-token"}); err != nil {
		t.Fatal(err)
	}
	source.wg.Add(1)
	go source.runOutgoingLoop(nil) // No periodic fallback is available in this fixture.
	params, _ := json.Marshal(map[string]any{"deviceId": device.ID, "paths": []string{original}, "requestId": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	offered, err := source.Control("offer", params)
	if err != nil {
		t.Fatal(err)
	}
	id := offered.(model.Transfer).ID
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := eng.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "failed" {
			t.Fatal(current.Error)
		}
		if current.State == "completed" {
			mu.Lock()
			got := blocks
			mu.Unlock()
			if got != 1 {
				t.Fatal("unexpected repeated payload", got)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("offer did not wake the non-polling sender or its TLS block was rejected")
}

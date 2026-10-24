package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestTransferCancellationInterruptsThrottledAndStalledBodies(t *testing.T) {
	for _, mode := range []string{"pause", "cancel", "permission", "guest-pause", "stalled-body"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationHost(t)
			d, token := integrationDevice(t, s, true)
			data := bytes.Repeat([]byte("payload!"), 128*1024)
			tr, e := s.engine.CreateIncoming(d, []model.File{{ID: "file", Path: "cancel.bin", Size: int64(len(data))}})
			if e != nil {
				t.Fatal(e)
			}
			if mode != "stalled-body" {
				if e = s.traffic.SetLimits(0, 1024); e != nil {
					t.Fatal(e)
				}
			}
			var body io.Reader = bytes.NewReader(data)
			var stalledWriter *io.PipeWriter
			if mode == "stalled-body" {
				reader, writer := io.Pipe()
				body = reader
				stalledWriter = writer
				defer reader.Close()
				defer writer.Close()
				go func() { _, _ = writer.Write(data[:4]) }()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "PUT", hostURL(s)+"/api/v1/transfers/"+tr.ID+"/files/file/chunks/0", body)
			req.ContentLength = int64(len(data))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Chunk-SHA256", digest(data))
			done := make(chan struct{})
			go func() {
				defer close(done)
				res, err := pinnedClient(s.fingerprint).Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, res.Body)
					res.Body.Close()
				}
			}()
			deadline := time.Now().Add(3 * time.Second)
			for s.traffic.Snapshot().ReceivedBytes == 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if s.traffic.Snapshot().ReceivedBytes == 0 {
				t.Fatal("body did not reach the receive reader")
			}
			before := time.Now()
			switch mode {
			case "permission":
				d.CanSend = false
				b, _ := json.Marshal(d)
				_, e = s.Control("device.update", b)
			case "guest-pause":
				e = requestJSON(context.Background(), pinnedClient(s.fingerprint), "POST", hostURL(s)+"/api/v1/transfers/"+tr.ID+"/pause", token, map[string]any{}, nil)
			default:
				method := "transfer.pause"
				if mode == "cancel" || mode == "stalled-body" {
					method = "transfer.cancel"
				}
				b, _ := json.Marshal(map[string]string{"id": tr.ID})
				_, e = s.Control(method, b)
			}
			if e != nil {
				t.Fatal(e)
			}
			if stalledWriter != nil {
				// The remote caller's deliberately blocked user-space producer
				// cannot be interrupted by this server. Verify our socket body,
				// request registration and receive slot have all been released.
				limit := time.Now().Add(2 * time.Second)
				for {
					s.mu.Lock()
					active := len(s.transferRequests[tr.ID])
					s.mu.Unlock()
					if active == 0 && len(s.chunkSlots) == 0 {
						break
					}
					if time.Now().After(limit) {
						t.Fatal("stalled body retained server request/receive slot")
					}
					time.Sleep(5 * time.Millisecond)
				}
				_ = stalledWriter.CloseWithError(context.Canceled)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled HTTP body kept the request/slot occupied")
			}
			if time.Since(before) > 2*time.Second {
				t.Fatal("cancellation was not prompt")
			}
			current, _ := s.engine.Get(tr.ID)
			if current.CompletedBytes != 0 || current.Files[0].Received != 0 {
				t.Fatal("cancelled body was acknowledged", current)
			}
			if mode == "cancel" || mode == "stalled-body" {
				if current.State != "cancelled" {
					t.Fatal(current.State)
				}
				return
			}
			if current.State != "paused" {
				t.Fatal(current.State)
			}
			if mode == "permission" {
				d.CanSend = true
				b, _ := json.Marshal(d)
				if _, e = s.Control("device.update", b); e != nil {
					t.Fatal(e)
				}
			}
			if e = s.traffic.SetLimits(0, 0); e != nil {
				t.Fatal(e)
			}
			if e = s.engine.Resume(tr.ID); e != nil {
				t.Fatal(e)
			}
			if uploadBlock(t, s, token, tr.ID, "file", 0, data) != 200 {
				t.Fatal("resume did not release the old receive slot")
			}
			if e = s.engine.CompleteFile(context.Background(), tr.ID, "file", digest(data)); e != nil {
				t.Fatal(e)
			}
		})
	}
}

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestChangedSourcesAreRecheckedAfterDownloadLimiterWait(t *testing.T) {
	for _, mode := range []string{"download", "archive"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationHost(t)
			d, token := integrationDevice(t, s, false)
			path := filepath.Join(t.TempDir(), "original.bin")
			data := bytes.Repeat([]byte("ORIGINAL-PRIVATE-CONTENT!"), 4096)
			if e := os.WriteFile(path, data, 0600); e != nil {
				t.Fatal(e)
			}
			info, _ := os.Stat(path)
			tr, e := s.engine.Offer(d, []string{path})
			if e != nil {
				t.Fatal(e)
			}
			if e = s.traffic.SetLimits(8192, 0); e != nil {
				t.Fatal(e)
			}
			changed := make(chan error, 1)
			go func() {
				time.Sleep(100 * time.Millisecond)
				temp := path + ".replacement"
				err := os.WriteFile(temp, bytes.Repeat([]byte("x"), len(data)), 0600)
				if err == nil {
					err = os.Chtimes(temp, info.ModTime(), info.ModTime())
				}
				if err == nil {
					err = os.Rename(temp, path)
				}
				changed <- err
			}()
			route := "/api/v1/transfers/" + tr.ID + "/archive"
			if mode == "download" {
				route = "/api/v1/transfers/" + tr.ID + "/files/" + tr.Files[0].ID + "/download"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", hostURL(s)+route, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res, requestErr := pinnedClient(s.fingerprint).Do(req)
			if requestErr == nil {
				body, readErr := io.ReadAll(res.Body)
				res.Body.Close()
				if res.StatusCode == 200 && readErr == nil {
					t.Fatal("changed source produced a successful download/archive")
				}
				if bytes.Contains(body, []byte("ORIGINAL-PRIVATE-CONTENT!")) {
					t.Fatal("limiter released invalidated source payload")
				}
			}
			if e := <-changed; e != nil {
				t.Fatal(e)
			}
			if s.traffic.Snapshot().SentBytes != 0 {
				t.Fatal("invalidated source bytes counted/delivered", s.traffic.Snapshot())
			}
		})
	}
}

func TestSenderPermanentProtocolErrorsStopWithoutRetryLoop(t *testing.T) {
	for _, stage := range []string{"offer", "chunk", "complete"} {
		t.Run(stage, func(t *testing.T) {
			source := integrationHost(t)
			cert, fp, _, err := identity(t.TempDir(), "Protocol test peer")
			if err != nil {
				t.Fatal(err)
			}
			var failures atomic.Int32
			var mu sync.Mutex
			remote := model.Transfer{ID: "remote", Direction: "receive", State: "running"}
			peer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				p := strings.TrimPrefix(r.URL.Path, "/api/v1/transfers")
				reject := func() {
					failures.Add(1)
					respond(w, 400, map[string]string{"error": "invalid immutable protocol/source identity"})
				}
				switch {
				case p == "" && r.Method == "POST":
					if stage == "offer" {
						reject()
						return
					}
					var offer model.Offer
					_ = json.NewDecoder(r.Body).Decode(&offer)
					remote.Files = offer.Files
					respond(w, 201, remote)
				case r.Method == "GET":
					respond(w, 200, remote)
				case strings.HasSuffix(p, "/prepare"):
					respond(w, 200, map[string]any{"decision": "upload", "transfer": remote})
				case r.Method == "PUT":
					if stage == "chunk" {
						reject()
						return
					}
					n, _ := io.Copy(io.Discard, r.Body)
					remote.Files[0].Received = n
					respond(w, 200, map[string]int64{"received": n})
				case strings.HasSuffix(p, "/complete"):
					reject()
				default:
					http.NotFound(w, r)
				}
			}))
			peer.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
			peer.StartTLS()
			defer peer.Close()
			device := model.Device{ID: randomID(), Kind: "engine", Name: "Test receiver", Address: peer.URL, Fingerprint: fp, Paired: true, CanSend: true, CanReceive: true}
			if err = source.store.put(deviceRecord{Device: device, OutboundToken: randomID()}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "source.bin")
			payload := []byte("unchanged payload")
			_ = os.WriteFile(path, payload, 0600)
			tr, err := source.engine.Offer(device, []string{path})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				current, _ := source.engine.Get(tr.ID)
				if current.State == "failed" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			current, _ := source.engine.Get(tr.ID)
			if current.State != "failed" || failures.Load() != 1 {
				t.Fatal("permanent protocol failure remained retrying", current, failures.Load())
			}
			time.Sleep(1100 * time.Millisecond)
			if failures.Load() != 1 {
				t.Fatal("failed sender continued retrying", failures.Load())
			}
			if got, e := os.ReadFile(path); e != nil || !bytes.Equal(got, payload) {
				t.Fatal("source changed", e)
			}
		})
	}
}

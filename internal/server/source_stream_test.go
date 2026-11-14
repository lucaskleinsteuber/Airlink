package server

import (
	"bytes"
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

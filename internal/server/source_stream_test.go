package server

import (
	"archive/zip"
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
			d, _ := integrationDevice(t, s, false)
			path := filepath.Join(t.TempDir(), "original.bin")
			data := bytes.Repeat([]byte("ORIGINAL-PRIVATE-CONTENT!"), 4096)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			tr, err := s.engine.Offer(d, []string{path})
			if err != nil {
				t.Fatal(err)
			}
			file, _, err := s.engine.OpenDownloadContext(context.Background(), tr.ID, tr.Files[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			// Buffer bytes while the source is valid, as ServeContent/ZIP do before a
			// throttled write. The policy must revalidate these already-buffered bytes.
			reader := &sourceReader{Context: context.Background(), File: file, server: s, transferID: tr.ID, fileID: tr.Files[0].ID}
			buffered, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.traffic.SetLimits(1024, 0); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			response := httptest.NewRecorder()
			var validations atomic.Int32
			policy := &sourcePolicyWriter{ResponseWriter: response, ctx: ctx, validate: func() error {
				validations.Add(1)
				return s.engine.ValidateSource(tr.ID, tr.Files[0].ID, file)
			}}
			writer := s.traffic.WrapWriter(ctx, policy, "send")
			entered, finished := make(chan struct{}), make(chan error, 1)
			go func() {
				close(entered)
				if mode == "download" {
					_, e := writer.Write(buffered)
					finished <- e
					return
				}
				archive := zip.NewWriter(writer)
				entry, e := archive.CreateHeader(&zip.FileHeader{Name: "original.bin", Method: zip.Store})
				if e == nil {
					_, e = entry.Write(buffered)
				}
				if e == nil {
					e = archive.Close()
				}
				finished <- e
			}()
			<-entered
			// An in-place edit works even while a Windows handle prevents replacement.
			// Keep the byte count, and force a different timestamp on coarse filesystems.
			info, _ := os.Stat(path)
			if err = os.WriteFile(path, bytes.Repeat([]byte("x"), len(data)), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Chtimes(path, info.ModTime(), info.ModTime().Add(2*time.Second)); err != nil {
				t.Fatal(err)
			}
			if err = s.traffic.SetLimits(0, 0); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-finished:
				if err == nil {
					t.Fatal("changed source produced a successful write")
				}
			case <-ctx.Done():
				t.Fatal("write did not finish", ctx.Err())
			}
			if validations.Load() == 0 {
				t.Fatal("source was not checked after the limiter")
			}
			if response.Body.Len() != 0 || s.traffic.Snapshot().SentBytes != 0 {
				t.Fatal("invalidated buffered payload was delivered", response.Body.Len(), s.traffic.Snapshot())
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

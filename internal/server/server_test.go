package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func integrationHost(t *testing.T) *Server {
	t.Helper()
	base := t.TempDir()
	s, e := New(Options{DataDir: filepath.Join(base, "state"), ReceiveDir: filepath.Join(base, "received"), NoDiscovery: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func hostURL(s *Server) string { return s.urls[len(s.urls)-1] }
func integrationDevice(t *testing.T, s *Server, auto bool) (model.Device, string) {
	t.Helper()
	token := randomID()
	d := model.Device{ID: randomID(), Name: "Integration peer", Kind: "browser", Paired: true, Online: true, CanSend: true, CanReceive: true, AutoAccept: auto}
	if e := s.store.put(deviceRecord{Device: d, TokenHash: digest([]byte(token))}); e != nil {
		t.Fatal(e)
	}
	return d, token
}
func uploadBlock(t *testing.T, s *Server, token, tid, fid string, index int64, data []byte) int {
	t.Helper()
	req, e := http.NewRequest("PUT", hostURL(s)+"/api/v1/transfers/"+tid+"/files/"+fid+"/chunks/"+stringIndex(index), bytes.NewReader(data))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Chunk-SHA256", digest(data))
	res, e := pinnedClient(s.fingerprint).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Log(string(b))
	}
	return res.StatusCode
}
func stringIndex(v int64) string {
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 20)
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
func TestNativePeerPairAndDirectTransfer(t *testing.T) {
	source, target := integrationHost(t), integrationHost(t)
	pairForTest(t, source, target)
	rec := source.store.snapshot().Devices[target.store.snapshot().ID]
	back := target.store.snapshot().Devices[source.store.snapshot().ID]
	var e error
	back.Device.AutoAccept = true
	if e = target.store.put(back); e != nil {
		t.Fatal(e)
	}
	data := bytes.Repeat([]byte("AirLink direct TLS transfer\x00"), 350000)
	path := filepath.Join(t.TempDir(), "original.bin")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	offered, e := source.engine.Offer(rec.Device, []string{path})
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		current, _ := source.engine.Get(offered.ID)
		if current.State == "completed" {
			got := target.engine.Transfers(source.store.snapshot().ID)
			if len(got) != 1 || got[0].State != "completed" {
				t.Fatal("receiver incomplete")
			}
			b, e := os.ReadFile(filepath.Join(got[0].Destination, "original.bin"))
			if e != nil || !bytes.Equal(b, data) {
				t.Fatal("direct data mismatch", e)
			}
			original, _ := os.ReadFile(path)
			if !bytes.Equal(original, data) {
				t.Fatal("source modified")
			}
			return
		}
		if current.State == "failed" {
			t.Fatal(current.Error)
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("transfer timeout: %#v receiver %#v", source.engine.Transfers(""), target.engine.Transfers(""))
}
func TestStdioControlAndPersistedSettings(t *testing.T) {
	s := integrationHost(t)
	var out bytes.Buffer
	if e := s.Stdio(strings.NewReader("{\"id\":7,\"method\":\"status\"}\n{\"id\":8,\"method\":\"shutdown\"}\n"), &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"id":7`) {
		t.Fatal(out.String())
	}
}
func TestStreamingAcceptance(t *testing.T) {
	raw := os.Getenv("AIRLINK_STRESS_GIB")
	if raw == "" {
		t.Skip("opt-in: AIRLINK_STRESS_GIB=100 for 100 GiB actual HTTPS payload")
	}
	count, parseErr := strconv.Atoi(raw)
	if parseErr != nil || count < 1 || count > 1024 {
		t.Fatal("invalid stress count")
	}
	s := integrationHost(t)
	_, token := integrationDevice(t, s, true)
	client := pinnedClient(s.fingerprint)
	block := make([]byte, int(model.ChunkSize))
	for i := range block {
		block[i] = byte(i % 251)
	}
	blockHash := digest(block)
	whole := sha256.New()
	for i := 0; i < 128; i++ {
		_, _ = whole.Write(block)
	}
	wholeHash := hex.EncodeToString(whole.Sum(nil))
	start := time.Now()
	var sent int64
	for batch := 0; batch < count; batch++ {
		var tr model.Transfer
		file := model.File{ID: "payload", Path: "payload.bin", Size: 1 << 30}
		if e := requestJSON(context.Background(), client, "POST", hostURL(s)+"/api/v1/transfers", token, model.Offer{Files: []model.File{file}}, &tr); e != nil {
			t.Fatal(e)
		}
		for index := 0; index < 128; index++ {
			req, _ := http.NewRequest("PUT", hostURL(s)+"/api/v1/transfers/"+tr.ID+"/files/payload/chunks/"+stringIndex(int64(index)), bytes.NewReader(block))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Chunk-SHA256", blockHash)
			res, e := client.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if res.StatusCode != 200 {
				t.Fatal(res.Status)
			}
			sent += int64(len(block))
		}
		if e := requestJSON(context.Background(), client, "POST", hostURL(s)+"/api/v1/transfers/"+tr.ID+"/files/payload/complete", token, map[string]string{"sha256": wholeHash}, &tr); e != nil {
			t.Fatal(e)
		}
		if tr.State != "completed" {
			t.Fatal(tr.State)
		}
		local, _ := s.engine.Get(tr.ID)
		if e := os.RemoveAll(local.Destination); e != nil {
			t.Fatal(e)
		}
		if (batch+1)%10 == 0 {
			t.Logf("verified %d GiB actual HTTPS payload", batch+1)
		}
	}
	t.Logf("verified bytes=%d duration=%s average=%.1f MiB/s; sequential 1GiB files, no single-100GiB claim", sent, time.Since(start), float64(sent)/(1<<20)/time.Since(start).Seconds())
}

package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"github.com/lucaskleinsteuber/Airlink/internal/peer"
)

// memConn delivers frames in order to the other engine, like a session's
// single reader goroutine. tamper may modify data frames in flight.
type memConn struct {
	peerID  string
	remote  *Engine
	back    *memConn
	queue   chan memFrame
	done    chan struct{}
	once    sync.Once
	streams atomic.Uint32
	tamper  func([]byte)
	delay   atomic.Int64
}

type memFrame struct {
	control []byte
	kind    string
	stream  uint32
	data    []byte
}

func (c *memConn) PeerID() string        { return c.peerID }
func (c *memConn) Done() <-chan struct{} { return c.done }
func (c *memConn) OpenStream() uint32    { return c.streams.Add(1) }
func (c *memConn) CloseStream(uint32)    {}
func (c *memConn) DropStream(uint32)     {}

func (c *memConn) enqueue(f memFrame) error {
	select {
	case <-c.done:
		return peer.ErrClosed
	default:
	}
	select {
	case c.queue <- f:
		return nil
	case <-c.done:
		return peer.ErrClosed
	}
}

func (c *memConn) Send(v any) error { return c.ordered(0, v) }
func (c *memConn) SendOrdered(stream uint32, v any) error {
	return c.ordered(stream, v)
}
func (c *memConn) ordered(stream uint32, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(body, &head)
	return c.enqueue(memFrame{control: body, kind: head.Type, stream: stream})
}
func (c *memConn) SendData(ctx context.Context, stream uint32, payload []byte, release func()) error {
	data := append([]byte(nil), payload...)
	if release != nil {
		release()
	}
	if c.tamper != nil {
		c.tamper(data)
	}
	return c.enqueue(memFrame{stream: stream, data: data})
}

func (c *memConn) deliver() {
	for {
		select {
		case <-c.done:
			return
		case f := <-c.queue:
			if d := time.Duration(c.delay.Load()); d > 0 {
				time.Sleep(d)
			}
			if f.data != nil {
				c.remote.HandleData(c.back, f.stream, f.data, func([]byte) {})
			} else {
				c.remote.HandleControl(c.back, f.kind, f.stream, f.control)
			}
		}
	}
}

func (c *memConn) close() {
	c.once.Do(func() { close(c.done) })
}

type host struct {
	id      string
	engine  *Engine
	data    string
	receive string
	mu      sync.Mutex
	conn    *memConn
	peers   map[string]model.Device
}

func newHost(t *testing.T, id string) *host {
	t.Helper()
	base := t.TempDir()
	h := &host{id: id, data: filepath.Join(base, "state"), receive: filepath.Join(base, "received"), peers: map[string]model.Device{}}
	h.open(t)
	return h
}

func (h *host) open(t *testing.T) {
	t.Helper()
	e, err := New(h.data, h.receive, Options{
		Session: func(id string) Conn {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.conn != nil && h.conn.peerID == id {
				select {
				case <-h.conn.done:
				default:
					return h.conn
				}
			}
			return nil
		},
		Device: func(id string) (model.Device, bool) {
			h.mu.Lock()
			defer h.mu.Unlock()
			d, ok := h.peers[id]
			return d, ok
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.engine = e
	t.Cleanup(func() { _ = e.Close() })
}

// connect links two hosts and runs the session-open logic on both.
func connect(a, b *host, auto bool) (*memConn, *memConn) {
	for _, pair := range [][2]*host{{a, b}, {b, a}} {
		pair[0].mu.Lock()
		pair[0].peers[pair[1].id] = model.Device{ID: pair[1].id, Name: pair[1].id, Kind: "engine", Paired: true, CanSend: true, CanReceive: true, AutoAccept: auto}
		pair[0].mu.Unlock()
	}
	ab := &memConn{peerID: b.id, remote: b.engine, queue: make(chan memFrame, 64), done: make(chan struct{})}
	ba := &memConn{peerID: a.id, remote: a.engine, queue: make(chan memFrame, 64), done: make(chan struct{})}
	ab.back, ba.back = ba, ab
	ba.done = ab.done // one connection: closing either side closes both
	a.mu.Lock()
	a.conn = ab
	a.mu.Unlock()
	b.mu.Lock()
	b.conn = ba
	b.mu.Unlock()
	go ab.deliver()
	go ba.deliver()
	a.engine.SessionOpened(ab)
	b.engine.SessionOpened(ba)
	return ab, ba
}

func disconnect(a, b *host, ab, ba *memConn) {
	ab.close()
	a.engine.SessionClosed(ab)
	b.engine.SessionClosed(ba)
}

func waitState(t *testing.T, h *host, id, state string, timeout time.Duration) model.Transfer {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		tr, err := h.engine.Header(id)
		if err == nil && tr.State == state {
			return tr
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: transfer %s state %q (want %q), error %q", h.id, id, tr.State, state, tr.Error)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func incoming(t *testing.T, h *host, from string) model.Transfer {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, tr := range h.engine.Transfers(from) {
			if tr.Direction == "receive" {
				return tr
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no incoming transfer")
	return model.Transfer{}
}

func writeRandom(t *testing.T, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	_, _ = rand.Read(data)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}

func inventory(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." || strings.HasPrefix(rel, partialsDir) {
			if d.IsDir() && rel != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			out[filepath.ToSlash(rel)+"/"] = "dir"
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		out[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sum)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func send(t *testing.T, h *host, to string, paths ...string) model.Transfer {
	t.Helper()
	h.mu.Lock()
	device := h.peers[to]
	h.mu.Unlock()
	tr, err := h.engine.Offer(context.Background(), device, paths, "")
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestSingleFileAutoAccept(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, true)
	source := filepath.Join(t.TempDir(), "Bericht.pdf")
	data := writeRandom(t, source, 300_000)
	out := send(t, a, "bbbb", source)
	waitState(t, a, out.ID, "completed", 10*time.Second)
	in := waitState(t, b, out.ID, "completed", 10*time.Second)
	got, err := os.ReadFile(filepath.Join(b.receive, "Bericht.pdf"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("content mismatch", err)
	}
	full, _ := b.engine.Get(in.ID)
	sum := sha256.Sum256(data)
	if full.Files[0].SHA256 != fmt.Sprintf("%x", sum) || full.CompletedBytes != int64(len(data)) {
		t.Fatalf("recorded result %#v", full.Files[0])
	}
	if _, err = os.Stat(filepath.Join(b.receive, partialsDir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("partial folder left behind", err)
	}
	items, err := b.engine.ReceivedItems(in.ID)
	if err != nil || len(items.Items) != 1 || filepath.Base(items.Items[0].Path) != "Bericht.pdf" || items.Items[0].Directory {
		t.Fatal("received items", items, err)
	}
}

func TestFolderWithManySmallFilesEmptyFolderAndUnicode(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, true)
	root := filepath.Join(t.TempDir(), "Urlaub Fotos")
	for i := range 1000 {
		writeRandom(t, filepath.Join(root, fmt.Sprintf("tag%02d", i%10), fmt.Sprintf("bild%04d.jpg", i)), 100+i)
	}
	writeRandom(t, filepath.Join(root, "Büro", "Notiz.txt"), 10) // decomposed ü
	writeRandom(t, filepath.Join(root, "leer.txt"), 0)
	if err := os.MkdirAll(filepath.Join(root, "leerer Ordner"), 0755); err != nil {
		t.Fatal(err)
	}
	out := send(t, a, "bbbb", root)
	waitState(t, a, out.ID, "completed", 30*time.Second)
	waitState(t, b, out.ID, "completed", 30*time.Second)
	want := inventory(t, filepath.Dir(root))
	got := inventory(t, b.receive)
	normalized := map[string]string{}
	for k, v := range want {
		normalized[composeLatin(k)] = v
	}
	if len(got) != len(normalized) {
		t.Fatalf("entries %d, want %d", len(got), len(normalized))
	}
	for k, v := range normalized {
		if got[k] != v {
			t.Fatalf("%s: got %q want %q", k, got[k], v)
		}
	}
	if _, ok := got["Urlaub Fotos/Büro/Notiz.txt"]; !ok {
		t.Fatal("decomposed name was not composed")
	}
}

func TestLargeFileAcrossSegmentsAndNameCollision(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, true)
	if err := os.MkdirAll(b.receive, 0755); err != nil {
		t.Fatal(err)
	}
	existing := []byte("keep me")
	if err := os.WriteFile(filepath.Join(b.receive, "video.mov"), existing, 0644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "video.mov")
	data := writeRandom(t, source, 2*SegmentSize+12345)
	out := send(t, a, "bbbb", source)
	waitState(t, a, out.ID, "completed", 60*time.Second)
	waitState(t, b, out.ID, "completed", 60*time.Second)
	if got, _ := os.ReadFile(filepath.Join(b.receive, "video.mov")); !bytes.Equal(got, existing) {
		t.Fatal("existing file was modified")
	}
	if got, err := os.ReadFile(filepath.Join(b.receive, "video (2).mov")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("large file mismatch", err)
	}
}

func TestManualAcceptPauseResumeAndCancel(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, false)
	source := filepath.Join(t.TempDir(), "a.bin")
	writeRandom(t, source, 1000)
	out := send(t, a, "bbbb", source)
	in := incoming(t, b, "aaaa")
	waitState(t, b, in.ID, "offered", 5*time.Second)
	waitState(t, a, out.ID, "offered", 5*time.Second)
	if err := b.engine.Accept(in.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, a, out.ID, "completed", 10*time.Second)
	waitState(t, b, in.ID, "completed", 10*time.Second)

	second := filepath.Join(t.TempDir(), "b.bin")
	writeRandom(t, second, 1000)
	out2 := send(t, a, "bbbb", second)
	waitState(t, b, out2.ID, "offered", 5*time.Second)
	if err := a.engine.Cancel(out2.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, b, out2.ID, "cancelled", 5*time.Second)
	if _, err := os.Stat(filepath.Join(b.receive, "b.bin")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("cancelled offer created a file")
	}
}

func TestDisconnectResumesFromCheckpoint(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	ab, ba := connect(a, b, true)
	ab.delay.Store(int64(2 * time.Millisecond)) // ~1 MiB frames: slow enough to interrupt
	source := filepath.Join(t.TempDir(), "big.bin")
	data := writeRandom(t, source, 3*SegmentSize)
	out := send(t, a, "bbbb", source)
	deadline := time.Now().Add(30 * time.Second)
	for {
		tr, _ := b.engine.Header(out.ID)
		if tr.CompletedBytes >= SegmentSize+SegmentSize/2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no progress", tr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	disconnect(a, b, ab, ba)
	waitState(t, b, out.ID, "reconnecting", 5*time.Second)
	durable, _ := b.engine.Header(out.ID)
	if durable.CompletedBytes < SegmentSize {
		t.Fatal("no durable checkpoint", durable.CompletedBytes)
	}
	connect(a, b, true)
	waitState(t, a, out.ID, "completed", 60*time.Second)
	waitState(t, b, out.ID, "completed", 60*time.Second)
	if got, err := os.ReadFile(filepath.Join(b.receive, "big.bin")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("resumed content mismatch", err)
	}
}

func TestRestartOfBothServicesKeepsProgress(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	ab, ba := connect(a, b, true)
	ab.delay.Store(int64(2 * time.Millisecond))
	source := filepath.Join(t.TempDir(), "big.bin")
	data := writeRandom(t, source, 2*SegmentSize+5)
	out := send(t, a, "bbbb", source)
	deadline := time.Now().Add(30 * time.Second)
	for {
		tr, _ := b.engine.Header(out.ID)
		if tr.CompletedBytes > SegmentSize {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no progress")
		}
		time.Sleep(10 * time.Millisecond)
	}
	disconnect(a, b, ab, ba)
	if err := a.engine.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.engine.Close(); err != nil {
		t.Fatal(err)
	}
	a.open(t)
	b.open(t)
	waitState(t, b, out.ID, "reconnecting", time.Second)
	connect(a, b, true)
	waitState(t, b, out.ID, "completed", 60*time.Second)
	waitState(t, a, out.ID, "completed", 10*time.Second)
	if got, err := os.ReadFile(filepath.Join(b.receive, "big.bin")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("content after restart", err)
	}
}

func TestReceiverPauseAndSenderResume(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	ab, _ := connect(a, b, true)
	ab.delay.Store(int64(3 * time.Millisecond))
	source := filepath.Join(t.TempDir(), "big.bin")
	data := writeRandom(t, source, SegmentSize+SegmentSize/2)
	out := send(t, a, "bbbb", source)
	deadline := time.Now().Add(30 * time.Second)
	for {
		tr, _ := b.engine.Header(out.ID)
		if tr.CompletedBytes > 4<<20 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no progress")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := b.engine.Pause(out.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, a, out.ID, "paused", 5*time.Second)
	ab.delay.Store(0)
	if err := a.engine.Resume(out.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, b, out.ID, "completed", 60*time.Second)
	waitState(t, a, out.ID, "completed", 10*time.Second)
	if got, err := os.ReadFile(filepath.Join(b.receive, "big.bin")); err != nil || !bytes.Equal(got, data) {
		t.Fatal("content after pause", err)
	}
}

func TestChangedSourceFailsWithoutPublishing(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, false)
	source := filepath.Join(t.TempDir(), "doc.txt")
	writeRandom(t, source, 5000)
	out := send(t, a, "bbbb", source)
	waitState(t, b, out.ID, "offered", 5*time.Second)
	writeRandom(t, source, 5001)
	if err := b.engine.Accept(out.ID); err != nil {
		t.Fatal(err)
	}
	failed := waitState(t, a, out.ID, "failed", 10*time.Second)
	if failed.Problem == nil || failed.Problem.Code != "source_changed" {
		t.Fatal("problem", failed.Problem, failed.Error)
	}
	waitState(t, b, out.ID, "cancelled", 10*time.Second)
	if _, err := os.Stat(filepath.Join(b.receive, "doc.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("changed source was published")
	}
}

func TestTamperedDataIsRejected(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	ab, _ := connect(a, b, true)
	ab.tamper = func(b []byte) { b[len(b)/2] ^= 1 }
	source := filepath.Join(t.TempDir(), "data.bin")
	writeRandom(t, source, 100_000)
	out := send(t, a, "bbbb", source)
	failed := waitState(t, b, out.ID, "failed", 10*time.Second)
	if failed.Problem == nil || failed.Problem.Code != "integrity" {
		t.Fatal("problem", failed.Problem, failed.Error)
	}
	waitState(t, a, out.ID, "failed", 10*time.Second)
	if _, err := os.Stat(filepath.Join(b.receive, "data.bin")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("tampered data was published")
	}
}

func TestOfferReplayAndHistory(t *testing.T) {
	a, b := newHost(t, "aaaa"), newHost(t, "bbbb")
	connect(a, b, true)
	source := filepath.Join(t.TempDir(), "x.txt")
	writeRandom(t, source, 10)
	a.mu.Lock()
	device := a.peers["bbbb"]
	a.mu.Unlock()
	first, err := a.engine.Offer(context.Background(), device, []string{source}, "request-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.engine.Offer(context.Background(), device, []string{source}, "request-1")
	if err != nil || again.ID != first.ID {
		t.Fatal("replay created another transfer", err)
	}
	waitState(t, a, first.ID, "completed", 10*time.Second)
	page, err := a.engine.SummaryPage(SummaryQuery{Query: "x.t"})
	if err != nil || len(page.Transfers) != 1 {
		t.Fatal("search", page, err)
	}
	if err = a.engine.RemoveHistory(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.engine.Get(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("history not removed")
	}
	if got, err := os.ReadFile(source); err != nil || len(got) != 10 {
		t.Fatal("source changed by history removal")
	}
}

func TestLocalNamesForEachPlatform(t *testing.T) {
	cases := []struct{ in, goos, want string }{
		{"a:b?.txt", "windows", "a_b_.txt"},
		{"a:b?.txt", "linux", "a:b?.txt"},
		{"con.txt", "windows", "_con.txt"},
		{"name. ", "windows", "name"},
		{"back\\slash", "windows", "back_slash"},
		{"back\\slash", "darwin", "back\\slash"},
		{".airlink-partials", "linux", "_.airlink-partials"},
		{"Büro", "linux", "Büro"},
	}
	for _, c := range cases {
		if got := localComponent(c.in, c.goos); got != c.want {
			t.Errorf("localComponent(%q, %s) = %q, want %q", c.in, c.goos, got, c.want)
		}
	}
	if numberedName("Report.pdf", 2, false) != "Report (2).pdf" || numberedName("Photos", 3, true) != "Photos (3)" || numberedName(".env", 2, false) != ".env (2)" {
		t.Fatal("numbered names")
	}
	for _, bad := range []string{"", "/abs", "a/../b", "a//b", "./a", "a/.", "x\x00y"} {
		if validWirePath(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	names := []string{}
	for k := range combining {
		names = append(names, string(k[0]))
	}
	sort.Strings(names)
	if len(combining) < 150 {
		t.Fatal("composition table too small", len(combining))
	}
}

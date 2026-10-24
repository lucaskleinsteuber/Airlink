// Package traffic applies shared per-direction payload limits and records
// lifetime payload counters. It never stores transferred content.
package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const quantum = 32 * 1024
const MaxLimit int64 = 1 << 40

type Counters struct {
	SentBytes     int64 `json:"sentBytes"`
	ReceivedBytes int64 `json:"receivedBytes"`
}
type bucket struct {
	limit   int64
	tokens  float64
	updated time.Time
	changed chan struct{}
}
type Meter struct {
	mu            sync.Mutex
	saveMu        sync.Mutex
	path          string
	send, receive bucket
	counters      Counters
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	closed        bool
}

func ValidLimit(limit int64) bool { return limit == 0 || limit >= 1024 && limit <= MaxLimit }
func New(dataDir string) (*Meter, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, errors.New("absolute traffic data directory required")
	}
	if e := os.MkdirAll(dataDir, 0700); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Meter{path: filepath.Join(dataDir, "traffic.json"), ctx: ctx, cancel: cancel, done: make(chan struct{}), send: bucket{changed: make(chan struct{})}, receive: bucket{changed: make(chan struct{})}}
	if b, e := os.ReadFile(m.path); e == nil {
		if e = json.Unmarshal(b, &m.counters); e != nil {
			cancel()
			return nil, e
		}
		if m.counters.SentBytes < 0 || m.counters.ReceivedBytes < 0 {
			cancel()
			return nil, errors.New("invalid traffic counters")
		}
	} else if !os.IsNotExist(e) {
		cancel()
		return nil, e
	}
	go m.run()
	return m, nil
}
func (m *Meter) run() {
	defer close(m.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
			_ = m.save()
		}
	}
}
func (m *Meter) save() error {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	b, e := json.Marshal(m.counters)
	m.mu.Unlock()
	if e != nil {
		return e
	}
	f, e := os.OpenFile(m.path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	if err := f.Close(); e == nil {
		e = err
	}
	if e != nil {
		_ = os.Remove(m.path + ".tmp")
		return e
	}
	return os.Rename(m.path+".tmp", m.path)
}
func (m *Meter) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.send.changed)
	close(m.receive.changed)
	m.mu.Unlock()
	m.cancel()
	<-m.done
	return m.save()
}
func (m *Meter) SetLimits(send, receive int64) error {
	if !ValidLimit(send) || !ValidLimit(receive) {
		return errors.New("speed limits must be zero or 1024–1099511627776 bytes/second")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("traffic meter closed")
	}
	now := time.Now()
	for _, v := range []struct {
		b *bucket
		n int64
	}{{&m.send, send}, {&m.receive, receive}} {
		if v.b.limit != v.n {
			close(v.b.changed)
			v.b.changed = make(chan struct{})
			v.b.limit = v.n
			v.b.tokens = 0
			v.b.updated = now
		}
	}
	return nil
}
func (m *Meter) Snapshot() Counters { m.mu.Lock(); defer m.mu.Unlock(); return m.counters }
func (m *Meter) wait(ctx context.Context, n int, direction string) error {
	for n > 0 {
		want := min(n, quantum)
		for {
			m.mu.Lock()
			if m.closed {
				m.mu.Unlock()
				return errors.New("traffic meter closed")
			}
			b := &m.send
			if direction == "receive" {
				b = &m.receive
			}
			if b.limit == 0 {
				m.mu.Unlock()
				return ctx.Err()
			}
			now := time.Now()
			b.tokens = min(float64(quantum), b.tokens+now.Sub(b.updated).Seconds()*float64(b.limit))
			b.updated = now
			if b.tokens >= float64(want) {
				b.tokens -= float64(want)
				m.mu.Unlock()
				break
			}
			delay := time.Duration((float64(want) - b.tokens) / float64(b.limit) * float64(time.Second))
			changed := b.changed
			m.mu.Unlock()
			timer := time.NewTimer(max(delay, time.Millisecond))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-m.ctx.Done():
				timer.Stop()
				return errors.New("traffic meter closed")
			case <-changed:
				timer.Stop()
			case <-timer.C:
			}
		}
		n -= want
	}
	return ctx.Err()
}
func (m *Meter) add(n int, direction string) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if direction == "receive" {
		m.counters.ReceivedBytes += min(int64(n), math.MaxInt64-m.counters.ReceivedBytes)
	} else {
		m.counters.SentBytes += min(int64(n), math.MaxInt64-m.counters.SentBytes)
	}
}
func (m *Meter) WrapReader(ctx context.Context, r io.Reader, direction string) io.Reader {
	return &reader{m: m, ctx: ctx, r: r, direction: direction}
}

type reader struct {
	m         *Meter
	ctx       context.Context
	r         io.Reader
	direction string
}

func (r *reader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	n, e := r.r.Read(p[:min(len(p), quantum)])
	r.m.add(n, r.direction)
	if n > 0 {
		if waitErr := r.m.wait(r.ctx, n, r.direction); waitErr != nil {
			return 0, waitErr
		}
	}
	return n, e
}
func (m *Meter) WrapWriter(ctx context.Context, w http.ResponseWriter, direction string) http.ResponseWriter {
	return &writer{m: m, ctx: ctx, ResponseWriter: w, direction: direction}
}

type writer struct {
	http.ResponseWriter
	m         *Meter
	ctx       context.Context
	direction string
	status    int
}

func (w *writer) WriteHeader(code int) {
	if code >= 200 && w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *writer) Write(p []byte) (int, error) {
	if w.status >= 300 {
		return w.ResponseWriter.Write(p)
	}
	total := 0
	for len(p) > 0 {
		n := min(len(p), quantum)
		if e := w.m.wait(w.ctx, n, w.direction); e != nil {
			return total, e
		}
		_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(time.Now().Add(30 * time.Second))
		sent, e := w.ResponseWriter.Write(p[:n])
		w.m.add(sent, w.direction)
		total += sent
		p = p[sent:]
		if e != nil {
			return total, e
		}
		if sent != n {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}
func (w *writer) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

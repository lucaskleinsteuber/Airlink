package traffic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestLimitsAreAggregateAndCountersSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	meter, e := New(dir)
	if e != nil {
		t.Fatal(e)
	}
	if e = meter.SetLimits(32768, 0); e != nil {
		t.Fatal(e)
	}
	start := time.Now()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := meter.WrapWriter(context.Background(), httptest.NewRecorder(), "send")
			if _, err := w.Write(make([]byte, 4096)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed < 220*time.Millisecond {
		t.Fatalf("per-request rather than aggregate limit: %v", elapsed)
	}
	received, e := io.ReadAll(meter.WrapReader(context.Background(), bytes.NewReader([]byte("received")), "receive"))
	if e != nil || string(received) != "received" {
		t.Fatal(e)
	}
	if e = meter.Close(); e != nil {
		t.Fatal(e)
	}
	again, e := New(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	if got := again.Snapshot(); got.SentBytes != 8192 || got.ReceivedBytes != 8 {
		t.Fatal(got)
	}
}
func TestHTTPErrorBodiesAreNotFilePayload(t *testing.T) {
	m, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	_ = m.SetLimits(1024, 0)
	w := m.WrapWriter(context.Background(), httptest.NewRecorder(), "send")
	w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	start := time.Now()
	_, e = w.Write([]byte("invalid Range"))
	if e != nil || m.Snapshot().SentBytes != 0 || time.Since(start) > 100*time.Millisecond {
		t.Fatal("error was treated as file payload", m.Snapshot(), e)
	}
}
func TestCancellationAndLiveUnlimitedWakeWaiters(t *testing.T) {
	m, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	_ = m.SetLimits(1024, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, e := m.WrapWriter(ctx, httptest.NewRecorder(), "send").Write(make([]byte, quantum))
		result <- e
	}()
	cancel()
	select {
	case e := <-result:
		if e == nil {
			t.Fatal("cancel ignored")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel blocked")
	}
	go func() {
		_, e := m.WrapWriter(context.Background(), httptest.NewRecorder(), "send").Write(make([]byte, quantum))
		result <- e
	}()
	time.Sleep(10 * time.Millisecond)
	_ = m.SetLimits(0, 0)
	select {
	case e := <-result:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("setting unlimited did not wake pending payload")
	}
}
func TestInvalidLimitsAndDirectionsRemainIndependent(t *testing.T) {
	m, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	for _, limit := range []int64{-1, 1, 1023, MaxLimit + 1} {
		if m.SetLimits(limit, 0) == nil {
			t.Fatal("invalid limit accepted", limit)
		}
	}
	_ = m.SetLimits(1024, 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	if _, e = io.Copy(io.Discard, m.WrapReader(ctx, bytes.NewReader(make([]byte, quantum)), "receive")); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("send cap incorrectly limited receive")
	}
}

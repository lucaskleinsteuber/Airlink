package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrefetchIsBoundedOrderedAndCancelsUnconsumedRead(t *testing.T) {
	var count atomic.Int64
	started := make(chan int64, 4)
	blocks, cancel := prefetchBlocks(context.Background(), 2, 100, func(ctx context.Context, i int64) ([]byte, string, error) {
		count.Add(1)
		started <- i
		return []byte{byte(i)}, "hash", nil
	})
	defer cancel()
	<-started
	if count.Load() != 1 {
		t.Fatal("read beyond first unconsumed block")
	}
	first := <-blocks
	if first.index != 2 || first.data[0] != 2 {
		t.Fatal(first)
	}
	<-started
	time.Sleep(10 * time.Millisecond)
	if count.Load() != 2 {
		t.Fatal("more than one block prefetched", count.Load())
	}
	cancel()
	// A handoff already in the select may win the cancellation race. It may
	// deliver the one prepared block, but must never start another read.
	select {
	case _, ok := <-blocks:
		if ok {
			select {
			case _, more := <-blocks:
				if more {
					t.Fatal("continued reading after cancellation")
				}
			case <-time.After(time.Second):
				t.Fatal("producer did not stop")
			}
		}
	case <-time.After(time.Second):
		t.Fatal("producer did not stop")
	}
}

func TestPrefetchPropagatesSourceFailureAndStops(t *testing.T) {
	failure := errors.New("source changed")
	blocks, cancel := prefetchBlocks(context.Background(), 0, 20, func(context.Context, int64) ([]byte, string, error) { return nil, "", failure })
	defer cancel()
	if b := <-blocks; !errors.Is(b.err, failure) {
		t.Fatal(b)
	}
	if _, ok := <-blocks; ok {
		t.Fatal("continued after source failure")
	}
}

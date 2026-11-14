package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/peer"
)

// sendJob is one outgoing stream, started by the receiver's status.
type sendJob struct {
	conn   Conn
	stream uint32
	epoch  uint64
	cancel context.CancelFunc
}

// startSendingLocked begins streaming at the receiver's checkpoint. A
// previous stream of the transfer is abandoned. Callers hold e.mu.
func (e *Engine) startSendingLocked(r *record, c Conn, next int, offset int64, epoch uint64) {
	if e.closed {
		return
	}
	device, ok := e.opts.Device(r.t.DeviceID)
	if !ok || !device.Paired || !device.CanReceive {
		r.t.State = "paused"
		r.t.Error = "Receiving device permission revoked"
		return
	}
	e.stopSendingLocked(r.t.ID)
	ctx, cancel := context.WithCancel(e.ctx)
	j := &sendJob{conn: c, stream: c.OpenStream(), epoch: epoch, cancel: cancel}
	e.senders[r.t.ID] = j
	files := make([]fileEntry, len(r.files))
	copy(files, r.files)
	r.live = r.durableBytes
	id := r.t.ID
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		err := e.stream(ctx, j, id, files, next, offset)
		if err != nil {
			c.DropStream(j.stream)
		} else {
			c.CloseStream(j.stream)
		}
		e.mu.Lock()
		current := e.senders[id] == j
		if current {
			delete(e.senders, id)
		}
		e.mu.Unlock()
		if current && err != nil && errors.Is(err, ErrSourceChanged) {
			e.failSend(id, err)
		}
	}()
}

func (e *Engine) stopSendingLocked(id string) {
	if j := e.senders[id]; j != nil {
		j.cancel()
		delete(e.senders, id)
	}
}

func (e *Engine) stream(ctx context.Context, j *sendJob, id string, files []fileEntry, next int, offset int64) error {
	for i := next; i < len(files); i++ {
		f := files[i]
		start := int64(0)
		if i == next {
			start = offset
		}
		if start < 0 || start > f.Size || start%SegmentSize != 0 {
			return fmt.Errorf("%w: invalid resume position", ErrInvalid)
		}
		if err := j.conn.SendOrdered(j.stream, fileMsg{Type: "file", Transfer: id, Epoch: j.epoch, Ordinal: i, Offset: start}); err != nil {
			return err
		}
		if f.Directory {
			continue
		}
		if err := e.sendFile(ctx, j, id, i, f, start); err != nil {
			return err
		}
	}
	return j.conn.SendOrdered(j.stream, endMsg{Type: "end"})
}

func (e *Engine) progress(id string, ordinal int, offset int64, delta int64) {
	e.mu.Lock()
	if r := e.records[id]; r != nil && !terminal(r.t.State) {
		r.live += delta
		r.liveOrdinal, r.liveOffset = ordinal, offset
		at := time.Now()
		if r.rateAt.IsZero() {
			r.rateAt, r.rateBytes = at, r.live
		} else if elapsed := at.Sub(r.rateAt).Seconds(); elapsed >= 0.5 {
			current := float64(r.live-r.rateBytes) / elapsed
			if r.t.BytesPerSecond > 0 {
				current = 0.6*current + 0.4*r.t.BytesPerSecond
			}
			r.t.BytesPerSecond = max(0, current)
			r.rateAt, r.rateBytes = at, r.live
		}
	}
	e.mu.Unlock()
}

func sameSource(f fileEntry, info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Size() == f.Size && sourceIdentity(info) == f.identity
}

func (e *Engine) sendFile(ctx context.Context, j *sendJob, id string, ordinal int, f fileEntry, start int64) error {
	src, err := os.Open(f.source)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrSourceChanged, f.Path)
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || !sameSource(f, info) {
		return fmt.Errorf("%w: %s", ErrSourceChanged, f.Path)
	}
	if start > 0 {
		if _, err = src.Seek(start, io.SeekStart); err != nil {
			return fmt.Errorf("%w: %s", ErrSourceChanged, f.Path)
		}
	}
	h := sha256.New()
	index := start / SegmentSize
	offset := start
	segmentEnd := min(f.Size, (index+1)*SegmentSize)
	if f.Size == 0 {
		return j.conn.SendOrdered(j.stream, segmentMsg{Type: "segment", Ordinal: ordinal, Index: 0, SHA256: hex.EncodeToString(h.Sum(nil)), Last: true})
	}
	for offset < f.Size {
		n := int(min(segmentEnd-offset, peer.MaxDataPayload))
		buf := peer.GetBuffer()
		if _, err = io.ReadFull(src, buf[:n]); err != nil {
			peer.ReleaseBuffer(buf)
			return fmt.Errorf("%w: %s", ErrSourceChanged, f.Path)
		}
		h.Write(buf[:n])
		if e.opts.Wait != nil {
			if err = e.opts.Wait(ctx, n, "send"); err != nil {
				peer.ReleaseBuffer(buf)
				return err
			}
		}
		if err = j.conn.SendData(ctx, j.stream, buf[:n], func() { peer.ReleaseBuffer(buf) }); err != nil {
			return err
		}
		if e.opts.Add != nil {
			e.opts.Add(n, "send")
		}
		offset += int64(n)
		e.progress(id, ordinal, offset, int64(n))
		if offset == segmentEnd {
			last := offset == f.Size
			if last {
				// Detect a source that changed while it was being read.
				if info, err = src.Stat(); err != nil || !sameSource(f, info) {
					return fmt.Errorf("%w: %s", ErrSourceChanged, f.Path)
				}
			}
			if err = j.conn.SendOrdered(j.stream, segmentMsg{Type: "segment", Ordinal: ordinal, Index: index, SHA256: hex.EncodeToString(h.Sum(nil)), Last: last}); err != nil {
				return err
			}
			h.Reset()
			index++
			segmentEnd = min(f.Size, (index+1)*SegmentSize)
		}
	}
	return nil
}

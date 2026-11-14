package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	opData byte = iota + 1
	opFile
	opSegment
	opEnd
)

const (
	// A batch of finished files is synced and published together.
	batchFiles = 512
	batchBytes = 64 << 20
	batchAge   = 700 * time.Millisecond
	// Large files are checkpointed at segment ends at most this often.
	checkpointInterval = time.Second
)

var (
	errDisconnected = errors.New("connection to the other device was interrupted")
	errStopped      = errors.New("receive stopped")
	errComplete     = errors.New("receive complete")
	errProtocol     = errors.New("connection protocol error")
)

type recvOp struct {
	kind    byte
	data    []byte
	release func([]byte)
	file    fileMsg
	seg     segmentMsg
}

type pendingFile struct {
	ordinal int
	file    *os.File // nil for directories
	sha     string
	size    int64
}

// recvJob writes one incoming transfer's stream. Its goroutine owns the
// open files; e.mu guards the record.
type recvJob struct {
	e      *Engine
	id     string
	conn   Conn
	epoch  uint64
	stream uint32 // bound by the first file message; guarded by e.mu
	ops    chan recvOp
	quit   chan struct{}
	done   chan struct{}
	once   sync.Once
	reason error
	ctx    context.Context
	cancel context.CancelFunc

	root         *os.Root
	dest         string
	sizes        []int64
	expect       int
	first        bool
	resumeAt     int64
	cur          *os.File
	curOrdinal   int
	curOffset    int64
	segStart     int64
	hash         hash.Hash
	pending      []pendingFile
	pendingBytes int64
	pendingSince time.Time
	checkpointAt time.Time
	completedSum int64
}

func (j *recvJob) push(op recvOp) {
	select {
	case j.ops <- op:
	case <-j.done:
		if op.data != nil && op.release != nil {
			op.release(op.data)
		}
	}
}

func (j *recvJob) stop(reason error) {
	j.once.Do(func() {
		j.reason = reason
		close(j.quit)
		j.cancel()
	})
}

func partialName(id string, ordinal int) string {
	return filepath.Join(partialsDir, id, fileID(ordinal))
}

// startReceiver begins (or restarts) receiving at the durable checkpoint and
// asks the sender to stream from there with a fresh epoch.
func (e *Engine) startReceiver(id string, c Conn) {
	e.mu.Lock()
	old := e.receivers[id]
	e.mu.Unlock()
	if old != nil {
		old.stop(errStopped)
		<-old.done
	}
	e.mu.Lock()
	r := e.records[id]
	if e.closed || r == nil || r.t.Direction != "receive" || r.t.State == "completed" || r.t.State == "cancelled" || r.t.State == "offered" {
		e.mu.Unlock()
		return
	}
	if err := e.loadFiles(r); err != nil {
		e.mu.Unlock()
		return
	}
	device, ok := e.opts.Device(r.t.DeviceID)
	if !ok || !device.Paired || !device.CanSend {
		r.t.State = "paused"
		r.t.Error = "Sending device permission revoked"
		r.t.UpdatedAt = now()
		_ = e.saveHeader(r)
		status := r.status()
		e.mu.Unlock()
		e.send(c, status)
		e.changed()
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	j := &recvJob{e: e, id: id, conn: c, epoch: nextEpoch(), ops: make(chan recvOp, 32), quit: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel,
		dest: r.t.Destination, expect: r.next, first: true, resumeAt: r.nextOffset, hash: sha256.New()}
	j.sizes = make([]int64, len(r.files))
	for i, f := range r.files {
		j.sizes[i] = f.Size
		if i < r.next {
			j.completedSum += f.Size
		}
	}
	e.receivers[id] = j
	r.t.State = "running"
	r.t.Error = ""
	r.t.UpdatedAt = now()
	r.live = r.durableBytes
	_ = e.saveHeader(r)
	status := r.status()
	status.Epoch = j.epoch
	e.wg.Add(1)
	go j.run()
	e.mu.Unlock()
	e.send(c, status)
	e.changed()
}

// stopReceiver ends the receive job, keeping verified progress, and moves
// the transfer to state (paused or cancelled).
func (e *Engine) stopReceiver(id, state, reason string) {
	e.mu.Lock()
	j := e.receivers[id]
	e.mu.Unlock()
	if j != nil {
		j.stop(errStopped)
		<-j.done
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.records[id]
	if r == nil || terminal(r.t.State) {
		return
	}
	r.t.State = state
	r.t.Error = reason
	r.t.UpdatedAt = now()
	r.live = r.durableBytes
	_ = e.saveHeader(r)
	if state == "cancelled" {
		e.cleanupPartialsLocked(r)
	}
}

// cleanupPartialsLocked removes unfinished data and empty folders created
// for a cancelled receive. Published files stay.
func (e *Engine) cleanupPartialsLocked(r *record) {
	root, err := os.OpenRoot(r.t.Destination)
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.RemoveAll(filepath.Join(partialsDir, r.t.ID))
	_ = root.Remove(partialsDir)
	if r.files == nil {
		return
	}
	// Remove folders this transfer created that stayed empty, deepest first.
	for i := len(r.files) - 1; i >= 0; i-- {
		if f := r.files[i]; f.Directory && f.DestinationPath != "" {
			_ = root.Remove(filepath.FromSlash(f.DestinationPath))
		}
	}
	for _, f := range r.files {
		if top := strings.SplitN(f.DestinationPath, "/", 2)[0]; top != "" && (f.Directory || strings.Contains(f.DestinationPath, "/")) {
			_ = root.Remove(filepath.FromSlash(top))
		}
	}
}

func (e *Engine) routeFile(c Conn, stream uint32, m fileMsg) {
	e.mu.Lock()
	j := e.receivers[m.Transfer]
	if j == nil || j.conn != c || j.epoch != m.Epoch || stream == 0 {
		e.mu.Unlock()
		return // a stream of an abandoned epoch; its data is dropped
	}
	if j.stream != stream {
		if j.stream != 0 {
			delete(e.streams, streamKey{c, j.stream})
		}
		j.stream = stream
		e.streams[streamKey{c, stream}] = j
	}
	e.mu.Unlock()
	j.push(recvOp{kind: opFile, file: m})
}

func (e *Engine) route(c Conn, stream uint32, op recvOp) {
	e.mu.Lock()
	j := e.streams[streamKey{c, stream}]
	e.mu.Unlock()
	if j != nil {
		j.push(op)
	}
}

func (j *recvJob) run() {
	defer j.e.wg.Done()
	err := j.loop()
	j.finish(err)
	close(j.done)
}

func (j *recvJob) loop() error {
	root, err := os.OpenRoot(j.dest)
	if err != nil {
		return fmt.Errorf("destination unavailable: %w", err)
	}
	j.root = root
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case op := <-j.ops:
			if err := j.handle(op); err != nil {
				return err
			}
		case <-ticker.C:
			if len(j.pending) > 0 && time.Since(j.pendingSince) >= batchAge {
				if err := j.commit(); err != nil {
					return err
				}
			}
		case <-j.quit:
			return j.reason
		case <-j.conn.Done():
			return errDisconnected
		}
	}
}

func (j *recvJob) handle(op recvOp) error {
	switch op.kind {
	case opFile:
		return j.beginFile(op.file)
	case opData:
		err := j.write(op.data)
		op.release(op.data)
		return err
	case opSegment:
		return j.endSegment(op.seg)
	case opEnd:
		if j.cur != nil || j.expect != len(j.sizes) {
			return fmt.Errorf("%w: stream ended early", errProtocol)
		}
		return errComplete
	}
	return nil
}

func (j *recvJob) record() *record {
	j.e.mu.Lock()
	defer j.e.mu.Unlock()
	return j.e.records[j.id]
}

func (j *recvJob) entry(ordinal int) fileEntry {
	j.e.mu.Lock()
	defer j.e.mu.Unlock()
	if r := j.e.records[j.id]; r != nil && r.files != nil && ordinal < len(r.files) {
		return r.files[ordinal]
	}
	return fileEntry{}
}

func (j *recvJob) beginFile(m fileMsg) error {
	if j.cur != nil || m.Ordinal != j.expect || m.Ordinal >= len(j.sizes) {
		return fmt.Errorf("%w: unexpected file %d", errProtocol, m.Ordinal)
	}
	want := int64(0)
	if j.first {
		want = j.resumeAt
	}
	j.first = false
	if m.Offset != want {
		return fmt.Errorf("%w: resume position", errProtocol)
	}
	f := j.entry(m.Ordinal)
	if f.DestinationPath == "" {
		return fmt.Errorf("destination unavailable: no planned name")
	}
	if f.Directory {
		if err := j.root.MkdirAll(filepath.FromSlash(f.DestinationPath), 0755); err != nil {
			return storageError(err)
		}
		j.addPending(pendingFile{ordinal: m.Ordinal})
		j.expect++
		return j.maybeCommit()
	}
	file, err := j.root.OpenFile(partialName(j.id, m.Ordinal), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return storageError(err)
	}
	if m.Offset > 0 {
		info, err := file.Stat()
		if err != nil || info.Size() < m.Offset {
			file.Close()
			return fmt.Errorf("%w: partial data missing", errProtocol)
		}
	}
	if err = file.Truncate(m.Offset); err == nil {
		_, err = file.Seek(m.Offset, io.SeekStart)
	}
	if err != nil {
		file.Close()
		return storageError(err)
	}
	j.cur, j.curOrdinal, j.curOffset, j.segStart = file, m.Ordinal, m.Offset, m.Offset
	j.hash.Reset()
	return nil
}

func (j *recvJob) segmentEnd() int64 {
	size := j.sizes[j.curOrdinal]
	return min(size, (j.segStart/SegmentSize+1)*SegmentSize)
}

func (j *recvJob) write(data []byte) error {
	if j.cur == nil {
		return fmt.Errorf("%w: data without file", errProtocol)
	}
	if j.curOffset+int64(len(data)) > j.segmentEnd() {
		return fmt.Errorf("%w: data beyond segment", errProtocol)
	}
	if j.e.opts.Wait != nil {
		if err := j.e.opts.Wait(j.ctx, len(data), "receive"); err != nil {
			return errStopped
		}
	}
	n, err := j.cur.Write(data)
	if err != nil {
		return storageError(err)
	}
	j.hash.Write(data[:n])
	j.curOffset += int64(n)
	if j.e.opts.Add != nil {
		j.e.opts.Add(n, "receive")
	}
	j.e.progress(j.id, j.curOrdinal, j.curOffset, int64(n))
	return nil
}

func (j *recvJob) endSegment(m segmentMsg) error {
	if j.cur == nil || m.Ordinal != j.curOrdinal || m.Index != j.segStart/SegmentSize {
		return fmt.Errorf("%w: unexpected segment", errProtocol)
	}
	end := j.segmentEnd()
	size := j.sizes[j.curOrdinal]
	if j.curOffset != end || m.Last != (end == size) {
		return fmt.Errorf("%w: incomplete segment", errProtocol)
	}
	digest := hex.EncodeToString(j.hash.Sum(nil))
	if digest != m.SHA256 {
		return fmt.Errorf("%w: segment %d of %s", ErrIntegrity, m.Index, j.entry(j.curOrdinal).Path)
	}
	single := j.segStart == 0 && m.Last
	j.hash.Reset()
	j.segStart = end
	if m.Last {
		sha := ""
		if single {
			sha = digest
		}
		j.addPending(pendingFile{ordinal: j.curOrdinal, file: j.cur, sha: sha, size: size})
		j.cur = nil
		j.expect++
		return j.maybeCommit()
	}
	if time.Since(j.checkpointAt) >= checkpointInterval {
		return j.checkpoint()
	}
	return nil
}

func (j *recvJob) addPending(p pendingFile) {
	if len(j.pending) == 0 {
		j.pendingSince = time.Now()
	}
	j.pending = append(j.pending, p)
	j.pendingBytes += p.size
}

func (j *recvJob) maybeCommit() error {
	if len(j.pending) >= batchFiles || j.pendingBytes >= batchBytes {
		return j.commit()
	}
	return nil
}

func storageError(err error) error {
	if errors.Is(err, syscall.ENOSPC) {
		return fmt.Errorf("storage_full: %w", err)
	}
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("file_access: %w", err)
	}
	return err
}

// syncFiles flushes finished files' data, several at a time.
func syncFiles(files []*os.File) error {
	if len(files) == 0 {
		return nil
	}
	workers := min(8, len(files))
	errs := make(chan error, workers)
	next := make(chan *os.File)
	for range workers {
		go func() {
			var first error
			for f := range next {
				if err := syncData(f); err != nil && first == nil {
					first = err
				}
			}
			errs <- first
		}()
	}
	for _, f := range files {
		next <- f
	}
	close(next)
	var first error
	for range workers {
		if err := <-errs; err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		return first
	}
	return barrier(files[len(files)-1])
}

// commit makes the pending files durable, publishes them under their planned
// names and records them in one transaction, then acknowledges the sender.
func (j *recvJob) commit() error {
	if len(j.pending) == 0 {
		return nil
	}
	open := make([]*os.File, 0, len(j.pending))
	for _, p := range j.pending {
		if p.file != nil {
			open = append(open, p.file)
		}
	}
	err := syncFiles(open)
	for _, f := range open {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		j.pending = nil
		return storageError(err)
	}
	dirs := map[string]bool{}
	for i := range j.pending {
		p := &j.pending[i]
		p.file = nil
		f := j.entry(p.ordinal)
		if f.Directory {
			continue
		}
		dest, err := j.publish(p.ordinal, f)
		if err != nil {
			j.pending = nil
			return storageError(err)
		}
		dirs[path.Dir(dest)] = true
	}
	for dir := range dirs {
		if d, err := j.root.Open(filepath.FromSlash(dir)); err == nil {
			_ = syncDir(d)
			d.Close()
		}
	}
	e := j.e
	e.mu.Lock()
	r := e.records[j.id]
	if r == nil {
		e.mu.Unlock()
		j.pending = nil
		return errStopped
	}
	ordinals := make([]int, 0, len(j.pending))
	for _, p := range j.pending {
		r.files[p.ordinal].Completed = true
		r.files[p.ordinal].SHA256 = p.sha
		ordinals = append(ordinals, p.ordinal)
		j.completedSum += j.sizes[p.ordinal]
	}
	r.next, r.nextOffset, r.durableBytes = j.expect, 0, j.completedSum
	r.t.UpdatedAt = now()
	err = e.commitFiles(r, ordinals)
	status := r.status()
	e.mu.Unlock()
	j.pending, j.pendingBytes = nil, 0
	j.checkpointAt = time.Now()
	if err != nil {
		return err
	}
	e.send(j.conn, status)
	return nil
}

// checkpoint records a large file's verified prefix so a resume continues
// from the last segment boundary.
func (j *recvJob) checkpoint() error {
	if err := j.commit(); err != nil {
		return err
	}
	if j.cur == nil || j.segStart == 0 {
		return nil
	}
	if err := syncData(j.cur); err != nil {
		return storageError(err)
	}
	if err := barrier(j.cur); err != nil {
		return storageError(err)
	}
	e := j.e
	e.mu.Lock()
	r := e.records[j.id]
	if r == nil {
		e.mu.Unlock()
		return errStopped
	}
	r.next, r.nextOffset, r.durableBytes = j.curOrdinal, j.segStart, j.completedSum+j.segStart
	r.t.UpdatedAt = now()
	err := e.saveHeader(r)
	status := r.status()
	e.mu.Unlock()
	j.checkpointAt = time.Now()
	if err != nil {
		return err
	}
	e.send(j.conn, status)
	return nil
}

// publishFile moves a finished partial file into place without ever
// replacing an existing file.
func publishFile(root *os.Root, oldName, newName string) error {
	err := renameNoReplace(root, oldName, newName)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	// Some file systems lack an exclusive rename; a hard link also refuses an
	// existing name.
	if linkErr := root.Link(oldName, newName); linkErr == nil {
		return root.Remove(oldName)
	} else if errors.Is(linkErr, fs.ErrExist) {
		return linkErr
	}
	if _, statErr := root.Lstat(newName); statErr == nil {
		return fs.ErrExist
	}
	return root.Rename(oldName, newName)
}

func (j *recvJob) publish(ordinal int, f fileEntry) (string, error) {
	dest := f.DestinationPath
	dir := path.Dir(dest)
	if dir != "." {
		if err := j.root.MkdirAll(filepath.FromSlash(dir), 0755); err != nil {
			return "", err
		}
	}
	base := localComponent(path.Base(f.Path), runtime.GOOS)
	partial := partialName(j.id, ordinal)
	for n := 2; ; n++ {
		err := publishFile(j.root, partial, filepath.FromSlash(dest))
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || n > 10000 {
			return "", err
		}
		// Someone created this name meanwhile: record the alternative before
		// using it, so crash recovery knows where the file went.
		dest = path.Join(dir, numberedName(base, n, false))
		e := j.e
		e.mu.Lock()
		if r := e.records[j.id]; r != nil {
			r.files[ordinal].DestinationPath = dest
			err = e.saveDestinations(r, []int{ordinal})
		}
		e.mu.Unlock()
		if err != nil {
			return "", err
		}
	}
	if f.Modified > 0 {
		at := time.UnixMilli(f.Modified)
		_ = j.root.Chtimes(filepath.FromSlash(dest), at, at)
	}
	return dest, nil
}

// finish ends the job: on completion it publishes the rest; otherwise it
// keeps every verified byte for a later resume.
func (j *recvJob) finish(cause error) {
	j.cancel()
	for drained := false; !drained; {
		select {
		case op := <-j.ops:
			if op.data != nil && op.release != nil {
				op.release(op.data)
			}
		default:
			drained = true
		}
	}
	e := j.e
	var failure error
	switch {
	case j.root == nil:
		failure = cause
	case errors.Is(cause, errComplete):
		if err := j.commit(); err != nil {
			failure = err
		}
	case errors.Is(cause, errDisconnected) || errors.Is(cause, errStopped):
		if err := j.commit(); err != nil {
			failure = err
		} else if err = j.checkpoint(); err != nil {
			failure = err
		}
	default:
		failure = cause
		for _, p := range j.pending {
			if p.file != nil {
				p.file.Close()
			}
		}
		j.pending = nil
	}
	if j.cur != nil {
		j.cur.Close()
		j.cur = nil
	}
	e.mu.Lock()
	if e.receivers[j.id] == j {
		delete(e.receivers, j.id)
	}
	if j.stream != 0 && e.streams[streamKey{j.conn, j.stream}] == j {
		delete(e.streams, streamKey{j.conn, j.stream})
	}
	r := e.records[j.id]
	var status *statusMsg
	if r != nil && !terminal(r.t.State) {
		switch {
		case failure != nil:
			r.t.State = "failed"
			if errors.Is(failure, errProtocol) {
				r.t.Error = "connection: " + failure.Error()
			} else {
				r.t.Error = failure.Error()
			}
			s := r.status()
			status = &s
		case errors.Is(cause, errComplete):
			r.t.State, r.t.Error = "completed", ""
			r.next, r.nextOffset, r.durableBytes = len(r.files), 0, r.t.TotalBytes
			s := r.status()
			status = &s
			if j.root != nil {
				_ = j.root.Remove(filepath.Join(partialsDir, j.id))
				_ = j.root.Remove(partialsDir)
			}
		case errors.Is(cause, errDisconnected):
			r.t.State = "reconnecting"
		}
		r.t.UpdatedAt = now()
		r.live = r.durableBytes
		_ = e.saveHeader(r)
	}
	e.mu.Unlock()
	if j.root != nil {
		j.root.Close()
	}
	if status != nil {
		e.send(j.conn, *status)
	}
	e.changed()
}

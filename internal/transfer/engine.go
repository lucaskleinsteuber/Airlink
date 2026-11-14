// Package transfer owns transfer state and runs AirLink protocol 3 transfers:
// streamed manifests, pipelined file data with per-segment SHA-256
// verification, batched durable checkpoints on the receiver and
// collision-free publication without overwriting existing files.
package transfer

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("transfer state conflict")
	ErrInvalid       = errors.New("invalid transfer input")
	ErrIntegrity     = errors.New("file integrity check failed")
	ErrSourceChanged = errors.New("source changed or became unavailable; select the original file again")
)

const (
	// SegmentSize is the verification and resume unit within a file.
	SegmentSize = 32 << 20
	partialsDir = ".airlink-partials"
	// MaxFiles bounds one offer's manifest.
	MaxFiles = 1_000_000
)

// Conn is one authenticated connection to a peer (implemented by peer.Session).
type Conn interface {
	PeerID() string
	Send(v any) error
	OpenStream() uint32
	SendOrdered(stream uint32, v any) error
	SendData(ctx context.Context, stream uint32, payload []byte, release func()) error
	CloseStream(stream uint32)
	DropStream(stream uint32)
	Done() <-chan struct{}
}

// Options connects the engine to the rest of the service.
type Options struct {
	// Session returns the active connection to a device or nil.
	Session func(deviceID string) Conn
	// Connect asks for a connection attempt to a device.
	Connect func(deviceID string)
	// Device returns the current paired-device record.
	Device func(deviceID string) (model.Device, bool)
	// Wait applies the configured speed limit; Add counts payload traffic.
	Wait func(ctx context.Context, n int, direction string) error
	Add  func(n int, direction string)
	// Changed is called after visible state changed (may be nil).
	Changed func()
}

type fileEntry struct {
	model.File
	source   string
	identity string
}

type record struct {
	t            model.Transfer
	files        []fileEntry // nil until loaded
	preview      []fileEntry
	next         int
	nextOffset   int64
	durableBytes int64
	live         int64
	liveOrdinal  int
	liveOffset   int64
	requestID    string
	offered      bool
	rateAt       time.Time
	rateBytes    int64
}

// Engine is safe for concurrent use.
type Engine struct {
	mu        sync.Mutex
	db        *sql.DB
	dataDir   string
	receive   string
	records   map[string]*record
	senders   map[string]*sendJob
	receivers map[string]*recvJob
	streams   map[streamKey]*recvJob
	offers    map[string]*pendingOffer
	opts      Options
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closed    bool
}

type streamKey struct {
	conn   Conn
	stream uint32
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func fileID(ordinal int) string { return strconv.Itoa(ordinal) }

func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled"
}

// New opens the transfer store in dataDir. receiveDir is the destination for
// new incoming transfers.
func New(dataDir, receiveDir string, opts Options) (*Engine, error) {
	if !filepath.IsAbs(dataDir) || !filepath.IsAbs(receiveDir) {
		return nil, fmt.Errorf("%w: directories must be absolute", ErrInvalid)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(receiveDir, 0700); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(receiveDir)
	if err != nil {
		return nil, err
	}
	db, err := openStore(dataDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{db: db, dataDir: dataDir, receive: canonical, records: map[string]*record{}, senders: map[string]*sendJob{}, receivers: map[string]*recvJob{}, streams: map[streamKey]*recvJob{}, offers: map[string]*pendingOffer{}, opts: opts, ctx: ctx, cancel: cancel}
	if err = e.migrateLegacy(); err != nil {
		cancel()
		db.Close()
		return nil, fmt.Errorf("transfer history migration: %w", err)
	}
	if err = e.loadHeaders(); err != nil {
		cancel()
		db.Close()
		return nil, err
	}
	if err = e.loadPreviews(); err != nil {
		cancel()
		db.Close()
		return nil, err
	}
	for _, r := range e.records {
		if terminal(r.t.State) {
			continue
		}
		if err = e.loadFiles(r); err != nil {
			cancel()
			db.Close()
			return nil, err
		}
		// A restart never asserts a live connection. Confirmed progress is kept
		// and the transfer continues when the peer connects again.
		if r.t.State == "running" || r.t.State == "queued" || r.t.State == "verifying" {
			r.t.State = "reconnecting"
			if err = e.saveHeader(r); err != nil {
				cancel()
				db.Close()
				return nil, err
			}
		}
	}
	return e, nil
}

func (e *Engine) loadPreviews() error {
	rows, err := e.db.Query(`SELECT transfer_id,ordinal,path,size,directory,completed,destination FROM files WHERE ordinal < 3 ORDER BY transfer_id,ordinal`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var f fileEntry
		var ordinal, directory, completed int
		if err = rows.Scan(&id, &ordinal, &f.Path, &f.Size, &directory, &completed, &f.DestinationPath); err != nil {
			return err
		}
		if r := e.records[id]; r != nil {
			f.ID, f.Directory, f.Completed = fileID(ordinal), directory == 1, completed == 1
			r.preview = append(r.preview, f)
		}
	}
	return rows.Err()
}

// Close stops all transfer work and closes the store.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.cancel()
	for _, j := range e.senders {
		j.cancel()
	}
	// Finished files are committed and partial files checkpointed, so a
	// restart continues where this one stopped.
	for _, j := range e.receivers {
		j.stop(errStopped)
	}
	e.mu.Unlock()
	e.wg.Wait()
	return e.db.Close()
}

// Busy reports whether a transfer is moving data right now; the app then
// receives progress updates several times per second.
func (e *Engine) Busy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range e.records {
		if r.t.State == "running" {
			return true
		}
	}
	return false
}

func (e *Engine) changed() {
	if e.opts.Changed != nil {
		e.opts.Changed()
	}
}

// view converts a record to the public model. Callers hold e.mu.
func (r *record) view(withFiles bool) model.Transfer {
	t := r.t
	t.Files = []model.File{}
	if r.files != nil {
		t.FileCount = len(r.files)
	}
	if !terminal(t.State) {
		t.CompletedBytes = max(r.live, r.durableBytes)
	} else {
		t.CompletedBytes = r.durableBytes
	}
	if t.State != "running" {
		t.BytesPerSecond = 0
	}
	t.Problem = model.DescribeTransferError(t.Error)
	t.Retryable = model.CanRetryTransfer(t)
	if withFiles && r.files != nil {
		t.Files = make([]model.File, len(r.files))
		for i := range r.files {
			t.Files[i] = r.fileView(i)
		}
	}
	if t.State == "completed" && t.Direction == "receive" {
		t.HasContent = t.FileCount > 0
	}
	return t
}

func (r *record) fileView(i int) model.File {
	f := r.files[i].File
	switch {
	case f.Completed:
		f.Received = f.Size
	case i == r.liveOrdinal && r.liveOffset > 0 && !terminal(r.t.State):
		f.Received = min(r.liveOffset, f.Size)
	case i == r.next:
		f.Received = r.nextOffset
	}
	if r.t.Direction == "send" {
		f.DestinationPath = ""
	}
	return f
}

func (e *Engine) get(id string) (*record, error) {
	if e.closed {
		return nil, errors.New("transfer engine closed")
	}
	r := e.records[id]
	if r == nil {
		return nil, ErrNotFound
	}
	return r, nil
}

// Get returns a transfer including its manifest.
func (e *Engine) Get(id string) (model.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.get(id)
	if err != nil {
		return model.Transfer{}, err
	}
	if err = e.loadFiles(r); err != nil {
		return model.Transfer{}, err
	}
	return r.view(true), nil
}

// Header returns a transfer without its manifest.
func (e *Engine) Header(id string) (model.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.get(id)
	if err != nil {
		return model.Transfer{}, err
	}
	return r.view(false), nil
}

// Transfers returns compact transfers (with at most three preview files),
// newest first. An empty deviceID selects all devices.
func (e *Engine) Transfers(deviceID string) []model.Transfer {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []model.Transfer{}
	for _, r := range e.records {
		if deviceID == "" || r.t.DeviceID == deviceID {
			out = append(out, e.summary(r))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func (e *Engine) summary(r *record) model.Transfer {
	t := r.view(false)
	t.Summary = true
	source := r.preview
	if r.files != nil {
		source = r.files[:min(3, len(r.files))]
	}
	t.Files = make([]model.File, 0, len(source))
	for i, f := range source {
		v := f.File
		if r.files != nil {
			v = r.fileView(i)
		}
		if t.Direction == "send" {
			v.DestinationPath = ""
		}
		t.Files = append(t.Files, v)
	}
	if len(t.Files) > 0 {
		t.Title = t.Files[0].Path
	}
	return t
}

// StatusSummaries returns bounded previews: all active transfers and the three
// newest finished ones per device, at most 200 entries.
func (e *Engine) StatusSummaries() ([]model.Transfer, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	items := make([]*record, 0, len(e.records))
	for _, r := range e.records {
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].t, items[j].t
		if terminal(a.State) != terminal(b.State) {
			return !terminal(a.State)
		}
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.ID > b.ID
	})
	out := []model.Transfer{}
	recent := map[string]int{}
	active := 0
	for _, r := range items {
		if !terminal(r.t.State) {
			active++
		} else {
			if recent[r.t.DeviceID] >= 3 {
				continue
			}
			recent[r.t.DeviceID]++
		}
		if len(out) < 200 {
			out = append(out, e.summary(r))
		}
	}
	return out, active
}

// SummaryQuery filters local history pages.
type SummaryQuery struct {
	Cursor    string `json:"cursor"`
	Limit     int    `json:"limit"`
	DeviceID  string `json:"deviceId"`
	State     string `json:"state"`
	Direction string `json:"direction"`
	Query     string `json:"query"`
}

type SummaryPage struct {
	Transfers []model.Transfer `json:"transfers"`
	Next      string           `json:"nextCursor,omitempty"`
}

// SummaryPage pages transfers by creation time (cursor is "createdAt|id").
func (e *Engine) SummaryPage(q SummaryQuery) (SummaryPage, error) {
	var beforeCreated, beforeID string
	if q.Cursor != "" {
		parts := strings.SplitN(q.Cursor, "|", 2)
		if len(parts) != 2 || len(q.Cursor) > 256 {
			return SummaryPage{}, ErrInvalid
		}
		beforeCreated, beforeID = parts[0], parts[1]
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	q.Limit = min(q.Limit, 100)
	query := strings.ToLower(strings.TrimSpace(q.Query))
	var matches map[string]bool
	if query != "" {
		var err error
		if matches, err = e.searchTransfers(query); err != nil {
			return SummaryPage{}, err
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return SummaryPage{}, errors.New("transfer engine closed")
	}
	items := []*record{}
	for _, r := range e.records {
		t := &r.t
		if q.DeviceID != "" && t.DeviceID != q.DeviceID || q.Direction != "" && t.Direction != q.Direction {
			continue
		}
		if q.State == "active" && terminal(t.State) || q.State == "finished" && !terminal(t.State) || q.State != "" && q.State != "active" && q.State != "finished" && t.State != q.State {
			continue
		}
		if beforeID != "" && (t.CreatedAt > beforeCreated || t.CreatedAt == beforeCreated && t.ID >= beforeID) {
			continue
		}
		if query != "" && !matches[t.ID] && !strings.Contains(strings.ToLower(t.DeviceName), query) {
			continue
		}
		items = append(items, r)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].t, items[j].t
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt > b.CreatedAt
		}
		return a.ID > b.ID
	})
	out := SummaryPage{Transfers: []model.Transfer{}}
	for _, r := range items[:min(q.Limit, len(items))] {
		out.Transfers = append(out.Transfers, e.summary(r))
	}
	if len(items) > q.Limit {
		last := out.Transfers[len(out.Transfers)-1]
		out.Next = last.CreatedAt + "|" + last.ID
	}
	return out, nil
}

type FilePage struct {
	Files []model.File `json:"files"`
	Next  int          `json:"nextOffset"`
	Total int          `json:"total"`
}

// FilePage returns up to 100 manifest entries starting at offset.
func (e *Engine) FilePage(id string, offset, limit int) (FilePage, error) {
	if offset < 0 || limit < 0 {
		return FilePage{}, ErrInvalid
	}
	if limit == 0 {
		limit = 50
	}
	limit = min(limit, 100)
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.get(id)
	if err != nil {
		return FilePage{}, err
	}
	if err = e.loadFiles(r); err != nil {
		return FilePage{}, err
	}
	if offset > len(r.files) {
		return FilePage{}, ErrInvalid
	}
	end := min(offset+limit, len(r.files))
	page := FilePage{Files: make([]model.File, 0, end-offset), Next: -1, Total: len(r.files)}
	for i := offset; i < end; i++ {
		page.Files = append(page.Files, r.fileView(i))
	}
	if end < len(r.files) {
		page.Next = end
	}
	if terminal(r.t.State) {
		r.files = nil // history manifests are loaded on demand only
	}
	return page, nil
}

// HistoryFilter selects history entries for the history.list method.
type HistoryFilter struct {
	Query     string `json:"query"`
	State     string `json:"state"`
	Direction string `json:"direction"`
	DeviceID  string `json:"deviceId"`
	Limit     int    `json:"limit"`
}

// History returns matching transfers with bounded file previews.
func (e *Engine) History(f HistoryFilter) ([]model.Transfer, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	page, err := e.SummaryPage(SummaryQuery{Limit: 100, DeviceID: f.DeviceID, State: f.State, Direction: f.Direction, Query: f.Query})
	if err != nil {
		return nil, err
	}
	out := page.Transfers
	for page.Next != "" && len(out) < min(limit, 500) {
		page, err = e.SummaryPage(SummaryQuery{Cursor: page.Next, Limit: 100, DeviceID: f.DeviceID, State: f.State, Direction: f.Direction, Query: f.Query})
		if err != nil {
			return nil, err
		}
		out = append(out, page.Transfers...)
	}
	return out[:min(len(out), min(limit, 500))], nil
}

// RemoveHistory deletes one finished transfer's metadata. Files stay.
func (e *Engine) RemoveHistory(id string) error {
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, "/\\") {
		return ErrInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.get(id)
	if err != nil {
		return err
	}
	if !terminal(r.t.State) {
		return ErrConflict
	}
	if err = e.deleteRecords([]string{id}); err != nil {
		return err
	}
	delete(e.records, id)
	return nil
}

// ClearHistory deletes finished transfers' metadata for one or all devices.
func (e *Engine) ClearHistory(deviceID string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	ids := []string{}
	for id, r := range e.records {
		if terminal(r.t.State) && (deviceID == "" || r.t.DeviceID == deviceID) {
			ids = append(ids, id)
		}
	}
	if err := e.deleteRecords(ids); err != nil {
		return 0, err
	}
	for _, id := range ids {
		delete(e.records, id)
	}
	return len(ids), nil
}

// DiagnosticSummary contains only aggregate counts.
type DiagnosticSummary struct {
	Transfers      int            `json:"transfers"`
	Files          int            `json:"files"`
	States         map[string]int `json:"states"`
	Problems       map[string]int `json:"problems"`
	ConfirmedBytes int64          `json:"confirmedBytes"`
}

func (e *Engine) Diagnostics() DiagnosticSummary {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := DiagnosticSummary{States: map[string]int{}, Problems: map[string]int{}}
	for _, r := range e.records {
		out.Transfers++
		out.Files += r.t.FileCount
		out.ConfirmedBytes += r.durableBytes
		state := "other"
		switch r.t.State {
		case "offered", "queued", "running", "paused", "reconnecting", "completed", "failed", "cancelled":
			state = r.t.State
		}
		out.States[state]++
		if p := model.DescribeTransferError(r.t.Error); p != nil {
			out.Problems[p.Code]++
		}
	}
	return out
}

// SetReceiveDirectory changes the destination of new incoming transfers. commit
// persists the setting; on failure the previous directory stays active.
func (e *Engine) SetReceiveDirectory(dir string, commit func() error) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: absolute directory required", ErrInvalid)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return err
	}
	root.Close()
	e.mu.Lock()
	defer e.mu.Unlock()
	previous := e.receive
	e.receive = canonical
	if commit != nil {
		if err = commit(); err != nil {
			e.receive = previous
			return err
		}
	}
	return nil
}

// ReceivedResult lists published top-level results of a completed receive.
type ReceivedResult struct {
	Directory string         `json:"directory"`
	Items     []ReceivedItem `json:"items"`
}

type ReceivedItem struct {
	FileID    string `json:"fileId"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
}

// ReceivedItems resolves the published results of a completed receive for a
// local reveal action. It never follows links out of the receive folder.
func (e *Engine) ReceivedItems(id string) (ReceivedResult, error) {
	e.mu.Lock()
	r, err := e.get(id)
	if err == nil {
		err = e.loadFiles(r)
	}
	if err != nil {
		e.mu.Unlock()
		return ReceivedResult{}, err
	}
	if r.t.Direction != "receive" || r.t.State != "completed" {
		e.mu.Unlock()
		return ReceivedResult{}, ErrConflict
	}
	destination := r.t.Destination
	tops := map[string]ReceivedItem{}
	order := []string{}
	for i, f := range r.files {
		if f.DestinationPath == "" || !f.Completed {
			continue
		}
		top := strings.SplitN(f.DestinationPath, "/", 2)[0]
		directory := f.Directory || strings.Contains(f.DestinationPath, "/")
		if _, ok := tops[top]; !ok {
			tops[top] = ReceivedItem{FileID: fileID(i), Path: filepath.Join(destination, filepath.FromSlash(top)), Directory: directory}
			order = append(order, top)
		}
	}
	e.mu.Unlock()
	root, err := os.OpenRoot(destination)
	if err != nil {
		return ReceivedResult{}, err
	}
	defer root.Close()
	out := ReceivedResult{Directory: destination, Items: []ReceivedItem{}}
	for _, top := range order {
		info, err := root.Lstat(filepath.FromSlash(top))
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || info.IsDir() != tops[top].Directory {
			continue
		}
		out.Items = append(out.Items, tops[top])
	}
	if len(out.Items) == 0 {
		return ReceivedResult{}, ErrNotFound
	}
	return out, nil
}

// Package relay brokers bounded RAM blocks. SQLite contains metadata only;
// source payloads are never written to disk by this package.
package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound    = errors.New("relay not found")
	ErrForbidden   = errors.New("relay permission denied")
	ErrConflict    = errors.New("relay state changed; refresh and retry")
	ErrInvalid     = errors.New("invalid relay manifest or block")
	ErrIntegrity   = errors.New("source changed or integrity verification failed")
	ErrUnavailable = errors.New("source is unavailable; keep its browser and selected files open")
)

type interval struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}
type record struct {
	Relay            model.Relay                 `json:"relay"`
	Accepted         bool                        `json:"accepted"`
	Epoch            int64                       `json:"epoch"`
	SuccessfulStream bool                        `json:"successfulStream"`
	Hidden           map[string]bool             `json:"hidden"`
	Blocks           map[string]map[int64]string `json:"blocks"`
	Ranges           map[string][]interval       `json:"ranges"`
}
type Store struct {
	mu      sync.Mutex
	db      *sql.DB
	records map[string]*record
	ops     map[string]map[string]*Stream
	pending map[string]*Block
	slots   chan struct{}
	closed  bool
}
type Stream struct {
	store                   *Store
	id, relayID, receiverID string
	epoch                   int64
	ctx                     context.Context
	cancel                  context.CancelFunc
	hasSlot                 bool
	once                    sync.Once
}
type Block struct {
	Request model.RelayRequest
	op      *Stream
	file    model.File
	data    chan []byte
	done    chan error
	claimed bool
	hash    string
	started time.Time
}

func New(dataDir string) (*Store, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "relays.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL;PRAGMA synchronous=FULL;PRAGMA busy_timeout=5000;CREATE TABLE IF NOT EXISTS relays(id TEXT PRIMARY KEY,document BLOB NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(filepath.Join(dataDir, "relays.sqlite"), 0600)
	s := &Store{db: db, records: map[string]*record{}, ops: map[string]map[string]*Stream{}, pending: map[string]*Block{}, slots: make(chan struct{}, 2)}
	rows, err := db.Query("SELECT document FROM relays")
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			break
		}
		var r record
		if err = json.Unmarshal(b, &r); err != nil {
			break
		}
		if r.Hidden == nil {
			r.Hidden = map[string]bool{}
		}
		if r.Blocks == nil {
			r.Blocks = map[string]map[int64]string{}
		}
		if r.Ranges == nil {
			r.Ranges = map[string][]interval{}
		}
		if r.Relay.State == "running" || r.Relay.State == "queued" {
			r.Relay.State = "reconnecting"
			r.Relay.BytesPerSecond = 0
		}
		s.records[r.Relay.ID] = &r
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		db.Close()
		return nil, err
	}
	for _, r := range s.records {
		if err = s.saveLocked(r); err != nil {
			db.Close()
			return nil, err
		}
	}
	return s, nil
}
func randomID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func clone(v model.Relay) model.Relay { v.Files = append([]model.File{}, v.Files...); return v }
func terminal(state string) bool {
	return state == "delivered" || state == "failed" || state == "cancelled"
}
func validHash(hash string) bool {
	b, err := hex.DecodeString(hash)
	return err == nil && len(b) == 32 && strings.ToLower(hash) == hash
}
func (s *Store) saveLocked(r *record) error {
	r.Relay.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO relays(id,document)VALUES(?,?) ON CONFLICT(id)DO UPDATE SET document=excluded.document", r.Relay.ID, b)
	return err
}
func (s *Store) getLocked(id, actor string) (*record, error) {
	if s.closed {
		return nil, ErrUnavailable
	}
	r := s.records[id]
	if r == nil {
		return nil, ErrNotFound
	}
	if actor != "" && (actor != r.Relay.SenderID && actor != r.Relay.ReceiverID || r.Hidden[actor]) {
		return nil, ErrNotFound
	}
	return r, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for _, ops := range s.ops {
		for _, op := range ops {
			op.cancel()
		}
	}
	return s.db.Close()
}
func (s *Store) Get(id, actor string) (model.Relay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(id, actor)
	if err != nil {
		return model.Relay{}, err
	}
	return clone(r.Relay), nil
}
func (s *Store) List(actor string) []model.Relay {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Relay{}
	for _, r := range s.records {
		if actor == "" || ((actor == r.Relay.SenderID || actor == r.Relay.ReceiverID) && !r.Hidden[actor]) {
			out = append(out, clone(r.Relay))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// ActiveSummaries is for the desktop's recurring status. Do not copy retained
// relay manifests or their transport bookkeeping just to draw a tray badge.
func (s *Store) ActiveSummaries() ([]model.Relay, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Relay{}
	active := 0
	for _, r := range s.records {
		v := r.Relay
		if v.State == "delivered" || v.State == "failed" || v.State == "cancelled" {
			continue
		}
		active++
		if len(out) >= 100 {
			continue
		}
		v.Files = append([]model.File{}, v.Files[:min(3, len(v.Files))]...)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, active
}
func cleanFilePath(p string) bool {
	if p == "" || p == "." || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "" || strings.TrimRight(part, ". ") != part {
			return false
		}
		for _, c := range part {
			if unicode.IsControl(c) || strings.ContainsRune(`<>"|?*`, c) {
				return false
			}
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
			return false
		}
	}
	return true
}
func normalize(files []model.File) ([]model.File, int64, error) {
	if len(files) == 0 || len(files) > 20000 {
		return nil, 0, ErrInvalid
	}
	out := make([]model.File, len(files))
	names := map[string]bool{}
	ids := map[string]bool{}
	var total int64
	empty := sha256.Sum256(nil)
	for i, f := range files {
		f.DestinationPath = ""
		f.DuplicateCandidates = nil
		f.DuplicateState = ""
		f.Skipped = false
		validID := len(f.ID) > 0 && len(f.ID) <= 128
		for _, c := range f.ID {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
				validID = false
			}
		}
		name := strings.ToLower(f.Path)
		_, nameExists := names[name]
		if !validID || !cleanFilePath(f.Path) || ids[f.ID] || nameExists || f.Size < 0 || f.Size > int64(^uint64(0)>>1)-total {
			return nil, 0, ErrInvalid
		}
		if f.Directory {
			if f.Size != 0 {
				return nil, 0, ErrInvalid
			}
			f.SHA256 = ""
		} else if !validHash(f.SHA256) || (f.Size == 0 && f.SHA256 != hex.EncodeToString(empty[:])) {
			return nil, 0, ErrInvalid
		}
		ids[f.ID] = true
		names[name] = f.Directory
		f.Received = 0
		f.Completed = f.Directory
		out[i] = f
		total += f.Size
	}
	for _, f := range out {
		for parent := path.Dir(strings.ToLower(f.Path)); parent != "."; parent = path.Dir(parent) {
			if directory, exists := names[parent]; exists && !directory {
				return nil, 0, ErrInvalid
			}
		}
	}
	return out, total, nil
}
func (s *Store) Create(sender, receiver model.Device, files []model.File) (model.Relay, error) {
	if sender.ID == receiver.ID || sender.ID == "" || receiver.ID == "" || !sender.Paired || !receiver.Paired || sender.Kind != "browser" || receiver.Kind != "browser" || !sender.CanRelay || !sender.CanSend || !receiver.CanRelay || !receiver.CanReceive {
		return model.Relay{}, ErrForbidden
	}
	files, total, err := normalize(files)
	if err != nil {
		return model.Relay{}, err
	}
	r := &record{Relay: model.Relay{ID: randomID(), SenderID: sender.ID, SenderName: sender.Name, ReceiverID: receiver.ID, ReceiverName: receiver.Name, State: "offered", Files: files, TotalBytes: total, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, Hidden: map[string]bool{}, Blocks: map[string]map[int64]string{}, Ranges: map[string][]interval{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.Relay{}, ErrUnavailable
	}
	if err = s.saveLocked(r); err != nil {
		return model.Relay{}, err
	}
	s.records[r.Relay.ID] = r
	return clone(r.Relay), nil
}
func (s *Store) Accept(id, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(id, actor)
	if err != nil {
		return err
	}
	if actor != r.Relay.ReceiverID {
		return ErrForbidden
	}
	if r.Accepted {
		return nil
	}
	if r.Relay.State != "offered" {
		return ErrConflict
	}
	r.Accepted = true
	r.Relay.State = "ready"
	return s.saveLocked(r)
}
func (s *Store) Action(id, actor, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(id, actor)
	if err != nil {
		return err
	}
	if terminal(r.Relay.State) {
		return ErrConflict
	}
	switch action {
	case "pause":
		if !r.Accepted {
			return ErrConflict
		}
		r.Relay.State = "paused"
	case "resume":
		if !r.Accepted || r.Relay.State != "paused" && r.Relay.State != "reconnecting" {
			return ErrConflict
		}
		r.Relay.State = "ready"
		r.Relay.Error = ""
	case "cancel":
		r.Relay.State = "cancelled"
	default:
		return ErrInvalid
	}
	r.Epoch++
	r.Relay.BytesPerSecond = 0
	s.cancelOpsLocked(id)
	return s.saveLocked(r)
}
func (s *Store) cancelOpsLocked(id string) {
	for _, op := range s.ops[id] {
		op.cancel()
	}
}
func (s *Store) CancelDevice(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for relayID, r := range s.records {
		if r.Relay.SenderID != id && r.Relay.ReceiverID != id {
			continue
		}
		s.cancelOpsLocked(relayID)
		if !terminal(r.Relay.State) {
			r.Epoch++
			r.Relay.State = "cancelled"
			r.Relay.Error = "Device permission revoked"
			r.Relay.BytesPerSecond = 0
			_ = s.saveLocked(r)
		}
	}
}
func (s *Store) FailForStream(op *Stream, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[op.relayID]
	if r == nil || r.Epoch != op.epoch || op.ctx.Err() != nil || s.ops[op.relayID][op.id] != op {
		return ErrConflict
	}
	return s.failLocked(r)
}
func (s *Store) failLocked(r *record) error {
	r.Epoch++
	r.Relay.State = "failed"
	r.Relay.Error = ErrIntegrity.Error()
	r.Relay.BytesPerSecond = 0
	s.cancelOpsLocked(r.Relay.ID)
	return s.saveLocked(r)
}

// Interrupt preserves delivered history while aborting an active retry.
func (s *Store) Interrupt(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(id, "")
	if err != nil {
		return err
	}
	r.Epoch++
	r.Relay.BytesPerSecond = 0
	s.cancelOpsLocked(id)
	return s.saveLocked(r)
}
func (s *Store) Clear(owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.records {
		if !terminal(r.Relay.State) || len(s.ops[id]) > 0 {
			continue
		}
		if owner != "" && (owner != r.Relay.SenderID && owner != r.Relay.ReceiverID || r.Hidden[owner]) {
			continue
		}
		if owner == "" {
			if _, err := s.db.Exec("DELETE FROM relays WHERE id=?", id); err != nil {
				return err
			}
			delete(s.records, id)
		} else {
			r.Hidden[owner] = true
			if err := s.saveLocked(r); err != nil {
				return err
			}
		}
	}
	return nil
}
func allComplete(files []model.File) bool {
	for _, f := range files {
		if !f.Completed {
			return false
		}
	}
	return true
}
func findFile(r *record, id string) (int, error) {
	for i := range r.Relay.Files {
		if r.Relay.Files[i].ID == id {
			return i, nil
		}
	}
	return 0, ErrNotFound
}

func (s *Store) BeginStream(ctx context.Context, id, receiver string) (*Stream, error) {
	s.mu.Lock()
	r, err := s.getLocked(id, receiver)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if receiver != r.Relay.ReceiverID {
		s.mu.Unlock()
		return nil, ErrForbidden
	}
	if !r.Accepted || r.Relay.State == "paused" || r.Relay.State == "cancelled" || r.Relay.State == "failed" {
		s.mu.Unlock()
		return nil, ErrConflict
	}
	if r.Hidden[r.Relay.SenderID] {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	streamCtx, cancel := context.WithCancel(ctx)
	op := &Stream{store: s, id: randomID(), relayID: id, receiverID: receiver, epoch: r.Epoch, ctx: streamCtx, cancel: cancel}
	if s.ops[id] == nil {
		s.ops[id] = map[string]*Stream{}
	}
	s.ops[id][op.id] = op
	if r.Relay.State != "delivered" {
		r.Relay.State = "queued"
		_ = s.saveLocked(r)
	}
	s.mu.Unlock()
	select {
	case s.slots <- struct{}{}:
		op.hasSlot = true
	case <-streamCtx.Done():
		op.Close(false)
		return nil, streamCtx.Err()
	}
	s.mu.Lock()
	r, err = s.getLocked(id, receiver)
	if err == nil && (r.Epoch != op.epoch || streamCtx.Err() != nil) {
		err = ErrConflict
	}
	if err == nil && r.Hidden[r.Relay.SenderID] {
		err = ErrUnavailable
	}
	if err == nil && r.Relay.State != "delivered" {
		r.Relay.State = "running"
		err = s.saveLocked(r)
	}
	s.mu.Unlock()
	if err != nil {
		op.Close(false)
		return nil, err
	}
	return op, nil
}
func (op *Stream) Context() context.Context { return op.ctx }
func (op *Stream) Interrupt()               { op.cancel() }
func (op *Stream) Close(success bool) {
	op.once.Do(func() {
		op.cancel()
		s := op.store
		s.mu.Lock()
		delete(s.ops[op.relayID], op.id)
		if len(s.ops[op.relayID]) == 0 {
			delete(s.ops, op.relayID)
		}
		for id, p := range s.pending {
			if p.op == op {
				delete(s.pending, id)
				select {
				case p.done <- ErrConflict:
				default:
				}
			}
		}
		if r := s.records[op.relayID]; r != nil && !s.closed && r.Epoch == op.epoch && !terminal(r.Relay.State) && r.Relay.State != "paused" {
			if success {
				r.SuccessfulStream = true
			}
			if allComplete(r.Relay.Files) && r.SuccessfulStream {
				r.Relay.State = "delivered"
			} else if len(s.ops[op.relayID]) > 0 {
				r.Relay.State = "running"
			} else if success {
				r.Relay.State = "ready"
			} else {
				r.Relay.State = "reconnecting"
			}
			r.Relay.BytesPerSecond = 0
			_ = s.saveLocked(r)
		}
		if r := s.records[op.relayID]; r != nil && !s.closed && r.Epoch == op.epoch && len(s.ops[op.relayID]) == 0 && r.Relay.BytesPerSecond != 0 {
			r.Relay.BytesPerSecond = 0
			_ = s.saveLocked(r)
		}
		s.mu.Unlock()
		if op.hasSlot {
			<-s.slots
		}
	})
}
func (s *Store) Fetch(op *Stream, fileID string, index int64) ([]byte, *Block, error) {
	s.mu.Lock()
	r, err := s.getLocked(op.relayID, op.receiverID)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	i, err := findFile(r, fileID)
	if err != nil {
		s.mu.Unlock()
		return nil, nil, err
	}
	f := r.Relay.Files[i]
	chunks := f.Size / model.ChunkSize
	if f.Size%model.ChunkSize != 0 {
		chunks++
	}
	if op.ctx.Err() != nil || r.Epoch != op.epoch || f.Directory || index < 0 || index >= chunks {
		s.mu.Unlock()
		return nil, nil, ErrConflict
	}
	if r.Hidden[r.Relay.SenderID] {
		s.mu.Unlock()
		return nil, nil, ErrUnavailable
	}
	p := &Block{Request: model.RelayRequest{RequestID: randomID(), FileID: fileID, Index: index}, op: op, file: f, data: make(chan []byte, 1), done: make(chan error, 1), started: time.Now()}
	s.pending[p.Request.RequestID] = p
	s.mu.Unlock()
	select {
	case b := <-p.data:
		return b, p, nil
	case <-op.ctx.Done():
		s.mu.Lock()
		delete(s.pending, p.Request.RequestID)
		s.mu.Unlock()
		return nil, nil, op.ctx.Err()
	}
}
func (s *Store) NextRequest(id, sender string) (*model.RelayRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(id, sender)
	if err != nil {
		return nil, err
	}
	if sender != r.Relay.SenderID {
		return nil, ErrForbidden
	}
	if r.Relay.State == "failed" || r.Relay.State == "cancelled" {
		return nil, ErrConflict
	}
	for _, p := range s.pending {
		if p.op.relayID == id && !p.claimed && p.op.ctx.Err() == nil {
			request := p.Request
			return &request, nil
		}
	}
	return nil, nil
}
func (s *Store) RequestContext(id, sender, requestID, fileID string, index int64) (context.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(id, sender)
	if err != nil {
		return nil, err
	}
	p := s.pending[requestID]
	if sender != r.Relay.SenderID || p == nil || p.op.relayID != id || p.Request.FileID != fileID || p.Request.Index != index || p.op.ctx.Err() != nil {
		return nil, ErrConflict
	}
	return p.op.ctx, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func (s *Store) Supply(ctx context.Context, id, sender, fileID string, index int64, requestID, hash string, reader io.Reader) error {
	if !validHash(hash) {
		return ErrInvalid
	}
	s.mu.Lock()
	r, err := s.getLocked(id, sender)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	p := s.pending[requestID]
	if sender != r.Relay.SenderID || p == nil || p.op.relayID != id || p.Request.FileID != fileID || p.Request.Index != index || p.claimed || p.op.ctx.Err() != nil {
		s.mu.Unlock()
		return ErrConflict
	}
	p.claimed = true
	expected := min(model.ChunkSize, p.file.Size-index*model.ChunkSize)
	s.mu.Unlock()
	combined, cancel := context.WithCancel(p.op.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	b := make([]byte, int(expected))
	_, err = io.ReadFull(contextReader{combined, reader}, b)
	if err == nil {
		var extra [1]byte
		var n int
		n, err = io.ReadFull(contextReader{combined, reader}, extra[:])
		if n != 0 {
			err = ErrInvalid
		} else if errors.Is(err, io.EOF) {
			err = nil
		}
	}
	if err != nil {
		p.op.Interrupt()
		return err
	}
	sum := sha256.Sum256(b)
	actual := hex.EncodeToString(sum[:])
	s.mu.Lock()
	r, err = s.getLocked(id, sender)
	if err == nil && (s.pending[requestID] != p || p.op.ctx.Err() != nil || r.Epoch != p.op.epoch) {
		err = ErrConflict
	}
	if err == nil {
		if actual != hash {
			err = ErrIntegrity
		}
		if previous := r.Blocks[fileID][index]; previous != "" && previous != actual {
			err = ErrIntegrity
		}
		if p.file.Size <= model.ChunkSize && actual != p.file.SHA256 {
			err = ErrIntegrity
		}
	}
	if err == nil {
		// Pin identity before releasing either concurrent RAM response.
		if r.Blocks[fileID] == nil {
			r.Blocks[fileID] = map[int64]string{}
		}
		if r.Blocks[fileID][index] == "" {
			r.Blocks[fileID][index] = actual
			err = s.saveLocked(r)
		}
	}
	if errors.Is(err, ErrIntegrity) {
		_ = s.failLocked(r)
	}
	if err == nil {
		p.hash = actual
		p.data <- b
	}
	s.mu.Unlock()
	if err != nil {
		p.op.Interrupt()
		return err
	}
	select {
	case err = <-p.done:
		return err
	case <-combined.Done():
		// Stream completion cancels its context immediately after its last
		// durable ACK. Prefer that ACK when both become ready together.
		select {
		case err = <-p.done:
			return err
		default:
		}
		p.op.Interrupt()
		return combined.Err()
	}
}
func covered(ranges []interval) int64 {
	var n int64
	for _, r := range ranges {
		n += r.End - r.Start
	}
	return n
}
func merge(ranges []interval, addition interval) []interval {
	ranges = append(append([]interval{}, ranges...), addition)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	out := []interval{}
	for _, r := range ranges {
		if r.End <= r.Start {
			continue
		}
		if len(out) == 0 || r.Start > out[len(out)-1].End {
			out = append(out, r)
		} else if r.End > out[len(out)-1].End {
			out[len(out)-1].End = r.End
		}
	}
	return out
}

// Confirm is called only after receiver transport writes (and ZIP flush) finish.
func (s *Store) Confirm(p *Block, start, end int64, transportErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(p.op.relayID, p.op.receiverID)
	if err == nil && (s.pending[p.Request.RequestID] != p || r.Epoch != p.op.epoch) {
		err = ErrConflict
	}
	if err == nil && end > start {
		index, fileErr := findFile(r, p.file.ID)
		if fileErr != nil {
			err = fileErr
		} else if start < 0 || end > p.file.Size || end < start || p.hash == "" {
			err = ErrInvalid
		} else if previous := r.Blocks[p.file.ID][p.Request.Index]; previous != "" && previous != p.hash {
			err = ErrIntegrity
		} else {
			if r.Blocks[p.file.ID] == nil {
				r.Blocks[p.file.ID] = map[int64]string{}
			}
			r.Blocks[p.file.ID][p.Request.Index] = p.hash
			ranges := merge(r.Ranges[p.file.ID], interval{start, end})
			before := r.Relay.Files[index].Received
			after := covered(ranges)
			r.Ranges[p.file.ID] = ranges
			r.Relay.Files[index].Received = after
			r.Relay.Files[index].Completed = after == p.file.Size
			r.Relay.DeliveredBytes += after - before
			r.Relay.BytesPerSecond = float64(end-start) / max(time.Since(p.started).Seconds(), 0.001)
			err = s.saveLocked(r)
		}
	}
	delete(s.pending, p.Request.RequestID)
	ack := transportErr
	if err != nil {
		ack = err
	}
	select {
	case p.done <- ack:
	default:
	}
	return err
}
func (s *Store) EmptyDelivered(op *Stream, fileID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.getLocked(op.relayID, op.receiverID)
	if err != nil {
		return err
	}
	if r.Epoch != op.epoch || op.ctx.Err() != nil {
		return ErrConflict
	}
	i, err := findFile(r, fileID)
	if err != nil {
		return err
	}
	if r.Relay.Files[i].Size != 0 {
		return ErrInvalid
	}
	r.Relay.Files[i].Completed = true
	return s.saveLocked(r)
}

// Package engine owns durable transfer state and bounded-memory file I/O.
package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lucasfd3/Airlink/internal/model"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("transfer state conflict")
	ErrInvalid       = errors.New("invalid transfer input")
	ErrIntegrity     = errors.New("file integrity check failed")
	ErrSourceChanged = fmt.Errorf("%w: source changed or became unavailable; select the original file again", ErrConflict)
)

type source struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Identity string `json:"identity,omitempty"`
}
type record struct {
	Transfer          model.Transfer     `json:"transfer"`
	Sources           map[string]source  `json:"sources,omitempty"`
	ReceiveRoot       string             `json:"receiveRoot,omitempty"`
	Organization      model.Organization `json:"organization"`
	LegacyVerified    map[string]string  `json:"legacyVerified,omitempty"`
	OfferRequestID    string             `json:"offerRequestId,omitempty"`
	OfferInputHash    string             `json:"offerInputHash,omitempty"`
	DirectReceive     bool               `json:"directReceive,omitempty"`
	DirectDirectories map[string]string  `json:"directDirectories,omitempty"`
}

type Engine struct {
	mu               sync.Mutex
	offerMu          sync.Mutex
	db               *sql.DB
	records          map[string]*record
	locks            sync.Map
	receiveDir       string
	closed           bool
	progress         map[string]progressSample
	verifications    map[string]*verificationOperation
	organization     model.Organization
	managed          map[string]managedFile
	managedSeen      map[string]bool
	ctx              context.Context
	cancel           context.CancelFunc
	autoWG           sync.WaitGroup
	autoPublishing   map[string]bool
	sourceOperations map[string]map[string]context.CancelFunc
}
type verificationOperation struct {
	ctx    context.Context
	cancel context.CancelFunc
}
type progressSample struct {
	at    time.Time
	bytes int64
}

func New(dataDir, receiveDir string) (*Engine, error) {
	if !filepath.IsAbs(dataDir) || !filepath.IsAbs(receiveDir) {
		return nil, fmt.Errorf("%w: directories must be absolute", ErrInvalid)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(receiveDir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "transfers.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS transfers(id TEXT PRIMARY KEY, document BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS chunks(transfer_id TEXT NOT NULL,file_id TEXT NOT NULL,idx INTEGER NOT NULL,hash TEXT NOT NULL,size INTEGER NOT NULL,PRIMARY KEY(transfer_id,file_id,idx));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS managed_files(id TEXT PRIMARY KEY,sha TEXT NOT NULL,size INTEGER NOT NULL,document BLOB NOT NULL);CREATE INDEX IF NOT EXISTS managed_hash_size ON managed_files(sha,size);CREATE TABLE IF NOT EXISTS managed_seen(path TEXT PRIMARY KEY);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	canonicalReceive, err := filepath.EvalSymlinks(receiveDir)
	if err != nil {
		db.Close()
		return nil, err
	}
	engineCtx, cancel := context.WithCancel(context.Background())
	e := &Engine{db: db, records: map[string]*record{}, receiveDir: canonicalReceive, progress: map[string]progressSample{}, verifications: map[string]*verificationOperation{}, organization: model.Organization{Rules: []model.SortRule{}}, managed: map[string]managedFile{}, managedSeen: map[string]bool{}, ctx: engineCtx, cancel: cancel, autoPublishing: map[string]bool{}, sourceOperations: map[string]map[string]context.CancelFunc{}}
	if err = e.loadManaged(); err != nil {
		cancel()
		db.Close()
		return nil, err
	}
	rows, err := db.Query("SELECT document FROM transfers")
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
		e.records[r.Transfer.ID] = &r
	}
	if rowErr := rows.Err(); err == nil {
		err = rowErr
	}
	rows.Close()
	if err != nil {
		db.Close()
		return nil, err
	}
	// A process restart never asserts a live network connection. Confirmed data
	// remains resumable; callers explicitly resume after reconnecting.
	for _, r := range e.records {
		if r.Transfer.State == "running" || r.Transfer.State == "verifying" || r.Transfer.State == "queued" || r.Transfer.State == "checkingDuplicate" {
			r.Transfer.State = "reconnecting"
			r.Transfer.BytesPerSecond = 0
		}
		if r.Transfer.State != "paused" && !terminal(r.Transfer.State) {
			for _, f := range r.Transfer.Files {
				if f.DuplicateState == "pending" {
					r.Transfer.State = "waitingDuplicate"
					break
				}
			}
		}
		if r.Transfer.Direction == "receive" {
			if recoverErr := e.recoverIncoming(r); recoverErr != nil {
				r.Transfer.State = "paused"
				r.Transfer.Error = "Destination recovery: " + recoverErr.Error()
			}
		}
		if err = e.saveLocked(r); err != nil {
			db.Close()
			return nil, err
		}
		for _, file := range r.Transfer.Files {
			if err = e.indexFileLocked(r, file); err != nil {
				cancel()
				db.Close()
				return nil, err
			}
		}
	}
	e.mu.Lock()
	for _, r := range e.records {
		e.queueCancelledCleanupLocked(r)
	}
	e.mu.Unlock()
	return e, nil
}

func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.cancel()
	for id := range e.verifications {
		e.cancelVerificationLocked(id)
	}
	for id := range e.sourceOperations {
		e.cancelSourceOperationsLocked(id)
	}
	e.mu.Unlock()
	e.autoWG.Wait()
	return e.db.Close()
}
func (e *Engine) SetReceiveDirectory(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%w: absolute directory required", ErrInvalid)
	}
	if err := os.MkdirAll(p, 0700); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.receiveDir = canonical
	return nil
}
func clone(t model.Transfer) model.Transfer {
	t.Files = append([]model.File{}, t.Files...)
	for i := range t.Files {
		t.Files[i].DuplicateCandidates = append([]model.DuplicateCandidate(nil), t.Files[i].DuplicateCandidates...)
	}
	return t
}
func (e *Engine) Transfers(deviceID string) []model.Transfer {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []model.Transfer{}
	for _, r := range e.records {
		if deviceID == "" || r.Transfer.DeviceID == deviceID {
			out = append(out, clone(r.Transfer))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}
func (e *Engine) Get(id string) (model.Transfer, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return model.Transfer{}, err
	}
	return clone(r.Transfer), nil
}
func (e *Engine) getLocked(id string) (*record, error) {
	if e.closed {
		return nil, errors.New("engine closed")
	}
	r, ok := e.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func identifier() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func (e *Engine) saveLocked(r *record) error {
	r.Transfer.UpdatedAt = now()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = e.db.Exec("INSERT INTO transfers(id,document) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document", r.Transfer.ID, b)
	return err
}
func fileIndex(t *model.Transfer, id string) (int, error) {
	for i := range t.Files {
		if t.Files[i].ID == id {
			return i, nil
		}
	}
	return 0, ErrNotFound
}
func terminal(s string) bool { return s == "completed" || s == "cancelled" || s == "failed" }
func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && strings.ToLower(s) == s
}

// cleanPath uses a common, conservative subset of Windows/macOS/Linux names.
// Every path remains relative; duplicate/case-only names are rejected before IO.
func cleanPath(p string) (string, error) {
	if p == "" || strings.Contains(p, "\\") || strings.HasPrefix(p, "/") || strings.ContainsAny(p, ":\x00") || path.Clean(p) != p || p == "." {
		return "", fmt.Errorf("%w: unsafe relative path", ErrInvalid)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || part == "" || strings.TrimRight(part, ". ") != part || strings.EqualFold(part, ".airlink-partials") {
			return "", fmt.Errorf("%w: unsafe path component", ErrInvalid)
		}
		for _, c := range part {
			if unicode.IsControl(c) || strings.ContainsRune(`<>"|?*`, c) {
				return "", fmt.Errorf("%w: unsupported filename", ErrInvalid)
			}
		}
		stem := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || (len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9') {
			return "", fmt.Errorf("%w: reserved filename", ErrInvalid)
		}
	}
	return p, nil
}

func normalize(files []model.File) ([]model.File, int64, error) {
	if len(files) == 0 {
		return nil, 0, fmt.Errorf("%w: empty offer", ErrInvalid)
	}
	out := make([]model.File, len(files))
	names := map[string]bool{}
	ids := map[string]bool{}
	var total int64
	for i, f := range files {
		p, err := cleanPath(f.Path)
		if err != nil {
			return nil, 0, err
		}
		if f.ID == "" {
			f.ID = identifier()
		}
		validID := len(f.ID) <= 128
		for _, c := range f.ID {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
				validID = false
				break
			}
		}
		if !validID {
			return nil, 0, fmt.Errorf("%w: invalid file id", ErrInvalid)
		}
		key := strings.ToLower(p)
		if _, exists := names[key]; exists || ids[f.ID] {
			return nil, 0, fmt.Errorf("%w: duplicate path or id", ErrInvalid)
		}
		if f.Size < 0 || (f.Directory && f.Size != 0) || f.Size > int64(^uint64(0)>>1)-total {
			return nil, 0, fmt.Errorf("%w: invalid file size", ErrInvalid)
		}
		names[key] = f.Directory
		ids[f.ID] = true
		f.Received = 0
		f.Completed = f.Directory
		f.SHA256 = ""
		f.DestinationPath = ""
		f.Skipped = false
		f.DuplicateState = ""
		f.DuplicateCandidates = nil
		out[i] = f
		total += f.Size
	}
	for p := range names {
		for parent := path.Dir(p); parent != "."; parent = path.Dir(parent) {
			if directory, exists := names[parent]; exists && !directory {
				return nil, 0, fmt.Errorf("%w: file conflicts with parent directory", ErrInvalid)
			}
		}
	}
	return out, total, nil
}

func (e *Engine) CreateIncoming(device model.Device, files []model.File) (model.Transfer, error) {
	if !device.CanSend {
		return model.Transfer{}, fmt.Errorf("%w: device cannot send", ErrConflict)
	}
	files, total, err := normalize(files)
	if err != nil {
		return model.Transfer{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return model.Transfer{}, errors.New("engine closed")
	}
	policy := cloneOrganization(e.organization)
	files, err = snapshotDestinations(files, policy)
	if err != nil {
		return model.Transfer{}, err
	}
	id := identifier()
	r := &record{Transfer: model.Transfer{ID: id, DeviceID: device.ID, DeviceName: device.Name, Direction: "receive", State: "offered", Files: files, TotalBytes: total, CreatedAt: now(), Destination: e.receiveDir}, ReceiveRoot: filepath.Join(e.receiveDir, ".airlink-partials", id), Organization: policy, LegacyVerified: map[string]string{}, DirectReceive: true}
	r.Transfer.Files, err = e.directDestinationsLocked(r)
	if err != nil {
		return model.Transfer{}, err
	}
	e.records[id] = r
	if err = e.saveLocked(r); err != nil {
		delete(e.records, id)
		return model.Transfer{}, err
	}
	if device.AutoAccept {
		if err = e.acceptLocked(r); err != nil {
			return model.Transfer{}, err
		}
	}
	return clone(r.Transfer), nil
}

func (e *Engine) ensureIncoming(r *record) error {
	if r.DirectReceive {
		return e.ensureDirectIncoming(r)
	}
	parentRoot, err := organizationRoot(filepath.Dir(r.ReceiveRoot))
	if err != nil {
		return err
	}
	err = parentRoot.Mkdir(filepath.Base(r.ReceiveRoot), 0700)
	parentRoot.Close()
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	root, err := organizationRoot(r.ReceiveRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = ensureOrganizationDirs(root, ".airlink-partials"); err != nil {
		return err
	}
	for _, f := range r.Transfer.Files {
		parent := path.Dir(destination(f))
		if f.Directory {
			parent = destination(f)
		}
		if parent != "." {
			if err = ensureOrganizationDirs(root, parent); err != nil {
				return err
			}
		}
	}
	if err := syncRootDirectory(root, "."); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(r.ReceiveRoot))
	if err != nil {
		return err
	}
	defer parent.Close()
	if runtime.GOOS != "windows" {
		return parent.Sync()
	}
	return nil
}
func (e *Engine) Accept(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	return e.acceptLocked(r)
}
func (e *Engine) acceptLocked(r *record) error {
	if r.Transfer.State != "offered" {
		return ErrConflict
	}
	if r.Transfer.Direction == "receive" {
		if err := e.ensureIncoming(r); err != nil {
			return err
		}
	}
	r.Transfer.State = "queued"
	if allComplete(r.Transfer.Files) {
		r.Transfer.State = "completed"
	}
	if err := e.saveLocked(r); err != nil {
		return err
	}
	cleanupCompletedReceive(r)
	return e.scheduleLocked()
}
func allComplete(files []model.File) bool {
	for _, f := range files {
		if !f.Completed {
			return false
		}
	}
	return true
}
func (e *Engine) scheduleLocked() error {
	// Receivers reserve their slots while waiting for remote chunks. A single
	// shared pool would deadlock when two peers both reserve all slots for
	// receiving and queue the outgoing transfers needed to supply those chunks.
	// Independent bounded pools preserve duplex progress on every peer.
	active := map[string]int{"receive": 0, "send": 0}
	queue := []*record{}
	for _, r := range e.records {
		if r.Transfer.State == "running" || r.Transfer.State == "verifying" || (r.Transfer.State == "checkingDuplicate" && r.Transfer.Direction == "receive") {
			active[r.Transfer.Direction]++
		}
		if r.Transfer.State == "queued" {
			queue = append(queue, r)
		}
	}
	sort.Slice(queue, func(i, j int) bool { return queue[i].Transfer.CreatedAt < queue[j].Transfer.CreatedAt })
	for _, r := range queue {
		if active[r.Transfer.Direction] >= 2 {
			continue
		}
		r.Transfer.State = "running"
		if err := e.saveLocked(r); err != nil {
			return err
		}
		active[r.Transfer.Direction]++
	}
	for _, r := range e.records {
		e.schedulePublicationLocked(r)
	}
	return nil
}
func (e *Engine) Pause(id string) error  { return e.stateChange(id, "paused", "") }
func (e *Engine) Cancel(id string) error { return e.stateChange(id, "cancelled", "") }
func (e *Engine) MarkReconnecting(id, reason string) error {
	return e.stateChange(id, "reconnecting", reason)
}
func (e *Engine) Fail(id, reason string) error { return e.stateChange(id, "failed", reason) }
func (e *Engine) stateChange(id, state, reason string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	if terminal(r.Transfer.State) {
		return ErrConflict
	}
	if state == "paused" && r.Transfer.State == "offered" {
		return ErrConflict
	}
	if r.Transfer.State == "paused" && (state == "reconnecting" || state == "failed") {
		return ErrConflict
	}
	// A failed durable write is a failed operation, not a committed in-memory
	// cancellation that unexpectedly resurrects after restart. Readers cannot
	// commit between this transaction and the following context cancellation.
	updated := *r
	updated.Transfer = clone(r.Transfer)
	updated.Transfer.State = state
	updated.Transfer.Error = reason
	updated.Transfer.BytesPerSecond = 0
	if err = e.saveLocked(&updated); err != nil {
		return err
	}
	*r = updated
	e.cancelVerificationLocked(id)
	e.cancelSourceOperationsLocked(id)
	if state == "cancelled" {
		e.queueCancelledCleanupLocked(r)
	}
	return e.scheduleLocked()
}
func (e *Engine) Resume(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	if r.Transfer.State == "offered" && r.Transfer.Direction == "send" {
		return e.acceptLocked(r)
	}
	if r.Transfer.State != "paused" && r.Transfer.State != "reconnecting" && r.Transfer.State != "queued" && !(r.Transfer.Direction == "send" && (r.Transfer.State == "waitingDuplicate" || r.Transfer.State == "checkingDuplicate")) {
		return ErrConflict
	}
	for _, f := range r.Transfer.Files {
		if f.DuplicateState == "pending" {
			r.Transfer.State = "waitingDuplicate"
			r.Transfer.Error = ""
			if err = e.saveLocked(r); err != nil {
				return err
			}
			return e.scheduleLocked()
		}
	}
	r.Transfer.State = "queued"
	r.Transfer.Error = ""
	if err = e.saveLocked(r); err != nil {
		return err
	}
	return e.scheduleLocked()
}
func (e *Engine) fileLock(id, file string) (*sync.Mutex, error) {
	e.mu.Lock()
	r, err := e.getLocked(id)
	if err == nil {
		_, err = fileIndex(&r.Transfer, file)
	}
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	v, _ := e.locks.LoadOrStore(id+"/"+file, &sync.Mutex{})
	return v.(*sync.Mutex), nil
}
func partialName(id string) string {
	s := sha256.Sum256([]byte(id))
	return filepath.Join(".airlink-partials", hex.EncodeToString(s[:])+".part")
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

func (e *Engine) WriteChunk(ctx context.Context, id, fileID string, index int64, hash string, reader io.Reader) (int64, error) {
	if index < 0 || !validHash(hash) {
		return 0, ErrInvalid
	}
	lock, err := e.fileLock(id, fileID)
	if err != nil {
		return 0, err
	}
	lock.Lock()
	defer lock.Unlock()
	e.mu.Lock()
	r, err := e.getLocked(id)
	if err != nil {
		e.mu.Unlock()
		return 0, err
	}
	i, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		e.mu.Unlock()
		return 0, err
	}
	f := r.Transfer.Files[i]
	if r.Transfer.Direction != "receive" || r.Transfer.State == "cancelled" || r.Transfer.State == "failed" || f.Directory {
		e.mu.Unlock()
		return 0, ErrConflict
	}
	if index > f.Size/model.ChunkSize {
		e.mu.Unlock()
		return 0, ErrInvalid
	}
	offset := index * model.ChunkSize
	expected := min(model.ChunkSize, f.Size-offset)
	if expected <= 0 {
		e.mu.Unlock()
		return 0, ErrInvalid
	}
	if offset > f.Received {
		e.mu.Unlock()
		return f.Received, ErrConflict
	}
	if offset == f.Received && (r.Transfer.State != "running" || f.Completed) {
		e.mu.Unlock()
		return f.Received, ErrConflict
	}
	for _, candidate := range r.Transfer.Files {
		if !candidate.Directory && !candidate.Completed {
			if offset == f.Received && candidate.ID != fileID {
				e.mu.Unlock()
				return f.Received, ErrConflict
			}
			break
		}
	}
	e.mu.Unlock()
	start := time.Now()
	b, err := readChunkBytes(ctx, reader, expected)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return f.Received, fmt.Errorf("%w: block length", ErrInvalid)
	}
	if err != nil {
		return f.Received, err
	}
	if err = requireChunkEOF(ctx, reader); err != nil {
		return f.Received, err
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != hash {
		return f.Received, ErrIntegrity
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err = e.getLocked(id)
	if err != nil {
		return 0, err
	}
	i, err = fileIndex(&r.Transfer, fileID)
	if err != nil {
		return 0, err
	}
	f = r.Transfer.Files[i]
	if r.Transfer.State == "cancelled" || r.Transfer.State == "failed" {
		return f.Received, ErrConflict
	}
	if offset < f.Received {
		var stored string
		var size int64
		err = e.db.QueryRow("SELECT hash,size FROM chunks WHERE transfer_id=? AND file_id=? AND idx=?", id, fileID, index).Scan(&stored, &size)
		if err != nil {
			return f.Received, err
		}
		if stored != hash || size != expected {
			return f.Received, ErrIntegrity
		}
		return f.Received, nil
	}
	if r.Transfer.State != "running" || f.Completed {
		return f.Received, ErrConflict
	}
	if offset != f.Received {
		return f.Received, ErrConflict
	}
	root, err := organizationRoot(r.ReceiveRoot)
	if err != nil {
		return f.Received, err
	}
	defer root.Close()
	out, err := openPrivatePartial(root, fileID)
	if err != nil {
		return f.Received, err
	}
	if _, err = out.WriteAt(b, offset); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return f.Received, err
	}
	if closeErr != nil {
		return f.Received, closeErr
	}
	if err = syncRootDirectory(root, ".airlink-partials"); err != nil {
		return f.Received, err
	}
	// Bytes hit stable storage before the block and offset are committed together.
	updated := *r
	updated.Transfer = clone(r.Transfer)
	updated.Transfer.Files[i].Received = offset + expected
	updated.Transfer.CompletedBytes += expected
	updated.Transfer.BytesPerSecond = float64(expected) / max(time.Since(start).Seconds(), 0.001)
	updated.Transfer.UpdatedAt = now()
	doc, err := json.Marshal(&updated)
	if err != nil {
		return f.Received, err
	}
	tx, err := e.db.Begin()
	if err != nil {
		return f.Received, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO chunks(transfer_id,file_id,idx,hash,size) VALUES(?,?,?,?,?)", id, fileID, index, hash, expected); err != nil {
		return f.Received, err
	}
	if _, err = tx.Exec("UPDATE transfers SET document=? WHERE id=?", doc, id); err != nil {
		return f.Received, err
	}
	if err = tx.Commit(); err != nil {
		return f.Received, err
	}
	*r = updated
	return offset + expected, nil
}

func (e *Engine) CompleteFile(ctx context.Context, id, fileID, hash string) error {
	if !validHash(hash) {
		return ErrInvalid
	}
	lock, err := e.fileLock(id, fileID)
	if err != nil {
		return err
	}
	lock.Lock()
	defer lock.Unlock()
	e.mu.Lock()
	r, err := e.getLocked(id)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	i, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		e.mu.Unlock()
		return err
	}
	f := r.Transfer.Files[i]
	if f.Completed {
		e.mu.Unlock()
		if f.SHA256 == hash {
			return nil
		}
		return ErrIntegrity
	}
	if f.DuplicateState != "" && f.SHA256 != "" && f.SHA256 != hash {
		e.mu.Unlock()
		return ErrIntegrity
	}
	if r.Transfer.Direction != "receive" || r.Transfer.State != "running" || f.Directory || f.Received != f.Size {
		e.mu.Unlock()
		return ErrConflict
	}
	r.Transfer.State = "verifying"
	r.Transfer.Files[i].SHA256 = hash
	if err = e.saveLocked(r); err != nil {
		e.mu.Unlock()
		return err
	}
	receiveRoot := r.ReceiveRoot
	verificationCtx, cancel := context.WithCancel(ctx)
	op := &verificationOperation{ctx: verificationCtx, cancel: cancel}
	e.verifications[id] = op
	e.mu.Unlock()
	defer e.finishDuplicateOperation(id, op)
	root, err := organizationRoot(receiveRoot)
	if err != nil {
		return e.verificationError(id, op, err)
	}
	defer root.Close()
	in, err := openPrivatePartial(root, fileID)
	if err != nil {
		return e.verificationError(id, op, err)
	}
	if info, statErr := in.Stat(); statErr != nil || !info.Mode().IsRegular() {
		in.Close()
		if statErr != nil {
			return e.verificationError(id, op, statErr)
		}
		return e.verificationError(id, op, ErrInvalid)
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, contextReader{verificationCtx, in}, make([]byte, 256*1024))
	if err == nil {
		err = in.Sync()
	}
	in.Close()
	if err != nil {
		return e.verificationError(id, op, err)
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != hash {
		return e.verificationError(id, op, ErrIntegrity)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err = e.getLocked(id)
	if err != nil {
		return err
	}
	if r.Transfer.State != "verifying" || e.verifications[id] != op || verificationCtx.Err() != nil {
		return ErrConflict
	}
	if r.Organization.DetectDuplicates && r.Transfer.Files[i].DuplicateState != "checked" && r.Transfer.Files[i].DuplicateState != "keep" {
		if r.LegacyVerified == nil {
			r.LegacyVerified = map[string]string{}
		}
		r.LegacyVerified[fileID] = hash
		r.Transfer.State = "checkingDuplicate"
		if err = e.saveLocked(r); err != nil {
			return err
		}
		e.mu.Unlock()
		candidate, checkErr := e.findDuplicate(op.ctx, hash, f.Size)
		e.mu.Lock()
		r, err = e.getLocked(id)
		if err != nil {
			return err
		}
		if e.verifications[id] != op || op.ctx.Err() != nil || r.Transfer.State != "checkingDuplicate" {
			return ErrConflict
		}
		if checkErr != nil {
			r.Transfer.State = "running"
			r.Transfer.Error = "Duplicate check interrupted; retry completion."
			_ = e.saveLocked(r)
			return checkErr
		}
		if candidate != nil {
			_, err = e.duplicateResultLocked(r, i, op, candidate)
			return err
		}
		r.Transfer.Files[i].DuplicateState = "checked"
		r.Transfer.State = "verifying"
		if err = e.saveLocked(r); err != nil {
			return err
		}
	}
	if r.DirectReceive {
		if err = e.publishDirectLocked(r, i); err != nil {
			return e.failLocked(r, err)
		}
	} else {
		if err = ensureOrganizationDirs(root, path.Dir(destination(f))); err != nil {
			return e.failLocked(r, err)
		}
		if _, err = root.Lstat(filepath.FromSlash(destination(f))); err == nil {
			return e.failLocked(r, fmt.Errorf("%w: destination already exists", ErrConflict))
		} else if !errors.Is(err, fs.ErrNotExist) {
			return e.failLocked(r, err)
		}
		// Both publication mechanisms refuse to replace an existing file. The
		// rename fallback also supports exFAT, which has no hardlinks.
		if err = publish(root, partialName(fileID), filepath.FromSlash(destination(f))); err != nil {
			return e.failLocked(r, err)
		}
		if err = syncRootDirectory(root, filepath.Dir(filepath.FromSlash(destination(f)))); err != nil {
			return e.failLocked(r, err)
		}
	}
	r.Transfer.Files[i].SHA256 = hash
	r.Transfer.Files[i].Completed = true
	delete(r.LegacyVerified, fileID)
	r.Transfer.State = "running"
	r.Transfer.BytesPerSecond = 0
	if allComplete(r.Transfer.Files) {
		r.Transfer.State = "completed"
	}
	if err = e.saveLocked(r); err != nil {
		return err
	}
	if r.DirectReceive {
		_ = root.Close()
	}
	cleanupCompletedReceive(r)
	if err = e.indexPublishedFileLocked(r, r.Transfer.Files[i]); err != nil {
		return err
	}
	return e.scheduleLocked()
}

func publish(root *os.Root, oldName, newName string) error {
	if err := root.Link(oldName, newName); err == nil {
		return root.Remove(oldName)
	} else if errors.Is(err, fs.ErrExist) {
		return err
	}
	return renameNoReplace(root, oldName, newName)
}
func (e *Engine) verificationError(id string, op *verificationOperation, cause error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.verifications[id] != op {
		return ErrConflict
	}
	if r, err := e.getLocked(id); err == nil {
		if r.Transfer.State == "verifying" {
			r.Transfer.State = "running"
			r.Transfer.Error = cause.Error()
			_ = e.saveLocked(r)
		}
	}
	return cause
}
func (e *Engine) failLocked(r *record, cause error) error {
	e.cancelVerificationLocked(r.Transfer.ID)
	r.Transfer.State = "failed"
	r.Transfer.Error = cause.Error()
	_ = e.saveLocked(r)
	_ = e.scheduleLocked()
	return cause
}

func (e *Engine) cancelVerificationLocked(id string) {
	if op := e.verifications[id]; op != nil {
		op.cancel()
		delete(e.verifications, id)
	}
}

func (e *Engine) recoverIncoming(r *record) error {
	if r.Transfer.State == "offered" || terminal(r.Transfer.State) {
		return nil
	}
	if err := e.ensureIncoming(r); err != nil {
		return err
	}
	root, err := organizationRoot(r.ReceiveRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	finalRoot := root
	if r.DirectReceive {
		finalRoot, err = organizationRoot(r.Transfer.Destination)
		if err != nil {
			return err
		}
		defer finalRoot.Close()
		if err = validateDirectDirectories(finalRoot, r); err != nil {
			return err
		}
	}
	r.Transfer.CompletedBytes = 0
	r.Transfer.SkippedBytes = 0
	for i, f := range r.Transfer.Files {
		if f.Directory {
			continue
		}
		if f.Completed {
			r.Transfer.CompletedBytes += f.Received
			if f.Skipped {
				r.Transfer.SkippedBytes += f.Size - f.Received
			}
			continue
		}
		// Publication may have completed immediately before a process crash,
		// while the SQLite completion record did not. Reconcile using the
		// expected hash persisted before publication; never overwrite it.
		if r.DirectReceive {
			finalInfo, finalErr := finalRoot.Lstat(filepath.FromSlash(destination(f)))
			partialInfo, partialErr := root.Lstat(partialName(f.ID))
			if finalErr == nil && (!validHash(f.SHA256) || partialErr == nil && !os.SameFile(finalInfo, partialInfo)) {
				chosen, chooseErr := unusedDestination(finalRoot, collisionBase(f), false, nil)
				if chooseErr != nil {
					return chooseErr
				}
				r.Transfer.Files[i].DestinationPath = chosen
				f = r.Transfer.Files[i]
			}
		}
		if finalInfo, statErr := finalRoot.Lstat(filepath.FromSlash(destination(f))); statErr == nil && finalInfo.Mode()&os.ModeSymlink != 0 {
			return ErrConflict
		}
		if final, openErr := finalRoot.Open(filepath.FromSlash(destination(f))); openErr == nil {
			info, statErr := final.Stat()
			h := sha256.New()
			_, hashErr := io.CopyBuffer(h, final, make([]byte, 256*1024))
			final.Close()
			if statErr != nil || hashErr != nil || !validHash(f.SHA256) || info.Size() != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
				return fmt.Errorf("%w: destination collision during recovery", ErrIntegrity)
			}
			r.Transfer.Files[i].Completed = true
			r.Transfer.Files[i].Received = f.Size
			r.Transfer.CompletedBytes += f.Size
			_ = root.Remove(partialName(f.ID))
			continue
		} else if !errors.Is(openErr, fs.ErrNotExist) {
			return openErr
		}
		partial, err := openPrivatePartial(root, f.ID)
		if err != nil {
			return err
		}
		info, err := partial.Stat()
		if err != nil {
			partial.Close()
			return err
		}
		confirmed := f.Received
		if info.Size() < confirmed {
			confirmed = info.Size() / model.ChunkSize * model.ChunkSize
			if confirmed > f.Size {
				confirmed = f.Size
			}
			r.Transfer.Files[i].Received = confirmed
			if _, err = e.db.Exec("DELETE FROM chunks WHERE transfer_id=? AND file_id=? AND idx>=?", r.Transfer.ID, f.ID, confirmed/model.ChunkSize); err != nil {
				partial.Close()
				return err
			}
		}
		err = partial.Truncate(confirmed)
		partial.Close()
		if err != nil {
			return err
		}
		r.Transfer.CompletedBytes += confirmed
	}
	if allComplete(r.Transfer.Files) {
		r.Transfer.State = "completed"
	}
	return nil
}

func syncRootDirectory(root *os.Root, name string) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	err = f.Sync()
	// Windows FlushFileBuffers does not accept directory handles; NTFS uses
	// its journal for directory-entry recovery. File content is still flushed.
	if runtime.GOOS == "windows" {
		return nil
	}
	return err
}

func (e *Engine) Offer(device model.Device, paths []string) (model.Transfer, error) {
	return e.offer(device, paths, "", "")
}
func (e *Engine) offer(device model.Device, paths []string, requestID, inputHash string) (model.Transfer, error) {
	if !device.CanReceive {
		return model.Transfer{}, fmt.Errorf("%w: device cannot receive", ErrConflict)
	}
	files := []model.File{}
	sources := map[string]source{}
	for _, selected := range paths {
		if !filepath.IsAbs(selected) {
			return model.Transfer{}, ErrInvalid
		}
		selected = filepath.Clean(selected)
		selectedInfo, err := os.Lstat(selected)
		if err != nil {
			return model.Transfer{}, err
		}
		if selectedInfo.Mode()&os.ModeSymlink != 0 {
			return model.Transfer{}, fmt.Errorf("%w: symbolic links are not transferred", ErrInvalid)
		}
		// Canonicalize ancestors (for example macOS /var -> /private/var)
		// while retaining the explicit rejection of a selected symlink itself.
		selected, err = filepath.EvalSymlinks(selected)
		if err != nil {
			return model.Transfer{}, err
		}
		base := filepath.Base(selected)
		err = filepath.WalkDir(selected, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: symbolic links are not transferred", ErrInvalid)
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("%w: only regular files and directories", ErrInvalid)
			}
			rel, err := filepath.Rel(selected, p)
			if err != nil {
				return err
			}
			name := base
			if rel != "." {
				name = filepath.ToSlash(filepath.Join(base, rel))
			}
			f := model.File{ID: identifier(), Path: name, Directory: info.IsDir(), Modified: info.ModTime().UnixMilli()}
			if !f.Directory {
				f.Size = info.Size()
				captured, captureErr := captureSource(p, info)
				if captureErr != nil {
					return captureErr
				}
				sources[f.ID] = captured
			}
			files = append(files, f)
			return nil
		})
		if err != nil {
			return model.Transfer{}, err
		}
	}
	files, total, err := normalize(files)
	if err != nil {
		return model.Transfer{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return model.Transfer{}, errors.New("engine closed")
	}
	id := identifier()
	r := &record{Transfer: model.Transfer{ID: id, DeviceID: device.ID, DeviceName: device.Name, Direction: "send", State: "offered", Files: files, TotalBytes: total, CreatedAt: now()}, Sources: sources, OfferRequestID: requestID, OfferInputHash: inputHash}
	e.records[id] = r
	if err = e.saveLocked(r); err != nil {
		delete(e.records, id)
		return model.Transfer{}, err
	}
	return clone(r.Transfer), nil
}

func (e *Engine) OpenDownload(id, fileID string) (*os.File, model.File, error) {
	return e.OpenDownloadContext(e.ctx, id, fileID)
}
func (e *Engine) SourceSHA256(ctx context.Context, id, fileID string) (string, error) {
	return e.hashSource(ctx, id, fileID)
}
func (e *Engine) ReadSourceChunk(ctx context.Context, id, fileID string, index int64) ([]byte, string, error) {
	if index < 0 {
		return nil, "", ErrInvalid
	}
	in, f, err := e.OpenDownloadContext(ctx, id, fileID)
	if err != nil {
		return nil, "", err
	}
	defer in.Close()
	if index > f.Size/model.ChunkSize {
		return nil, "", ErrInvalid
	}
	offset := index * model.ChunkSize
	size := min(model.ChunkSize, f.Size-offset)
	if size <= 0 {
		return nil, "", io.EOF
	}
	if _, err = in.Seek(offset, io.SeekStart); err != nil {
		return nil, "", err
	}
	b, err := readChunkBytes(ctx, in, size)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return nil, "", ErrSourceChanged
	}
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) != size {
		return nil, "", ErrSourceChanged
	}
	if err = ctx.Err(); err != nil {
		return nil, "", err
	}
	if err = e.ValidateSource(id, fileID, in); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}
func (e *Engine) MarkOutgoingProgress(id, fileID string, received int64, hash string) error {
	if hash != "" && !validHash(hash) {
		return ErrInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	i, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		return err
	}
	f := &r.Transfer.Files[i]
	if r.Transfer.Direction != "send" || terminal(r.Transfer.State) || received < 0 || received > f.Size {
		return ErrConflict
	}
	if hash != "" && f.SHA256 != "" && hash != f.SHA256 {
		return ErrSourceChanged
	}
	if f.Skipped {
		if received == f.Received && (hash == "" || hash == f.SHA256) {
			return nil
		}
		return ErrConflict
	}
	if received == f.Received && (hash == "" || hash == f.SHA256) {
		// Source hashing and earlier acknowledgements already persisted this
		// exact offset/hash. Rewriting the same document adds a durable SQLite
		// transaction before every file without confirming any new bytes.
		e.progress[id] = progressSample{at: time.Now(), bytes: r.Transfer.CompletedBytes}
		return nil
	}
	r.Transfer.CompletedBytes += received - f.Received
	f.Received = received
	tick := time.Now()
	if previous, ok := e.progress[id]; ok {
		elapsed := tick.Sub(previous.at).Seconds()
		if elapsed > 0 && r.Transfer.CompletedBytes >= previous.bytes {
			r.Transfer.BytesPerSecond = float64(r.Transfer.CompletedBytes-previous.bytes) / elapsed
		}
	}
	e.progress[id] = progressSample{at: tick, bytes: r.Transfer.CompletedBytes}
	if hash != "" {
		f.SHA256 = hash
	}
	return e.saveLocked(r)
}
func (e *Engine) CompleteOutgoingFile(id, fileID, hash string) error {
	if !validHash(hash) {
		return ErrInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	i, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		return err
	}
	f := &r.Transfer.Files[i]
	if r.Transfer.Direction == "send" && f.Completed && f.SHA256 == hash {
		return nil
	}
	if f.SHA256 != "" && f.SHA256 != hash {
		return ErrSourceChanged
	}
	if r.Transfer.Direction != "send" || r.Transfer.State == "paused" || terminal(r.Transfer.State) || f.Received != f.Size {
		return ErrConflict
	}
	f.Completed = true
	f.SHA256 = hash
	if allComplete(r.Transfer.Files) {
		r.Transfer.State = "completed"
		r.Transfer.BytesPerSecond = 0
	}
	if err = e.saveLocked(r); err != nil {
		return err
	}
	return e.scheduleLocked()
}

// Downloaded records initiation only: a browser cannot attest to saved bytes.
func (e *Engine) Downloaded(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	if r.Transfer.Direction != "send" || terminal(r.Transfer.State) {
		return ErrConflict
	}
	r.Transfer.Error = "Download started in browser; completion is managed by the browser."
	return e.saveLocked(r)
}

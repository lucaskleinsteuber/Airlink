// Package shares provides durable, revocable LAN shares without copying sources.
package shares

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"github.com/lucaskleinsteuber/Airlink/internal/model"
	"golang.org/x/crypto/argon2"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound     = errors.New("share not found")
	ErrUnauthorized = errors.New("share authorization required")
	ErrGone         = errors.New("share expired or revoked")
	ErrQuota        = errors.New("share download limit reached")
	ErrUnavailable  = errors.New("original source unavailable or changed")
	ErrInvalid      = errors.New("invalid share input")
	ErrRateLimit    = errors.New("too many unlock attempts; try again later")
)

// Source is an immutable reference, never a copy. Root optionally constrains a
// caller-supplied transfer mapping during capture; it is not exposed over HTTP.
type Source struct {
	File         model.File `json:"file"`
	Path         string     `json:"path"`
	Identity     string     `json:"identity"`
	ModifiedNano int64      `json:"modifiedNano"`
	Root         string     `json:"-"`
}
type shareRecord struct {
	Share        model.Share `json:"share"`
	Owner        string      `json:"owner"`
	Secret       string      `json:"secret"`
	PasswordHash string      `json:"passwordHash,omitempty"`
	Sources      []Source    `json:"sources"`
	sourceIndex  map[string]int
}

func (r *shareRecord) indexSources() {
	r.sourceIndex = make(map[string]int, len(r.Sources))
	for i, src := range r.Sources {
		r.sourceIndex[src.File.ID] = i
	}
}

type session struct {
	ShareID   string `json:"shareId"`
	Counted   bool   `json:"counted"`
	CreatedAt string `json:"createdAt"`
}

const SessionLifetime = 30 * 24 * time.Hour

func sessionCurrent(ss session) bool {
	created, err := time.Parse(time.RFC3339Nano, ss.CreatedAt)
	return err == nil && time.Since(created) < SessionLifetime
}

type Store struct {
	mu         sync.Mutex
	db         *sql.DB
	records    map[string]*shareRecord
	sessions   map[string]session
	failures   map[string][]time.Time
	argonSlots chan struct{}
	closed     bool
}

func New(dataDir string) (*Store, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "shares.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL;PRAGMA synchronous=FULL;PRAGMA busy_timeout=5000;CREATE TABLE IF NOT EXISTS shares(id TEXT PRIMARY KEY,document BLOB NOT NULL);CREATE TABLE IF NOT EXISTS share_sessions(hash TEXT PRIMARY KEY,document BLOB NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(filepath.Join(dataDir, "shares.sqlite"), 0600)
	s := &Store{db: db, records: map[string]*shareRecord{}, sessions: map[string]session{}, failures: map[string][]time.Time{}, argonSlots: make(chan struct{}, 1)}
	rows, err := db.Query("SELECT document FROM shares")
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			break
		}
		var r shareRecord
		if err = json.Unmarshal(b, &r); err != nil {
			break
		}
		r.indexSources()
		s.records[r.Share.ID] = &r
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		db.Close()
		return nil, err
	}
	rows, err = db.Query("SELECT hash,document FROM share_sessions")
	if err != nil {
		db.Close()
		return nil, err
	}
	for rows.Next() {
		var h string
		var b []byte
		if err = rows.Scan(&h, &b); err != nil {
			break
		}
		var ss session
		if err = json.Unmarshal(b, &ss); err != nil {
			break
		}
		s.sessions[h] = ss
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func tokenHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func equalSecret(a, b string) bool {
	aa := sha256.Sum256([]byte(a))
	bb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(aa[:], bb[:]) == 1
}
func copied(v model.Share) model.Share { v.Files = append([]model.File{}, v.Files...); return v }
func ownerShare(r *shareRecord) model.Share {
	v := copied(r.Share)
	v.URL = "/s/" + v.ID + "#" + r.Secret
	return v
}
func publicShare(r *shareRecord) model.Share { v := copied(r.Share); v.URL = ""; return v }
func (s *Store) List(owner string) []model.Share {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Share{}
	for _, r := range s.records {
		if owner == "" || r.Owner == owner {
			out = append(out, ownerShare(r))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}
func (s *Store) Get(id string) (model.Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[id]
	if r == nil {
		return model.Share{}, ErrNotFound
	}
	return ownerShare(r), nil
}
func alive(r *shareRecord) error {
	if r.Share.Revoked {
		return ErrGone
	}
	if r.Share.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339Nano, r.Share.ExpiresAt)
		if err != nil || !time.Now().Before(expiry) {
			return ErrGone
		}
	}
	return nil
}

func (s *Store) Create(owner string, input model.ShareCreate, sources []Source) (model.Share, error) {
	if owner == "" || len(sources) == 0 || input.MaxDownloads < 0 || len(input.Password) > 1024 || len(input.Name) > 200 {
		return model.Share{}, ErrInvalid
	}
	if input.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, input.ExpiresAt)
		if err != nil || !time.Now().Before(expiry) {
			return model.Share{}, ErrInvalid
		}
		input.ExpiresAt = expiry.UTC().Format(time.RFC3339Nano)
	}
	files := make([]model.File, len(sources))
	var total int64
	for i, src := range sources {
		files[i] = src.File
		if src.File.Size < 0 || src.File.Size > int64(^uint64(0)>>1)-total {
			return model.Share{}, ErrInvalid
		}
		total += src.File.Size
		if !src.File.Directory {
			f, err := openSource(src)
			if err != nil {
				return model.Share{}, ErrUnavailable
			}
			f.Close()
		}
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = path.Base(files[0].Path)
	}
	hash := ""
	var err error
	if input.Password != "" {
		hash, err = s.passwordHash(context.Background(), input.Password)
		if err != nil {
			return model.Share{}, err
		}
	}
	r := &shareRecord{Share: model.Share{ID: randomHex(16), Name: name, Files: files, TotalBytes: total, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), ExpiresAt: input.ExpiresAt, MaxDownloads: input.MaxDownloads, PasswordProtected: hash != ""}, Owner: owner, Secret: randomHex(32), PasswordHash: hash, Sources: append([]Source{}, sources...)}
	r.indexSources()
	b, err := json.Marshal(r)
	if err != nil {
		return model.Share{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.Share{}, ErrUnavailable
	}
	if _, err = s.db.Exec("INSERT INTO shares(id,document)VALUES(?,?)", r.Share.ID, b); err != nil {
		return model.Share{}, err
	}
	s.records[r.Share.ID] = r
	return ownerShare(r), nil
}
func (s *Store) Revoke(id, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.records[id]
	if r == nil || (owner != "local" && owner != r.Owner) {
		return ErrNotFound
	}
	if r.Share.Revoked {
		return nil
	}
	updated := *r
	updated.Share = copied(r.Share)
	updated.Share.Revoked = true
	b, _ := json.Marshal(&updated)
	if _, err := s.db.Exec("UPDATE shares SET document=? WHERE id=?", b, id); err != nil {
		return err
	}
	*r = updated
	return nil
}

func (s *Store) passwordHash(ctx context.Context, password string) (string, error) {
	select {
	case s.argonSlots <- struct{}{}:
		defer func() { <-s.argonSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		return "", ErrRateLimit
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return "$argon2id$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func (s *Store) checkPassword(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=1" {
		return false, ErrUnavailable
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false, ErrUnavailable
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false, ErrUnavailable
	}
	select {
	case s.argonSlots <- struct{}{}:
		defer func() { <-s.argonSlots }()
	case <-ctx.Done():
		return false, ctx.Err()
	default:
		return false, ErrRateLimit
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(key, expected) == 1, nil
}
func (s *Store) failureLimitedLocked(id string) bool {
	cutoff := time.Now().Add(-time.Minute)
	recent := []time.Time{}
	for _, t := range s.failures[id] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	s.failures[id] = recent
	return len(recent) >= 5
}
func (s *Store) Unlock(ctx context.Context, id, secret, password, existing string) (model.Share, string, error) {
	if len(secret) > 128 || len(password) > 1024 {
		return model.Share{}, "", ErrUnauthorized
	}
	s.mu.Lock()
	r := s.records[id]
	if r == nil {
		s.mu.Unlock()
		return model.Share{}, "", ErrNotFound
	}
	if existing != "" {
		if ss, ok := s.sessions[tokenHash(existing)]; ok && ss.ShareID == id && sessionCurrent(ss) {
			err := alive(r)
			v := publicShare(r)
			s.mu.Unlock()
			return v, existing, err
		}
	}
	if !equalSecret(secret, r.Secret) {
		s.mu.Unlock()
		return model.Share{}, "", ErrUnauthorized
	}
	if err := alive(r); err != nil {
		s.mu.Unlock()
		return model.Share{}, "", err
	}
	if s.failureLimitedLocked(id) {
		s.mu.Unlock()
		return model.Share{}, "", ErrRateLimit
	}
	encoded := r.PasswordHash
	s.mu.Unlock()
	if encoded != "" {
		ok, err := s.checkPassword(ctx, password, encoded)
		if err != nil {
			return model.Share{}, "", err
		}
		if !ok {
			s.mu.Lock()
			s.failures[id] = append(s.failures[id], time.Now())
			s.mu.Unlock()
			return model.Share{}, "", ErrUnauthorized
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r = s.records[id]
	if err := alive(r); err != nil {
		return model.Share{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return model.Share{}, "", err
	}
	token := randomHex(32)
	ss := session{ShareID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(ss)
	if _, err := s.db.Exec("INSERT INTO share_sessions(hash,document)VALUES(?,?)", tokenHash(token), b); err != nil {
		return model.Share{}, "", err
	}
	s.sessions[tokenHash(token)] = ss
	delete(s.failures, id)
	return publicShare(r), token, nil
}
func (s *Store) sessionLocked(id, token string) (*shareRecord, session, string, error) {
	r := s.records[id]
	if r == nil {
		return nil, session{}, "", ErrNotFound
	}
	h := tokenHash(token)
	ss, ok := s.sessions[h]
	if !ok || token == "" || ss.ShareID != id || !sessionCurrent(ss) {
		return nil, session{}, "", ErrUnauthorized
	}
	if err := alive(r); err != nil {
		return nil, session{}, "", err
	}
	return r, ss, h, nil
}
func (s *Store) Session(id, token string) (model.Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, _, _, err := s.sessionLocked(id, token)
	if err != nil {
		return model.Share{}, err
	}
	return publicShare(r), nil
}
func (s *Store) Check(id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, _, err := s.sessionLocked(id, token)
	return err
}
func (s *Store) BeginDownload(id, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ss, h, err := s.sessionLocked(id, token)
	if err != nil {
		return err
	}
	if ss.Counted {
		return nil
	}
	if r.Share.MaxDownloads > 0 && r.Share.Downloads >= r.Share.MaxDownloads {
		return ErrQuota
	}
	updated := *r
	updated.Share = copied(r.Share)
	updated.Share.Downloads++
	ss.Counted = true
	shareJSON, _ := json.Marshal(&updated)
	sessionJSON, _ := json.Marshal(ss)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE shares SET document=? WHERE id=?", shareJSON, id); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE share_sessions SET document=? WHERE hash=?", sessionJSON, h); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*r = updated
	s.sessions[h] = ss
	return nil
}
func (s *Store) Open(id, fileID string) (*os.File, model.File, error) {
	s.mu.Lock()
	r := s.records[id]
	if r == nil {
		s.mu.Unlock()
		return nil, model.File{}, ErrNotFound
	}
	if err := alive(r); err != nil {
		s.mu.Unlock()
		return nil, model.File{}, err
	}
	index, found := r.sourceIndex[fileID]
	var src Source
	if found {
		src = r.Sources[index]
		found = !src.File.Directory
	}
	s.mu.Unlock()
	if !found {
		return nil, model.File{}, ErrNotFound
	}
	f, err := openSource(src)
	if err != nil {
		return nil, src.File, ErrUnavailable
	}
	return f, src.File, nil
}
func (s *Store) ValidateOpen(id, fileID string, f *os.File) error {
	s.mu.Lock()
	r := s.records[id]
	if r == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	index, found := r.sourceIndex[fileID]
	var src Source
	if found {
		src = r.Sources[index]
	}
	s.mu.Unlock()
	if !found {
		return ErrNotFound
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return ErrUnavailable
	}
	identity, err := fileIdentity(f, info)
	if err != nil || identity != src.Identity || info.Size() != src.File.Size || info.ModTime().UnixNano() != src.ModifiedNano {
		return ErrUnavailable
	}
	return nil
}

func safeRelative(p string) bool {
	if p == "" || p == "." || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:\x00") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "" {
			return false
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	return true
}
func CapturePaths(paths []string) ([]Source, error) {
	return CapturePathsContext(context.Background(), paths)
}
func CapturePathsContext(ctx context.Context, paths []string) ([]Source, error) {
	out := []Source{}
	for _, selected := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(selected) {
			return nil, ErrInvalid
		}
		info, err := os.Lstat(selected)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrUnavailable
		}
		selected, err = filepath.EvalSymlinks(selected)
		if err != nil {
			return nil, ErrUnavailable
		}
		base := filepath.Base(selected)
		err = filepath.WalkDir(selected, func(p string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				return ErrUnavailable
			}
			if d.Type()&os.ModeSymlink != 0 {
				return ErrUnavailable
			}
			rel, err := filepath.Rel(selected, p)
			if err != nil {
				return ErrUnavailable
			}
			name := base
			if rel != "." {
				name = filepath.ToSlash(filepath.Join(base, rel))
			}
			if !safeRelative(name) {
				return ErrInvalid
			}
			fi, err := d.Info()
			if err != nil {
				return ErrUnavailable
			}
			src := Source{Path: p, File: model.File{ID: randomHex(16), Path: name, Directory: d.IsDir(), Modified: fi.ModTime().UnixMilli()}}
			if !d.IsDir() {
				src.File.Size = fi.Size()
				src, err = captureOne(src)
				if err != nil {
					return err
				}
			}
			out = append(out, src)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return validateSources(out)
}
func CaptureFiles(sources []Source) ([]Source, error) {
	return CaptureFilesContext(context.Background(), sources)
}
func CaptureFilesContext(ctx context.Context, sources []Source) ([]Source, error) {
	out := make([]Source, len(sources))
	for i, src := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !safeRelative(src.File.Path) {
			return nil, ErrInvalid
		}
		if src.File.ID == "" {
			src.File.ID = randomHex(16)
		}
		if src.File.Directory {
			out[i] = src
			continue
		}
		captured, err := captureOne(src)
		if err != nil {
			return nil, err
		}
		out[i] = captured
	}
	return validateSources(out)
}
func validateSources(sources []Source) ([]Source, error) {
	if len(sources) == 0 {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, src := range sources {
		k := strings.ToLower(src.File.Path)
		if seen[k] || ids[src.File.ID] {
			return nil, ErrInvalid
		}
		seen[k] = true
		ids[src.File.ID] = true
	}
	return sources, nil
}
func rejectLinks(p string) error {
	for {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrUnavailable
		}
		next := filepath.Dir(p)
		if next == p {
			return nil
		}
		p = next
	}
}
func captureOne(src Source) (Source, error) {
	if !filepath.IsAbs(src.Path) {
		return Source{}, ErrInvalid
	}
	var root *os.Root
	var relative, canonical string
	var err error
	if src.Root != "" {
		rel, relErr := filepath.Rel(src.Root, src.Path)
		if relErr != nil || !safeRelative(filepath.ToSlash(rel)) {
			return Source{}, ErrInvalid
		}
		canonicalRoot := systemPath(src.Root)
		root, err = secureRoot(canonicalRoot)
		if err != nil {
			return Source{}, ErrUnavailable
		}
		for p := rel; p != "."; p = filepath.Dir(p) {
			info, err := root.Lstat(p)
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				root.Close()
				return Source{}, ErrUnavailable
			}
		}
		relative = rel
		canonical = filepath.Join(canonicalRoot, rel)
	} else {
		canonical = systemPath(src.Path)
		relative = filepath.Base(canonical)
		root, err = secureRoot(filepath.Dir(canonical))
		if err != nil {
			return Source{}, ErrUnavailable
		}
	}
	defer root.Close()
	before, err := root.Lstat(relative)
	if err != nil || before.Mode()&os.ModeSymlink != 0 {
		return Source{}, ErrUnavailable
	}
	in, err := root.Open(relative)
	if err != nil {
		return Source{}, ErrUnavailable
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return Source{}, ErrUnavailable
	}
	if src.File.Size != info.Size() || (src.File.Modified != 0 && src.File.Modified != info.ModTime().UnixMilli()) {
		return Source{}, ErrUnavailable
	}
	identity, err := fileIdentity(in, info)
	if err != nil {
		return Source{}, ErrUnavailable
	}
	src.Path = canonical
	src.Identity = identity
	src.ModifiedNano = info.ModTime().UnixNano()
	src.File.Modified = info.ModTime().UnixMilli()
	src.File.SHA256 = ""
	src.Root = ""
	return src, nil
}
func openSource(src Source) (*os.File, error) {
	if err := rejectLinks(src.Path); err != nil {
		return nil, err
	}
	root, err := secureRoot(filepath.Dir(src.Path))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer root.Close()
	before, err := root.Lstat(filepath.Base(src.Path))
	if err != nil || before.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	}
	f, err := root.Open(filepath.Base(src.Path))
	if err != nil {
		return nil, ErrUnavailable
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		f.Close()
		return nil, ErrUnavailable
	}
	identity, err := fileIdentity(f, info)
	if err != nil || identity != src.Identity || info.Size() != src.File.Size || info.ModTime().UnixNano() != src.ModifiedNano {
		f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}

// System aliases are stable macOS filesystem entry points. Other symlinks in
// captured transfer roots are refused, including a replaced receive directory.
func systemPath(p string) string {
	if runtime.GOOS == "darwin" {
		for _, prefix := range []string{"/var", "/tmp", "/etc"} {
			if p == prefix || strings.HasPrefix(p, prefix+"/") {
				if resolved, err := filepath.EvalSymlinks(prefix); err == nil {
					return resolved + strings.TrimPrefix(p, prefix)
				}
			}
		}
	}
	return p
}
func secureRoot(absolute string) (*os.Root, error) {
	if !filepath.IsAbs(absolute) {
		return nil, ErrUnavailable
	}
	volume := filepath.VolumeName(absolute) + string(filepath.Separator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, ErrUnavailable
	}
	rel, err := filepath.Rel(volume, absolute)
	if err != nil {
		root.Close()
		return nil, ErrUnavailable
	}
	if rel == "." {
		return root, nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		before, err := root.Lstat(part)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, ErrUnavailable
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, ErrUnavailable
		}
		dir, err := next.Open(".")
		if err != nil {
			next.Close()
			root.Close()
			return nil, ErrUnavailable
		}
		after, err := dir.Stat()
		dir.Close()
		root.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, ErrUnavailable
		}
		root = next
	}
	return root, nil
}

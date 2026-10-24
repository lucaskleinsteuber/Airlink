package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

type managedFile struct {
	ID       string `json:"id"`
	Root     string `json:"root"`
	Relative string `json:"relative"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Identity string `json:"identity"`
	Modified int64  `json:"modified"`
}

func cloneOrganization(p model.Organization) model.Organization {
	p.Rules = append([]model.SortRule{}, p.Rules...)
	for i := range p.Rules {
		p.Rules[i].Extensions = append([]string{}, p.Rules[i].Extensions...)
	}
	return p
}
func ValidateOrganization(policy model.Organization) (model.Organization, error) {
	policy = cloneOrganization(policy)
	if len(policy.Rules) > 100 || (policy.SortEnabled && len(policy.Rules) == 0) {
		return model.Organization{}, fmt.Errorf("%w: sorting needs valid rules", ErrInvalid)
	}
	ids := map[string]bool{}
	folders := map[string]string{}
	for i := range policy.Rules {
		r := &policy.Rules[i]
		if r.ID == "" || len(r.ID) > 128 || ids[r.ID] || len(r.Extensions) == 0 || len(r.Extensions) > 100 {
			return model.Organization{}, ErrInvalid
		}
		ids[r.ID] = true
		if _, err := cleanPath(r.Folder); err != nil {
			return model.Organization{}, err
		}
		if len(r.Folder) > 512 {
			return model.Organization{}, ErrInvalid
		}
		seen := map[string]bool{}
		exts := []string{}
		for _, ext := range r.Extensions {
			ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
			if ext == "" || len(ext) > 32 {
				return model.Organization{}, ErrInvalid
			}
			for _, c := range ext {
				if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
					return model.Organization{}, ErrInvalid
				}
			}
			if !seen[ext] {
				exts = append(exts, ext)
				seen[ext] = true
			}
		}
		r.Extensions = exts
		for prefix := r.Folder; prefix != "."; prefix = path.Dir(prefix) {
			key := strings.ToLower(prefix)
			if old, ok := folders[key]; ok && old != prefix {
				return model.Organization{}, fmt.Errorf("%w: conflicting folder casing", ErrInvalid)
			}
			folders[key] = prefix
		}
	}
	return policy, nil
}
func (e *Engine) SetOrganization(policy model.Organization) error {
	normalized, err := ValidateOrganization(policy)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrConflict
	}
	e.organization = normalized
	return nil
}
func destination(f model.File) string {
	if f.DestinationPath != "" {
		return f.DestinationPath
	}
	return f.Path
}
func snapshotDestinations(files []model.File, policy model.Organization) ([]model.File, error) {
	files = append([]model.File{}, files...)
	folderRoots := map[string]bool{}
	for _, f := range files {
		if f.Directory || strings.Contains(f.Path, "/") {
			folderRoots[strings.ToLower(strings.SplitN(f.Path, "/", 2)[0])] = true
		}
	}
	for i := range files {
		f := &files[i]
		f.DestinationPath = f.Path
		if !policy.SortEnabled || f.Directory || strings.Contains(f.Path, "/") {
			continue
		}
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(f.Path), "."))
		for _, r := range policy.Rules {
			matched := false
			for _, candidate := range r.Extensions {
				if ext == candidate {
					matched = true
					break
				}
			}
			if matched {
				if folderRoots[strings.ToLower(strings.SplitN(r.Folder, "/", 2)[0])] {
					return nil, fmt.Errorf("%w: sorting collides with a transferred folder", ErrInvalid)
				}
				f.DestinationPath = path.Join(r.Folder, f.Path)
				break
			}
		}
	}
	types := map[string]bool{}
	directoryCase := map[string]string{}
	for _, f := range files {
		p := destination(f)
		if _, err := cleanPath(p); err != nil {
			return nil, err
		}
		key := strings.ToLower(p)
		if _, ok := types[key]; ok {
			return nil, fmt.Errorf("%w: conflicting destination", ErrInvalid)
		}
		types[key] = f.Directory
		parent := path.Dir(p)
		if f.Directory {
			parent = p
		}
		for parent != "." {
			key := strings.ToLower(parent)
			if existing, ok := directoryCase[key]; ok && existing != parent {
				return nil, fmt.Errorf("%w: destination directory casing conflict", ErrInvalid)
			}
			directoryCase[key] = parent
			parent = path.Dir(parent)
		}
	}
	for dir := range directoryCase {
		if isDir, exists := types[dir]; exists && !isDir {
			return nil, fmt.Errorf("%w: file conflicts with destination directory", ErrInvalid)
		}
	}
	return files, nil
}
func (e *Engine) loadManaged() error {
	rows, err := e.db.Query("SELECT document FROM managed_files")
	if err != nil {
		return err
	}
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			break
		}
		var c managedFile
		if err = json.Unmarshal(b, &c); err != nil {
			break
		}
		e.managed[c.ID] = c
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = e.db.Query("SELECT path FROM managed_seen")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err = rows.Scan(&p); err != nil {
			return err
		}
		e.managedSeen[p] = true
	}
	return rows.Err()
}
func (e *Engine) indexFileLocked(r *record, f model.File) error {
	if r.Transfer.Direction != "receive" || !f.Completed || f.Directory || f.Skipped || !validHash(f.SHA256) {
		return nil
	}
	relative := destination(f)
	key := filepath.Join(organizationSystemPath(r.ReceiveRoot), filepath.FromSlash(relative))
	if e.managedSeen[key] {
		return nil
	}
	c := managedFile{ID: identifier(), Root: organizationSystemPath(r.ReceiveRoot), Relative: relative, Size: f.Size, SHA256: f.SHA256}
	in, info, err := openManagedRaw(c)
	if err == nil {
		c.Identity, err = managedIdentity(in, info)
		c.Modified = info.ModTime().UnixNano()
		in.Close()
	}
	if err != nil {
		_, err = e.db.Exec("INSERT OR IGNORE INTO managed_seen(path)VALUES(?)", key)
		if err == nil {
			e.managedSeen[key] = true
		}
		return err
	}
	b, _ := json.Marshal(c)
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO managed_files(id,sha,size,document)VALUES(?,?,?,?)", c.ID, c.SHA256, c.Size, b); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO managed_seen(path)VALUES(?)", key); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	e.managed[c.ID] = c
	e.managedSeen[key] = true
	return nil
}
func (e *Engine) invalidateManaged(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	delete(e.managed, id)
	_, _ = e.db.Exec("DELETE FROM managed_files WHERE id=?", id)
}
func openManaged(c managedFile) (*os.File, error) {
	f, info, err := openManagedRaw(c)
	if err != nil {
		return nil, err
	}
	identity, err := managedIdentity(f, info)
	if err != nil || identity != c.Identity || info.ModTime().UnixNano() != c.Modified {
		f.Close()
		return nil, ErrConflict
	}
	return f, nil
}
func verifyManaged(ctx context.Context, c managedFile) error {
	in, err := openManaged(c)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, contextReader{ctx, in}, make([]byte, 256*1024))
	info, statErr := in.Stat()
	in.Close()
	if err != nil {
		return err
	}
	if statErr != nil || info.ModTime().UnixNano() != c.Modified || n != c.Size || hex.EncodeToString(h.Sum(nil)) != c.SHA256 {
		return ErrIntegrity
	}
	again, err := openManaged(c)
	if err != nil {
		return err
	}
	again.Close()
	return ctx.Err()
}
func (e *Engine) findDuplicate(ctx context.Context, hash string, size int64) (*managedFile, error) {
	e.mu.Lock()
	candidates := []managedFile{}
	for _, c := range e.managed {
		if c.Size == size && c.SHA256 == hash {
			candidates = append(candidates, c)
		}
	}
	e.mu.Unlock()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		in, err := openManaged(c)
		if err != nil {
			e.invalidateManaged(c.ID)
			continue
		}
		in.Close()
		err = verifyManaged(ctx, c)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			e.invalidateManaged(c.ID)
			return nil, nil
		}
		return &c, nil
	}
	return nil, nil
}
func privateCandidate(c managedFile) model.DuplicateCandidate {
	return model.DuplicateCandidate{ID: c.ID, Path: filepath.Join(c.Root, filepath.FromSlash(c.Relative)), Size: c.Size, SHA256: c.SHA256}
}
func (e *Engine) DuplicatePath(id string) (string, error) {
	e.mu.Lock()
	candidate, ok := e.managed[id]
	e.mu.Unlock()
	if !ok {
		return "", ErrNotFound
	}
	f, err := openManaged(candidate)
	if err != nil {
		e.invalidateManaged(id)
		return "", ErrConflict
	}
	f.Close()
	return privateCandidate(candidate).Path, nil
}

func (e *Engine) beginDuplicateLocked(ctx context.Context, r *record, index int, hash string) (*verificationOperation, error) {
	r.Transfer.State = "checkingDuplicate"
	r.Transfer.Files[index].SHA256 = hash
	opCtx, cancel := context.WithCancel(ctx)
	op := &verificationOperation{ctx: opCtx, cancel: cancel}
	e.verifications[r.Transfer.ID] = op
	if err := e.saveLocked(r); err != nil {
		cancel()
		delete(e.verifications, r.Transfer.ID)
		return nil, err
	}
	return op, nil
}
func (e *Engine) finishDuplicateOperation(id string, op *verificationOperation) {
	op.cancel()
	e.mu.Lock()
	if e.verifications[id] == op {
		delete(e.verifications, id)
		if r := e.records[id]; r != nil && !e.closed {
			pending := false
			for _, f := range r.Transfer.Files {
				if f.DuplicateState == "pending" && !f.Completed {
					pending = true
					break
				}
			}
			if r.Transfer.State == "checkingDuplicate" || r.Transfer.State == "verifying" || ((r.Transfer.State == "queued" || r.Transfer.State == "running") && pending) {
				if pending {
					r.Transfer.State = "waitingDuplicate"
				} else {
					r.Transfer.State = "queued"
				}
				_ = e.saveLocked(r)
				_ = e.scheduleLocked()
			}
		}
	}
	e.mu.Unlock()
}
func firstIncomplete(t model.Transfer, fileID string) bool {
	for _, f := range t.Files {
		if !f.Directory && !f.Completed {
			return f.ID == fileID
		}
	}
	return false
}
func (e *Engine) duplicateResultLocked(r *record, index int, op *verificationOperation, candidate *managedFile) (string, error) {
	if e.verifications[r.Transfer.ID] != op || op.ctx.Err() != nil || r.Transfer.State != "checkingDuplicate" {
		return "", ErrConflict
	}
	f := &r.Transfer.Files[index]
	if candidate != nil {
		f.DuplicateState = "pending"
		f.DuplicateCandidates = []model.DuplicateCandidate{privateCandidate(*candidate)}
		r.Transfer.State = "waitingDuplicate"
	} else {
		f.DuplicateState = "checked"
		f.DuplicateCandidates = nil
		r.Transfer.State = "queued"
	}
	if err := e.saveLocked(r); err != nil {
		return "", err
	}
	if err := e.scheduleLocked(); err != nil {
		return "", err
	}
	if candidate != nil {
		return "pending", nil
	}
	return "upload", nil
}
func (e *Engine) PrepareFile(ctx context.Context, id, fileID, hash string) (string, error) {
	if !validHash(hash) {
		return "", ErrInvalid
	}
	lock, err := e.fileLock(id, fileID)
	if err != nil {
		return "", err
	}
	lock.Lock()
	defer lock.Unlock()
	e.mu.Lock()
	r, err := e.getLocked(id)
	if err != nil {
		e.mu.Unlock()
		return "", err
	}
	index, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		e.mu.Unlock()
		return "", err
	}
	f := r.Transfer.Files[index]
	if r.Transfer.Direction != "receive" || f.Directory || r.Transfer.State == "offered" || r.Transfer.State == "paused" || r.Transfer.State == "cancelled" || r.Transfer.State == "failed" {
		e.mu.Unlock()
		return "", ErrConflict
	}
	if f.Skipped {
		e.mu.Unlock()
		if f.SHA256 == hash {
			return "skip", nil
		}
		return "", ErrIntegrity
	}
	if f.SHA256 != "" && f.SHA256 != hash {
		e.mu.Unlock()
		return "", ErrIntegrity
	}
	if f.DuplicateState == "pending" {
		e.mu.Unlock()
		return "pending", nil
	}
	if f.Completed || !r.Organization.DetectDuplicates || f.DuplicateState == "checked" || f.DuplicateState == "keep" {
		e.mu.Unlock()
		return "upload", nil
	}
	if r.Transfer.State != "running" || !firstIncomplete(r.Transfer, fileID) {
		e.mu.Unlock()
		return "", ErrConflict
	}
	op, err := e.beginDuplicateLocked(ctx, r, index, hash)
	e.mu.Unlock()
	if err != nil {
		return "", err
	}
	defer e.finishDuplicateOperation(id, op)
	candidate, err := e.findDuplicate(op.ctx, hash, f.Size)
	if err != nil {
		return "", e.duplicateOperationError(id, op, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err = e.getLocked(id)
	if err != nil {
		return "", err
	}
	return e.duplicateResultLocked(r, index, op, candidate)
}
func (e *Engine) duplicateOperationError(id string, op *verificationOperation, cause error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.verifications[id] != op {
		return ErrConflict
	}
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	if r.Transfer.State == "checkingDuplicate" {
		r.Transfer.State = "queued"
		r.Transfer.Error = "Duplicate check interrupted; retry preparation."
		_ = e.saveLocked(r)
		_ = e.scheduleLocked()
	}
	return cause
}
func (e *Engine) ResolveDuplicate(ctx context.Context, id, fileID, action, candidateID string) (model.Transfer, error) {
	if action != "keep" && action != "skip" {
		return model.Transfer{}, ErrInvalid
	}
	lock, err := e.fileLock(id, fileID)
	if err != nil {
		return model.Transfer{}, err
	}
	lock.Lock()
	defer lock.Unlock()
	e.mu.Lock()
	r, err := e.getLocked(id)
	if err != nil {
		e.mu.Unlock()
		return model.Transfer{}, err
	}
	index, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		e.mu.Unlock()
		return model.Transfer{}, err
	}
	f := r.Transfer.Files[index]
	if r.Transfer.Direction != "receive" || f.Directory || f.Completed || f.DuplicateState != "pending" || r.Transfer.State != "waitingDuplicate" {
		e.mu.Unlock()
		return model.Transfer{}, ErrConflict
	}
	if action == "keep" {
		r.Transfer.Files[index].DuplicateState = "keep"
		r.Transfer.Files[index].DuplicateCandidates = nil
		r.Transfer.State = "queued"
		if err = e.saveLocked(r); err == nil {
			err = e.scheduleLocked()
		}
		out := clone(r.Transfer)
		e.mu.Unlock()
		return out, err
	}
	var candidate managedFile
	found := false
	for _, c := range f.DuplicateCandidates {
		if c.ID == candidateID {
			candidate, found = e.managed[c.ID]
			break
		}
	}
	if !found || candidate.SHA256 != f.SHA256 || candidate.Size != f.Size {
		e.mu.Unlock()
		return model.Transfer{}, ErrConflict
	}
	opCtx, cancel := context.WithCancel(ctx)
	op := &verificationOperation{ctx: opCtx, cancel: cancel}
	e.verifications[id] = op
	r.Transfer.State = "queued"
	if err = e.saveLocked(r); err == nil {
		err = e.scheduleLocked()
	}
	e.mu.Unlock()
	if err != nil {
		e.finishDuplicateOperation(id, op)
		return model.Transfer{}, err
	}
	defer e.finishDuplicateOperation(id, op)
	for {
		e.mu.Lock()
		r, err = e.getLocked(id)
		if err == nil && (e.verifications[id] != op || op.ctx.Err() != nil) {
			err = ErrConflict
		}
		if err != nil {
			e.mu.Unlock()
			return model.Transfer{}, err
		}
		if r.Transfer.State == "running" {
			r.Transfer.State = "checkingDuplicate"
			err = e.saveLocked(r)
			e.mu.Unlock()
			if err != nil {
				return model.Transfer{}, err
			}
			break
		}
		e.mu.Unlock()
		select {
		case <-op.ctx.Done():
			return model.Transfer{}, e.restoreDuplicateWait(id, op, op.ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	err = verifyManaged(op.ctx, candidate)
	if err != nil {
		if op.ctx.Err() == nil {
			e.invalidateManaged(candidate.ID)
		}
		return model.Transfer{}, e.restoreDuplicateWait(id, op, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err = e.getLocked(id)
	if err != nil {
		return model.Transfer{}, err
	}
	if e.verifications[id] != op || op.ctx.Err() != nil || r.Transfer.State != "checkingDuplicate" {
		return model.Transfer{}, ErrConflict
	}
	if _, stillIndexed := e.managed[candidate.ID]; !stillIndexed {
		return model.Transfer{}, ErrConflict
	}
	fresh, validateErr := openManaged(candidate)
	if validateErr != nil {
		delete(e.managed, candidate.ID)
		_, _ = e.db.Exec("DELETE FROM managed_files WHERE id=?", candidate.ID)
		return model.Transfer{}, ErrConflict
	}
	fresh.Close()
	root, err := organizationRoot(r.ReceiveRoot)
	if err != nil {
		return model.Transfer{}, err
	}
	err = root.Remove(partialName(fileID))
	root.Close()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return model.Transfer{}, err
	}
	selected := &r.Transfer.Files[index]
	selected.Skipped = true
	selected.Completed = true
	selected.DuplicateState = "skipped"
	selected.DuplicateCandidates = nil
	r.Transfer.SkippedBytes += selected.Size - selected.Received
	r.Transfer.State = "queued"
	if allComplete(r.Transfer.Files) {
		r.Transfer.State = "completed"
	}
	r.Transfer.BytesPerSecond = 0
	if err = e.saveLocked(r); err == nil {
		err = e.scheduleLocked()
	}
	return clone(r.Transfer), err
}
func (e *Engine) restoreDuplicateWait(id string, op *verificationOperation, cause error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.verifications[id] != op {
		return ErrConflict
	}
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	r.Transfer.State = "waitingDuplicate"
	r.Transfer.Error = "Existing file changed or duplicate check was interrupted. Keep the new copy or retry."
	_ = e.saveLocked(r)
	_ = e.scheduleLocked()
	return cause
}
func (e *Engine) MarkOutgoingSkipped(id, fileID string, received int64, hash string) error {
	if !validHash(hash) {
		return ErrInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	index, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		return err
	}
	f := &r.Transfer.Files[index]
	if r.Transfer.Direction != "send" || received < 0 || received > f.Size || f.Directory || r.Transfer.State == "paused" || r.Transfer.State == "cancelled" || r.Transfer.State == "failed" {
		return ErrConflict
	}
	if f.Skipped && f.SHA256 == hash && f.Received == received {
		return nil
	}
	if f.SHA256 != "" && f.SHA256 != hash {
		return ErrIntegrity
	}
	if f.Completed {
		return ErrConflict
	}
	r.Transfer.CompletedBytes += received - f.Received
	r.Transfer.SkippedBytes += f.Size - received
	f.Received = received
	f.SHA256 = hash
	f.Skipped = true
	f.Completed = true
	f.DuplicateState = "skipped"
	f.DuplicateCandidates = nil
	if allComplete(r.Transfer.Files) {
		r.Transfer.State = "completed"
		r.Transfer.BytesPerSecond = 0
	}
	if err = e.saveLocked(r); err != nil {
		return err
	}
	return e.scheduleLocked()
}
func (e *Engine) schedulePublicationLocked(r *record) {
	if r.Transfer.Direction != "receive" || r.Transfer.State != "running" {
		return
	}
	for _, f := range r.Transfer.Files {
		if f.Directory || f.Completed {
			continue
		}
		if f.DuplicateState == "pending" || f.Received != f.Size || !validHash(f.SHA256) || (r.LegacyVerified[f.ID] == "" && !(f.Size == 0 && f.DuplicateState == "keep")) {
			return
		}
		key := r.Transfer.ID + "/" + f.ID
		if e.autoPublishing[key] {
			return
		}
		e.autoPublishing[key] = true
		id, fileID, hash := r.Transfer.ID, f.ID, f.SHA256
		e.autoWG.Add(1)
		go func() {
			defer e.autoWG.Done()
			_ = e.CompleteFile(e.ctx, id, fileID, hash)
			e.mu.Lock()
			delete(e.autoPublishing, key)
			e.mu.Unlock()
		}()
		return
	}
}
func (e *Engine) MarkOutgoingWaiting(id, state string) error {
	if state != "checkingDuplicate" && state != "waitingDuplicate" {
		return ErrInvalid
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	if r.Transfer.Direction != "send" || r.Transfer.State == "paused" || terminal(r.Transfer.State) {
		return ErrConflict
	}
	r.Transfer.State = state
	r.Transfer.BytesPerSecond = 0
	if err = e.saveLocked(r); err != nil {
		return err
	}
	return e.scheduleLocked()
}

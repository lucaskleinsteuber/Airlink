package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lucasfd3/Airlink/internal/model"
)

func captureSource(p string, before fs.FileInfo) (source, error) {
	c := managedFile{Root: filepath.Dir(p), Relative: filepath.Base(p), Size: before.Size()}
	file, info, err := openManagedRaw(c)
	if err != nil {
		return source{}, ErrSourceChanged
	}
	defer file.Close()
	if !os.SameFile(before, info) || !before.ModTime().Equal(info.ModTime()) {
		return source{}, ErrSourceChanged
	}
	identity, err := managedIdentity(file, info)
	if err != nil {
		return source{}, ErrSourceChanged
	}
	return source{Path: p, Size: info.Size(), Modified: info.ModTime().UnixNano(), Identity: identity}, nil
}
func (e *Engine) sourceSnapshot(id, fileID string) (source, model.File, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, err := e.getLocked(id)
	if err != nil {
		return source{}, model.File{}, err
	}
	index, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		return source{}, model.File{}, err
	}
	f := r.Transfer.Files[index]
	src, ok := r.Sources[fileID]
	if r.Transfer.Direction != "send" || !ok || f.Directory || f.Skipped || r.Transfer.State == "paused" || r.Transfer.State == "cancelled" || r.Transfer.State == "failed" {
		return source{}, model.File{}, ErrConflict
	}
	return src, f, nil
}
func openSourceSnapshot(src source) (*os.File, string, error) {
	ref := managedFile{Root: filepath.Dir(src.Path), Relative: filepath.Base(src.Path), Size: src.Size}
	f, info, err := openManagedRaw(ref)
	if err != nil {
		return nil, "", ErrSourceChanged
	}
	identity, err := managedIdentity(f, info)
	if err != nil || info.ModTime().UnixNano() != src.Modified || (src.Identity != "" && src.Identity != identity) {
		f.Close()
		return nil, "", ErrSourceChanged
	}
	return f, identity, nil
}
func validateSourceFD(src source, file *os.File, identity string) error {
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != src.Size || info.ModTime().UnixNano() != src.Modified {
		return ErrSourceChanged
	}
	actual, err := managedIdentity(file, info)
	if err != nil || actual != identity || (src.Identity != "" && src.Identity != actual) {
		return ErrSourceChanged
	}
	return nil
}
func (e *Engine) ValidateSource(id, fileID string, file *os.File) error {
	src, _, err := e.sourceSnapshot(id, fileID)
	if err != nil {
		return err
	}
	if src.Identity == "" {
		return ErrSourceChanged
	}
	if err = validateSourceFD(src, file, src.Identity); err != nil {
		return err
	}
	current, _, err := openSourceSnapshot(src)
	if err != nil {
		return err
	}
	current.Close()
	return nil
}
func (e *Engine) beginSourceOperation(ctx context.Context, id string) (context.Context, func(), error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := e.getLocked(id); err != nil {
		return nil, nil, err
	}
	opCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(e.ctx, cancel)
	key := identifier()
	if e.sourceOperations[id] == nil {
		e.sourceOperations[id] = map[string]context.CancelFunc{}
	}
	e.sourceOperations[id][key] = cancel
	return opCtx, func() {
		stop()
		cancel()
		e.mu.Lock()
		delete(e.sourceOperations[id], key)
		if len(e.sourceOperations[id]) == 0 {
			delete(e.sourceOperations, id)
		}
		e.mu.Unlock()
	}, nil
}
func (e *Engine) cancelSourceOperationsLocked(id string) {
	for _, cancel := range e.sourceOperations[id] {
		cancel()
	}
	delete(e.sourceOperations, id)
}
func (e *Engine) persistSourceAnchor(ctx context.Context, id, fileID string, original source, identity, hash string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := e.getLocked(id)
	if err != nil {
		return err
	}
	index, err := fileIndex(&r.Transfer, fileID)
	if err != nil {
		return err
	}
	current, ok := r.Sources[fileID]
	if !ok || current != original || r.Transfer.State == "paused" || r.Transfer.State == "cancelled" || r.Transfer.State == "failed" || r.Transfer.Files[index].Skipped {
		return ErrConflict
	}
	known := r.Transfer.Files[index].SHA256
	if hash != "" && known != "" && known != hash {
		return ErrSourceChanged
	}
	if original.Identity == "" && hash == "" && validHash(known) {
		return ErrConflict
	}
	original.Identity = identity
	check, _, err := openSourceSnapshot(original)
	if err != nil {
		return err
	}
	check.Close()
	updated := *r
	updated.Transfer = clone(r.Transfer)
	updated.Sources = make(map[string]source, len(r.Sources))
	for key, value := range r.Sources {
		updated.Sources[key] = value
	}
	updated.Sources[fileID] = original
	if hash != "" {
		updated.Transfer.Files[index].SHA256 = hash
	}
	if err = e.saveLocked(&updated); err != nil {
		return err
	}
	*r = updated
	return nil
}

// OpenDownloadContext validates old references lazily. A pre-upgrade cached
// digest is never reused merely because a replacement preserved size and time.
func (e *Engine) OpenDownloadContext(ctx context.Context, id, fileID string) (*os.File, model.File, error) {
	src, f, err := e.sourceSnapshot(id, fileID)
	if err != nil {
		return nil, model.File{}, err
	}
	if err = ctx.Err(); err != nil {
		return nil, f, err
	}
	if src.Identity == "" {
		if validHash(f.SHA256) {
			if _, err = e.SourceSHA256(ctx, id, fileID); err != nil {
				return nil, f, err
			}
		} else {
			lock, lockErr := e.fileLock(id, fileID)
			if lockErr != nil {
				return nil, f, lockErr
			}
			lock.Lock()
			latest, _, snapshotErr := e.sourceSnapshot(id, fileID)
			if snapshotErr == nil && latest.Identity == "" {
				file, identity, openErr := openSourceSnapshot(latest)
				if openErr != nil {
					snapshotErr = openErr
				} else {
					file.Close()
					snapshotErr = e.persistSourceAnchor(ctx, id, fileID, latest, identity, "")
				}
			}
			lock.Unlock()
			if snapshotErr != nil {
				return nil, f, snapshotErr
			}
		}
		src, f, err = e.sourceSnapshot(id, fileID)
		if err != nil {
			return nil, f, err
		}
	}
	file, _, err := openSourceSnapshot(src)
	if err != nil {
		return nil, f, err
	}
	return file, f, nil
}
func (e *Engine) hashSource(ctx context.Context, id, fileID string) (string, error) {
	lock, err := e.fileLock(id, fileID)
	if err != nil {
		return "", err
	}
	lock.Lock()
	defer lock.Unlock()
	opCtx, finish, err := e.beginSourceOperation(ctx, id)
	if err != nil {
		return "", err
	}
	defer finish()
	src, offered, err := e.sourceSnapshot(id, fileID)
	if err != nil {
		return "", err
	}
	file, identity, err := openSourceSnapshot(src)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if src.Identity != "" && validHash(offered.SHA256) {
		return offered.SHA256, opCtx.Err()
	}
	h := sha256.New()
	n, err := io.CopyBuffer(h, contextReader{opCtx, file}, make([]byte, 256*1024))
	if err != nil {
		return "", err
	}
	if n != src.Size {
		return "", ErrSourceChanged
	}
	if err = validateSourceFD(src, file, identity); err != nil {
		return "", err
	}
	hash := hex.EncodeToString(h.Sum(nil))
	if validHash(offered.SHA256) && hash != offered.SHA256 {
		return "", ErrSourceChanged
	}
	if err = e.persistSourceAnchor(opCtx, id, fileID, src, identity, hash); err != nil {
		return "", err
	}
	return hash, nil
}

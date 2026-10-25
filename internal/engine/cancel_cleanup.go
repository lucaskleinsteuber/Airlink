package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type cancelledPartial struct {
	name string
	lock *sync.Mutex
}

// Called only under mu after the cancelled state is durable, or at the end of
// startup. It never waits for a file lock while holding the engine lock.
func (e *Engine) queueCancelledCleanupLocked(r *record) {
	if e.closed || !r.DirectReceive || r.Transfer.Direction != "receive" || r.Transfer.State != "cancelled" {
		return
	}
	key := "cancelled-cleanup:" + r.Transfer.ID
	if e.autoPublishing[key] {
		return
	}
	stage := organizationSystemPath(r.ReceiveRoot)
	destination := organizationSystemPath(r.Transfer.Destination)
	id := r.Transfer.ID
	expected := filepath.Join(destination, ".airlink-partials", id)
	if stage != expected || len(id) != 32 {
		return
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return
		}
	}
	files := make([]cancelledPartial, 0, len(r.Transfer.Files))
	for _, file := range r.Transfer.Files {
		if file.Directory {
			continue
		}
		value, _ := e.locks.LoadOrStore(r.Transfer.ID+"/"+file.ID, new(sync.Mutex))
		files = append(files, cancelledPartial{name: filepath.Base(partialName(file.ID)), lock: value.(*sync.Mutex)})
	}
	e.autoPublishing[key] = true
	e.autoWG.Add(1)
	go func() {
		defer e.autoWG.Done()
		defer func() { e.mu.Lock(); delete(e.autoPublishing, key); e.mu.Unlock() }()
		e.cleanupCancelledStage(e.ctx, stage, destination, id, files)
	}()
}

func waitCancelledFileLock(ctx context.Context, lock *sync.Mutex, deadline time.Time) bool {
	for !lock.TryLock() {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return false
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return true
}

func (e *Engine) cleanupCancelledStage(ctx context.Context, stage, destination, id string, files []cancelledPartial) {
	if ctx.Err() != nil {
		return
	}
	root, err := organizationRoot(stage)
	if err != nil {
		return
	}
	defer root.Close()
	identity, err := directoryIdentity(root, ".")
	if err != nil {
		return
	}
	var partials *os.Root
	var partialIdentity string
	before, err := root.Lstat(".airlink-partials")
	if err == nil {
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			return
		}
		partials, err = root.OpenRoot(".airlink-partials")
		if err != nil {
			return
		}
		defer partials.Close()
		opened, err := partials.Open(".")
		if err != nil {
			return
		}
		after, err := opened.Stat()
		opened.Close()
		if err != nil || !os.SameFile(before, after) {
			return
		}
		partialIdentity, err = directoryIdentity(partials, ".")
		if err != nil {
			return
		}
	} else if !os.IsNotExist(err) {
		return
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, file := range files {
		if partials == nil {
			break
		}
		if ctx.Err() != nil || time.Now().After(deadline) || !waitCancelledFileLock(ctx, file.lock, deadline) {
			return
		}
		if ctx.Err() != nil {
			file.lock.Unlock()
			return
		}
		// File IDs come only from this manifest. Unknown files, links and any
		// public destination are outside the cleanup set.
		info, statErr := partials.Lstat(file.name)
		if statErr == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
			_ = partials.Remove(file.name)
		}
		file.lock.Unlock()
	}
	// Serialize empty-directory removal with acceptance creating another stage.
	// File I/O has finished and no file lock is held when mu is acquired.
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || ctx.Err() != nil {
		return
	}
	if partials != nil {
		partials.Close()
		if current, err := directoryIdentity(root, ".airlink-partials"); err != nil || current != partialIdentity {
			return
		}
	}
	if err := root.Remove(".airlink-partials"); err != nil && !os.IsNotExist(err) {
		return
	}
	_ = syncRootDirectory(root, ".")
	root.Close()
	parent, err := organizationRoot(destination)
	if err != nil {
		return
	}
	defer parent.Close()
	relative := filepath.Join(".airlink-partials", id)
	currentIdentity, err := directoryIdentity(parent, relative)
	if err != nil || currentIdentity != identity {
		return
	}
	privateIdentity, err := directoryIdentity(parent, ".airlink-partials")
	if err != nil {
		return
	}
	if err = parent.Remove(relative); err != nil {
		return
	}
	_ = syncRootDirectory(parent, ".airlink-partials")
	if current, err := directoryIdentity(parent, ".airlink-partials"); err == nil && current == privateIdentity {
		_ = parent.Remove(".airlink-partials")
		_ = syncRootDirectory(parent, ".")
	}
}

package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

// sourceIdentity binds a source to its file system object and modification
// time, both taken from directory metadata that is read anyway.
func sourceIdentity(info fs.FileInfo) string {
	return fileIdentity(info) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

// collectSources walks the selected files and folders. Only metadata is
// read; files are opened when they are sent. Links and special files inside
// folders are skipped; a selected link is rejected.
func collectSources(ctx context.Context, paths []string) ([]fileEntry, int64, error) {
	files := []fileEntry{}
	tops := map[string]bool{}
	var total int64
	add := func(f fileEntry) error {
		if len(files) >= MaxFiles {
			return fmt.Errorf("%w: more than %d files selected", ErrInvalid, MaxFiles)
		}
		if err := validWirePath(f.Path); err != nil {
			return fmt.Errorf("%w: %s", err, f.Path)
		}
		f.ID = fileID(len(files))
		files = append(files, f)
		total += f.Size
		return nil
	}
	for _, selected := range paths {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if !filepath.IsAbs(selected) {
			return nil, 0, fmt.Errorf("%w: absolute path required", ErrInvalid)
		}
		selected = filepath.Clean(selected)
		info, err := os.Lstat(selected)
		if err != nil {
			return nil, 0, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, 0, fmt.Errorf("%w: symbolic links are not transferred: %s", ErrInvalid, filepath.Base(selected))
		}
		// Canonical ancestors (macOS /var -> /private/var); the item itself is
		// not a link.
		canonical, err := filepath.EvalSymlinks(selected)
		if err != nil {
			return nil, 0, err
		}
		base := filepath.Base(canonical)
		top := base
		for n := 2; tops[strings.ToLower(top)]; n++ {
			top = numberedName(base, n, info.IsDir())
		}
		tops[strings.ToLower(top)] = true
		switch {
		case info.Mode().IsRegular():
			f := fileEntry{source: canonical, identity: sourceIdentity(info)}
			f.Path, f.Size, f.Modified = top, info.Size(), info.ModTime().UnixMilli()
			if err = add(f); err != nil {
				return nil, 0, err
			}
		case info.IsDir():
			err = filepath.WalkDir(canonical, func(p string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				rel, err := filepath.Rel(canonical, p)
				if err != nil {
					return err
				}
				wire := top
				if rel != "." {
					wire = top + "/" + filepath.ToSlash(rel)
				}
				switch {
				case d.Type()&fs.ModeSymlink != 0:
					return nil
				case d.IsDir():
					f := fileEntry{}
					f.Path, f.Directory = wire, true
					if info, err := d.Info(); err == nil {
						f.Modified = info.ModTime().UnixMilli()
					}
					return add(f)
				case d.Type().IsRegular():
					info, err := d.Info()
					if err != nil {
						return err
					}
					f := fileEntry{source: p, identity: sourceIdentity(info)}
					f.Path, f.Size, f.Modified = wire, info.Size(), info.ModTime().UnixMilli()
					return add(f)
				}
				return nil
			})
			if err != nil {
				return nil, 0, err
			}
		default:
			return nil, 0, fmt.Errorf("%w: only regular files and folders can be sent", ErrInvalid)
		}
	}
	if len(files) == 0 {
		return nil, 0, fmt.Errorf("%w: nothing to send", ErrInvalid)
	}
	return files, total, nil
}

// Offer prepares a send to a paired device. A repeated requestID returns the
// transfer created by the first request.
func (e *Engine) Offer(ctx context.Context, device model.Device, paths []string, requestID string) (model.Transfer, error) {
	if !device.Paired || !device.CanReceive {
		return model.Transfer{}, fmt.Errorf("%w: device cannot receive", ErrConflict)
	}
	if len(requestID) > 128 {
		return model.Transfer{}, ErrInvalid
	}
	if requestID != "" {
		e.mu.Lock()
		for _, r := range e.records {
			if r.requestID == requestID && r.t.DeviceID == device.ID && r.t.Direction == "send" {
				view := e.summary(r)
				e.mu.Unlock()
				return view, nil
			}
		}
		e.mu.Unlock()
	}
	files, total, err := collectSources(ctx, paths)
	if err != nil {
		return model.Transfer{}, err
	}
	stamp := now()
	r := &record{files: files, requestID: requestID}
	r.t.ID, r.t.DeviceID, r.t.DeviceName, r.t.Direction, r.t.State = newID(), device.ID, device.Name, "send", "offered"
	r.t.TotalBytes, r.t.FileCount, r.t.Title, r.t.CreatedAt, r.t.UpdatedAt = total, len(files), files[0].Path, stamp, stamp
	r.preview = files[:min(3, len(files))]
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return model.Transfer{}, errors.New("transfer engine closed")
	}
	if err = ctx.Err(); err != nil {
		e.mu.Unlock()
		return model.Transfer{}, err
	}
	tx, err := e.db.Begin()
	if err == nil {
		if err = insertRecord(tx, r); err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if err != nil {
		e.mu.Unlock()
		return model.Transfer{}, err
	}
	e.records[r.t.ID] = r
	c := e.opts.Session(device.ID)
	if c != nil {
		e.contactReceiverLocked(r, c)
	}
	view := e.summary(r)
	e.mu.Unlock()
	if c == nil && e.opts.Connect != nil {
		e.opts.Connect(device.ID)
	}
	e.changed()
	return view, nil
}

// prepareIncomingLocked creates the private partial folder and plans
// collision-free destination names. Callers hold e.mu.
func (e *Engine) prepareIncomingLocked(r *record) error {
	root, err := os.OpenRoot(r.t.Destination)
	if err != nil {
		return fmt.Errorf("destination unavailable: %w", err)
	}
	defer root.Close()
	if err = root.MkdirAll(filepath.Join(partialsDir, r.t.ID), 0700); err != nil {
		return storageError(err)
	}
	_ = hideDirectory(root, partialsDir)
	return e.planDestinationsLocked(r, root)
}

func chooseTop(root *os.Root, name string, folder bool, reserved map[string]bool) (string, error) {
	for n := 1; n <= 10000; n++ {
		candidate := name
		if n > 1 {
			candidate = numberedName(name, n, folder)
		}
		if reserved[strings.ToLower(candidate)] {
			continue
		}
		exists, err := destinationExists(root, candidate)
		if err != nil {
			return "", err
		}
		if exists {
			continue
		}
		if !folder {
			return candidate, nil
		}
		// Creating the folder reserves the name against concurrent writers.
		if err = root.Mkdir(candidate, 0755); err == nil {
			return candidate, nil
		} else if !errors.Is(err, fs.ErrExist) {
			return "", storageError(err)
		}
	}
	return "", fmt.Errorf("%w: too many items with the same name", ErrConflict)
}

// planDestinationsLocked maps every manifest entry to a local relative path:
// names valid on this platform, top-level items that do not collide with
// existing files or other incoming transfers, and no two entries that differ
// only in letter case.
func (e *Engine) planDestinationsLocked(r *record, root *os.Root) error {
	goos := runtime.GOOS
	reserved := map[string]bool{}
	for _, other := range e.records {
		if other == r || other.t.Direction != "receive" || terminal(other.t.State) || other.t.Destination != r.t.Destination || other.files == nil {
			continue
		}
		for _, f := range other.files {
			if f.DestinationPath != "" {
				reserved[strings.ToLower(strings.SplitN(f.DestinationPath, "/", 2)[0])] = true
			}
		}
	}
	used := map[string]bool{}
	dirs := map[string]string{} // sanitized source folder -> destination folder
	var resolve func(local string) (string, error)
	unique := func(dest string, directory bool) string {
		base, dir := path.Base(dest), path.Dir(dest)
		for n := 2; used[strings.ToLower(dest)]; n++ {
			dest = path.Join(dir, numberedName(base, n, directory))
		}
		used[strings.ToLower(dest)] = true
		return dest
	}
	resolve = func(local string) (string, error) {
		if dest, ok := dirs[local]; ok {
			return dest, nil
		}
		parent := path.Dir(local)
		var dest string
		if parent == "." {
			top, err := chooseTop(root, local, true, reserved)
			if err != nil {
				return "", err
			}
			reserved[strings.ToLower(top)] = true
			dest = unique(top, true)
		} else {
			parentDest, err := resolve(parent)
			if err != nil {
				return "", err
			}
			dest = unique(parentDest+"/"+path.Base(local), true)
		}
		dirs[local] = dest
		return dest, nil
	}
	changed := make([]int, 0, len(r.files))
	for i := range r.files {
		f := &r.files[i]
		if f.DestinationPath != "" {
			used[strings.ToLower(f.DestinationPath)] = true
			continue
		}
		local := localPath(f.Path, goos)
		if f.Directory {
			dest, err := resolve(local)
			if err != nil {
				return err
			}
			f.DestinationPath = dest
		} else if parent := path.Dir(local); parent == "." {
			top, err := chooseTop(root, local, false, reserved)
			if err != nil {
				return err
			}
			reserved[strings.ToLower(top)] = true
			f.DestinationPath = unique(top, false)
		} else {
			parentDest, err := resolve(parent)
			if err != nil {
				return err
			}
			f.DestinationPath = unique(parentDest+"/"+path.Base(local), false)
		}
		changed = append(changed, i)
	}
	return e.saveDestinations(r, changed)
}

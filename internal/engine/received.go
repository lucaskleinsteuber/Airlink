package engine

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ReceivedResult is a private local-control result. It must never be included
// in guest status, peer responses, or public share metadata.
type ReceivedResult struct {
	Directory string         `json:"directory"`
	Items     []ReceivedItem `json:"items"`
}

type ReceivedItem struct {
	FileID    string `json:"fileId"`
	Path      string `json:"path"`
	Directory bool   `json:"directory"`
}

// ReceivedItems resolves published results for an explicit local reveal action.
// The transfer's historical destination remains authoritative after settings
// change; skipped payloads never redirect to indexed duplicate candidates.
func (e *Engine) ReceivedItems(id string) (ReceivedResult, error) {
	e.mu.Lock()
	r, err := e.getLocked(id)
	if err != nil {
		e.mu.Unlock()
		return ReceivedResult{}, err
	}
	t := clone(r.Transfer)
	directory := r.ReceiveRoot
	if directory == "" {
		directory = t.Destination
	}
	e.mu.Unlock()
	if t.Direction != "receive" || t.State != "completed" {
		return ReceivedResult{}, ErrConflict
	}
	regularFiles, newFiles := 0, 0
	for _, f := range t.Files {
		if !f.Directory {
			regularFiles++
			if f.Completed && !f.Skipped {
				newFiles++
			}
		}
	}
	if regularFiles > 0 && newFiles == 0 {
		return ReceivedResult{}, ErrNotFound
	}
	root, err := organizationRoot(directory)
	if err != nil {
		return ReceivedResult{}, err
	}
	defer root.Close()
	type item struct {
		relative string
		ReceivedItem
	}
	items := []item{}
	directories := map[string]bool{}
	for _, f := range t.Files {
		if !f.Completed || f.Skipped {
			continue
		}
		relative := destination(f)
		if _, err := cleanPath(relative); err != nil {
			return ReceivedResult{}, ErrConflict
		}
		if err := validateReceivedPath(root, relative, f.Directory); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return ReceivedResult{}, err
		}
		items = append(items, item{relative: relative, ReceivedItem: ReceivedItem{FileID: f.ID, Path: filepath.Join(organizationSystemPath(directory), filepath.FromSlash(relative)), Directory: f.Directory}})
		if f.Directory {
			directories[relative] = true
		}
	}
	out := ReceivedResult{Directory: organizationSystemPath(directory), Items: []ReceivedItem{}}
	for _, current := range items {
		covered := false
		for parent := path.Dir(current.relative); parent != "."; parent = path.Dir(parent) {
			if directories[parent] {
				covered = true
				break
			}
		}
		if !covered {
			out.Items = append(out.Items, current.ReceivedItem)
		}
	}
	if len(out.Items) == 0 {
		return ReceivedResult{}, ErrNotFound
	}
	return out, nil
}

func validateReceivedPath(root *os.Root, relative string, directory bool) error {
	prefix := ""
	for _, component := range strings.Split(relative, "/") {
		prefix = path.Join(prefix, component)
		info, err := root.Lstat(filepath.FromSlash(prefix))
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || prefix != relative && !info.IsDir() {
			return ErrConflict
		}
	}
	before, err := root.Lstat(filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	if directory != before.IsDir() || !directory && !before.Mode().IsRegular() {
		return ErrConflict
	}
	file, err := root.Open(filepath.FromSlash(relative))
	if err != nil {
		return err
	}
	after, statErr := file.Stat()
	file.Close()
	if statErr != nil {
		return statErr
	}
	if !os.SameFile(before, after) {
		return ErrConflict
	}
	return nil
}

// Only a newly committed completion calls this. Remove refuses nonempty
// directories, and cleanup failure never invalidates already verified payloads.
// Historical result/reveal queries perform no cleanup or other filesystem write.
func cleanupCompletedReceive(r *record) {
	if r.Transfer.Direction != "receive" || r.Transfer.State != "completed" || r.ReceiveRoot == "" {
		return
	}
	root, err := organizationRoot(r.ReceiveRoot)
	if err != nil {
		return
	}
	defer root.Close()
	info, err := root.Lstat(".airlink-partials")
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	_ = root.Remove(".airlink-partials")
}

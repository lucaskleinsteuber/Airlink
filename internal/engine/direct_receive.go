package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lucasfd3/Airlink/internal/model"
)

// ConfigureIncoming publishes the destination and organization policy only when
// the caller's settings commit succeeds. Callers holding their settings lock use
// the same store -> engine lock order as local acceptance/permission operations.
func (e *Engine) ConfigureIncoming(receive string, policy model.Organization, commit func() error) error {
	if !filepath.IsAbs(receive) {
		return fmt.Errorf("%w: absolute directory required", ErrInvalid)
	}
	policy, err := ValidateOrganization(policy)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(receive, 0700); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(receive)
	if err != nil {
		return err
	}
	root, err := organizationRoot(canonical)
	if err != nil {
		return err
	}
	root.Close()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return errors.New("engine closed")
	}
	previousRoot, previousPolicy := e.receiveDir, e.organization
	e.receiveDir, e.organization = canonical, policy
	if commit != nil {
		if err = commit(); err != nil {
			e.receiveDir, e.organization = previousRoot, previousPolicy
			return err
		}
	}
	return nil
}

// Old records keep their private per-transfer destination. New receives stage
// under the chosen root, while their public destination is the chosen root itself.
func publishedRoot(r *record) string {
	if r.DirectReceive {
		return r.Transfer.Destination
	}
	if r.ReceiveRoot == "" {
		return r.Transfer.Destination
	}
	return r.ReceiveRoot
}

func collisionBase(file model.File) string {
	return path.Join(path.Dir(destination(file)), path.Base(file.Path))
}

func (e *Engine) directDestinationsLocked(r *record) ([]model.File, error) {
	files, err := snapshotDestinations(r.Transfer.Files, r.Organization)
	if err != nil {
		return nil, err
	}
	root, err := organizationRoot(r.Transfer.Destination)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	reserved := map[string]bool{}
	for _, other := range e.records {
		if other.Transfer.ID == r.Transfer.ID || other.Transfer.Direction != "receive" || terminal(other.Transfer.State) || organizationSystemPath(publishedRoot(other)) != organizationSystemPath(r.Transfer.Destination) {
			continue
		}
		for _, file := range other.Transfer.Files {
			reserved[strings.ToLower(destination(file))] = true
		}
	}
	folders := map[string]string{}
	for _, file := range files {
		if file.Directory || strings.Contains(file.Path, "/") {
			name := strings.SplitN(file.Path, "/", 2)[0]
			folders[name] = ""
		}
	}
	ordered := make([]string, 0, len(folders))
	for folder := range folders {
		ordered = append(ordered, folder)
	}
	sort.Strings(ordered)
	for _, folder := range ordered {
		chosen, err := unusedDestination(root, folder, true, reserved)
		if err != nil {
			return nil, err
		}
		folders[folder] = chosen
		reserved[strings.ToLower(chosen)] = true
	}
	for i := range files {
		file := &files[i]
		base := strings.SplitN(file.Path, "/", 2)[0]
		if folder, ok := folders[base]; ok {
			file.DestinationPath = folder + strings.TrimPrefix(file.Path, base)
			continue
		}
		chosen, err := unusedDestination(root, destination(*file), false, reserved)
		if err != nil {
			return nil, err
		}
		file.DestinationPath = chosen
		reserved[strings.ToLower(chosen)] = true
	}
	return files, nil
}

func unusedDestination(root *os.Root, original string, directory bool, reserved map[string]bool) (string, error) {
	if _, err := cleanPath(original); err != nil {
		return "", err
	}
	for number := 1; number <= 10000; number++ {
		candidate := original
		if number > 1 {
			name, err := numberedName(path.Base(original), number, directory)
			if err != nil {
				return "", err
			}
			candidate = path.Join(path.Dir(original), name)
		}
		key := strings.ToLower(candidate)
		conflict := reserved[key]
		if !conflict {
			for other := range reserved {
				if strings.HasPrefix(other, key+"/") {
					conflict = true
					break
				}
			}
		}
		if conflict {
			continue
		}
		exists, err := destinationExists(root, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: too many files with the same name", ErrConflict)
}

func numberedName(original string, number int, directory bool) (string, error) {
	ext := ""
	if !directory && !strings.HasPrefix(original, ".") {
		ext = path.Ext(original)
	}
	stem := strings.TrimSuffix(original, ext)
	suffix := fmt.Sprintf(" (%d)", number)
	limit := 255 - len(ext) - len(suffix)
	if limit < 1 {
		return "", fmt.Errorf("%w: filename extension is too long", ErrInvalid)
	}
	for len(stem) > limit {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + suffix + ext, nil
}

// Check spelling case-insensitively even on case-sensitive hosts, and never
// follow a pre-existing link in a destination parent.
func destinationExists(root *os.Root, relative string) (bool, error) {
	parent := path.Dir(relative)
	if parent != "." {
		prefix := ""
		for _, part := range strings.Split(parent, "/") {
			prefix = path.Join(prefix, part)
			info, err := root.Lstat(filepath.FromSlash(prefix))
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return false, ErrConflict
			}
		}
	}
	dir, err := root.Open(filepath.FromSlash(parent))
	if err != nil {
		return false, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), path.Base(relative)) {
			return true, nil
		}
	}
	return false, nil
}

func directoryIdentity(root *os.Root, relative string) (string, error) {
	before, err := root.Lstat(filepath.FromSlash(relative))
	if err != nil {
		return "", err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return "", ErrConflict
	}
	dir, err := root.Open(filepath.FromSlash(relative))
	if err != nil {
		return "", err
	}
	defer dir.Close()
	after, err := dir.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", ErrConflict
	}
	return managedIdentity(dir, after)
}

func validateDirectDirectories(root *os.Root, r *record) error {
	for relative, expected := range r.DirectDirectories {
		identity, err := directoryIdentity(root, relative)
		if err != nil || identity != expected {
			return fmt.Errorf("%w: received folder was removed or replaced", ErrConflict)
		}
	}
	return nil
}

func (e *Engine) ensureDirectIncoming(r *record) error {
	root, err := organizationRoot(r.Transfer.Destination)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = ensureOrganizationDirs(root, ".airlink-partials"); err != nil {
		return err
	}
	if err = hidePrivateDirectory(root, ".airlink-partials"); err != nil {
		return err
	}
	stage := path.Join(".airlink-partials", r.Transfer.ID)
	if err = ensureOrganizationDirs(root, stage); err != nil {
		return err
	}
	if r.Transfer.State == "offered" && len(r.DirectDirectories) == 0 {
		prepared := false
		for attempt := 0; attempt < 100; attempt++ {
			files, planErr := e.directDestinationsLocked(r)
			if planErr != nil {
				return planErr
			}
			folders := map[string]bool{}
			for _, file := range files {
				if file.Directory || strings.Contains(file.Path, "/") {
					folders[strings.SplitN(destination(file), "/", 2)[0]] = true
				}
			}
			created := map[string]string{}
			collision := false
			for folder := range folders {
				if err = root.Mkdir(filepath.FromSlash(folder), 0700); errors.Is(err, fs.ErrExist) {
					collision = true
					break
				} else if err != nil {
					return err
				}
				identity, identityErr := directoryIdentity(root, folder)
				if identityErr != nil {
					return identityErr
				}
				created[folder] = identity
			}
			if collision {
				for folder, expected := range created {
					if identity, err := directoryIdentity(root, folder); err == nil && identity == expected {
						_ = root.Remove(filepath.FromSlash(folder))
					}
				}
				continue
			}
			r.Transfer.Files = files
			r.DirectDirectories = created
			prepared = true
			break
		}
		if !prepared {
			return fmt.Errorf("%w: destination kept changing", ErrConflict)
		}
	}
	if err = validateDirectDirectories(root, r); err != nil {
		return err
	}
	for _, file := range r.Transfer.Files {
		parent := path.Dir(destination(file))
		if file.Directory {
			parent = destination(file)
		}
		if err = ensureOrganizationDirs(root, parent); err != nil {
			return err
		}
	}
	if err = syncRootDirectory(root, "."); err != nil {
		return err
	}
	private, err := organizationRoot(r.ReceiveRoot)
	if err != nil {
		return err
	}
	defer private.Close()
	return ensureOrganizationDirs(private, ".airlink-partials")
}

// The mapping is committed before publication, so crash recovery knows exactly
// which collision-free name belongs to this verified payload.
func (e *Engine) publishDirectLocked(r *record, index int) error {
	root, err := organizationRoot(r.Transfer.Destination)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = validateDirectDirectories(root, r); err != nil {
		return err
	}
	file := r.Transfer.Files[index]
	reserved := map[string]bool{}
	for _, other := range e.records {
		if other.Transfer.Direction != "receive" || terminal(other.Transfer.State) || organizationSystemPath(publishedRoot(other)) != organizationSystemPath(r.Transfer.Destination) {
			continue
		}
		for otherIndex, otherFile := range other.Transfer.Files {
			if other == r && otherIndex == index {
				continue
			}
			reserved[strings.ToLower(destination(otherFile))] = true
		}
	}
	old := filepath.FromSlash(path.Join(".airlink-partials", r.Transfer.ID, partialName(file.ID)))
	for attempt := 0; attempt < 100; attempt++ {
		chosen, pickErr := unusedDestination(root, collisionBase(file), false, reserved)
		if pickErr != nil {
			return pickErr
		}
		if chosen != destination(r.Transfer.Files[index]) {
			r.Transfer.Files[index].DestinationPath = chosen
			if err = e.saveLocked(r); err != nil {
				return err
			}
		}
		if err = ensureOrganizationDirs(root, path.Dir(chosen)); err != nil {
			return err
		}
		err = publish(root, old, filepath.FromSlash(chosen))
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		return syncRootDirectory(root, filepath.Dir(filepath.FromSlash(chosen)))
	}
	return fmt.Errorf("%w: destination kept changing", ErrConflict)
}

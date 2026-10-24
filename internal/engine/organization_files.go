package engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func organizationSystemPath(p string) string {
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
func organizationRoot(absolute string) (*os.Root, error) {
	absolute = organizationSystemPath(absolute)
	if !filepath.IsAbs(absolute) {
		return nil, ErrInvalid
	}
	volume := filepath.VolumeName(absolute) + string(filepath.Separator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(volume, absolute)
	if err != nil {
		root.Close()
		return nil, err
	}
	if rel == "." {
		return root, nil
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		before, err := root.Lstat(part)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, ErrConflict
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		dir, err := next.Open(".")
		if err != nil {
			next.Close()
			root.Close()
			return nil, err
		}
		after, err := dir.Stat()
		dir.Close()
		root.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, ErrConflict
		}
		root = next
	}
	return root, nil
}
func openManagedRaw(c managedFile) (*os.File, fs.FileInfo, error) {
	if _, err := cleanPath(c.Relative); err != nil {
		return nil, nil, err
	}
	root, err := organizationRoot(c.Root)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	rel := filepath.FromSlash(c.Relative)
	for p := rel; p != "."; p = filepath.Dir(p) {
		info, err := root.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, ErrConflict
		}
	}
	before, err := root.Lstat(rel)
	if err != nil {
		return nil, nil, err
	}
	f, err := root.Open(rel)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != c.Size || !os.SameFile(before, info) {
		f.Close()
		return nil, nil, ErrConflict
	}
	return f, info, nil
}
func ensureOrganizationDirs(root *os.Root, relative string) error {
	if relative == "." {
		return nil
	}
	prefix := ""
	for _, part := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		prefix = filepath.Join(prefix, part)
		if err := root.Mkdir(prefix, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := root.Lstat(prefix)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrConflict
		}
	}
	return nil
}
func openPrivatePartial(root *os.Root, fileID string) (*os.File, error) {
	if err := ensureOrganizationDirs(root, ".airlink-partials"); err != nil {
		return nil, err
	}
	name := partialName(fileID)
	before, err := root.Lstat(name)
	if err == nil && (!before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0) {
		return nil, ErrConflict
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	after, err := root.Lstat(name)
	info, statErr := f.Stat()
	if err != nil || statErr != nil || !after.Mode().IsRegular() || !os.SameFile(after, info) || (before != nil && !os.SameFile(before, info)) {
		f.Close()
		return nil, ErrConflict
	}
	return f, nil
}

package update

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func WriteReadiness(path string, ready bool, message string) error {
	if !filepath.IsAbs(path) || filepath.Base(path) != "ready.json" {
		return errors.New("private update readiness path is invalid")
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return errors.New("private update readiness path is linked or invalid")
	}
	data, _ := json.Marshal(map[string]any{"ready": ready, "pid": os.Getpid(), "error": message})
	f, err := os.CreateTemp(filepath.Dir(path), ".ready-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

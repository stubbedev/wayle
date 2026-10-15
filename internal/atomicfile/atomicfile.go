// Package atomicfile replaces a file's content in one step: a temporary
// file beside it, synced, then renamed over the target, so a reader
// sees the old content or the new and never half of either.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces path with data at perm.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("atomicfile: temp for %s: %w", path, err)
	}
	tmp := f.Name()
	err = func() error {
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Chmod(perm); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}()
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("atomicfile: write %s: %w", path, err)
	}
	return nil
}

package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

// Write replaces the content and mode and leaves no temporary behind;
// a write into a missing directory fails and creates nothing.
func TestWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("content %q, %v", got, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("%d entries, want only the target", len(entries))
	}
	if err := Write(filepath.Join(dir, "missing", "f"), []byte("x"), 0o600); err == nil {
		t.Error("a write into a missing directory succeeded")
	}
}

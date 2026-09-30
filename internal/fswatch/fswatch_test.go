package fswatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func newWatcher(t *testing.T, mask uint32, recursive bool, root string) *Watcher {
	t.Helper()
	w, err := New(mask, recursive)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Add(root); err != nil {
		t.Fatal(err)
	}
	return w
}

func expectTick(t *testing.T, w *Watcher, what string) {
	t.Helper()
	select {
	case _, ok := <-w.Changed():
		if !ok {
			t.Fatalf("%s: channel closed", what)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no tick", what)
	}
}

func expectQuiet(t *testing.T, w *Watcher, what string) {
	t.Helper()
	select {
	case <-w.Changed():
		t.Fatalf("%s: unexpected tick", what)
	case <-time.After(150 * time.Millisecond):
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRecursiveWatchSeesNestedAndLaterDirectories(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "INBOX", "cur")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	w := newWatcher(t, Changes, true, root)

	write(t, filepath.Join(nested, "1:2,"), "mail")
	expectTick(t, w, "file in an existing subdirectory")

	// A maildir flag change is a rename.
	if err := os.Rename(filepath.Join(nested, "1:2,"), filepath.Join(nested, "1:2,S")); err != nil {
		t.Fatal(err)
	}
	expectTick(t, w, "rename")

	later := filepath.Join(root, "Work")
	if err := os.Mkdir(later, 0o700); err != nil {
		t.Fatal(err)
	}
	expectTick(t, w, "directory created")
	// Let the reader pick the new directory up before writing into it.
	time.Sleep(50 * time.Millisecond)
	write(t, filepath.Join(later, "2:2,"), "mail")
	expectTick(t, w, "file in a directory created after Add")
}

func TestNonRecursiveWatchIgnoresSubdirectoriesAndOtherEvents(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	w := newWatcher(t, unix.IN_MODIFY, false, root)

	write(t, filepath.Join(sub, "deep"), "x")
	expectQuiet(t, w, "write below a non-recursive watch")

	// Creating a file fires IN_CREATE (not in the mask) before the
	// write's IN_MODIFY; only the latter may tick.
	path := filepath.Join(root, "brightness")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	expectQuiet(t, w, "create outside the mask")
	write(t, path, "42")
	expectTick(t, w, "modify in the mask")
}

func TestCloseEndsTheChannelAndRefusesAdds(t *testing.T) {
	root := t.TempDir()
	w, err := New(Changes, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-w.Changed():
		if ok {
			t.Fatal("tick after Close, want a closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("Changed not closed by Close")
	}
	if err := w.Add(root); err == nil {
		t.Error("Add after Close: want an error")
	}
	if err := w.Close(); err != nil {
		t.Errorf("second Close = %v, want nil", err)
	}
}

func TestBadArgumentsError(t *testing.T) {
	if _, err := New(0, false); err == nil {
		t.Error("empty mask: want an error")
	}
	w, err := New(Changes, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	if err := w.Add(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing root: want an error")
	}
}

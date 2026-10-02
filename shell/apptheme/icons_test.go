package apptheme

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/wayle/internal/icons"
)

// fakeIconSearch records what initIcons does to the icon search path.
type fakeIconSearch struct {
	mu        sync.Mutex
	paths     []string
	refreshes int
}

func (f *fakeIconSearch) search() iconSearch {
	return iconSearch{
		paths: func() []string { f.mu.Lock(); defer f.mu.Unlock(); return slices.Clone(f.paths) },
		set:   func(p []string) { f.mu.Lock(); defer f.mu.Unlock(); f.paths = p },
		refresh: func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.refreshes++
		},
	}
}

func (f *fakeIconSearch) state() ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.paths), f.refreshes
}

func TestInitIcons(t *testing.T) {
	t.Setenv("XDG_DATA_DIRS", "")
	base := t.TempDir()
	r := icons.RegistryAt(base)
	fake := &fakeIconSearch{paths: []string{"/usr/share/icons"}}

	stop := initIcons(r, fake.search())
	defer stop()

	if !r.IsValid() {
		t.Error("the icons directory and index.theme were not set up")
	}
	paths, refreshes := fake.state()
	if !slices.Equal(paths, []string{base, "/usr/share/icons"}) || refreshes != 1 {
		t.Fatalf("after init: paths %v, %d refreshes; want the registry first, one refresh", paths, refreshes)
	}

	// An icon installed while the shell runs is picked up.
	if err := os.WriteFile(filepath.Join(r.IconsDir(), "ld-new-symbolic.svg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, n := fake.state(); n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("installing an icon did not refresh the icon theme")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if paths, _ := fake.state(); !slices.Equal(paths, []string{base, "/usr/share/icons"}) {
		t.Errorf("a refresh changed the paths to %v", paths)
	}
}

func TestInitIconsMigratesStaleIcons(t *testing.T) {
	t.Setenv("XDG_DATA_DIRS", "")
	r := icons.RegistryAt(t.TempDir())
	if err := r.EnsureSetup(); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(r.IconsDir(), "ld-x-symbolic.svg")
	legacy := "<svg width='16' height='16'><path d='M2 8L14 8' stroke='rgb(0,0,0)' fill='none' gpa:stroke='foreground'/></svg>"
	if err := os.WriteFile(stale, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeIconSearch{}
	initIcons(r, fake.search())()
	got, _ := os.ReadFile(stale)
	if want, _ := icons.ToSymbolic(legacy); string(got) != want {
		t.Errorf("stale icon not migrated:\n%s", got)
	}
}

func TestInitIconsFailureLeavesSearchAlone(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeIconSearch{paths: []string{"/usr/share/icons"}}
	initIcons(icons.RegistryAt(file), fake.search())()
	if paths, refreshes := fake.state(); !slices.Equal(paths, []string{"/usr/share/icons"}) || refreshes != 0 {
		t.Errorf("a registry that cannot be set up touched the search path: %v, %d refreshes", paths, refreshes)
	}
}

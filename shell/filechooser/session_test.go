package filechooser

import (
	"path/filepath"
	"slices"
	"testing"
)

// tree is a chooser fixture: root/{docs/, pics/a.png, b.txt, a.png,
// .dot.txt}.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	touch(t, root, "docs/", "pics/a.png", "b.txt", "a.png", ".dot.txt")
	return root
}

func open(t *testing.T, mode Mode, folder string, filters []Filter) *session {
	t.Helper()
	return newSession(mode, folder, filters, testDB(t), "/nonexistent-home", defaultView)
}

func TestSessionStartsInTheRequestedFolderElseHome(t *testing.T) {
	root := tree(t)
	if s := open(t, ModeOpenFile, root, nil); s.dir != root {
		t.Errorf("dir = %s, want the requested %s", s.dir, root)
	}
	if s := open(t, ModeOpenFile, filepath.Join(root, "b.txt"), nil); s.dir != "/nonexistent-home" {
		t.Errorf("a file as the folder = %s, want home", s.dir)
	}
	if s := newSession(ModeOpenFile, "", nil, testDB(t), "", defaultView); s.dir != "/" {
		t.Errorf("no folder and no home = %s, want /", s.dir)
	}
}

func TestSessionPartitionsTheListing(t *testing.T) {
	root := tree(t)
	s := open(t, ModeOpenFile, root, []Filter{{"Images", []Rule{{RuleGlob, "*.png"}}}})
	// Folders pass the type filter; dot files hide; the filter list
	// gained All Files.
	if got := names(s.entries); !slices.Equal(got, []string{"docs", "pics", "a.png"}) {
		t.Errorf("filtered = %v", got)
	}
	if len(s.filters) != 2 {
		t.Fatalf("filters = %+v, want All Files appended", s.filters)
	}
	s.selectFilter(1)
	if got := names(s.entries); !slices.Equal(got, []string{"docs", "pics", "a.png", "b.txt"}) {
		t.Errorf("all files = %v", got)
	}
	s.selectFilter(7)
	if s.active != 1 {
		t.Errorf("an out-of-range filter changed the active one to %d", s.active)
	}
	s.hidden = true
	s.refresh()
	if got := names(s.entries); !slices.Contains(got, ".dot.txt") {
		t.Errorf("hidden shown = %v", got)
	}
	s.setSearch("A.P")
	if got := names(s.entries); !slices.Equal(got, []string{"a.png"}) {
		t.Errorf("search = %v", got)
	}
}

func TestSessionListsOnlyOnADirectoryChange(t *testing.T) {
	root := tree(t)
	s := open(t, ModeOpenFile, root, nil)
	touch(t, root, "late.txt")
	s.setSearch("")
	s.sortBy(SortSize)
	if slices.Contains(names(s.entries), "late.txt") {
		t.Error("a repaint re-read the directory")
	}
	s.goTo(filepath.Join(root, "docs"))
	s.step(true)
	if !slices.Contains(names(s.entries), "late.txt") {
		t.Error("navigating back did not re-list")
	}
}

func TestSessionHistoryForksOnFreshNavigation(t *testing.T) {
	root := tree(t)
	docs, pics := filepath.Join(root, "docs"), filepath.Join(root, "pics")
	s := open(t, ModeOpenFile, root, nil)
	s.goTo(docs)
	s.goTo(pics)
	if !s.step(true) || s.dir != docs {
		t.Fatalf("back = %s, want %s", s.dir, docs)
	}
	if !s.step(false) || s.dir != pics {
		t.Fatalf("forward = %s, want %s", s.dir, pics)
	}
	s.step(true)
	s.goTo(root)
	if s.step(false) {
		t.Errorf("forward survived a fresh navigation (now %s)", s.dir)
	}
	// Going to the folder already shown records nothing.
	depth := len(s.back)
	s.goTo(root)
	if len(s.back) != depth {
		t.Error("a navigation to the same folder grew the history")
	}
	s.back = nil
	if s.step(true) {
		t.Error("back past the start moved")
	}
}

func TestSessionNavigationClearsTheSearch(t *testing.T) {
	root := tree(t)
	s := open(t, ModeOpenFile, root, nil)
	s.recursive = true
	s.setSearch("a")
	s.addFound("a", []Entry{entry(filepath.Join(root, "pics", "a.png"), false, 0)})
	s.goTo(filepath.Join(root, "pics"))
	if s.search != "" || s.found != nil {
		t.Errorf("search %q / %d found survived navigation", s.search, len(s.found))
	}
	if !s.up() || s.dir != root {
		t.Errorf("up = %s, want %s", s.dir, root)
	}
	s.dir = "/"
	if s.up() {
		t.Error("up from / moved")
	}
}

func TestSessionRecursiveResultsAreForTheCurrentQueryOnly(t *testing.T) {
	root := tree(t)
	s := open(t, ModeOpenFile, root, []Filter{{"Images", []Rule{{RuleGlob, "*.png"}}}})
	s.recursive = true
	s.setSearch("a")
	deep := entry(filepath.Join(root, "pics", "a.png"), false, 0)
	txt := entry(filepath.Join(root, "b.txt"), false, 0)
	if !s.addFound("a", []Entry{deep, txt}) {
		t.Fatal("a batch for the live query was dropped")
	}
	// The type filter still applies; the label is relative.
	if len(s.entries) != 1 || s.displayName(s.entries[0]) != filepath.Join("pics", "a.png") {
		t.Fatalf("entries = %v", names(s.entries))
	}
	if s.addFound("old", []Entry{deep}) {
		t.Error("a superseded query's batch was kept")
	}
	s.recursive = false
	if s.addFound("a", []Entry{deep}) {
		t.Error("a batch after the toggle went off was kept")
	}
	// Without recursion the label is the basename.
	s.setSearch("a")
	if got := s.displayName(deep); got != "a.png" {
		t.Errorf("plain label = %q", got)
	}
}

func TestSessionDroppedRevealsFilesAndEntersFolders(t *testing.T) {
	root := tree(t)
	s := open(t, ModeOpenFile, root, nil)
	s.dropped(filepath.Join(root, "pics", "a.png"))
	if s.dir != filepath.Join(root, "pics") {
		t.Errorf("a dropped file = %s, want its folder", s.dir)
	}
	s.dropped(filepath.Join(root, "docs"))
	if s.dir != filepath.Join(root, "docs") {
		t.Errorf("a dropped folder = %s", s.dir)
	}
}

func TestSessionSortToggles(t *testing.T) {
	s := open(t, ModeOpenFile, tree(t), nil)
	s.sortBy(SortName)
	if s.view.asc {
		t.Error("sorting by the sort column did not flip the direction")
	}
	s.sortBy(SortSize)
	if s.view.sort != SortSize || !s.view.asc {
		t.Errorf("a new column = %+v, want it ascending", s.view)
	}
}

func index(t *testing.T, s *session, name string) int {
	t.Helper()
	i := slices.Index(names(s.entries), name)
	if i < 0 {
		t.Fatalf("no %s in %v", name, names(s.entries))
	}
	return i
}

func TestSessionActivate(t *testing.T) {
	root := tree(t)
	s := open(t, ModeOpenFile, root, nil)
	if got := s.activate(index(t, s, "a.png")); got != activateConfirm {
		t.Errorf("a file in open mode = %v, want confirm", got)
	}
	if got := s.activate(index(t, s, "docs")); got != activateDescend || s.dir != filepath.Join(root, "docs") {
		t.Errorf("a folder = %v at %s, want descend", got, s.dir)
	}
	if got := s.activate(42); got != activateNone {
		t.Errorf("a missing row = %v", got)
	}
	save := open(t, ModeSave, root, nil)
	if got := save.activate(index(t, save, "b.txt")); got != activateName {
		t.Errorf("a file in save mode = %v, want the name taken", got)
	}
}

func TestSessionResult(t *testing.T) {
	root := tree(t)
	uri := func(rel string) string { return "file://" + filepath.Join(root, rel) }

	o := open(t, ModeOpenMultiple, root, nil)
	got, ok := o.result([]int{index(t, o, "a.png"), index(t, o, "docs"), index(t, o, "b.txt"), 99}, "")
	if !ok || !slices.Equal(got, []string{uri("a.png"), uri("b.txt")}) {
		t.Errorf("open = %v %v, want the files only", got, ok)
	}
	if got, ok := o.result([]int{index(t, o, "docs")}, ""); ok {
		t.Errorf("only a folder selected answered %v, want the dialog kept", got)
	}
	if _, ok := o.result(nil, ""); ok {
		t.Error("nothing selected answered")
	}

	sv := open(t, ModeSave, root, nil)
	if got, ok := sv.result(nil, "new file.txt"); !ok || !slices.Equal(got, []string{"file://" + filepath.Join(root, "new%20file.txt")}) {
		t.Errorf("save = %v %v", got, ok)
	}
	if _, ok := sv.result(nil, ""); ok {
		t.Error("an empty save name answered")
	}

	f := open(t, ModeFolder, root, nil)
	if got := names(f.entries); !slices.Equal(got, []string{"docs", "pics"}) {
		t.Errorf("folder mode lists %v, want folders only", got)
	}
	if got, ok := f.result(nil, ""); !ok || !slices.Equal(got, []string{"file://" + root}) {
		t.Errorf("folder with none selected = %v, want the folder shown", got)
	}
	if got, _ := f.result([]int{index(t, f, "pics")}, ""); !slices.Equal(got, []string{uri("pics")}) {
		t.Errorf("folder with one selected = %v, want it", got)
	}
}

func TestOpenModeAndConfirmLabel(t *testing.T) {
	for _, c := range []struct {
		r     OpenRequest
		mode  Mode
		label string
	}{
		{OpenRequest{}, ModeOpenFile, "Open"},
		{OpenRequest{Multiple: true}, ModeOpenMultiple, "Open"},
		{OpenRequest{Multiple: true, Directory: true}, ModeFolder, "Select"},
	} {
		if got := openMode(c.r); got != c.mode || got.confirmLabel() != c.label {
			t.Errorf("openMode(%+v) = %v %q, want %v %q", c.r, got, got.confirmLabel(), c.mode, c.label)
		}
	}
	if ModeSave.confirmLabel() != "Save" {
		t.Error("save label")
	}
}

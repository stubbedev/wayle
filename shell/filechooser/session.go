package filechooser

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/wayle/internal/fileuri"
	"github.com/stubbedev/wayle/internal/mime"
)

// session is one open request's state, free of widgets: where it is,
// how the listing is filtered and sorted, the history, and what a
// confirm answers. The host renders entries and feeds gestures in.
type session struct {
	mode    Mode
	db      *mime.Database
	dir     string
	filters []Filter
	active  int

	// all is the unfiltered listing of listed, re-read only when the
	// directory changes: every other repaint partitions it in memory.
	all    []Entry
	listed string

	search    string
	recursive bool
	hidden    bool
	// found accumulates the streamed recursive matches for search.
	found []Entry

	view view
	// entries is what the view shows, in order.
	entries []Entry

	back, forward []string
}

// newSession starts at folder when it is a directory, else home, else /.
func newSession(mode Mode, folder string, filters []Filter, db *mime.Database, home string, v view) *session {
	s := &session{mode: mode, db: db, filters: withAllFiles(filters), view: v}
	s.dir = startDir(folder, home)
	s.refresh()
	return s
}

func startDir(folder, home string) string {
	if folder != "" {
		if info, err := os.Stat(folder); err == nil && info.IsDir() {
			return folder
		}
	}
	if home != "" {
		return home
	}
	return "/"
}

// searching reports a recursive search in progress: the rows are its
// matches, labelled relative to dir.
func (s *session) searching() bool { return s.recursive && s.search != "" }

// refresh recomputes entries: the cached listing (or the recursive
// matches) through the hidden toggle, the type filter (files only)
// and the search text, then sorted by the column, or by relevance
// while searching.
func (s *session) refresh() {
	var source []Entry
	if s.searching() {
		source = s.found
	} else {
		if s.listed != s.dir || s.all == nil {
			s.all, s.listed = ListDir(s.dir, s.mode), s.dir
		}
		source = s.all
	}
	match := newTextFilter(s.search)
	s.entries = s.entries[:0]
	for _, e := range source {
		name := e.Name()
		if !s.hidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !e.IsDir && !MatchesFilter(s.db, name, s.filters, s.active) {
			continue
		}
		// Recursive matches already matched the query on the walk.
		if !s.searching() && !match.matches(name) {
			continue
		}
		s.entries = append(s.entries, e)
	}
	if s.searching() {
		SortSearch(s.entries, s.dir, s.search)
	} else {
		SortEntries(s.entries, s.view.sort, s.view.asc)
	}
}

// displayName is a row's label: the basename, or the path below dir
// for a recursive match.
func (s *session) displayName(e Entry) string {
	if s.searching() {
		if rel, err := filepath.Rel(s.dir, e.Path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return e.Name()
}

// setSearch changes the query, dropping the previous matches.
func (s *session) setSearch(query string) {
	s.search, s.found = query, nil
	s.refresh()
}

// addFound appends a batch of recursive matches for query; a batch
// for a superseded query, or after the toggle went off, is dropped.
func (s *session) addFound(query string, batch []Entry) bool {
	if query != s.search || !s.recursive {
		return false
	}
	s.found = append(s.found, batch...)
	s.refresh()
	return true
}

// goTo navigates to dir, recording the departure in the back history
// (a fresh navigation forks it: forward clears) and dropping the
// search so the directory lists in full.
func (s *session) goTo(dir string) {
	if dir != s.dir {
		s.back = append(s.back, s.dir)
		s.forward = nil
		s.dir = dir
	}
	s.search, s.found = "", nil
	s.refresh()
}

// step walks the history one way; false at its end.
func (s *session) step(back bool) bool {
	from, to := &s.forward, &s.back
	if back {
		from, to = &s.back, &s.forward
	}
	if len(*from) == 0 {
		return false
	}
	dir := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, s.dir)
	s.dir = dir
	s.search, s.found = "", nil
	s.refresh()
	return true
}

// up goes to the parent; false at the root.
func (s *session) up() bool {
	parent := filepath.Dir(s.dir)
	if parent == s.dir {
		return false
	}
	s.goTo(parent)
	return true
}

// dropped navigates to a dropped folder, or reveals a dropped file in
// its folder.
func (s *session) dropped(path string) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		s.goTo(path)
		return
	}
	s.goTo(filepath.Dir(path))
}

// selectFilter makes filter i the active one.
func (s *session) selectFilter(i int) {
	if i >= 0 && i < len(s.filters) {
		s.active = i
		s.refresh()
	}
}

// sortBy sorts by column, flipping the direction when it already is.
func (s *session) sortBy(column SortColumn) {
	if s.view.sort == column {
		s.view.asc = !s.view.asc
	} else {
		s.view.sort, s.view.asc = column, true
	}
	s.refresh()
}

// activation is what activating a row does.
type activation uint8

const (
	activateNone activation = iota
	// activateDescend entered the folder.
	activateDescend
	// activateName put the file's name in the save entry.
	activateName
	// activateConfirm opens: the row is selected, confirm picks it up.
	activateConfirm
)

// activate handles a double-click or Enter on row i.
func (s *session) activate(i int) activation {
	if i < 0 || i >= len(s.entries) {
		return activateNone
	}
	e := s.entries[i]
	switch {
	case e.IsDir:
		s.goTo(e.Path)
		return activateDescend
	case s.mode == ModeSave:
		return activateName
	}
	return activateConfirm
}

// result is what confirming answers given the selected rows and the
// save entry's text; false keeps the dialog open (nothing to answer
// yet). A folder pick answers the selected folder, else the one shown,
// as GTK's chooser does; a save answers name in the folder shown; an
// open answers the selected files, never folders.
func (s *session) result(selected []int, name string) ([]string, bool) {
	switch s.mode {
	case ModeSave:
		if name == "" {
			return nil, false
		}
		return []string{fileuri.FromPath(filepath.Join(s.dir, name))}, true
	case ModeFolder:
		for _, i := range selected {
			if i >= 0 && i < len(s.entries) && s.entries[i].IsDir {
				return []string{fileuri.FromPath(s.entries[i].Path)}, true
			}
		}
		return []string{fileuri.FromPath(s.dir)}, true
	}
	var uris []string
	for _, i := range selected {
		if i >= 0 && i < len(s.entries) && !s.entries[i].IsDir {
			uris = append(uris, fileuri.FromPath(s.entries[i].Path))
		}
	}
	return uris, len(uris) > 0
}

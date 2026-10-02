package filechooser

import (
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/shell/treetest"
)

type fakeWindow struct {
	closed bool
	focus  widget.Widget
}

func (w *fakeWindow) Close()                   { w.closed = true }
func (w *fakeWindow) SetFocus(f widget.Widget) { w.focus = f }

// harness runs the chooser with a lock-step loop: Invoke and the
// test's gestures take turns, as on the one UI goroutine.
type harness struct {
	t       *testing.T
	loop    sync.Mutex
	mu      sync.Mutex
	c       *Chooser
	root    string
	opened  chan app.LayerConfig
	windows []*fakeWindow
	openErr error
	cfg     app.LayerConfig
	queue   chan func()
	qmu     sync.Mutex
	stopped bool
}

func newHarness(t *testing.T, viewFile string) *harness {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	conf := config.Defaults()
	conf.Animations.Enabled = false
	hs := &harness{t: t, root: tree(t), opened: make(chan app.LayerConfig, 4)}
	hs.queue = make(chan func(), 256)
	go func() {
		for fn := range hs.queue {
			hs.do(fn)
		}
	}()
	t.Cleanup(func() {
		hs.qmu.Lock()
		defer hs.qmu.Unlock()
		hs.stopped = true
		close(hs.queue)
	})
	hs.c = New(Deps{
		Config: func() *config.Config { return conf },
		Open: func(c app.LayerConfig) (Window, error) {
			hs.mu.Lock()
			defer hs.mu.Unlock()
			if hs.openErr != nil {
				return nil, hs.openErr
			}
			w := &fakeWindow{}
			hs.windows = append(hs.windows, w)
			hs.opened <- c
			return w, nil
		},
		Invoke:    hs.invoke,
		Font:      face,
		Ink:       render.RGBA(0, 0, 0, 255),
		MIME:      testDB(t),
		Home:      hs.root,
		Places:    func() []Place { return []Place{{"Home", hs.root, "user-home-symbolic"}} },
		Locations: func() []Place { return []Place{{"Computer", "/", "drive-harddisk-symbolic"}} },
		ViewFile:  viewFile,
	})
	return hs
}

// flush waits until the loop ran everything queued so far.
func (hs *harness) flush() {
	done := make(chan struct{})
	hs.invoke(func() { close(done) })
	<-done
}

// invoke queues fn for the loop, in order, as Application.Invoke does;
// after the test, like Invoke after Run, it drops fn.
func (hs *harness) invoke(fn func()) {
	hs.qmu.Lock()
	defer hs.qmu.Unlock()
	if !hs.stopped {
		hs.queue <- fn
	}
}

func (hs *harness) do(fn func()) {
	hs.loop.Lock()
	defer hs.loop.Unlock()
	fn()
}

// ask starts a request and waits for its surface.
func (hs *harness) ask(t *testing.T, fn func() []string) chan []string {
	t.Helper()
	got := make(chan []string, 1)
	go func() { got <- fn() }()
	select {
	case hs.cfg = <-hs.opened:
	case <-time.After(2 * time.Second):
		t.Fatal("no chooser opened")
	}
	return got
}

func (hs *harness) open(t *testing.T, r OpenRequest) chan []string {
	t.Helper()
	if r.CurrentFolder == "" {
		r.CurrentFolder = hs.root
	}
	return hs.ask(t, func() []string { return hs.c.Open(r) })
}

func answer(t *testing.T, got chan []string) []string {
	t.Helper()
	select {
	case uris := <-got:
		return uris
	case <-time.After(2 * time.Second):
		t.Fatal("the request was never answered")
		return nil
	}
}

func pending(t *testing.T, got chan []string) {
	t.Helper()
	select {
	case uris := <-got:
		t.Fatalf("answered %v while it should still be open", uris)
	case <-time.After(20 * time.Millisecond):
	}
}

func (hs *harness) click(t *testing.T, name string) {
	t.Helper()
	b := treetest.Button(t, hs.cfg.Root, name)
	hs.do(b.OnClick)
}

// shown is the active view's labels, in order.
func (hs *harness) shown() []string {
	var out []string
	hs.do(func() {
		for _, e := range hs.c.s.entries {
			out = append(out, hs.c.s.displayName(e))
		}
	})
	return out
}

// pick selects the named entry in the active view.
func (hs *harness) pick(t *testing.T, name string) {
	t.Helper()
	hs.do(func() {
		j := slices.Index(names(hs.c.s.entries), name)
		if j < 0 {
			t.Fatalf("no %s in %v", name, names(hs.c.s.entries))
		}
		hs.c.activeView().Select(j)
	})
}

func (hs *harness) uri(rel string) string { return "file://" + filepath.Join(hs.root, rel) }

func TestOpenAnswersTheSelectedFile(t *testing.T) {
	hs := newHarness(t, "")
	got := hs.open(t, OpenRequest{Title: "Pick one"})
	if hs.cfg.Layer != app.LayerOverlay || hs.cfg.Namespace != "wayle-file-chooser" || hs.cfg.Keyboard != app.KeyboardExclusive {
		t.Errorf("surface = %+v", hs.cfg)
	}
	if l := treetest.Labels(hs.cfg.Root); !slices.Contains(l, "Pick one") {
		t.Errorf("no title in %q", l)
	}
	if got := hs.shown(); !slices.Equal(got, []string{"docs", "pics", "a.png", "b.txt"}) {
		t.Errorf("listing = %v", got)
	}
	// Nothing selected: Open keeps the dialog.
	hs.click(t, "Open")
	pending(t, got)
	hs.pick(t, "b.txt")
	hs.click(t, "Open")
	if uris := answer(t, got); !slices.Equal(uris, []string{hs.uri("b.txt")}) {
		t.Errorf("answered %v", uris)
	}
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if !hs.windows[0].closed {
		t.Error("an answered chooser stayed mapped")
	}
}

func TestOpenMultipleAnswersEveryFile(t *testing.T) {
	hs := newHarness(t, "")
	got := hs.open(t, OpenRequest{Multiple: true})
	hs.do(func() {
		v := hs.c.activeView()
		if v.SelectionMode() != widget.SelectionMultiple {
			t.Errorf("selection mode = %v, want multiple", v.SelectionMode())
		}
		v.SelectAll()
	})
	hs.click(t, "Open")
	if uris := answer(t, got); !slices.Equal(uris, []string{hs.uri("a.png"), hs.uri("b.txt")}) {
		t.Errorf("answered %v, want the two files and no folders", uris)
	}
}

func TestActivatingAFileOpensItAndAFolderEntersIt(t *testing.T) {
	hs := newHarness(t, "")
	got := hs.open(t, OpenRequest{})
	hs.do(func() { hs.c.activate(slices.Index(names(hs.c.s.entries), "pics")) })
	if got := hs.shown(); !slices.Equal(got, []string{"a.png"}) {
		t.Fatalf("after entering pics = %v", got)
	}
	crumbs := treetest.WithClass(hs.cfg.Root, "file-chooser-crumb")
	if last := crumbs[len(crumbs)-1]; widget.Describe(last).Name != "pics" || !widget.HasClass(last, "current") {
		t.Errorf("last crumb = %q, want the current pics", widget.Describe(last).Name)
	}
	// One gesture's later activations index the listing that is gone.
	hs.do(func() {
		hs.c.activate(0)
		hs.c.navigating = true
		hs.c.activate(0)
		hs.c.navigating = false
	})
	pending(t, got)
	hs.do(func() {
		hs.c.activeView().Select(0)
		hs.c.activate(0)
	})
	if uris := answer(t, got); !slices.Equal(uris, []string{hs.uri("pics/a.png")}) {
		t.Errorf("activating a file answered %v", uris)
	}
}

func TestHistoryButtonsAndUp(t *testing.T) {
	hs := newHarness(t, "")
	hs.open(t, OpenRequest{})
	hs.do(func() { hs.c.goTo(filepath.Join(hs.root, "docs")) })
	hs.do(func() { hs.cfg.OnPress(btnBack, 0, nil) })
	if dir := hs.c.s.dir; dir != hs.root {
		t.Errorf("back = %s", dir)
	}
	hs.do(func() { hs.cfg.OnPress(btnForward, 0, nil) })
	if dir := hs.c.s.dir; dir != filepath.Join(hs.root, "docs") {
		t.Errorf("forward = %s", dir)
	}
	hs.do(func() { hs.cfg.OnPress(widget.BTNRight, 0, nil) })
	if dir := hs.c.s.dir; dir != filepath.Join(hs.root, "docs") {
		t.Errorf("another button moved to %s", dir)
	}
	hs.do(hs.c.goUp)
	if dir := hs.c.s.dir; dir != hs.root {
		t.Errorf("up = %s", dir)
	}
}

func TestCancelEscapeAndPreemption(t *testing.T) {
	hs := newHarness(t, "")
	got := hs.open(t, OpenRequest{Filters: []Filter{{"Images", []Rule{{RuleGlob, "*.png"}}}}})
	// Escape closes the filter popup first, then quick look, then the
	// dialog.
	hs.click(t, "Images")
	if !hs.c.ui.filterPopup.Visible() {
		t.Fatal("the filter button did not open the popup")
	}
	hs.do(func() { hs.cfg.OnKey(nil, keyEscape, 0) })
	pending(t, got)
	if hs.c.ui.filterPopup.Visible() {
		t.Error("Escape left the filter popup open")
	}
	hs.pick(t, "a.png")
	hs.do(func() { hs.cfg.OnKey(&widget.Router{}, keySpace, 0) })
	if !hs.c.ui.quicklook.Visible() {
		t.Fatal("space did not open quick look")
	}
	hs.do(func() { hs.cfg.OnKey(nil, keyEscape, 0) })
	pending(t, got)
	hs.do(func() { hs.cfg.OnKey(nil, keyEscape, 0) })
	if uris := answer(t, got); uris != nil {
		t.Errorf("Escape answered %v", uris)
	}

	first := hs.open(t, OpenRequest{})
	second := hs.ask(t, func() []string { return hs.c.Save(SaveRequest{CurrentFolder: hs.root, CurrentName: "x"}) })
	if uris := answer(t, first); uris != nil {
		t.Errorf("a preempted request answered %v", uris)
	}
	hs.click(t, "Cancel")
	if uris := answer(t, second); uris != nil {
		t.Errorf("Cancel answered %v", uris)
	}
}

func TestSpaceInATextFieldTypes(t *testing.T) {
	hs := newHarness(t, "")
	hs.open(t, OpenRequest{})
	hs.pick(t, "a.png")
	r := &widget.Router{Root: hs.cfg.Root}
	hs.do(func() {
		r.SetFocus(hs.c.ui.search)
		hs.cfg.OnKey(r, keySpace, 0)
	})
	if hs.c.ui.quicklook.Visible() {
		t.Error("space in the search box opened quick look")
	}
}

func TestSaveAnswersTheNameInTheFolder(t *testing.T) {
	hs := newHarness(t, "")
	got := hs.ask(t, func() []string {
		return hs.c.Save(SaveRequest{Title: "Save", CurrentName: "draft.txt", CurrentFolder: hs.root})
	})
	var focused bool
	var text string
	hs.do(func() { focused, text = hs.windows[0].focus == hs.c.ui.name, hs.c.ui.name.Text() })
	if !focused || text != "draft.txt" {
		t.Errorf("name entry focused %v, text %q", focused, text)
	}
	// Activating a file takes its name.
	hs.do(func() { hs.c.activate(slices.Index(names(hs.c.s.entries), "b.txt")) })
	if hs.c.ui.name.Text() != "b.txt" {
		t.Errorf("name = %q after activating b.txt", hs.c.ui.name.Text())
	}
	hs.do(func() { hs.c.ui.name.SetText("") })
	hs.click(t, "Save")
	pending(t, got)
	hs.do(func() {
		hs.c.ui.name.SetText("new.txt")
		hs.c.ui.name.OnActivate("new.txt")
	})
	if uris := answer(t, got); !slices.Equal(uris, []string{hs.uri("new.txt")}) {
		t.Errorf("save answered %v", uris)
	}
}

func TestFolderPickAnswersTheFolder(t *testing.T) {
	hs := newHarness(t, "")
	got := hs.open(t, OpenRequest{Directory: true})
	if got := hs.shown(); !slices.Equal(got, []string{"docs", "pics"}) {
		t.Errorf("folder pick lists %v", got)
	}
	hs.click(t, "Select")
	if uris := answer(t, got); !slices.Equal(uris, []string{"file://" + hs.root}) {
		t.Errorf("answered %v", uris)
	}
}

func TestFiltersHiddenAndSort(t *testing.T) {
	viewFile := filepath.Join(t.TempDir(), "state", "file-chooser-view")
	hs := newHarness(t, viewFile)
	hs.open(t, OpenRequest{Filters: []Filter{{"Images", []Rule{{RuleGlob, "*.png"}}}}})
	if got := hs.shown(); !slices.Equal(got, []string{"docs", "pics", "a.png"}) {
		t.Errorf("filtered = %v", got)
	}
	hs.click(t, "Images")
	hs.click(t, "All Files")
	if got := hs.shown(); !slices.Equal(got, []string{"docs", "pics", "a.png", "b.txt"}) {
		t.Errorf("all files = %v", got)
	}
	if hs.c.ui.filterLabel.Text() != "All Files" || hs.c.ui.filterPopup.Visible() {
		t.Errorf("filter label %q, popup open %v", hs.c.ui.filterLabel.Text(), hs.c.ui.filterPopup.Visible())
	}
	hs.click(t, "Show hidden files")
	if got := hs.shown(); !slices.Contains(got, ".dot.txt") {
		t.Errorf("hidden shown = %v", got)
	}
	hs.click(t, "Name ↑")
	if got := hs.shown(); !slices.Equal(got[:2], []string{"pics", "docs"}) {
		t.Errorf("name descending = %v", got)
	}
	if b, _ := os.ReadFile(viewFile); string(b) != "name desc list\n" {
		t.Errorf("persisted view = %q", b)
	}
	hs.click(t, "Toggle grid view")
	if !hs.c.ui.grid.Visible() || hs.c.ui.list.Visible() || hs.c.ui.colHeader.Visible() {
		t.Error("grid view did not swap the views")
	}
	// The next chooser starts as this one was left.
	again := newHarness(t, viewFile)
	if again.c.view != (view{SortName, false, true}) {
		t.Errorf("reloaded view = %+v", again.c.view)
	}
	// And the hidden toggle sticks to the next request.
	got := hs.open(t, OpenRequest{})
	if !slices.Contains(hs.shown(), ".dot.txt") {
		t.Error("the hidden toggle did not stick")
	}
	hs.click(t, "Cancel")
	answer(t, got)
}

func TestRecursiveSearchStreamsMatchesForTheLiveQuery(t *testing.T) {
	hs := newHarness(t, "")
	touch(t, hs.root, ".hidden/a.png")
	hs.open(t, OpenRequest{})
	hs.do(func() { hs.c.ui.search.SetText("a.p") })
	if got := hs.shown(); !slices.Equal(got, []string{"a.png"}) {
		t.Errorf("in-folder search = %v", got)
	}
	hs.click(t, "Search subfolders")
	deadline := time.Now().Add(2 * time.Second)
	want := []string{"a.png", filepath.Join("pics", "a.png")}
	for !slices.Equal(hs.shown(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("recursive search = %v, want %v", hs.shown(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The walk skipped the dot folder; showing hidden files walks again.
	hs.click(t, "Show hidden files")
	want = []string{"a.png", filepath.Join(".hidden", "a.png"), filepath.Join("pics", "a.png")}
	for !slices.Equal(hs.shown(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("with hidden files = %v, want %v", hs.shown(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Navigating drops the query and its walk.
	hs.do(func() { hs.c.goTo(filepath.Join(hs.root, "docs")) })
	if hs.c.ui.search.Text() != "" || hs.c.stopWalk != nil || len(hs.shown()) != 0 {
		t.Errorf("after navigating: search %q, walk %v, rows %v", hs.c.ui.search.Text(), hs.c.stopWalk != nil, hs.shown())
	}
	if !hs.c.ui.empty.Visible() {
		t.Error("an empty folder hides the empty label")
	}
}

func TestDropNavigates(t *testing.T) {
	hs := newHarness(t, "")
	hs.open(t, OpenRequest{})
	s := hs.c.ui.sheet
	if got := s.DragEnter([]string{"text/plain"}, widget.Point{}); got != "" {
		t.Errorf("a text drag was accepted as %q", got)
	}
	if got := s.DragEnter([]string{"text/plain", uriListMime}, widget.Point{}); got != uriListMime {
		t.Errorf("a file drag = %q", got)
	}
	hs.do(func() { s.Drop(uriListMime, []byte("# comment\r\n"+hs.uri("pics/a.png")+"\r\n"), widget.Point{}) })
	if hs.c.s.dir != filepath.Join(hs.root, "pics") {
		t.Errorf("drop navigated to %s", hs.c.s.dir)
	}
	hs.do(func() { s.Drop(uriListMime, []byte("https://example.com/x\n"), widget.Point{}) })
	if hs.c.s.dir != filepath.Join(hs.root, "pics") {
		t.Errorf("a remote drop navigated to %s", hs.c.s.dir)
	}
}

func TestOpenFailureAnswersNothing(t *testing.T) {
	hs := newHarness(t, "")
	hs.openErr = errors.New("no layer shell")
	if uris := hs.c.Open(OpenRequest{CurrentFolder: hs.root}); uris != nil {
		t.Errorf("an unmapped chooser answered %v", uris)
	}
}

func TestImageRowsGetThumbnails(t *testing.T) {
	hs := newHarness(t, "")
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	f, err := os.Create(filepath.Join(hs.root, "pics", "real.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	hs.open(t, OpenRequest{CurrentFolder: filepath.Join(hs.root, "pics")})
	var row widget.Widget
	hs.do(func() { row = rows{hs.c, false}.Row(slices.Index(names(hs.c.s.entries), "real.png")) })
	thumb := treetest.First[*widget.Image](t, row)
	deadline := time.Now().Add(2 * time.Second)
	for {
		var ok bool
		hs.do(func() { _, ok = hs.c.thumbs[thumbKey{filepath.Join(hs.root, "pics", "real.png"), rowIconPx}] })
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the thumbnail never arrived")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !thumb.Loaded() {
		t.Error("the waiting row's image was not filled")
	}
	// A row rebuilt later reuses the decoded thumbnail at once; the
	// empty a.png cannot decode and keeps a blank square.
	hs.do(func() {
		again := treetest.First[*widget.Image](t, rows{hs.c, false}.Row(slices.Index(names(hs.c.s.entries), "real.png")))
		if !again.Loaded() {
			t.Error("a rebuilt row decoded again")
		}
	})
}

func TestColumnGripResizesAndRelaysRows(t *testing.T) {
	hs := newHarness(t, "")
	hs.open(t, OpenRequest{})
	grips := treetest.All[*dragArea](hs.cfg.Root)
	var col *dragArea
	for _, g := range grips {
		if g.cursor == "col-resize" {
			col = g
		}
	}
	if col == nil {
		t.Fatal("no column grip")
	}
	// paint renders the list's rows as they are cached.
	paint := func() []uint8 {
		var px []uint8
		hs.do(func() {
			l := hs.c.ui.list
			l.Measure(widget.Constraints{Max: widget.Size{W: 600, H: 200}})
			l.Arrange(render.Rect{W: 600, H: 200})
			px = make([]uint8, 600*200*4)
			l.Paint(render.New(px, 600*4, 600, 200))
		})
		return px
	}
	before := paint()
	hs.do(func() {
		col.HoverMove(widget.Point{X: 100})
		col.SetPressed(true)
		col.DragMove(widget.Point{X: 70})
	})
	if hs.c.columns[2] != 120 {
		t.Errorf("kind column = %d after dragging its edge 30px left, want 120", hs.c.columns[2])
	}
	hs.do(func() { col.DragMove(widget.Point{X: -1000}) })
	if hs.c.columns[2] != maxColumnW {
		t.Errorf("kind column = %d, want clamped to %d", hs.c.columns[2], maxColumnW)
	}
	// The rows re-lay on release, not on every motion.
	if !slices.Equal(paint(), before) {
		t.Error("the rows re-laid mid-drag")
	}
	hs.do(col.PressEnd)
	if slices.Equal(paint(), before) {
		t.Error("releasing the grip left the rows at the old widths")
	}
	hs.do(func() { col.SetPressed(true); col.DragMove(widget.Point{X: 5000}) })
	if hs.c.columns[2] != minColumnW {
		t.Errorf("kind column = %d, want clamped to %d", hs.c.columns[2], minColumnW)
	}
}

func TestSheetMovesResizesAndStaysOnTheOutput(t *testing.T) {
	s := newSheet(widget.NewSpacer(0, 0), 760, 520)
	area := render.Rect{W: 1920, H: 1080}
	s.Arrange(area)
	if s.x != (1920-760)/2 || s.y != (1080-520)/2 {
		t.Errorf("first placement = %d,%d, want centred", s.x, s.y)
	}
	s.moveTo(-50, 5000)
	s.Arrange(area)
	if s.x != 0 || s.y != 1080-520 {
		t.Errorf("moved off the output = %d,%d, want clamped", s.x, s.y)
	}
	s.resize(10, 99999)
	s.Arrange(area)
	if s.w != sheetMinW || s.h != 1080 {
		t.Errorf("resized = %dx%d, want the floor and the output height", s.w, s.h)
	}
	if s.y+s.h > 1080 {
		t.Errorf("the sheet runs off the bottom: y %d h %d", s.y, s.h)
	}
}

func TestDroppedPath(t *testing.T) {
	for in, want := range map[string]string{
		"file:///a/b%20c\r\nfile:///d": "/a/b c",
		"#c\nfile:///x":                "/x",
		"https://e.com/x":              "",
		"":                             "",
	} {
		got, ok := droppedPath([]byte(in))
		if got != want || ok != (want != "") {
			t.Errorf("droppedPath(%q) = %q %v", in, got, ok)
		}
	}
}

// Enter on several selected folders activates each; the first one
// entered replaces the listing, so the rest must not descend again
// from indices into the new one.
func TestEnterOnSeveralFoldersEntersOnlyTheFirst(t *testing.T) {
	hs := newHarness(t, "")
	touch(t, hs.root, "docs/d1/", "docs/d2/")
	hs.open(t, OpenRequest{Multiple: true})
	hs.do(func() {
		l := hs.c.activeView()
		l.SelectAll()
		l.KeyAction(widget.KeyEnter, 0)
	})
	var dir string
	hs.do(func() { dir = hs.c.s.dir })
	if dir != filepath.Join(hs.root, "docs") {
		t.Errorf("Enter on docs and pics went to %s, want docs", dir)
	}
	// The guard lifts once the gesture is over.
	var open bool
	hs.flush()
	hs.do(func() {
		hs.c.activate(0)
		open = hs.c.s.dir == filepath.Join(hs.root, "docs", "d1")
	})
	if !open {
		t.Error("a later activation was still ignored")
	}
}

// The sheet measures its child at its own size before arranging it: a
// Box lays its children out from what it measured, and nothing else
// measures the sheet's content.
func TestSheetMeasuresItsChild(t *testing.T) {
	fixed := widget.NewSpacer(30, 10)
	box := widget.NewBox(widget.Row, 0, 0)
	box.Append(fixed, false)
	box.Append(widget.NewSpacer(0, 0), true)
	s := newSheet(box, 600, 400)
	s.Measure(widget.Constraints{Max: widget.Size{W: 1000, H: 800}})
	s.Arrange(render.Rect{W: 1000, H: 800})
	if b := fixed.Bounds(); b.W != 30 || b.X != 200 || b.Y != 200 {
		t.Errorf("the child's first cell = %+v, want 30px at the sheet's 200,200", b)
	}
	if b := box.Bounds(); b.W != 600 || b.H != 400 {
		t.Errorf("the child = %+v, want the sheet's 600x400", b)
	}
}

// The sidebar's sections pack at the top of its scroll, not mid-view.
func TestSidebarPacksAtTheTop(t *testing.T) {
	hs := newHarness(t, "")
	hs.open(t, OpenRequest{})
	var labelY, scrollY int
	hs.do(func() {
		hs.cfg.Root.Measure(widget.Constraints{Max: widget.Size{W: 1280, H: 720}})
		hs.cfg.Root.Arrange(render.Rect{W: 1280, H: 720})
		for _, l := range treetest.All[*widget.Label](hs.cfg.Root) {
			if l.Text() == "Favorites" {
				labelY = l.Bounds().Y
			}
		}
		scrollY = treetest.First[*widget.Scroll](t, hs.cfg.Root).Bounds().Y
	})
	if labelY-scrollY > 16 {
		t.Errorf("Favorites sits %dpx into the sidebar, want it at the top", labelY-scrollY)
	}
}

// A long name ellipsizes instead of pushing its row's cells out of
// line with the column headers.
func TestLongNamesKeepTheColumnsAligned(t *testing.T) {
	hs := newHarness(t, "")
	touch(t, hs.root, "a-file-name-long-enough-to-overflow-any-sane-name-column-width.txt")
	hs.open(t, OpenRequest{})
	xs := map[int]bool{}
	hs.do(func() {
		for i := range hs.c.s.entries {
			row := rows{hs.c, false}.Row(i)
			row.Measure(widget.Constraints{Max: widget.Size{W: 600, H: listRowH}})
			row.Arrange(render.Rect{W: 600, H: listRowH})
			cells := treetest.WithClass(row, "file-chooser-cell")
			xs[cells[0].(interface{ Bounds() render.Rect }).Bounds().X] = true
		}
	})
	if len(xs) != 1 {
		t.Errorf("the size cells start at %v, want one column", xs)
	}
}

// The chooser's containers report their children to the focus ring.
func TestLayoutWidgetsReportTheirChildToTheFocusRing(t *testing.T) {
	for name, wrap := range map[string]func(widget.Widget) widget.Widget{
		"place":    func(c widget.Widget) widget.Widget { return newPlace(c, alignStart, alignStart) },
		"sheet":    func(c widget.Widget) widget.Widget { return newSheet(c, 600, 400) },
		"dragArea": func(c widget.Widget) widget.Widget { return &dragArea{child: c} },
	} {
		child := widget.NewSpacer(10, 10)
		w := wrap(child)
		w.Measure(widget.Constraints{Max: widget.Size{W: 800, H: 600}})
		w.Arrange(render.Rect{W: 800, H: 600})
		cv := render.New(make([]byte, render.Stride(800)*600), render.Stride(800), 800, 600)
		drawn := false
		cv.MarkFocus(child, func(*render.Canvas) { drawn = true })
		w.Paint(cv)
		if !drawn {
			t.Errorf("%s did not report its child", name)
		}
		cv.FinishFocus()
	}
}

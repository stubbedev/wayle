package portaldialogs

import (
	"errors"
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
	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/shell/treetest"
)

type fakeWindow struct{ closed bool }

func (w *fakeWindow) Close() { w.closed = true }

// harness runs the host with a synchronous loop: the UI work happens
// on the asking goroutine, and Open hands the tree to the test.
type harness struct {
	mu       sync.Mutex
	loop     sync.Mutex
	cfg      *config.Config
	h        *Dialogs
	opened   chan app.LayerConfig
	windows  []*fakeWindow
	openErr  error
	defaults []string
}

func newHarness(t *testing.T, apps []desktopentry.App) *harness {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Animations.Enabled = false
	hs := &harness{cfg: cfg, opened: make(chan app.LayerConfig, 4)}
	hs.h = New(Deps{
		Config: func() *config.Config { return hs.cfg },
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
		Invoke:     hs.do,
		Font:       face,
		Candidates: func([]string, string) []desktopentry.App { return apps },
		SetDefault: func(id, mime string) error {
			hs.mu.Lock()
			defer hs.mu.Unlock()
			hs.defaults = append(hs.defaults, id+"="+mime)
			return nil
		},
		Avatar: func() string { return "" },
	})
	return hs
}

// do runs fn as the loop: the dialog's work and the test's clicks
// take turns, as on the one UI goroutine.
func (hs *harness) do(fn func()) {
	hs.loop.Lock()
	defer hs.loop.Unlock()
	fn()
}

// open waits for the next mapped dialog.
func (hs *harness) open(t *testing.T) app.LayerConfig {
	t.Helper()
	select {
	case c := <-hs.opened:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no dialog opened")
		return app.LayerConfig{}
	}
}

func TestConfirmDialogsAnswer(t *testing.T) {
	hs := newHarness(t, nil)
	for _, tc := range []struct {
		click string
		want  bool
	}{{"Allow", true}, {"Deny", false}} {
		got := make(chan bool, 1)
		go func() {
			got <- hs.h.Access(AccessRequest{Title: "Camera", Subtitle: "Sub", Body: "Body", GrantLabel: "Allow", DenyLabel: "Deny", Icon: "camera-web"})
		}()
		c := hs.open(t)
		if l := treetest.Labels(c.Root); !slices.Contains(l, "Camera") || !slices.Contains(l, "Sub\nBody") {
			t.Errorf("labels = %q", l)
		}
		if c.Layer != app.LayerOverlay || c.Namespace != "wayle-portal-dialog" || c.Keyboard != app.KeyboardOnDemand {
			t.Errorf("surface = %+v", c)
		}
		hs.do(func() { treetest.Button(t, c.Root, tc.click).OnClick() })
		if r := <-got; r != tc.want {
			t.Errorf("%s answered %v", tc.click, r)
		}
	}
	hs.mu.Lock()
	closed := hs.windows[0].closed && hs.windows[1].closed
	hs.mu.Unlock()
	if !closed {
		t.Error("an answered dialog stayed mapped")
	}
}

func TestEscapeAndPreemption(t *testing.T) {
	hs := newHarness(t, nil)
	first := make(chan bool, 1)
	go func() { first <- hs.h.ConfirmInstall("App", "") }()
	c := hs.open(t)
	if l := treetest.Labels(c.Root); !slices.Contains(l, "Install “App”?") {
		t.Errorf("labels = %q", l)
	}
	// A second request answers the first no.
	second := make(chan bool, 1)
	go func() { second <- hs.h.Account("") }()
	select {
	case r := <-first:
		if r {
			t.Error("a preempted request answered yes")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a preempted request was never answered")
	}
	c = hs.open(t)
	if l := treetest.Labels(c.Root); !slices.Contains(l, "An application is requesting your name and avatar.") {
		t.Errorf("account labels = %q", l)
	}
	hs.do(func() { c.OnKey(nil, escapeKeycode, 0) })
	if r := <-second; r {
		t.Error("Escape answered yes")
	}
}

func TestOpenFailureAnswersNo(t *testing.T) {
	hs := newHarness(t, nil)
	hs.openErr = errors.New("no layer shell")
	if hs.h.ConfirmWallpaper("file:///x.png") {
		t.Error("an unmapped dialog answered yes")
	}
}

func TestWallpaperPreviewShowsTheImage(t *testing.T) {
	hs := newHarness(t, nil)
	got := make(chan bool, 1)
	go func() { got <- hs.h.ConfirmWallpaper("file:///home/u/My%20Walls/a.png") }()
	c := hs.open(t)
	var img *widget.Image
	treetest.Walk(c.Root, func(w widget.Widget) {
		if i, ok := w.(*widget.Image); ok {
			img = i
		}
	})
	if img == nil {
		t.Error("no preview image")
	}
	hs.do(func() { treetest.Button(t, c.Root, "Set wallpaper").OnClick() })
	if !<-got {
		t.Error("confirm answered no")
	}
}

// installApp writes a loadable entry and loads it.
func installApp(t *testing.T, dir, id, name string) desktopentry.App {
	t.Helper()
	path := filepath.Join(dir, id)
	if err := os.WriteFile(path, []byte("[Desktop Entry]\nType=Application\nName="+name+"\nExec=sh\nComment="+name+" app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, ok := desktopentry.LoadApp(id, path)
	if !ok {
		t.Fatal("entry did not load")
	}
	return a
}

func TestAppChooserFiltersAndPicks(t *testing.T) {
	dir := t.TempDir()
	apps := []desktopentry.App{installApp(t, dir, "viewer.desktop", "Image Viewer"), installApp(t, dir, "editor.desktop", "Text Editor")}
	hs := newHarness(t, apps)
	got := make(chan string, 1)
	go func() { got <- hs.h.ChooseApplication(nil, "image/png", "") }()
	c := hs.open(t)
	// The search is a gtk::SearchEntry; its search-changed delay runs
	// instantly here.
	defer widget.SetAnimationsInstant(true)()
	var search *widget.SearchEntry
	var remember *widget.CheckButton
	treetest.Walk(c.Root, func(w widget.Widget) {
		switch v := w.(type) {
		case *widget.SearchEntry:
			search = v
		case *widget.CheckButton:
			remember = v
		}
	})
	if search == nil || remember == nil {
		t.Fatal("no search entry or remember box")
	}
	viewer, editor := treetest.Button(t, c.Root, "Image Viewer"), treetest.Button(t, c.Root, "Text Editor")
	hs.do(func() { search.SetText("text") })
	if viewer.Visible() || !editor.Visible() {
		t.Errorf("filter: viewer %v editor %v", viewer.Visible(), editor.Visible())
	}
	hs.do(func() { search.SetText("") })
	if !viewer.Visible() {
		t.Error("clearing the search kept a row hidden")
	}
	hs.do(func() { remember.SetChecked(true) })
	hs.do(func() { editor.OnClick() })
	if id := <-got; id != "editor.desktop" {
		t.Errorf("picked %q", id)
	}
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if !slices.Equal(hs.defaults, []string{"editor.desktop=image/png"}) {
		t.Errorf("set-default = %v", hs.defaults)
	}
}

func TestAppChooserCancelAndNoType(t *testing.T) {
	dir := t.TempDir()
	hs := newHarness(t, []desktopentry.App{installApp(t, dir, "viewer.desktop", "Image Viewer")})
	got := make(chan string, 1)
	go func() { got <- hs.h.ChooseApplication([]string{"viewer.desktop"}, "", "") }()
	c := hs.open(t)
	treetest.Walk(c.Root, func(w widget.Widget) {
		if _, ok := w.(*widget.CheckButton); ok {
			t.Error("an \"always use\" box without a content type")
		}
	})
	hs.do(func() { treetest.Button(t, c.Root, "Cancel").OnClick() })
	if id := <-got; id != "" {
		t.Errorf("cancel picked %q", id)
	}
}

// A short app list packs at the top of its scroll, not mid-view.
func TestAppChooserShortListPacksAtTheTop(t *testing.T) {
	hs := newHarness(t, []desktopentry.App{installApp(t, t.TempDir(), "viewer.desktop", "Image Viewer")})
	got := make(chan string, 1)
	go func() { got <- hs.h.ChooseApplication(nil, "", "") }()
	c := hs.open(t)
	var rowY, scrollY int
	hs.do(func() {
		c.Root.Measure(widget.Constraints{Max: widget.Size{W: 1280, H: 720}})
		c.Root.Arrange(render.Rect{W: 1280, H: 720})
		rowY = treetest.Button(t, c.Root, "Image Viewer").Bounds().Y
		scrollY = treetest.First[*widget.Scroll](t, c.Root).Bounds().Y
	})
	if rowY-scrollY > 8 {
		t.Errorf("the only row sits %dpx into the list, want it at the top", rowY-scrollY)
	}
	hs.do(func() { treetest.Button(t, c.Root, "Cancel").OnClick() })
	<-got
}

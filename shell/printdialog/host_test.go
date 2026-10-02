package printdialog

import (
	"context"
	"errors"
	"io"
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
	"github.com/stubbedev/wayle/internal/ipp"
	"github.com/stubbedev/wayle/shell/treetest"
)

type fakeWindow struct{ closed bool }

func (w *fakeWindow) Close() { w.closed = true }

// spooled is one job the fake scheduler took.
type spooled struct {
	job ipp.Job
	doc string
}

type harness struct {
	loop     sync.Mutex
	mu       sync.Mutex
	h        *Dialog
	opened   chan app.LayerConfig
	printers []ipp.Printer
	listErr  error
	spoolErr error
	jobs     []spooled
	out      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Animations.Enabled = false
	hs := &harness{opened: make(chan app.LayerConfig, 4), out: filepath.Join(t.TempDir(), "output.pdf")}
	hs.printers = []ipp.Printer{{Name: "office", Location: "Hall", State: ipp.StateIdle, Accepting: true}}
	hs.h = New(Deps{
		Config: func() *config.Config { return cfg },
		Open: func(c app.LayerConfig) (Window, error) {
			hs.opened <- c
			return &fakeWindow{}, nil
		},
		Invoke: hs.do,
		Font:   face,
		Printers: func(context.Context) ([]ipp.Printer, error) {
			hs.mu.Lock()
			defer hs.mu.Unlock()
			return hs.printers, hs.listErr
		},
		Spool: func(_ context.Context, j ipp.Job) error {
			b, _ := io.ReadAll(j.Document)
			hs.mu.Lock()
			defer hs.mu.Unlock()
			hs.jobs = append(hs.jobs, spooled{j, string(b)})
			return hs.spoolErr
		},
		OutputFile: func() string { return hs.out },
		User:       "u",
	})
	return hs
}

func (hs *harness) do(fn func()) {
	hs.loop.Lock()
	defer hs.loop.Unlock()
	fn()
}

type prepareResult struct {
	ok       bool
	settings []Setting
	token    uint32
}

func (hs *harness) prepare(t *testing.T) (chan prepareResult, app.LayerConfig) {
	t.Helper()
	got := make(chan prepareResult, 1)
	go func() {
		ok, s, tok := hs.h.Prepare("Doc")
		got <- prepareResult{ok, s, tok}
	}()
	select {
	case c := <-hs.opened:
		return got, c
	case <-time.After(2 * time.Second):
		t.Fatal("no dialog opened")
		return nil, app.LayerConfig{}
	}
}

func document(t *testing.T, body string) *os.File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPrepareListsQueuesAndAnswersTheForm(t *testing.T) {
	hs := newHarness(t)
	got, c := hs.prepare(t)
	if c.Namespace != "wayle-print" || c.Layer != app.LayerOverlay {
		t.Errorf("surface = %+v", c)
	}
	labels := treetest.Labels(c.Root)
	for _, q := range hs.h.queues() {
		labels = append(labels, treetest.Labels(queueRows{hs.h, []queue{q}}.Row(0))...)
	}
	for _, want := range []string{"office", "Hall · Ready", fileQueue, "Copies", "Paper size"} {
		if !slices.Contains(labels, want) {
			t.Errorf("no %q in %q", want, labels)
		}
	}
	dropdowns := treetest.All[*widget.Dropdown](c.Root)
	entries := treetest.All[*widget.Entry](c.Root)
	hs.do(func() {
		treetest.Button(t, c.Root, "+").OnClick()
		entries[1].SetText("2-3")
		dropdowns[0].SetSelected(1) // landscape
		dropdowns[2].SetSelected(1) // grayscale
		treetest.Button(t, c.Root, "Print").OnClick()
	})
	r := <-got
	if !r.ok || r.token == 0 {
		t.Fatalf("prepare = %+v", r)
	}
	for _, want := range []Setting{{"printer", "office"}, {"n-copies", "2"}, {"orientation", "landscape"}, {"use-color", "false"}, {"page-ranges", "1-2"}} {
		if !slices.Contains(r.settings, want) {
			t.Errorf("settings %v lack %v", r.settings, want)
		}
	}

	if !hs.h.Print("Doc", document(t, "%PDF body"), r.token) {
		t.Fatal("the prepared job did not spool")
	}
	hs.mu.Lock()
	j := hs.jobs[0]
	hs.mu.Unlock()
	g := ipp.Group{Attrs: j.job.Attrs}
	if j.job.Printer != "office" || j.job.Title != "Doc" || j.job.User != "u" || j.doc != "%PDF body" {
		t.Errorf("spooled %+v", j)
	}
	if c, _ := g.Int("copies"); c != 2 || g.Text("print-color-mode") != "monochrome" {
		t.Errorf("job attrs = %+v", g)
	}
	// A token spools once.
	if hs.h.Print("Doc", document(t, "again"), r.token) {
		t.Error("a used token spooled again")
	}
}

func TestCopiesStayInBounds(t *testing.T) {
	hs := newHarness(t)
	got, c := hs.prepare(t)
	copies := treetest.All[*widget.Entry](c.Root)[0]
	hs.do(func() {
		treetest.Button(t, c.Root, "−").OnClick()
		if copies.Text() != "1" {
			t.Errorf("one copy minus one = %s, want 1", copies.Text())
		}
		copies.SetText("998")
		treetest.Button(t, c.Root, "+").OnClick()
		treetest.Button(t, c.Root, "+").OnClick()
		if copies.Text() != "999" {
			t.Errorf("past the top = %s, want 999", copies.Text())
		}
		copies.SetText("lots")
		treetest.Button(t, c.Root, "Print").OnClick()
	})
	r := <-got
	if !slices.Contains(r.settings, Setting{"n-copies", "1"}) {
		t.Errorf("garbage copies = %v, want 1", r.settings)
	}
}

func TestCancelEscapeAndPreemption(t *testing.T) {
	hs := newHarness(t)
	got, c := hs.prepare(t)
	hs.do(func() { treetest.Button(t, c.Root, "Cancel").OnClick() })
	if r := <-got; r.ok || r.token != 0 {
		t.Errorf("cancel = %+v", r)
	}
	got, c = hs.prepare(t)
	hs.do(func() { c.OnKey(nil, escapeKeycode, 0) })
	if r := <-got; r.ok {
		t.Errorf("escape = %+v", r)
	}
	first, _ := hs.prepare(t)
	second, c := hs.prepare(t)
	select {
	case r := <-first:
		if r.ok {
			t.Errorf("a preempted prepare answered %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a preempted prepare was never answered")
	}
	hs.do(func() { treetest.Button(t, c.Root, "Print").OnClick() })
	if r := <-second; !r.ok {
		t.Error("the second prepare did not answer")
	}
}

func TestPrintToFileAndAMissingScheduler(t *testing.T) {
	hs := newHarness(t)
	hs.listErr = errors.New("connect: no such file")
	hs.printers = nil
	got, c := hs.prepare(t)
	if q := hs.h.queues(); len(q) != 1 || q[0].name != fileQueue {
		t.Errorf("without CUPS the queues are %+v, want only %s", q, fileQueue)
	}
	_ = c
	hs.do(func() { treetest.Button(t, c.Root, "Print").OnClick() })
	r := <-got
	if !slices.Contains(r.settings, Setting{"printer", fileQueue}) {
		t.Fatalf("settings = %v", r.settings)
	}
	if !hs.h.Print("Doc", document(t, "%PDF to file"), r.token) {
		t.Fatal("print to file failed")
	}
	if b, err := os.ReadFile(hs.out); err != nil || string(b) != "%PDF to file" {
		t.Errorf("output = %q %v", b, err)
	}
	if len(hs.jobs) != 0 {
		t.Error("print to file reached the scheduler")
	}
}

func TestPrintFailures(t *testing.T) {
	hs := newHarness(t)
	doc := document(t, "x")
	if hs.h.Print("Doc", doc, 77) {
		t.Error("an unknown token spooled")
	}
	if _, err := doc.Read(make([]byte, 1)); err == nil {
		t.Error("an unspooled document was left open")
	}
	got, c := hs.prepare(t)
	hs.do(func() { treetest.Button(t, c.Root, "Print").OnClick() })
	r := <-got
	hs.spoolErr = errors.New("printer on fire")
	if hs.h.Print("Doc", document(t, "x"), r.token) {
		t.Error("a refused job reported sent")
	}
	got, c = hs.prepare(t)
	hs.do(func() {
		treetest.First[*widget.List](t, c.Root).Select(1) // Print to File
		treetest.Button(t, c.Root, "Print").OnClick()
	})
	r = <-got
	hs.out = filepath.Join(t.TempDir(), "missing-dir", "output.pdf")
	if hs.h.Print("Doc", document(t, "x"), r.token) {
		t.Error("an unwritable output reported sent")
	}
}

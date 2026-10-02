package printdialog

import (
	"context"
	"io"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/ipp"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/shell/reveal"
)

// The sheet's geometry (shell/print: the surface's width request and
// the printer list's content heights).
const (
	surfaceWidth  = 460
	labelWidth    = 110
	printerRowH   = 44
	listMinHeight = 64
	listMaxHeight = 240
	escapeKeycode = 1
)

// documentFormat lets CUPS type the document, as GTK's backend does for
// a source fd.
const documentFormat = "application/octet-stream"

// Window is the overlay surface the dialog maps (app.LayerWindow).
type Window interface{ Close() }

// Deps are the dialog's collaborators.
type Deps struct {
	// Config is the live config (the print animation).
	Config func() *config.Config
	// Open maps the overlay; Invoke runs on the UI loop.
	Open   func(cfg app.LayerConfig) (Window, error)
	Invoke func(func())
	// Font labels the sheet in Ink until Sheet (the print-* classes)
	// restyles it.
	Font  render.Font
	Ink   render.Color
	Sheet *widget.Stylesheet
	// Printers lists the CUPS queues and Spool sends a job to one.
	Printers func(context.Context) ([]ipp.Printer, error)
	Spool    func(context.Context, ipp.Job) error
	// OutputFile is where Print to File writes.
	OutputFile func() string
	// User names the job's owner.
	User string
}

// queue is one printer row.
type queue struct {
	name, detail string
}

// prepared is a confirmed form stashed under its token until Print.
type prepared struct {
	printer string
	form    form
}

// Dialog is the com.wayle.Print1 host. Prepare and Print may be called
// from any goroutine; the UI runs on the loop.
type Dialog struct {
	d       Deps
	win     Window
	rev     *widget.Revealer
	closing bool
	answer  func(p prepared, ok bool)

	mu   sync.Mutex
	jobs map[uint32]prepared
	next uint32
}

// New builds the host; nothing maps until a request.
func New(d Deps) *Dialog { return &Dialog{d: d, jobs: map[uint32]prepared{}, next: 1} }

// Prepare shows the printer picker and settings form; a confirmed one
// is stashed under the returned token.
func (h *Dialog) Prepare(title string) (bool, []Setting, uint32) {
	queues := h.queues()
	ch := make(chan struct {
		p  prepared
		ok bool
	}, 1)
	h.d.Invoke(func() {
		h.settle(prepared{}, false)
		h.answer = func(p prepared, ok bool) {
			ch <- struct {
				p  prepared
				ok bool
			}{p, ok}
		}
		h.show(h.build(queues))
	})
	r := <-ch
	if !r.ok {
		return false, nil, 0
	}
	h.mu.Lock()
	token := h.next
	h.next = max(h.next+1, 1)
	h.jobs[token] = r.p
	h.mu.Unlock()
	return true, r.p.form.settings(r.p.printer), token
}

// Print spools document to the printer prepared under token, once;
// the dialog owns and closes the document.
func (h *Dialog) Print(title string, document *os.File, token uint32) bool {
	defer func() { _ = document.Close() }()
	h.mu.Lock()
	job, ok := h.jobs[token]
	delete(h.jobs, token)
	h.mu.Unlock()
	if !ok {
		log.Printf("print: no prepared printer for token %d", token)
		return false
	}
	if job.printer == fileQueue {
		return h.toFile(document)
	}
	err := h.d.Spool(context.Background(), ipp.Job{
		Printer: job.printer, Title: title, User: h.d.User, Format: documentFormat,
		Attrs: job.form.jobAttrs(), Document: document,
	})
	if err != nil {
		log.Printf("print: spooling to %s: %v", job.printer, err)
		return false
	}
	return true
}

// toFile writes the document where Print to File puts it.
func (h *Dialog) toFile(document io.Reader) bool {
	path := h.d.OutputFile()
	out, err := os.Create(path) //nolint:gosec // the user's own output file
	if err != nil {
		log.Printf("print: to file: %v", err)
		return false
	}
	_, err = io.Copy(out, document)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		log.Printf("print: to file %s: %v", path, err)
		return false
	}
	return true
}

// queues are the CUPS printers, then Print to File; an unreachable
// scheduler leaves just the file.
func (h *Dialog) queues() []queue {
	var out []queue
	printers, err := h.d.Printers(context.Background())
	if err != nil && !ipp.IsNotFound(err) {
		log.Printf("print: listing printers: %v", err)
	}
	for _, p := range printers {
		out = append(out, queue{p.Name, printerDetail(p.Location, printerStatus(p))})
	}
	return append(out, queue{fileQueue, ""})
}

// settle answers the open request, if any.
func (h *Dialog) settle(p prepared, ok bool) {
	if h.answer != nil {
		answer := h.answer
		h.answer = nil
		answer(p, ok)
	}
}

// finish answers and plays the exit.
func (h *Dialog) finish(p prepared, ok bool) {
	h.settle(p, ok)
	h.hide()
}

func (h *Dialog) label(text, class string) *widget.Label {
	l := widget.NewLabel(h.d.Font, 13, text, h.d.Ink)
	l.AddClass(class)
	return l
}

func (h *Dialog) button(text, class string, onClick func()) *widget.Button {
	l := h.label(text, class+"-label")
	l.SetAlignment(render.AlignCenter)
	b := widget.NewButton(l, 0, 0)
	b.AddClass(class)
	b.OnClick = onClick
	return b
}

func (h *Dialog) dropdown(items []string) *widget.Dropdown {
	return widget.NewDropdown(h.d.Font, 13, items, 0)
}

// formRow is a label column and its control.
func (h *Dialog) formRow(label string, control widget.Widget) widget.Widget {
	row := widget.NewBox(widget.Row, 12, 0)
	l := h.label(label, "print-form-label")
	row.Append(credential.NewFixed(l, labelWidth, 0), false)
	row.Append(control, true)
	return row
}

// queueRows is the printer list's model.
type queueRows struct {
	h      *Dialog
	queues []queue
}

func (m queueRows) Len() int { return len(m.queues) }

func (m queueRows) Row(i int) widget.Widget {
	q := m.queues[i]
	content := widget.NewBox(widget.Column, 0, 0)
	content.Append(m.h.label(q.name, "print-printer-name"), false)
	if q.detail != "" {
		content.Append(m.h.label(q.detail, "print-printer-detail"), false)
	}
	// shell/print's printer_row margins.
	row := widget.NewBox(widget.Column, 0, 0)
	row.SetElement("row")
	row.Append(credential.NewInset(content, render.Insets{Top: 6, Right: 10, Bottom: 6, Left: 10}), false)
	return row
}

// build is the sheet: the printer list over the settings form.
func (h *Dialog) build(queues []queue) widget.Widget {
	list := widget.NewList[widget.Widget](queueRows{h, queues}, printerRowH)
	list.AddClass("print-printer-list")
	list.Select(0)
	listH := min(max(len(queues)*printerRowH, listMinHeight), listMaxHeight)

	copies := widget.NewEntry(h.d.Font, 13, h.d.Ink)
	copies.SetText("1")
	step := func(by int) func() {
		return func() { copies.SetText(strconv.Itoa(clampCopies(parseCopies(copies.Text()) + by))) }
	}
	copiesRow := widget.NewBox(widget.Row, 4, 0)
	copiesRow.Append(h.button("−", "print-copies-step", step(-1)), false)
	copiesRow.Append(copies, true)
	copiesRow.Append(h.button("+", "print-copies-step", step(1)), false)

	pages := widget.NewEntry(h.d.Font, 13, h.d.Ink)
	pages.SetPlaceholder("All pages — or e.g. 1-5, 8")
	orientation, paperDrop := h.dropdown(orientations), h.dropdown(paperLabels())
	color, duplex, quality := h.dropdown(colors), h.dropdown(duplexes), h.dropdown(qualities)

	formBox := widget.NewBox(widget.Column, 8, 0)
	formBox.AddClass("print-form")
	for _, r := range []struct {
		label   string
		control widget.Widget
	}{
		{"Copies", copiesRow},
		{"Pages", pages},
		{"Orientation", orientation},
		{"Paper size", paperDrop},
		{"Color", color},
		{"Two-sided", duplex},
		{"Quality", quality},
	} {
		formBox.Append(h.formRow(r.label, r.control), false)
	}

	confirm := func() {
		sel := list.Selected()
		if sel < 0 || sel >= len(queues) {
			h.finish(prepared{}, false)
			return
		}
		h.finish(prepared{printer: queues[sel].name, form: form{
			copies:    clampCopies(parseCopies(copies.Text())),
			pages:     pages.Text(),
			landscape: orientation.Selected() == 1,
			paper:     paperDrop.Selected(),
			grayscale: color.Selected() == 1,
			duplex:    max(duplex.Selected(), 0),
			quality:   max(quality.Selected(), 0),
		}}, true)
	}
	actions := widget.NewBox(widget.Row, 8, 0)
	actions.Append(widget.NewSpacer(0, 0), true)
	actions.Append(h.button("Cancel", "portal-dialog-cancel", func() { h.finish(prepared{}, false) }), false)
	printButton := h.button("Print", "portal-dialog-confirm", confirm)
	printButton.AddClass("suggested-action")
	actions.Append(printButton, false)

	surface := widget.NewBox(widget.Column, 12, 0)
	surface.AddClass("print-surface")
	title := h.label("Print", "print-title")
	surface.Append(title, false)
	surface.Append(credential.NewFixed(list, 0, listH), false)
	surface.Append(formBox, false)
	surface.Append(actions, false)
	return credential.NewFixed(surface, surfaceWidth, 0)
}

func paperLabels() []string {
	out := make([]string, len(papers))
	for i, p := range papers {
		out[i] = p.label
	}
	return out
}

// parseCopies reads the copies field; garbage is one copy.
func parseCopies(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return minCopies
	}
	return n
}

func clampCopies(n int) int { return min(max(n, minCopies), maxCopies) }

// show maps the overlay with tree and plays the entry.
func (h *Dialog) show(tree widget.Widget) {
	if h.win != nil {
		h.win.Close()
		h.win, h.closing = nil, false
	}
	h.rev = widget.NewRevealer(credential.Center(tree))
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass("print-window")
	if h.d.Sheet != nil {
		root.AttachStylesheet(h.d.Sheet)
	}
	root.Append(h.rev, true)
	win, err := h.d.Open(app.LayerConfig{
		Layer:         app.LayerOverlay,
		Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardOnDemand,
		Namespace:     "wayle-print",
		Root:          root,
		OnKey: func(_ *widget.Router, keycode uint32, _ app.Mods) {
			if keycode == escapeKeycode {
				h.finish(prepared{}, false)
			}
		},
	})
	if err != nil {
		log.Printf("print: cannot map the overlay: %v", err)
		h.settle(prepared{}, false)
		return
	}
	h.win = win
	reveal.Show(h.rev, h.d.Config().Animations, config.AnimPrint)
}

// hide plays the exit and closes the surface.
func (h *Dialog) hide() {
	if h.win == nil || h.closing {
		return
	}
	h.closing = true
	win := h.win
	reveal.Hide(h.rev, h.d.Config().Animations, config.AnimPrint, func() {
		win.Close()
		if h.win == win {
			h.win, h.rev, h.closing = nil, nil, false
		}
	})
}

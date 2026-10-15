package printdialog

import (
	"context"
	"io"
	"log"
	"os"
	"sync"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/ipp"
	"github.com/stubbedev/wayle/shell/reveal"
)

// The sheet's geometry (shell/print: the surface's width request, the
// printer list's content heights, the form grid's spacing).
const (
	surfaceWidth  = 460
	listMinHeight = 64
	listMaxHeight = 240
	formColGap    = 12
	formRowGap    = 8
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
	// Font shapes the text; Sheet styles the surface, the labels'
	// and entries' ink included.
	Font  render.Font
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
	l := widget.NewLabel(h.d.Font, 13, text, 0)
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

// printerRow is printer_row: the name over the location and status,
// inside the row's 6px/10px margins.
func (h *Dialog) printerRow(q queue) widget.Widget {
	row := widget.NewBox(widget.Column, 0, 0)
	row.SetMargin(render.Insets{Top: 6, Right: 10, Bottom: 6, Left: 10})
	row.Append(h.label(q.name, "print-printer-name"), false)
	if q.detail != "" {
		row.Append(h.label(q.detail, "print-printer-detail"), false)
	}
	return row
}

// build is the sheet (the print view!): the title, the printer list in
// a scroll that grows with it from 64 to 240px, the .print-form grid of
// labels and controls, and the actions.
func (h *Dialog) build(queues []queue) widget.Widget {
	list := widget.NewListBox()
	list.SetClasses("print-printer-list")
	for _, q := range queues {
		list.Append(h.printerRow(q))
	}
	if len(queues) > 0 {
		list.Select(0)
	}
	printers := widget.NewScroll(list)
	printers.FillX = true
	printers.PropagateNaturalHeight = true
	printers.SetMaxContentHeight(listMaxHeight)
	printers.SetSizeRequest(0, listMinHeight)

	copies := widget.NewSpinButton(h.d.Font, 13, 0, minCopies, maxCopies, 1, 0)
	copies.SetValue(minCopies)
	pages := widget.NewEntry(h.d.Font, 13, 0)
	pages.SetPlaceholder("All pages — or e.g. 1-5, 8")
	orientation, paperDrop := h.dropdown(orientations), h.dropdown(paperLabels())
	color, duplex, quality := h.dropdown(colors), h.dropdown(duplexes), h.dropdown(qualities)

	grid := widget.NewGrid(formColGap, formRowGap)
	grid.AddClass("print-form")
	for i, r := range []struct {
		label   string
		control interface {
			widget.Widget
			SetHExpand(bool)
		}
	}{
		{"Copies", copies},
		{"Pages", pages},
		{"Orientation", orientation},
		{"Paper size", paperDrop},
		{"Color", color},
		{"Two-sided", duplex},
		{"Quality", quality},
	} {
		grid.Attach(h.label(r.label, "print-form-label"), 0, i, 1, 1)
		r.control.SetHExpand(true)
		grid.Attach(r.control, 1, i, 1, 1)
	}

	confirm := func() {
		sel := list.Selected()
		if sel < 0 || sel >= len(queues) {
			h.finish(prepared{}, false)
			return
		}
		h.finish(prepared{printer: queues[sel].name, form: form{
			copies:    int(copies.Value()),
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
	surface.SetSizeRequest(surfaceWidth, 0)
	surface.Append(h.label("Print", "print-title"), false)
	surface.Append(printers, false)
	surface.Append(grid, false)
	surface.Append(actions, false)
	return surface
}

func paperLabels() []string {
	out := make([]string, len(papers))
	for i, p := range papers {
		out[i] = p.label
	}
	return out
}

// show maps the overlay with tree and plays the entry.
func (h *Dialog) show(tree widget.Widget) {
	if h.win != nil {
		h.win.Close()
		h.win, h.closing = nil, false
	}
	// window.print-window > revealer > the sheet, on an unanchored
	// layer the compositor centers, sized to its content.
	h.rev = widget.NewRevealer(tree)
	root := widget.NewBox(widget.Column, 0, 0)
	root.SetElement("window")
	root.AddClass("print-window")
	if h.d.Sheet != nil {
		root.AttachStylesheet(h.d.Sheet)
	}
	root.Append(h.rev, true)
	win, err := h.d.Open(app.LayerConfig{
		Layer:         app.LayerOverlay,
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

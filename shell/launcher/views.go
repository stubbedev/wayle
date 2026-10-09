package launcher

import (
	"math"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/styling"
)

// fontPx is GTK's default 11pt at 96 dpi, which the launcher's labels
// take before the stylesheet (or -font) sizes them.
const fontPx = 11.0 * 96 / 72

// sessionCSSPriority puts a session's -font/-style CSS above the
// shell's own stylesheet, as the Rust provider sits above the shell's
// (PRIORITY_USER + 200 over + 100): at anything below, a -font simply
// lost to [styling] and looked like it did nothing.
const sessionCSSPriority = 100

// views is one session's widget tree (the view! of mod.rs).
type views struct {
	root     *widget.Box
	surface  *widget.Box
	inputRow *widget.Box
	prompt   *widget.Label
	entry    *widget.Entry
	message  *widget.RichLabel
	frame    *widget.Box
	list     *resultList
	tabs     *widget.Box
	// width is the surface width it was mapped at; lastH the height it
	// was last sized to.
	width, lastH int
}

// buildViews builds and fills the tree for session a (apply_ui).
func (s *Surface) buildViews(a *active) *views {
	ui := a.ui
	v := &views{}
	v.root = widget.NewBox(widget.Column, 0, 0)
	v.root.AddClass("launcher-window")
	if s.d.Sheet != nil {
		v.root.AttachStylesheet(s.d.Sheet)
	}
	if ui.css != "" {
		v.root.AttachStylesheet(widget.NewStylesheet(ui.css, s.d.SheetPriority+sessionCSSPriority))
	}
	v.surface = widget.NewBox(widget.Column, 0, 0)
	v.surface.AddClass("launcher-surface")
	v.root.Append(v.surface, true)

	v.inputRow = widget.NewBox(widget.Row, 0, 0)
	v.inputRow.AddClass("launcher-input")
	v.prompt = widget.NewLabel(s.d.Font, fontPx, "", s.d.Ink)
	v.prompt.AddClass("launcher-prompt")
	v.inputRow.Append(v.prompt, false)
	v.entry = widget.NewEntry(s.d.Font, fontPx, s.d.Ink)
	v.entry.AddClass("launcher-entry")
	if ui.filter != nil {
		v.entry.SetText(*ui.filter)
	}
	if ui.password {
		v.entry.SetEcho(widget.EchoPassword)
	}
	v.entry.OnChanged = func(text string) {
		if s.cur == a {
			s.send(cmdSetQuery{query: text})
		}
	}
	v.inputRow.Append(v.entry, true)
	v.surface.Append(v.inputRow, false)

	v.message = widget.NewRichLabel(s.d.Font, fontPx, "", s.d.Ink)
	v.message.AddClass("launcher-message")
	v.message.SetWrap(true)
	v.surface.Append(v.message, false)

	st := rowStyle{
		font: s.d.Font, px: fontPx, ink: s.d.Ink, iconPx: iconPx(s.d), showIcons: ui.showIcons,
		display: rowDisplay{columns: ui.displayColumns, separator: ui.columnSeparator, ellipsize: ui.ellipsize},
	}
	v.list = newResultList(func(pos int) widget.Widget { return s.makeRow(a, pos, st) })
	v.list.visibleRows = max(ui.lines, 1)
	v.list.fixed = ui.fixedNumLines
	v.list.onScroll = s.listScrolled
	v.frame = widget.NewBox(widget.Column, 0, 0)
	v.frame.AddClass("launcher-list")
	v.frame.Append(v.list, true)
	v.surface.Append(v.frame, true)

	v.tabs = widget.NewBox(widget.Row, 0, 0)
	v.tabs.AddClass("launcher-tabs")
	v.tabs.SetVisible(ui.sidebar)
	v.surface.Append(v.tabs, false)

	v.setPrompt(deref(ui.prompt, ""))
	message := ui.errorMessage
	if message == nil {
		message = ui.mesg
	}
	v.setMessage(message)
	v.inputRow.SetVisible(!a.dialog)
	v.frame.SetVisible(!a.dialog)
	return v
}

// makeRow is the row factory for list position pos; a thumbnail still
// being made lands on the row if it is still showing that file.
func (s *Surface) makeRow(a *active, pos int, st rowStyle) widget.Widget {
	it, _ := s.model.item(pos)
	index, _ := s.model.itemIndex(pos)
	row := buildRow(pos, it, pos == s.sel, st, &s.multi, index)
	row.onButton = s.rowPressed
	if path := thumbnailPending(it.Icon); path != "" && st.showIcons {
		generateThumbnail(a.ctx, a.thumbs, path, func(made string) {
			s.d.Invoke(func() {
				if s.cur != a || a.views.list.rows[pos] != widget.Widget(row) {
					return
				}
				row.iconSlot.Clear()
				icon := widget.NewFileIcon(made, st.iconPx)
				icon.AddClass("launcher-row-icon")
				row.iconSlot.Append(icon, false)
			})
		})
	}
	return row
}

// setPrompt shows the prompt, hidden when empty.
func (v *views) setPrompt(text string) {
	v.prompt.SetText(text)
	v.prompt.SetVisible(text != "")
}

// setMessage shows the message line, hidden without one.
func (v *views) setMessage(markup *string) {
	if markup == nil {
		v.message.SetVisible(false)
		return
	}
	v.message.SetMarkup(messageMarkup(*markup))
	v.message.SetVisible(true)
}

// height is the tree's natural height at width.
func (v *views) height(width int) int {
	return v.root.Measure(widget.Constraints{Max: widget.Size{W: width, H: math.MaxInt32}}).H
}

// iconPx is -gtk-icon-size: 1.5rem at the styling scale.
func iconPx(d Deps) int {
	scale := 1.0
	if d.Config != nil {
		if cfg := d.Config(); cfg != nil && cfg.Styling.Scale > 0 {
			scale = float64(cfg.Styling.Scale)
		}
	}
	return int(math.Round(1.5 * styling.RemBase * scale))
}

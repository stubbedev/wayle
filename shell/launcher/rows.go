package launcher

import (
	"context"
	"strings"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/pango"
	engine "github.com/stubbedev/wayle/service/launcher"
)

// rowDisplay is RowDisplay: which columns of a row show, split on what,
// and where a long row truncates.
type rowDisplay struct {
	// columns are 1-based (-display-columns); nil shows the whole text.
	columns   []uint32
	separator string
	// ellipsize is start, middle, or anything else for end.
	ellipsize string
}

// applyColumns keeps the configured columns, joined by a space.
func (d rowDisplay) applyColumns(text string) string {
	if d.columns == nil {
		return text
	}
	parts := strings.Split(text, d.separator)
	var kept []string
	for _, c := range d.columns {
		if i := int(c) - 1; c > 0 && i < len(parts) {
			kept = append(kept, parts[i])
		}
	}
	return strings.Join(kept, " ")
}

// mode is the label's truncation.
func (d rowDisplay) mode() widget.EllipsizeMode {
	switch d.ellipsize {
	case "start":
		return widget.EllipsizeStart
	case "middle":
		return widget.EllipsizeMiddle
	}
	return widget.EllipsizeEnd
}

// multiSelect is MultiSelect: whether the mode takes several rows, the
// rows picked so far (by item index), and the ballots that mark them.
type multiSelect struct {
	enabled                          bool
	picked                           map[uint32]bool
	ballotSelected, ballotUnselected string
}

// rowStyle is what every row in a session is built with.
type rowStyle struct {
	font      render.Font
	px        float64
	ink       render.Color
	iconPx    int
	showIcons bool
	display   rowDisplay
}

// launcherRow is one list row: GTK's listview `row` element (selected
// rows carry :selected) around the .launcher-row box. It is the hit
// leaf, so every button and click count reaches the bindings.
type launcherRow struct {
	*widget.Box
	pos      int
	content  *widget.Box
	iconSlot *widget.Box
	// onButton runs the pointer bindings for a press on the row.
	onButton func(pos int, button engine.MouseButton, presses int)
}

// HitTest makes the row the leaf the pointer lands on.
func (r *launcherRow) HitTest(p widget.Point) widget.Widget { return r.HitLeaf(r, p) }

// ClickAt is a single primary click.
func (r *launcherRow) ClickAt(widget.Point) { r.press(engine.MousePrimary, 1) }

// DoubleClickAt is the second click of a double.
func (r *launcherRow) DoubleClickAt(widget.Point) { r.press(engine.MousePrimary, 2) }

// PointerButton is any other button.
func (r *launcherRow) PointerButton(code uint32) { r.press(gdkButton(code), 1) }

func (r *launcherRow) press(button engine.MouseButton, presses int) {
	if r.onButton != nil {
		r.onButton(r.pos, button, presses)
	}
}

// buildRow is the factory's setup and bind for list position pos.
func buildRow(pos int, it engine.Item, selected bool, st rowStyle, multi *multiSelect, itemIndex uint32) *launcherRow {
	content := widget.NewBox(widget.Row, 8, 0)
	content.AddClass("launcher-row")
	for flag, class := range map[engine.ItemFlags]string{
		engine.FlagUrgent: "urgent", engine.FlagActive: "active", engine.FlagNonselectable: "nonselectable",
	} {
		if it.Flags.Has(flag) {
			content.AddClass(class)
		}
	}
	if multi != nil && multi.enabled {
		ballot := multi.ballotUnselected
		if multi.picked[itemIndex] {
			ballot = multi.ballotSelected
		}
		b := widget.NewLabel(st.font, st.px, ballot, st.ink)
		b.AddClass("launcher-row-ballot")
		content.Append(b, false)
	}
	slot := widget.NewBox(widget.Row, 0, 0)
	if st.showIcons {
		if icon := rowIcon(it.Icon, st.iconPx); icon != nil {
			slot.Append(icon, false)
		}
		content.Append(slot, false)
	}
	shown := st.display.applyColumns(it.Display)
	var label widget.Widget
	if it.Flags.Has(engine.FlagMarkup) {
		markup, ok := pango.Translate(shown)
		if !ok {
			markup = pango.Escape(shown)
		}
		rl := widget.NewRichLabel(st.font, st.px, markup, st.ink)
		rl.SetEllipsize(st.display.mode())
		label = rl
	} else {
		l := widget.NewLabel(st.font, st.px, shown, st.ink)
		l.SetEllipsize(st.display.mode())
		label = l
	}
	if c, ok := label.(interface{ AddClass(...string) }); ok {
		c.AddClass("launcher-row-label")
	}
	content.Append(label, true)

	row := &launcherRow{Box: widget.NewBox(widget.Row, 0, 0), pos: pos, content: content, iconSlot: slot}
	row.SetElement("row")
	row.SetState(widget.StateSelected, selected)
	row.Append(content, true)
	return row
}

// rowIcon is the icon a row shows right away: a theme name, a file, a
// cached thumbnail, or a thumbnail's fallback until one is made.
func rowIcon(icon engine.Icon, px int) *widget.Icon {
	var w *widget.Icon
	switch ic := icon.(type) {
	case engine.IconName:
		w = widget.NewThemeIcon(string(ic), px)
	case engine.IconFile:
		w = widget.NewFileIcon(string(ic), px)
	case engine.IconThumbnail:
		if cached, ok := engine.CachedThumbnail(ic.Path); ok {
			w = widget.NewFileIcon(cached, px)
		} else {
			w = widget.NewThemeIcon(ic.Fallback, px)
		}
	default:
		return nil
	}
	w.AddClass("launcher-row-icon")
	return w
}

// thumbnailPending is the file a row's icon waits on, "" when it shows
// its final icon.
func thumbnailPending(icon engine.Icon) string {
	if ic, ok := icon.(engine.IconThumbnail); ok {
		if _, cached := engine.CachedThumbnail(ic.Path); !cached {
			return ic.Path
		}
	}
	return ""
}

// generateThumbnail makes path's thumbnail off the loop and hands the
// made file to done (nothing when none could be made).
func generateThumbnail(ctx context.Context, t engine.Thumbnailer, path string, done func(string)) {
	go func() {
		if made, ok := t.Generate(ctx, path); ok {
			done(made)
		}
	}()
}

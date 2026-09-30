// Package credential is the credential prompt the lock screen and the
// greeter share (wayle-widgets' credential_box.rs): clock, date, an
// optional username entry, the secret entry, and the error line, on a
// translucent card centered over the background. Both screens build it
// the same way, so they render identically; the styling is the
// _lock.scss rules resolved against the palette.
package credential

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/styling"
)

// The _lock.scss metrics, in logical pixels (1rem = 16px).
const (
	cardPadding   = 32 // .lock-center padding: 2rem
	cardRadius    = 12 // .lock-center border-radius
	columnSpacing = 12 // the credential column's spacing
	clockPx       = 64 // .lock-clock font-size: 4rem
	datePx        = 20 // .lock-date font-size: 1.25rem
	entryPx       = 16
	entryWidth    = 24 * 10 // width-chars 24 at the entry size
	entryGap      = 16      // .lock-entry / .lock-user margin-top: 1rem
	errorGap      = 8       // .lock-error margin-top: 0.5rem
)

// cardColor is .lock-center's background: rgba(0, 0, 0, 0.35).
var cardColor = render.RGBA(0, 0, 0, 89)

// Fonts are the faces the prompt paints with.
type Fonts struct {
	// Text is the regular face (date, entries, messages).
	Text render.Font
	// Clock is the bold face of the clock (.lock-clock weight 700).
	Clock render.Font
}

// Options configure one prompt.
type Options struct {
	Fonts   Fonts
	Palette *styling.Palette
	// ShowClock shows the clock and date.
	ShowClock bool
	// WithUsername adds a visible username entry above the secret one
	// (the greeter; the lock screen authenticates the session user).
	WithUsername bool
	// Header goes between the date and the entries (the greeter's user
	// list); Extra between the secret entry and the error line (the
	// greeter's session picker). Either may be nil.
	Header, Extra widget.Widget
	// Focus moves keyboard focus; the username entry's Enter uses it
	// to move on to the secret entry.
	Focus func(widget.Widget)
}

// Prompt is a built credential prompt and the handles its screen
// drives.
type Prompt struct {
	// Root is the full-surface layer holding the centered card.
	Root widget.Widget
	// Username is the visible username entry; nil without
	// WithUsername.
	Username *widget.Entry
	// Entry is the secret entry; Enter submits it.
	Entry *widget.Entry
	// Clock and Date are hidden without ShowClock.
	Clock, Date *widget.Label
	// Error is the error and info line, hidden until it has text.
	Error *widget.Label
}

// Build assembles a prompt; onSubmit fires with the secret entry's text
// each time the user presses Enter in it.
func Build(o Options, onSubmit func(string)) *Prompt {
	pal := o.Palette
	p := &Prompt{
		Clock: widget.NewLabel(o.Fonts.Clock, clockPx, "", pal.Fg),
		Date:  widget.NewLabel(o.Fonts.Text, datePx, "", pal.FgMuted),
		Entry: widget.NewEntry(o.Fonts.Text, entryPx, pal.Fg),
		Error: widget.NewLabel(o.Fonts.Text, entryPx, "", pal.Red),
	}
	for _, l := range []*widget.Label{p.Clock, p.Date, p.Error} {
		l.SetAlignment(render.AlignCenter)
	}
	p.Clock.SetVisible(o.ShowClock)
	p.Date.SetVisible(o.ShowClock)
	p.Entry.SetEcho(widget.EchoPassword)
	p.Entry.OnActivate = onSubmit
	p.Error.SetVisible(false)

	column := widget.NewBox(widget.Column, columnSpacing, 0)
	column.Append(p.Clock, false).Append(p.Date, false)
	if o.Header != nil {
		column.Append(o.Header, false)
	}
	if o.WithUsername {
		p.Username = widget.NewEntry(o.Fonts.Text, entryPx, pal.Fg)
		p.Username.SetPlaceholder("Username")
		p.Username.OnActivate = func(string) {
			if o.Focus != nil {
				o.Focus(p.Entry)
			}
		}
		column.Append(withGap(p.Username, entryGap), false)
	}
	column.Append(withGap(p.Entry, entryGap), false)
	if o.Extra != nil {
		column.Append(o.Extra, false)
	}
	column.Append(withGap(p.Error, errorGap), false)

	p.Root = Center(NewPanel(column, cardPadding, cardRadius, cardColor))
	return p
}

// SetMessage shows text on the error line, or hides it when empty.
func (p *Prompt) SetMessage(text string) {
	p.Error.SetText(text)
	p.Error.SetVisible(text != "")
}

// withGap puts a fixed gap above w (a CSS margin-top) and pins the
// entries to their width-chars width.
func withGap(w widget.Widget, gap int) widget.Widget {
	col := widget.NewBox(widget.Column, 0, 0)
	col.Append(NewSpacer(0, gap), false)
	if _, isEntry := w.(*widget.Entry); isEntry {
		w = NewFixed(w, entryWidth, 0)
	}
	col.Append(w, false)
	return col
}

// Center places w at its natural size in the middle of the surface.
func Center(w widget.Widget) widget.Widget {
	g := widget.NewGrid(0, 0)
	g.Attach(w, 0, 0, 1, 1)
	g.SetAlign(w, widget.AlignCenter, widget.AlignCenter)
	return g
}

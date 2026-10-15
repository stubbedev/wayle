// Package credential is the credential prompt the lock screen and the
// greeter share (wayle-widgets' credential_box.rs): clock, date, an
// optional username entry, the secret entry, and the error line in the
// centered .lock-center card. Both screens build it the same way and
// style it with the same _lock.scss rules, so they render identically.
package credential

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// The prompt's fixed metrics: GTK's defaults the stylesheet starts
// from, not styling.
const (
	columnSpacing = 12 // the credential column's spacing
	entryChars    = 24 // the entries' width-chars
	textPx        = 16 // the labels' size before the stylesheet's
)

// The PasswordEntry peek icons (GtkPasswordEntry show-peek-icon): the
// eye reveals masked text, the crossed eye masks it again.
const (
	peekReveal  = "view-reveal-symbolic"
	peekConceal = "view-conceal-symbolic"
)

// Options configure one prompt.
type Options struct {
	// Font is the face the labels and entries shape with; the
	// stylesheet sets their sizes and weights.
	Font render.Font
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
	// Reveal is the root: the centered card's reveal animation, hidden
	// until the screen shows it (CredentialBox::reveal).
	Reveal *widget.Revealer
	// Username is the visible username entry; nil without
	// WithUsername.
	Username *widget.Entry
	// Entry is the secret entry; Enter submits it.
	Entry *widget.Entry
	// Clock and Date are hidden without ShowClock.
	Clock, Date *widget.Label
	// Error is the error and info line, hidden until it has text.
	Error  *widget.Label
	peeked bool
}

// Build assembles a prompt (CredentialBox::build); onSubmit fires with
// the secret entry's text each time the user presses Enter in it.
func Build(o Options, onSubmit func(string)) *Prompt {
	p := &Prompt{
		Clock: label(o.Font, "lock-clock"),
		Date:  label(o.Font, "lock-date"),
		Entry: widget.NewEntry(o.Font, textPx, 0),
		Error: label(o.Font, "lock-error"),
	}
	p.Clock.SetVisible(o.ShowClock)
	p.Date.SetVisible(o.ShowClock)
	p.Entry.AddClass("lock-entry")
	p.Entry.SetEcho(widget.EchoPassword)
	p.Entry.SetWidthChars(entryChars, 0)
	p.Entry.SetTrailingIcon(peekReveal, textPx, p.togglePeek)
	p.Entry.OnActivate = onSubmit
	p.Error.SetVisible(false)

	center := widget.NewBox(widget.Column, columnSpacing, 0)
	center.AddClass("lock-center")
	center.SetHAlign(widget.AlignCenter)
	center.SetVAlign(widget.AlignCenter)
	center.Append(p.Clock, false).Append(p.Date, false)
	if o.Header != nil {
		center.Append(o.Header, false)
	}
	if o.WithUsername {
		p.Username = widget.NewEntry(o.Font, textPx, 0)
		p.Username.AddClass("lock-user")
		p.Username.SetPlaceholder("Username")
		p.Username.SetWidthChars(entryChars, 0)
		p.Username.OnActivate = func(string) {
			if o.Focus != nil {
				o.Focus(p.Entry)
			}
		}
		center.Append(p.Username, false)
	}
	center.Append(p.Entry, false)
	if o.Extra != nil {
		center.Append(o.Extra, false)
	}
	center.Append(p.Error, false)

	p.Reveal = widget.NewRevealer(center)
	p.Reveal.SetHAlign(widget.AlignCenter)
	p.Reveal.SetVAlign(widget.AlignCenter)
	return p
}

// label is a GtkLabel in the column: it fills the row, its text
// centered (GTK's default xalign 0.5).
func label(font render.Font, class string) *widget.Label {
	l := widget.NewLabel(font, textPx, "", 0)
	l.AddClass(class)
	l.SetAlignment(render.AlignCenter)
	return l
}

// togglePeek is the peek icon's click: the masked text shows, or hides
// again.
func (p *Prompt) togglePeek() {
	p.peeked = !p.peeked
	p.Entry.EchoReveal(p.peeked)
	icon := peekReveal
	if p.peeked {
		icon = peekConceal
	}
	p.Entry.SetTrailingIcon(icon, 0, p.togglePeek)
}

// SetMessage shows text on the error line, or hides it when empty.
func (p *Prompt) SetMessage(text string) {
	p.Error.SetText(text)
	p.Error.SetVisible(text != "")
}

// Window is a lock or greeter surface's root (the GTK window node the
// .lock-window rules select), styled by sheet, holding the layers
// bottom to top.
func Window(sheet *widget.Stylesheet, layers ...widget.Widget) widget.Widget {
	overlay := widget.NewOverlay()
	for _, l := range layers {
		overlay.Append(l)
	}
	root := widget.NewBox(widget.Column, 0, 0)
	root.SetElement("window")
	root.AddClass("background", "lock-window")
	if sheet != nil {
		root.AttachStylesheet(sheet)
	}
	root.Append(overlay, true)
	return root
}

// Scrim is the blank-screen blackout (.lock-scrim), hidden until the
// blank timer shows it.
func Scrim() *widget.Box {
	s := widget.NewBox(widget.Column, 0, 0)
	s.AddClass("lock-scrim")
	s.SetVisible(false)
	return s
}

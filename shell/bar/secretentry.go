package bar

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// Reveal toggle glyphs (helpers.rs's ICON_EYE / ICON_EYE_OFF).
const (
	iconEye    = "ld-eye-symbolic"
	iconEyeOff = "ld-eye-off-symbolic"
)

// secretEntry is a masked Entry with the eye button every secret box in
// the network dropdown carries (attach_reveal_toggle): a mistyped key
// or password is otherwise only fixable by retyping it.
type secretEntry struct {
	*widget.Box
	entry *widget.Entry
	eye   *widget.Icon
}

func newSecretEntry(ctx ModuleContext, font render.Font, px float64) *secretEntry {
	s := &secretEntry{Box: widget.NewBox(widget.Row, 4, 0)}
	s.entry = widget.NewEntry(font, px, ctx.Style.fg)
	s.entry.SetEcho(widget.EchoPassword)
	s.entry.AddClass("network-password-input")
	s.Append(s.entry, true)
	s.eye = widget.NewThemeIcon(iconEyeOff, int(px))
	s.eye.SetTint(mutedFg(ctx.Style.palette))
	s.Append(dropdownButton(s.eye, "network-reveal-toggle", s.toggle), false)
	return s
}

// toggle flips the reveal.
func (s *secretEntry) toggle() {
	on := !s.entry.Revealing()
	s.entry.EchoReveal(on)
	if on {
		s.eye.SetThemeName(iconEye)
	} else {
		s.eye.SetThemeName(iconEyeOff)
	}
}

// reset is reset_reveal_toggle: masked again, and emptied.
func (s *secretEntry) reset() {
	s.entry.SetText("")
	s.entry.EchoReveal(false)
	s.eye.SetThemeName(iconEyeOff)
}

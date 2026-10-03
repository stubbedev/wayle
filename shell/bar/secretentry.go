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

// secretEntry is a masked Entry with the peek icon every secret box in
// the network dropdown carries (attach_reveal_toggle): a mistyped key
// or password is otherwise only fixable by retyping it. The eye is the
// entry's trailing icon — the entry > image node, which the
// stylesheet's fg-subtle rule colors, exactly the Rust peek.
type secretEntry struct {
	*widget.Box
	entry *widget.Entry
	eye   bool
}

func newSecretEntry(_ ModuleContext, font render.Font, px float64) *secretEntry {
	s := &secretEntry{Box: widget.NewBox(widget.Row, 4, 0)}
	s.entry = widget.NewEntry(font, px, 0)
	s.entry.SetEcho(widget.EchoPassword)
	s.entry.AddClass("network-password-input")
	s.entry.SetTrailingIcon(iconEyeOff, px, s.toggle)
	s.Append(s.entry, true)
	return s
}

// toggle flips the reveal.
func (s *secretEntry) toggle() {
	s.eye = !s.eye
	s.entry.EchoReveal(s.eye)
	if s.eye {
		s.entry.SetTrailingIcon(iconEye, 0, s.toggle)
	} else {
		s.entry.SetTrailingIcon(iconEyeOff, 0, s.toggle)
	}
}

// reset is reset_reveal_toggle: masked again, and emptied.
func (s *secretEntry) reset() {
	s.entry.SetText("")
	s.entry.EchoReveal(false)
	s.eye = false
	s.entry.SetTrailingIcon(iconEyeOff, 0, s.toggle)
}

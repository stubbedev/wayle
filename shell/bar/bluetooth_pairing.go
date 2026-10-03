package bar

import (
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/bluetooth"
)

// pairingVariant is PairingVariant: which prompt the card shows. The
// Rust Failed variant is never entered there and is not carried.
type pairingVariant uint8

const (
	variantNone pairingVariant = iota
	variantDisplayPin
	variantRequestPasskey
	variantDisplayPasskey
	variantRequestConfirmation
	variantRequestAuthorization
	variantRequestServiceAuthorization
	variantRequestPinCode
)

// pairingOutputKind is PairingCardOutput.
type pairingOutputKind uint8

const (
	outputCancelled pairingOutputKind = iota
	outputPasskeySubmitted
	outputPasskeyConfirmed
	outputPasskeyRejected
	outputAuthorizationAccepted
	outputAuthorizationRejected
	outputServiceAuthorizationAccepted
	outputServiceAuthorizationRejected
	outputLegacyPinSubmitted
)

// pairingOutput is one answer from the card.
type pairingOutput struct {
	kind    pairingOutputKind
	pin     string
	passkey uint32
}

// passkeyTotal is the passkey's digit count (passkey_total).
const passkeyTotal = 6

// btPairingCard is pairing_card: the prompt for the pending agent
// request, above the device lists.
type btPairingCard struct {
	root    *widget.Box
	variant pairingVariant
	// focus moves the keyboard focus inside the hosting popover (the
	// Rust entries' grab_focus); nil until the popover attaches.
	focus func(widget.Widget)
	// onOutput receives the user's answer; the dropdown clears the
	// card and forwards it to the service.
	onOutput func(pairingOutput)

	icon       *widget.Icon
	deviceName *widget.Label
	deviceType *widget.Label

	// sections holds each variant-scoped widget with the variants it
	// shows under.
	sections []cardSection

	pinCode []*widget.Label
	// pinRow holds the six single-digit passkey boxes
	// (pairing_card's pin_input_row); the row is the key controller
	// (handle_pin_key) so backspace steps to the previous box.
	pinRow         *pinKeyRow
	pinDigits      []*widget.Entry
	progress       *widget.Label
	serviceName    *widget.Label
	legacyPinEntry *widget.Entry

	leftLabel, rightLabel *widget.Label
	right                 *widget.Button
}

type cardSection struct {
	w        widget.Widget
	variants []pairingVariant
}

func newBtPairingCard(ctx ModuleContext) *btPairingCard {
	font, px := dropdownFont(ctx)
	c := &btPairingCard{}
	// Rust's boxes are spacing-0; the card SCSS's margins carry the gaps
	// (pairing_card/mod.rs:47-101).
	col := widget.NewBox(widget.Column, 0, 0)
	col.AddClass("bluetooth-pairing-card")

	// The header: device icon, name and type, the close button.
	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("bluetooth-pairing-header")
	c.icon = widget.NewThemeIcon("ld-bluetooth-symbolic", int(px*1.4))
	header.Append(iconTile(c.icon, "bluetooth-device-icon", "bluetooth-icon"), false)
	info := widget.NewBox(widget.Column, 0, 0)
	info.AddClass("bluetooth-pairing-device-info")
	c.deviceName = widget.NewLabel(font, px, "", 0)
	c.deviceName.SetEllipsize(widget.EllipsizeEnd)
	c.deviceName.AddClass("bluetooth-device-name")
	c.deviceType = widget.NewLabel(font, px*0.9, "", 0)
	c.deviceType.AddClass("bluetooth-device-detail")
	info.Append(c.deviceName, false)
	info.Append(c.deviceType, false)
	header.AppendAligned(info, true, widget.AlignCenter)
	closeIcon := widget.NewThemeIcon("ld-x-symbolic", int(px))
	closeBtn := dropdownButton(closeIcon, "ghost-icon", nil)
	closeBtn.AddClass("bluetooth-pairing-close")
	closeBtn.OnClick = func() { c.emit(pairingOutput{kind: outputCancelled}) }
	header.Append(closeBtn, false)
	col.Append(header, false)

	message := func(key string) *widget.Label {
		l := widget.NewLabel(font, px*0.95, btText(key), 0)
		l.SetWrap(true)
		l.AddClass("bluetooth-pairing-message")
		return l
	}
	codeBlock := func(labelKey string) *widget.Box {
		box := widget.NewBox(widget.Column, 0, 0)
		box.AddClass("bluetooth-pin-display")
		if labelKey != "" {
			l := widget.NewLabel(font, px*0.95, btText(labelKey), 0)
			l.AddClass("bluetooth-pin-label")
			box.Append(l, false)
		}
		code := widget.NewLabel(font, px*1.8, "", 0)
		code.AddClass("bluetooth-pin-code")
		box.Append(code, false)
		c.pinCode = append(c.pinCode, code)
		return box
	}
	add := func(w widget.Widget, variants ...pairingVariant) {
		c.sections = append(c.sections, cardSection{w: w, variants: variants})
		col.Append(w, false)
	}

	displayPin := codeBlock("dropdown-bluetooth-pairing-enter-pin")
	add(displayPin, variantDisplayPin)
	add(message("dropdown-bluetooth-pairing-type-on-device"), variantDisplayPin)
	add(message("dropdown-bluetooth-pairing-allow-pairing"), variantRequestAuthorization)
	add(message("dropdown-bluetooth-pairing-enter-shown-pin"), variantRequestPasskey)

	// The passkey prompt: six single-digit boxes in a centered row
	// (pairing_card/mod.rs:147-190). The entry.bluetooth-pin-digit rules
	// size and ink them; the mono font is the stylesheet's.
	c.pinRow = &pinKeyRow{Box: widget.NewBox(widget.Row, 0, 0), card: c}
	c.pinRow.AddClass("bluetooth-pin-input-row")
	for i := range passkeyTotal {
		digit := widget.NewEntry(font, px*1.4, 0)
		digit.AddClass("bluetooth-pin-digit")
		// set_max_length 1 (the digit filter) and set_max_width_chars 1;
		// a typed digit moves to the next box, as handle_pin_key does.
		digit.SetWidthChars(0, 1)
		digit.OnChanged = func(string) {
			limitEntry(digit, 1, isDigit)(digit.Text())
			if runes := []rune(digit.Text()); len(runes) == 1 && i < passkeyTotal-1 {
				c.focusDigit(i + 1)
			}
		}
		c.pinDigits = append(c.pinDigits, digit)
		c.pinRow.Append(digit, false)
	}
	add(c.pinRow, variantRequestPasskey)

	add(message("dropdown-bluetooth-pairing-confirm-code"), variantRequestConfirmation)
	confirmPin := codeBlock("")
	add(confirmPin, variantRequestConfirmation)

	displayPasskey := codeBlock("dropdown-bluetooth-pairing-enter-pin")
	// The typing-progress dots box under the code
	// (pairing_card/mod.rs:229-230): an empty box the stylesheet spaces.
	dots := widget.NewBox(widget.Row, 0, 0)
	dots.AddClass("bluetooth-progress-dots")
	displayPasskey.Append(dots, false)
	add(displayPasskey, variantDisplayPasskey)
	c.progress = message("dropdown-bluetooth-pairing-entering")
	add(c.progress, variantDisplayPasskey)

	c.serviceName = widget.NewLabel(font, px, "", 0)
	c.serviceName.AddClass("bluetooth-service-name")
	service := widget.NewBox(widget.Column, 0, 0)
	service.AddClass("bluetooth-service-info")
	service.Append(c.serviceName, false)
	add(service, variantRequestServiceAuthorization)
	add(message("dropdown-bluetooth-pairing-service-allow"), variantRequestServiceAuthorization)

	add(message("dropdown-bluetooth-pairing-enter-legacy-pin"), variantRequestPinCode)
	c.legacyPinEntry = widget.NewEntry(font, px*1.2, 0)
	c.legacyPinEntry.AddClass("bluetooth-legacy-pin-input")
	c.legacyPinEntry.SetPlaceholder(btText("dropdown-bluetooth-pairing-pin-placeholder"))
	c.legacyPinEntry.OnChanged = limitEntry(c.legacyPinEntry, 16, func(rune) bool { return true })
	add(c.legacyPinEntry, variantRequestPinCode)
	hint := widget.NewLabel(font, px*0.9, btText("dropdown-bluetooth-pairing-common-pins"), 0)
	hint.AddClass("bluetooth-pin-hint")
	add(hint, variantRequestPinCode)

	// The actions: reject/cancel on the left, the confirm on the right
	// (homogeneous, spacing-0: the shared buttons split the width).
	actions := widget.NewBox(widget.Row, 0, 0)
	actions.AddClass("bluetooth-pairing-actions")
	c.leftLabel = widget.NewLabel(font, px, "", 0)
	left := ghostButton(c.leftLabel)
	left.OnClick = func() { c.emit(c.rejectOutput()) }
	actions.Append(left, true)
	c.rightLabel = widget.NewLabel(font, px, "", 0)
	c.right = dropdownButton(c.rightLabel, "primary", nil)
	c.right.OnClick = func() {
		if out, ok := c.confirmOutput(); ok {
			c.emit(out)
		}
	}
	actions.Append(c.right, true)
	col.Append(actions, false)

	c.root = col
	c.apply()
	return c
}

// ghostButton is the GhostButton template: the `ghost` class's
// stylesheet rules paint it (transparent until hovered, muted ink).
func ghostButton(child widget.Widget) *widget.Button {
	return dropdownButton(child, "ghost", nil)
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// pinKeyRow is the digit row and its key controller (handle_pin_key):
// backspace on an empty box clears and focuses the previous one, on a
// filled box just clears it, and the action never reaches the entry.
type pinKeyRow struct {
	*widget.Box
	card *btPairingCard
}

// InterceptKey implements widget.KeyInterceptor for the focused digit
// box.
func (r *pinKeyRow) InterceptKey(target widget.Widget, a widget.KeyAction, mods widget.Mods) bool {
	if a != widget.KeyBackspace {
		return false
	}
	for i, d := range r.card.pinDigits {
		if target != widget.Widget(d) {
			continue
		}
		if d.Text() == "" && i > 0 {
			r.card.pinDigits[i-1].SetText("")
			r.card.focusDigit(i - 1)
		} else {
			d.SetText("")
		}
		return true
	}
	return false
}

// limitEntry keeps an entry to max runes that pass keep (set_max_length
// plus the Rust digit key filter).
func limitEntry(e *widget.Entry, maxRunes int, keep func(rune) bool) func(string) {
	return func(text string) {
		var b strings.Builder
		n := 0
		for _, r := range text {
			if n == maxRunes {
				break
			}
			if keep(r) {
				b.WriteRune(r)
				n++
			}
		}
		if filtered := b.String(); filtered != text {
			e.SetText(filtered)
		}
	}
}

func (c *btPairingCard) emit(out pairingOutput) {
	if c.onOutput != nil {
		c.onOutput(out)
	}
}

// focusDigit moves the keyboard to digit i (grab_focus), a no-op
// before the popover attaches.
func (c *btPairingCard) focusDigit(i int) {
	if c.focus != nil && i >= 0 && i < len(c.pinDigits) {
		c.focus(c.pinDigits[i])
	}
}

// passkeyText is the six boxes' contents joined: what the confirm
// sends (build_confirm_output's RequestPasskey arm).
func (c *btPairingCard) passkeyText() string {
	var b strings.Builder
	for _, digit := range c.pinDigits {
		b.WriteString(digit.Text())
	}
	return b.String()
}

// set shows req for the device (SetRequest + apply_request).
func (c *btPairingCard) set(req bluetooth.PairingRequest, display deviceDisplay) {
	c.deviceName.SetText(display.name)
	c.deviceType.SetText(btText(display.typeKey))
	c.icon.SetThemeName(display.icon)
	for _, digit := range c.pinDigits {
		digit.SetText("")
	}
	c.legacyPinEntry.SetText("")
	code := ""
	switch r := req.(type) {
	case bluetooth.DisplayPinCode:
		c.variant = variantDisplayPin
		code = r.PinCode
	case bluetooth.RequestPasskey:
		c.variant = variantRequestPasskey
	case bluetooth.DisplayPasskey:
		c.variant = variantDisplayPasskey
		code = formatPasskey(r.Passkey)
		c.progress.SetText(btText("dropdown-bluetooth-pairing-entering", "entered", r.Entered, "total", passkeyTotal))
	case bluetooth.RequestConfirmation:
		c.variant = variantRequestConfirmation
		code = formatPasskey(r.Passkey)
	case bluetooth.RequestAuthorization:
		c.variant = variantRequestAuthorization
	case bluetooth.RequestServiceAuthorization:
		c.variant = variantRequestServiceAuthorization
		c.serviceName.SetText(btText(serviceNameKey(r.UUID)))
	case bluetooth.RequestPinCode:
		c.variant = variantRequestPinCode
	default:
		c.variant = variantNone
	}
	for _, l := range c.pinCode {
		l.SetText(code)
	}
	c.apply()
}

// clear hides the card (PairingCardMsg::Clear).
func (c *btPairingCard) clear() {
	c.variant = variantNone
	c.apply()
}

// apply syncs visibility and the action labels to the variant.
func (c *btPairingCard) apply() {
	c.root.SetVisible(c.variant != variantNone)
	for _, s := range c.sections {
		shown := false
		for _, v := range s.variants {
			if v == c.variant {
				shown = true
			}
		}
		if setter, ok := s.w.(interface{ SetVisible(bool) }); ok {
			setter.SetVisible(shown)
		}
	}
	c.leftLabel.SetText(btText(c.leftActionKey()))
	c.rightLabel.SetText(btText(c.rightActionKey()))
	c.right.SetVisible(c.hasConfirmAction())
}

// leftActionKey is left_action_label.
func (c *btPairingCard) leftActionKey() string {
	switch c.variant {
	case variantRequestConfirmation, variantRequestPasskey:
		return "dropdown-bluetooth-reject"
	case variantRequestAuthorization, variantRequestServiceAuthorization:
		return "dropdown-bluetooth-deny"
	}
	return "dropdown-bluetooth-cancel"
}

// rightActionKey is right_action_label.
func (c *btPairingCard) rightActionKey() string {
	switch c.variant {
	case variantRequestPasskey, variantRequestPinCode:
		return "dropdown-bluetooth-pair"
	case variantRequestAuthorization, variantRequestServiceAuthorization:
		return "dropdown-bluetooth-allow"
	}
	return "dropdown-bluetooth-confirm"
}

// hasConfirmAction is has_confirm_action: the display requests only
// have a cancel.
func (c *btPairingCard) hasConfirmAction() bool {
	switch c.variant {
	case variantDisplayPin, variantDisplayPasskey, variantNone:
		return false
	}
	return true
}

// confirmOutput is build_confirm_output. The six digit boxes answer
// RequestPasskey with the number they spell (the Rust sends it as a
// PIN, which its own provider refuses); an incomplete passkey has
// nothing to send.
func (c *btPairingCard) confirmOutput() (pairingOutput, bool) {
	switch c.variant {
	case variantRequestPasskey:
		passkey, err := strconv.ParseUint(c.passkeyText(), 10, 32)
		if err != nil {
			return pairingOutput{}, false
		}
		return pairingOutput{kind: outputPasskeySubmitted, passkey: uint32(passkey)}, true
	case variantRequestConfirmation:
		return pairingOutput{kind: outputPasskeyConfirmed}, true
	case variantRequestAuthorization:
		return pairingOutput{kind: outputAuthorizationAccepted}, true
	case variantRequestServiceAuthorization:
		return pairingOutput{kind: outputServiceAuthorizationAccepted}, true
	case variantRequestPinCode:
		return pairingOutput{kind: outputLegacyPinSubmitted, pin: c.legacyPinEntry.Text()}, true
	}
	return pairingOutput{}, false
}

// rejectOutput is build_reject_output: the yes/no prompts reject, the
// rest cancel.
func (c *btPairingCard) rejectOutput() pairingOutput {
	switch c.variant {
	case variantRequestConfirmation:
		return pairingOutput{kind: outputPasskeyRejected}
	case variantRequestAuthorization:
		return pairingOutput{kind: outputAuthorizationRejected}
	case variantRequestServiceAuthorization:
		return pairingOutput{kind: outputServiceAuthorizationRejected}
	}
	return pairingOutput{kind: outputCancelled}
}

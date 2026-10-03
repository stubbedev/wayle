package bar

import (
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/bluetooth"
	"github.com/stubbedev/wayle/styling"
)

// btPalette is the token set the bluetooth dropdown paints with.
type btPalette struct {
	fg, muted, subtle, accent, accentSubtle, onAccent render.Color
	elevated, overlay, hover, border                  render.Color
	radius                                            int
}

func newBtPalette(ctx ModuleContext) btPalette {
	palette := styling.Default()
	if ctx.Style != nil && ctx.Style.palette != nil {
		palette = ctx.Style.palette
	}
	token := func(t config.CssToken) render.Color { return tokenColor(palette, t) }
	return btPalette{
		fg:           token(config.TokenFgDefault),
		muted:        token(config.TokenFgMuted),
		subtle:       token(config.TokenFgSubtle),
		accent:       token(config.TokenAccent),
		accentSubtle: token(config.TokenAccentSubtle),
		onAccent:     token(config.TokenFgOnAccent),
		elevated:     token(config.TokenBgElevated),
		overlay:      token(config.TokenBgOverlay),
		hover:        token(config.TokenBgHover),
		border:       token(config.TokenBorderSubtle),
		radius:       styling.RoundingRadiusPx(config.RoundingMd, 1),
	}
}

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
	// onOutput receives the user's answer; the dropdown clears the
	// card and forwards it to the service.
	onOutput func(pairingOutput)

	icon       *widget.Icon
	deviceName *widget.Label
	deviceType *widget.Label

	// sections holds each variant-scoped widget with the variants it
	// shows under.
	sections []cardSection

	pinCode        []*widget.Label
	progress       *widget.Label
	serviceName    *widget.Label
	passkeyEntry   *widget.Entry
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
	col := widget.NewBox(widget.Column, 8, 12)
	col.AddClass("bluetooth-pairing-card")

	// The header: device icon, name and type, the close button.
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("bluetooth-pairing-header")
	c.icon = widget.NewThemeIcon("ld-bluetooth-symbolic", int(px*1.4))
	header.Append(iconTile(c.icon, "bluetooth-device-icon", "bluetooth-icon"), false)
	info := widget.NewBox(widget.Column, 2, 0)
	info.AddClass("bluetooth-pairing-device-info")
	c.deviceName = widget.NewLabel(font, px, "", 0)
	c.deviceName.SetEllipsize(widget.EllipsizeEnd)
	c.deviceName.AddClass("bluetooth-device-name")
	c.deviceType = widget.NewLabel(font, px*0.9, "", 0)
	c.deviceType.AddClass("bluetooth-device-detail")
	info.Append(c.deviceName, false)
	info.Append(c.deviceType, false)
	header.Append(info, true)
	closeIcon := widget.NewThemeIcon("ld-x-symbolic", int(px))
	closeBtn := ghostButton(closeIcon)
	closeBtn.AddClass("ghost-icon", "bluetooth-pairing-close")
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
		box := widget.NewBox(widget.Column, 4, 12)
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

	// One digits-only entry stands in for the Rust six single-digit
	// boxes: gelm has no programmatic focus to step between them.
	c.passkeyEntry = widget.NewEntry(font, px*1.4, 0)
	c.passkeyEntry.AddClass("bluetooth-pin-input-row")
	c.passkeyEntry.OnChanged = limitEntry(c.passkeyEntry, passkeyTotal, isDigit)
	add(c.passkeyEntry, variantRequestPasskey)

	add(message("dropdown-bluetooth-pairing-confirm-code"), variantRequestConfirmation)
	confirmPin := codeBlock("")
	add(confirmPin, variantRequestConfirmation)

	displayPasskey := codeBlock("dropdown-bluetooth-pairing-enter-pin")
	add(displayPasskey, variantDisplayPasskey)
	c.progress = message("dropdown-bluetooth-pairing-entering")
	add(c.progress, variantDisplayPasskey)

	c.serviceName = widget.NewLabel(font, px, "", 0)
	c.serviceName.AddClass("bluetooth-service-name")
	service := widget.NewBox(widget.Column, 0, 12)
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

	// The actions: reject/cancel on the left, the confirm on the right.
	actions := widget.NewBox(widget.Row, 8, 0)
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

// set shows req for the device (SetRequest + apply_request).
func (c *btPairingCard) set(req bluetooth.PairingRequest, display deviceDisplay) {
	c.deviceName.SetText(display.name)
	c.deviceType.SetText(btText(display.typeKey))
	c.icon.SetThemeName(display.icon)
	c.passkeyEntry.SetText("")
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

// confirmOutput is build_confirm_output. The passkey entry answers
// RequestPasskey with the number (the Rust sends it as a PIN, which
// its own provider refuses); an empty entry has nothing to send.
func (c *btPairingCard) confirmOutput() (pairingOutput, bool) {
	switch c.variant {
	case variantRequestPasskey:
		passkey, err := strconv.ParseUint(c.passkeyEntry.Text(), 10, 32)
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

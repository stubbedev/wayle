package bar

import (
	"context"
	"errors"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/portal"
	"github.com/stubbedev/wayle/service/network"
)

// This file is the network dropdown's VPNs: the section listing NM's
// VPN profiles (dropdowns/network/vpn_connections), each row toggling
// its tunnel and opening it in the editor, and the editor itself
// (vpn_form), the dropdown's second page. The form is built from the
// picked kind's fields rather than drawn per VPN type, so a plugin
// wayle has never heard of still gets the free-form key = value
// editor and lands in NetworkManager the same way.

// vpnControl is the VPN profiles the dropdown drives
// (*network.VPNService).
type vpnControl interface {
	Entries() []network.VPN
	Toggle(ctx context.Context, uuid string) error
	Add(ctx context.Context, kind, name string, values map[string]string) error
	Update(ctx context.Context, uuid, kind, name string, values map[string]string) error
	Remove(ctx context.Context, uuid string) error
	SettingsOf(ctx context.Context, uuid string) (network.ConnectionDict, error)
}

// fileOpener is the portal's file chooser (portal.OpenFile on the
// session bus).
type fileOpener func(ctx context.Context, title string, filters ...portal.FileFilter) (string, error)

// openFileOnSessionBus is the live fileOpener.
func openFileOnSessionBus(ctx context.Context, title string, filters ...portal.FileFilter) (string, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return "", err
	}
	return portal.OpenFile(ctx, conn, title, filters...)
}

// vpnFieldsMaxHeight is FIELDS_MAX_HEIGHT: WireGuard's nine fields run
// past the popover, so the form scrolls past this instead.
const vpnFieldsMaxHeight = 260

// The WireGuard field holding this end's private key, and the glyph of
// the button that generates one.
const (
	wireGuardPrivateKey = "private-key"
	iconGenerateKey     = "ld-refresh-cw-symbolic"
)

// vpnStateIcon is a row's glyph (vpn_item.rs's state_icon): the
// dropdown's own, not the bar icon the user themes. A failed attempt
// reads as not connected; the reason is the row's second line.
func vpnStateIcon(state network.VPNState) string {
	switch state {
	case network.VPNConnected:
		return "ld-lock-symbolic"
	case network.VPNConnecting:
		return "ld-refresh-cw-symbolic"
	}
	return "ld-unplug-symbolic"
}

// vpnStateLabel is state_label.
func vpnStateLabel(state network.VPNState) string {
	switch state {
	case network.VPNConnected:
		return i18n.T("dropdown-network-vpn-connected")
	case network.VPNConnecting:
		return i18n.T("dropdown-network-vpn-connecting")
	case network.VPNFailed:
		return i18n.T("dropdown-network-vpn-failed")
	}
	return i18n.T("dropdown-network-vpn-disconnected")
}

// vpnRowCaption is the row's second line: the failure reason as a
// sentence when there is one, the state otherwise.
func vpnRowCaption(row network.VPN) string {
	if row.Detail != "" {
		return sentence(row.Detail)
	}
	return vpnStateLabel(row.State)
}

// sentence capitalises a reason for its own line (vpn_item.rs): wayle's
// errors are written lowercase to read mid-sentence in a log; only the
// first letter is touched.
func sentence(reason string) string {
	reason = strings.TrimSpace(reason)
	first, size := utf8.DecodeRuneInString(reason)
	if size == 0 {
		return ""
	}
	return string(unicode.ToUpper(first)) + reason[size:]
}

// vpnSlug is slug: a plugin key as a Fluent message-id fragment.
// vpnc's keys are "IPSec gateway" and "Xauth password", which verbatim
// build an id no Fluent file can declare.
func vpnSlug(key string) string {
	var b strings.Builder
	for _, c := range key {
		switch {
		case c < utf8.RuneSelf && (unicode.IsLetter(c) || unicode.IsDigit(c)):
			b.WriteRune(unicode.ToLower(c))
		case !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// vpnTranslated is the shell's string for id, or fallback when wayle
// ships none: what keeps an unknown plugin's vocabulary usable.
func vpnTranslated(id, fallback string) string {
	if i18n.Shell().Has(id) {
		return i18n.T(id)
	}
	return fallback
}

// vpnFieldName is label_for.
func vpnFieldName(f network.VPNField) string {
	return vpnTranslated("dropdown-network-vpn-field-"+vpnSlug(f.Key), f.Label)
}

// vpnFieldLabel is field_label: the name, required ones marked.
func vpnFieldLabel(f network.VPNField) string {
	if f.Required {
		return vpnFieldName(f) + " *"
	}
	return vpnFieldName(f)
}

// vpnSectionHeading is section_heading's text.
func vpnSectionHeading(section string) string {
	return vpnTranslated("dropdown-network-vpn-section-"+section, section)
}

// vpnChoiceLabel is choice_label: a choice wayle cannot sign into says
// so before it is picked.
func vpnChoiceLabel(c network.VPNChoice) string {
	if c.NativeSignIn {
		return c.Label
	}
	return c.Label + " — " + i18n.T("dropdown-network-vpn-no-native-sign-in")
}

// parseRaw is parse_raw: one key = value per line, # comments, blank
// lines and lines without a key ignored.
func parseRaw(text string) map[string]string {
	out := map[string]string{}
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if key = strings.TrimSpace(key); ok && key != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

// vpnTyped is one typed box's key and value.
type vpnTyped struct{ key, value string }

// mergeVPNValues is merge: the raw editor's keys with the typed fields
// over them. A typed field wins (it is the one the user sees), an
// emptied box clears its key rather than reviving the raw one: no blank
// value reaches NetworkManager.
func mergeVPNValues(raw map[string]string, typed []vpnTyped) map[string]string {
	values := maps.Clone(raw)
	if values == nil {
		values = map[string]string{}
	}
	for _, t := range typed {
		values[t.key] = t.value
	}
	maps.DeleteFunc(values, func(_, v string) bool { return strings.TrimSpace(v) == "" })
	return values
}

// vpnLeftovers is leftovers: the saved values the kind's form has no
// box for, kept in the raw editor so an edit never drops them.
func vpnLeftovers(values map[string]string, kind network.VPNKind) map[string]string {
	out := map[string]string{}
	for k, v := range values {
		if !slices.ContainsFunc(kind.Fields, func(f network.VPNField) bool { return f.Key == k }) {
			out[k] = v
		}
	}
	return out
}

// renderRaw is render_raw: the values as the raw editor shows them,
// sorted.
func renderRaw(values map[string]string) string {
	lines := make([]string, 0, len(values))
	for k, v := range values {
		lines = append(lines, k+" = "+v)
	}
	slices.Sort(lines)
	return strings.Join(lines, "\n")
}

// vpnMalformedMessage is malformed_message: what the box wanted.
func vpnMalformedMessage(f network.VPNField) string {
	id := map[network.VPNFormat]string{
		network.FormatText:     "dropdown-network-vpn-invalid-text",
		network.FormatHost:     "dropdown-network-vpn-invalid-host",
		network.FormatHostPort: "dropdown-network-vpn-invalid-host-port",
		network.FormatIPList:   "dropdown-network-vpn-invalid-ip-list",
		network.FormatCIDRList: "dropdown-network-vpn-invalid-cidr-list",
		network.FormatKey:      "dropdown-network-vpn-invalid-key",
		network.FormatNumber:   "dropdown-network-vpn-invalid-number",
	}[f.Format]
	if id == "" {
		id = "dropdown-network-vpn-invalid-text"
	}
	return i18n.T(id, i18n.Str("field", vpnFieldName(f)))
}

// vpnMalformed is malformed: every field holding a value not in its
// format.
func vpnMalformed(kind network.VPNKind, values map[string]string) []network.VPNField {
	var out []network.VPNField
	for _, f := range kind.Fields {
		if v, ok := values[f.Key]; ok && !f.Format.Accepts(v) {
			out = append(out, f)
		}
	}
	return out
}

// vpnMissingRequired is missing_required: every required field left
// empty, all at once, in drawing order.
func vpnMissingRequired(kind network.VPNKind, values map[string]string) []network.VPNField {
	var out []network.VPNField
	for _, f := range kind.Fields {
		if f.Required && values[f.Key] == "" {
			out = append(out, f)
		}
	}
	return out
}

// vpnRawHint is raw_hint: for a typed kind the keys it covers, for an
// untyped one the plugin's service its documentation is indexed by.
func vpnRawHint(kind network.VPNKind, ok bool) string {
	switch {
	case !ok:
		return i18n.T("dropdown-network-vpn-raw-hint")
	case !kind.IsTyped():
		return i18n.T("dropdown-network-vpn-raw-hint-unknown", i18n.Str("service", kind.ID))
	}
	keys := make([]string, len(kind.Fields))
	for i, f := range kind.Fields {
		keys[i] = f.Key
	}
	return i18n.T("dropdown-network-vpn-raw-hint-typed", i18n.Str("covered", strings.Join(keys, ", ")))
}

// publicKeyReadout is attach_key_generation's readout: the public key
// for the private key typed, and nothing for a half-typed or mistyped
// one (an old public key would hand the other end a key that opens
// nothing).
func publicKeyReadout(private string) string {
	if public, ok := network.PublicKeyFor(private); ok {
		return i18n.T("dropdown-network-vpn-public-key", i18n.Str("key", public))
	}
	return i18n.T("dropdown-network-vpn-public-key-empty")
}

// kindsWith is the kinds the form offers, plus the one a saved profile
// names when this machine no longer has its plugin: the profile still
// opens, in the raw editor, and saving keeps its type instead of
// rewriting it as the first kind.
func kindsWith(kinds []network.VPNKind, kind string) []network.VPNKind {
	if kind == "" || slices.ContainsFunc(kinds, func(k network.VPNKind) bool { return k.ID == kind }) {
		return kinds
	}
	return append(slices.Clone(kinds), network.VPNKind{ID: kind, Label: kind})
}

// readWgQuick reads and parses a wg-quick file.
func readWgQuick(path string) (map[string]string, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // the file the user picked
	if err != nil {
		return nil, false
	}
	return network.ParseWgQuick(string(data), filepath.Base(path))
}

// netVPNs is VpnConnections: the VPN label, one row per profile, and
// the "Add VPN" row that gives a machine with none its first.
type netVPNs struct {
	*widget.Box
	v    *networkView
	rows *widget.Box
	add  *widget.Box
}

func newNetVPNs(v *networkView) *netVPNs {
	s := &netVPNs{v: v, Box: widget.NewBox(widget.Column, 0, 0)}
	s.Append(v.sectionLabel(i18n.T("dropdown-network-vpn")), false)
	card := widget.NewBox(widget.Column, 0, 0)
	card.AddClass("card", "network-list")
	s.rows = widget.NewBox(widget.Column, 0, 0)
	card.Append(s.rows, false)
	// The add row is a plain box the whole of which clicks
	// (vpn_connections/mod.rs:75-107), not a button.
	s.add = widget.NewBox(widget.Row, 0, 0)
	s.add.AddClass("network-item", "vpn-item", "vpn-add")
	s.add.SetCursorName("pointer")
	s.add.SetOnClickWithin(v.vpnAdd)
	plus := widget.NewThemeIcon("ld-plus-symbolic", int(v.px*1.1))
	plus.AddClass("network-item-signal")
	s.add.AppendAligned(plus, false, widget.AlignCenter)
	label := widget.NewLabel(v.font, v.px, i18n.T("dropdown-network-vpn-add"), 0)
	label.AddClass("network-item-name")
	s.add.Append(label, true)
	card.Append(s.add, false)
	s.Append(card, false)
	s.SetVisible(v.vpn != nil)
	return s
}

// apply rebuilds the rows from one read of the profiles.
func (s *netVPNs) apply(rows []network.VPN) {
	s.rows.Clear()
	for _, row := range rows {
		s.rows.Append(s.row(row), false)
	}
	if len(rows) > 0 {
		s.add.AddClass("separated")
	} else {
		s.add.RemoveClass("separated")
	}
}

// row is VpnItem: a plain box carrying the whole row's click
// (GestureClick on the root, vpn_item.rs:190-197) and the pointer
// cursor; the edit button, being a button, keeps its own.
func (s *netVPNs) row(r network.VPN) widget.Widget {
	v := s.v
	uuid := r.UUID
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("network-item", "vpn-item")
	row.SetCursorName("pointer")
	row.SetOnClickWithin(func() { v.vpnToggle(uuid) })
	icon := widget.NewThemeIcon(vpnStateIcon(r.State), int(v.px*1.1))
	icon.AddClass("network-item-signal")
	switch r.State {
	case network.VPNConnected:
		icon.AddClass("connected")
	case network.VPNConnecting:
		icon.AddClass("connecting")
	}
	row.AppendAligned(icon, false, widget.AlignCenter)
	info := widget.NewBox(widget.Column, 0, 0)
	info.AddClass("network-item-info")
	name := widget.NewLabel(v.font, v.px, r.Name, 0)
	name.AddClass("network-item-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	name.SetMaxWidthChars(24)
	info.Append(name, false)
	// A gateway's reason is a sentence: wrapped at the row's width, three
	// lines at most, the tooltip holding the rest.
	caption := widget.NewLabel(v.font, v.px*0.8, vpnRowCaption(r), 0)
	caption.AddClass("network-item-security")
	caption.SetWrap(true)
	caption.SetMaxWidthChars(24)
	caption.SetMaxLines(3)
	if r.Detail != "" {
		caption.AddClass("failed")
		caption.SetTooltip(r.Detail)
	}
	info.Append(caption, false)
	row.Append(info, true)
	gear := widget.NewThemeIcon("ld-settings-symbolic", int(v.px))
	edit := dropdownButton(gear, "network-vpn-edit", func() { v.vpnEdit(uuid) })
	edit.AddClass("ghost-icon")
	edit.SetTooltip(i18n.T("dropdown-network-vpn-edit"))
	row.AppendAligned(edit, false, widget.AlignCenter)
	return row
}

// vpnEntry is a typed box and the key it fills.
type vpnEntry struct {
	key   string
	entry *widget.Entry
}

// vpnPicker is a fixed-choice field and the values behind its labels.
type vpnPicker struct {
	key    string
	picker *widget.Dropdown
	values []string
}

// netVPNForm is VpnForm: the editor page. The header goes back and, for
// WireGuard, imports a wg-quick file; the scrolled body holds the name,
// the type picker (creating only: another type is another profile),
// the kind's fields, and the raw editor; the footer holds the error and
// Delete (inline-confirmed), Cancel, Save.
type netVPNForm struct {
	*widget.Box
	v          *networkView
	kinds      []network.VPNKind
	selected   int
	editing    string // the profile's UUID; "" creates one
	title      *widget.Label
	importBtn  *widget.Button
	name       *widget.Entry
	kindLabel  *widget.Label
	kindSlot   *widget.Box
	kindPicker *widget.Dropdown
	fields     *widget.Box
	entries    []vpnEntry
	pickers    []vpnPicker
	advanced   *widget.Button
	advLabel   *widget.Label
	rawHint    *widget.Label
	raw        *widget.TextArea
	rawTyped   bool // the kind has a typed form; the raw editor is extra
	rawOpen    bool
	errLabel   *widget.Label
	actions    *widget.Box
	deleteBtn  *widget.Button
	confirm    *widget.Box
	confirmMsg *widget.Label
}

func newNetVPNForm(v *networkView) *netVPNForm {
	f := &netVPNForm{v: v, Box: widget.NewBox(widget.Column, 0, 0)}
	f.AddClass("card", "network-password-card", "network-vpn-form")

	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("network-password-header")
	back := widget.NewThemeIcon("ld-arrow-left-symbolic", int(v.px))
	backBtn := dropdownButton(back, "network-vpn-back", f.cancel)
	backBtn.AddClass("ghost-icon")
	backBtn.SetTooltip(i18n.T("dropdown-network-vpn-back"))
	header.Append(backBtn, false)
	// The header title is always "New VPN" (vpn_form/mod.rs:341-346):
	// creating and editing share it.
	f.title = widget.NewLabel(v.font, v.px, i18n.T("dropdown-network-vpn-new"), 0)
	f.title.AddClass("network-password-name")
	header.Append(f.title, true)
	folder := widget.NewThemeIcon("ld-folder-open-symbolic", int(v.px))
	f.importBtn = dropdownButton(folder, "network-vpn-import", f.importFile)
	f.importBtn.AddClass("ghost-icon")
	f.importBtn.SetTooltip(i18n.T("dropdown-network-vpn-import"))
	header.Append(f.importBtn, false)
	f.Append(header, false)

	body := widget.NewBox(widget.Column, 0, 0)
	body.Append(f.fieldLabel(i18n.T("dropdown-network-vpn-name")), false)
	f.name = widget.NewEntry(v.font, v.px, 0)
	f.name.AddClass("network-password-input")
	body.Append(f.name, false)
	f.kindLabel = f.fieldLabel(i18n.T("dropdown-network-vpn-type"))
	body.Append(f.kindLabel, false)
	f.kindSlot = widget.NewBox(widget.Column, 0, 0)
	body.Append(f.kindSlot, false)
	f.fields = widget.NewBox(widget.Column, 0, 0)
	f.fields.AddClass("network-secret-fields")
	body.Append(f.fields, false)
	f.advLabel = widget.NewLabel(v.font, v.px*0.85, "", 0)
	f.advLabel.AddClass("network-secret-label")
	f.advanced = dropdownButton(f.advLabel, "network-vpn-advanced", f.toggleAdvanced)
	f.advanced.AddClass("ghost")
	body.Append(f.advanced, false)
	f.rawHint = f.fieldLabel("")
	f.rawHint.SetWrap(true)
	body.Append(f.rawHint, false)
	f.raw = widget.NewTextArea(monoFont(v.ctx, v.font, v.px), v.px*0.9, 0)
	f.raw.AddClass("network-vpn-raw")
	body.Append(f.raw, false)
	scroll := dropdownScroll(body, "network-vpn-form-scroll")
	scroll.SetMaxContentHeight(vpnFieldsMaxHeight)
	f.Append(scroll, true)

	f.errLabel = v.errorLabel()
	f.Append(f.errLabel, false)

	f.actions = widget.NewBox(widget.Row, 0, 0)
	f.actions.AddClass("network-password-actions")
	f.actions.Append(widget.NewSpacer(0, 0), true)
	f.deleteBtn = v.ghostText(i18n.T("dropdown-network-vpn-delete"), "network-vpn-delete", f.askDelete)
	f.actions.Append(f.deleteBtn, false)
	f.actions.Append(v.ghostText(i18n.T("dropdown-network-cancel"), "network-password-cancel", f.cancel), false)
	f.actions.Append(v.primaryText(i18n.T("dropdown-network-vpn-save"), "network-password-connect", f.save), false)
	f.Append(f.actions, false)

	// A profile is recoverable from nowhere else (a WireGuard private key
	// lives only in NM), so Delete asks first, in place of the buttons.
	f.confirm = widget.NewBox(widget.Column, 0, 0)
	f.confirm.AddClass("network-vpn-delete-confirm")
	f.confirmMsg = widget.NewLabel(v.font, v.px*0.9, "", 0)
	f.confirmMsg.SetWrap(true)
	f.confirmMsg.AddClass("network-password-name")
	f.confirm.Append(f.confirmMsg, false)
	detail := widget.NewLabel(v.font, v.px*0.8, i18n.T("dropdown-network-vpn-delete-confirm-detail"), 0)
	detail.SetWrap(true)
	detail.AddClass("network-secret-message")
	f.confirm.Append(detail, false)
	buttons := widget.NewBox(widget.Row, 0, 0)
	buttons.Append(widget.NewSpacer(0, 0), true)
	buttons.Append(v.ghostText(i18n.T("dropdown-network-cancel"), "network-vpn-delete-dismiss", f.dismissDelete), false)
	really := v.ghostText(i18n.T("dropdown-network-vpn-delete"), "network-vpn-delete-confirmed", f.confirmDelete)
	really.RemoveClass("ghost")
	really.AddClass("danger")
	buttons.Append(really, false)
	f.confirm.Append(buttons, false)
	f.Append(f.confirm, false)
	f.setConfirming(false)
	return f
}

// monoFont is the configured monospace face for the raw editor, the
// dropdown's own when it cannot be loaded.
func monoFont(ctx ModuleContext, fallback render.Font, px float64) render.Font {
	if ctx.Config == nil || ctx.Config.General.FontMono == "" {
		return fallback
	}
	face, err := app.Font(ctx.Config.General.FontMono, px)
	if err != nil {
		return fallback
	}
	return app.FontFallback(face)
}

func (f *netVPNForm) fieldLabel(text string) *widget.Label {
	l := widget.NewLabel(f.v.font, f.v.px*0.85, text, 0)
	l.AddClass("network-secret-label")
	return l
}

// kind is the picked kind.
func (f *netVPNForm) kind() (network.VPNKind, bool) {
	if f.selected < 0 || f.selected >= len(f.kinds) {
		return network.VPNKind{}, false
	}
	return f.kinds[f.selected], true
}

// open resets the form onto kinds, picking kind (the first when "").
func (f *netVPNForm) open(uuid, name, kind string, values map[string]string) {
	var kinds []network.VPNKind
	if f.v.vpnKinds != nil {
		kinds = f.v.vpnKinds()
	}
	f.kinds = kindsWith(kinds, kind)
	f.selected = max(0, slices.IndexFunc(f.kinds, func(k network.VPNKind) bool { return k.ID == kind }))
	f.editing = uuid
	f.setConfirming(false)
	f.setError("")
	f.name.SetText(name)
	labels := make([]string, len(f.kinds))
	for i, k := range f.kinds {
		labels[i] = k.Label
	}
	f.kindPicker = widget.NewDropdown(f.v.font, f.v.px, labels, f.selected)
	f.kindPicker.AddClass("network-vpn-kind")
	f.kindPicker.OnSelect = f.kindSelected
	f.kindSlot.Clear()
	f.kindSlot.Append(f.kindPicker, false)
	creating := uuid == ""
	f.kindLabel.SetVisible(creating)
	f.kindSlot.SetVisible(creating)
	f.deleteBtn.SetVisible(!creating)
	f.rebuild(values)
}

// showNew is VpnFormInput::ShowNew: empty, the name box focused.
func (f *netVPNForm) showNew() {
	f.open("", "", "", nil)
	f.v.focus(f.name)
}

// showEdit is VpnFormInput::ShowEdit: prefilled from what is stored.
func (f *netVPNForm) showEdit(uuid, name, kind string, values map[string]string) {
	f.open(uuid, name, kind, values)
}

// kindSelected is KindSelected: another type, other fields.
func (f *netVPNForm) kindSelected(i int) {
	if i < 0 || i >= len(f.kinds) || i == f.selected {
		return
	}
	f.selected = i
	f.setError("")
	f.rebuild(nil)
}

// rebuild redraws the fields for the picked kind, prefilled from
// values. A kind without a typed form is the raw editor, open; a typed
// one keeps it behind the advanced toggle, holding what the form has no
// box for, and opened when that is anything.
func (f *netVPNForm) rebuild(values map[string]string) {
	f.entries, f.pickers = nil, nil
	f.fields.Clear()
	kind, ok := f.kind()
	f.importBtn.SetVisible(ok && kind.ID == network.WireGuard)
	f.rawTyped = ok && kind.IsTyped()
	if !f.rawTyped {
		f.raw.SetText(renderRaw(values))
		f.rawOpen = true
		f.syncRaw(kind, ok)
		return
	}
	extra := vpnLeftovers(values, kind)
	f.raw.SetText(renderRaw(extra))
	f.rawOpen = len(extra) > 0
	section := ""
	for _, field := range kind.Fields {
		if field.Section != "" && field.Section != section {
			heading := widget.NewLabel(f.v.font, f.v.px*0.9, vpnSectionHeading(field.Section), 0)
			heading.AddClass("network-vpn-section")
			f.fields.Append(heading, false)
			section = field.Section
		}
		label := f.fieldLabel(vpnFieldLabel(field))
		if field.Required {
			label.AddClass("required")
		}
		f.fields.Append(label, false)
		current, has := values[field.Key]
		if len(field.Choices) > 0 {
			f.fields.Append(f.picker(field, current, has), false)
			continue
		}
		f.entry(kind, field, current)
	}
	f.syncRaw(kind, ok)
}

// picker draws a fixed-choice field. A stored value this build does not
// offer keeps the first choice rather than rewriting itself unseen.
func (f *netVPNForm) picker(field network.VPNField, current string, has bool) *widget.Dropdown {
	labels := make([]string, len(field.Choices))
	values := make([]string, len(field.Choices))
	for i, c := range field.Choices {
		labels[i], values[i] = vpnChoiceLabel(c), c.Value
	}
	selected := 0
	if i := slices.Index(values, current); has && i >= 0 {
		selected = i
	}
	p := widget.NewDropdown(f.v.font, f.v.px, labels, selected)
	p.AddClass("network-vpn-kind")
	f.pickers = append(f.pickers, vpnPicker{key: field.Key, picker: p, values: values})
	return p
}

// entry draws a typed box: masked with the reveal toggle when secret,
// and for WireGuard's private key with the generate button and the
// public key it derives.
func (f *netVPNForm) entry(kind network.VPNKind, field network.VPNField, current string) {
	var entry *widget.Entry
	var input *widget.Box
	if field.Secret {
		s := newSecretEntry(f.v.ctx, f.v.font, f.v.px)
		entry, input = s.entry, s.Box
	} else {
		entry = widget.NewEntry(f.v.font, f.v.px, 0)
		entry.AddClass("network-password-input")
		input = widget.NewBox(widget.Row, 0, 0)
		input.Append(entry, true)
	}
	entry.SetPlaceholder(field.Placeholder)
	entry.SetText(current)
	entry.OnActivate = func(string) { f.save() }
	f.fields.Append(input, false)
	var readout *widget.Label
	// WireGuard's key generation is the entry's primary icon in Rust
	// (vpn_form/mod.rs:147-160); gelm entries have one icon slot and the
	// reveal toggle owns it, so the generator stands beside the entry —
	// reported as a gap.
	if kind.ID == network.WireGuard && field.Key == wireGuardPrivateKey {
		key := widget.NewThemeIcon(iconGenerateKey, int(f.v.px))
		generate := dropdownButton(key, "network-vpn-generate", func() {
			entry.SetText(network.GenerateKeyPair().Private)
		})
		generate.AddClass("ghost-icon")
		generate.SetTooltip(i18n.T("dropdown-network-vpn-generate-key"))
		input.Append(generate, false)
		readout = f.fieldLabel(publicKeyReadout(current))
		readout.AddClass("network-vpn-public-key")
		readout.SetWrap(true)
		f.fields.Append(readout, false)
	}
	// A box stops being wrong the moment it is edited.
	entry.OnChanged = func(text string) {
		entry.RemoveClass("error")
		if readout != nil {
			readout.SetText(publicKeyReadout(text))
		}
	}
	f.entries = append(f.entries, vpnEntry{key: field.Key, entry: entry})
}

// syncRaw shows the raw editor's parts for the current state.
func (f *netVPNForm) syncRaw(kind network.VPNKind, ok bool) {
	f.advanced.SetVisible(f.rawTyped)
	if f.rawOpen {
		f.advLabel.SetText(i18n.T("dropdown-network-vpn-advanced-hide"))
	} else {
		f.advLabel.SetText(i18n.T("dropdown-network-vpn-advanced-show"))
	}
	f.rawHint.SetText(vpnRawHint(kind, ok))
	f.rawHint.SetVisible(f.rawOpen)
	f.raw.SetVisible(f.rawOpen)
}

func (f *netVPNForm) toggleAdvanced() {
	f.rawOpen = !f.rawOpen
	kind, ok := f.kind()
	f.syncRaw(kind, ok)
}

// values is both editors: the raw keys with the typed boxes and the
// pickers over them.
func (f *netVPNForm) values() map[string]string {
	typed := make([]vpnTyped, 0, len(f.entries)+len(f.pickers))
	for _, e := range f.entries {
		typed = append(typed, vpnTyped{e.key, e.entry.Text()})
	}
	for _, p := range f.pickers {
		if i := p.picker.Selected(); i >= 0 && i < len(p.values) {
			typed = append(typed, vpnTyped{p.key, p.values[i]})
		}
	}
	return mergeVPNValues(parseRaw(f.raw.Text()), typed)
}

// save validates, marks every refused box, and hands a valid profile to
// the view, which writes it off the loop.
func (f *netVPNForm) save() {
	kind, ok := f.kind()
	if !ok {
		return
	}
	name := f.name.Text()
	if strings.TrimSpace(name) == "" {
		f.setError(i18n.T("dropdown-network-vpn-name-required"))
		return
	}
	values := f.values()
	missing := vpnMissingRequired(kind, values)
	malformed := vpnMalformed(kind, values)
	f.markBad(append(missing, malformed...))
	switch {
	case len(missing) > 0:
		f.setError(i18n.T("dropdown-network-vpn-field-required", i18n.Str("field", vpnFieldName(missing[0]))))
		return
	case len(malformed) > 0:
		f.setError(vpnMalformedMessage(malformed[0]))
		return
	}
	f.setError("")
	f.v.vpnSave(f.editing, kind.ID, name, values)
}

// markBad puts the error mark on the refused boxes and takes it off the
// rest.
func (f *netVPNForm) markBad(bad []network.VPNField) {
	for _, e := range f.entries {
		if slices.ContainsFunc(bad, func(b network.VPNField) bool { return b.Key == e.key }) {
			e.entry.AddClass("error")
		} else {
			e.entry.RemoveClass("error")
		}
	}
}

// fail is VpnFormInput::Failed: NM refused; the form stays, values
// intact, so the user can correct what it refused.
func (f *netVPNForm) fail(reason string) { f.setError(reason) }

func (f *netVPNForm) setError(reason string) {
	f.errLabel.SetText(reason)
	f.errLabel.SetVisible(reason != "")
}

func (f *netVPNForm) cancel() {
	f.setConfirming(false)
	f.v.vpnCloseForm()
}

func (f *netVPNForm) setConfirming(on bool) {
	f.confirm.SetVisible(on)
	f.actions.SetVisible(!on)
}

func (f *netVPNForm) confirming() bool { return f.confirm.Visible() }

// askDelete is DeleteClicked: nothing goes until it is confirmed.
func (f *netVPNForm) askDelete() {
	if f.editing == "" {
		return
	}
	f.confirmMsg.SetText(i18n.T("dropdown-network-vpn-delete-confirm", i18n.Str("name", f.name.Text())))
	f.setConfirming(true)
}

func (f *netVPNForm) dismissDelete() { f.setConfirming(false) }

func (f *netVPNForm) confirmDelete() {
	if !f.confirming() || f.editing == "" {
		return
	}
	f.setConfirming(false)
	f.v.vpnDelete(f.editing)
}

// importFile is ImportClicked: a wg-quick file through the portal's
// chooser, its outcome handled back on the loop.
func (f *netVPNForm) importFile() {
	open := f.v.openFile
	if open == nil {
		return
	}
	life := f.v.life
	go func() {
		path, err := open(life, i18n.T("dropdown-network-vpn-import"),
			portal.FileFilter{Name: i18n.T("dropdown-network-vpn-import-filter"), Patterns: []string{"*.conf"}})
		f.v.ctx.Invoke(func() { f.importDone(path, err) })
	}()
}

// importDone reads the picked file here rather than off the loop, as
// the Rust form does: a wg-quick file is a few hundred bytes on local
// disk. Dismissing the chooser is not a failure, and saying so would
// be noise.
func (f *netVPNForm) importDone(path string, err error) {
	switch {
	case errors.Is(err, portal.ErrCancelled):
		return
	case err != nil:
		log.Printf("network: VPN import: %v", err)
		return
	}
	if values, ok := readWgQuick(path); ok {
		f.imported(values["interface"], values)
	} else {
		f.setError(i18n.T("dropdown-network-vpn-import-failed"))
	}
}

// imported is Imported: the file's values, and its interface as the
// name unless one is typed already.
func (f *netVPNForm) imported(name string, values map[string]string) {
	f.setError("")
	if strings.TrimSpace(f.name.Text()) == "" {
		f.name.SetText(name)
	}
	f.rebuild(values)
}

// The page names in the dropdown's body stack.
const (
	netPageBrowse = "browse"
	netPageEdit   = "edit"
)

// vpnToggle connects a VPN that is down and disconnects one that is up;
// the rows follow NM's state on their own.
func (v *networkView) vpnToggle(uuid string) {
	vpn := v.vpn
	if vpn == nil {
		return
	}
	go func() {
		if err := vpn.Toggle(context.Background(), uuid); err != nil {
			log.Printf("network: VPN toggle: %v", err)
		}
	}()
}

// vpnAdd opens the editor empty.
func (v *networkView) vpnAdd() {
	v.vpnForm.showNew()
	v.body.Show(netPageEdit)
}

// vpnEdit reads the saved profile (not what the row shows) and opens
// the editor on it. A profile that cannot be read stays closed: an
// editor open on stale values could save them over it. (Rust logs and
// keeps the list, as here.)
func (v *networkView) vpnEdit(uuid string) {
	vpn := v.vpn
	if vpn == nil {
		return
	}
	name := ""
	if i := slices.IndexFunc(v.cur.vpns, func(r network.VPN) bool { return r.UUID == uuid }); i >= 0 {
		name = v.cur.vpns[i].Name
	}
	go func() {
		dict, err := vpn.SettingsOf(v.life, uuid)
		v.ctx.Invoke(func() {
			if err != nil {
				log.Printf("network: cannot read VPN profile %s: %v", uuid, err)
				return
			}
			v.vpnForm.showEdit(uuid, name, network.KindOf(dict), network.ReadProfileValues(dict))
			v.body.Show(netPageEdit)
		})
	}()
}

// vpnSave writes the form's profile: created without a UUID, rewritten
// in place with one. The page goes back at once; a refusal brings the
// form back with the reason.
func (v *networkView) vpnSave(uuid, kind, name string, values map[string]string) {
	vpn := v.vpn
	if vpn == nil {
		return
	}
	v.body.Show(netPageBrowse)
	go func() {
		var err error
		if uuid == "" {
			err = vpn.Add(context.Background(), kind, name, values)
		} else {
			err = vpn.Update(context.Background(), uuid, kind, name, values)
		}
		v.vpnWritten(err, "cannot save VPN profile")
	}()
}

// vpnDelete removes a profile.
func (v *networkView) vpnDelete(uuid string) {
	vpn := v.vpn
	if vpn == nil {
		return
	}
	v.body.Show(netPageBrowse)
	go func() { v.vpnWritten(vpn.Remove(context.Background(), uuid), "cannot delete VPN profile") }()
}

// vpnWritten reopens the form with NM's refusal; success needs nothing,
// NM announces the profile and the rows rebuild.
func (v *networkView) vpnWritten(err error, what string) {
	if err == nil {
		return
	}
	log.Printf("network: %s: %v", what, err)
	v.ctx.Invoke(func() {
		v.vpnForm.fail(err.Error())
		v.body.Show(netPageEdit)
	})
}

// vpnCloseForm is VpnFormOutput::Cancel.
func (v *networkView) vpnCloseForm() { v.body.Show(netPageBrowse) }

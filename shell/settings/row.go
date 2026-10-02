package settings

import (
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/shell/apptheme"
)

// hiddenPriority puts the reset button's hidden opacity above every
// stylesheet: Rust sets the widget's opacity, which no CSS overrides.
const hiddenPriority = apptheme.Priority + 1

// Row text limits (row/mod.rs).
const (
	descriptionMaxChars        = 120
	descriptionTooltipMinChars = 80
)

// kit builds the window's widgets: the text face every label starts
// from (the stylesheet then sizes and weighs it) and the store the
// controls write to.
type kit struct {
	face  render.Font
	store store
	// invoke runs a function on the loop (the slider's trailing commit).
	invoke func(func())
}

// label is a styled label; color, size and weight come from the
// stylesheet.
func (k *kit) label(text string, classes ...string) *widget.Label {
	l := widget.NewLabel(k.face, 14, text, 0)
	l.AddClass(classes...)
	return l
}

// icon is a theme icon the stylesheet sizes and tints, matching GTK's
// `image` selectors.
func (k *kit) icon(name string, classes ...string) *widget.Icon {
	ic := widget.NewThemeIcon(name, 16)
	ic.SetElement("image")
	ic.AddClass(classes...)
	return ic
}

// button wraps child in a plain button carrying classes.
func (k *kit) button(child widget.Widget, onClick func(), classes ...string) *widget.Button {
	b := widget.NewButton(child, 0, 0)
	b.AddClass(classes...)
	b.OnClick = onClick
	return b
}

// control is a row's editor: its widget, and refresh re-reading the
// value after the config changed.
type control interface {
	widget.Widget
	refresh()
}

// settingRow is SettingRow: the label with its unit and source badge,
// the description, the reset button and the control.
type settingRow struct {
	*widget.Box
	kit   *kit
	path  string
	ctl   control
	badge *widget.Label
	reset *widget.Button
}

// newSettingRow lays a row out (row/mod.rs view): a full-width control
// stacks under the text, with the reset button beside the label.
func newSettingRow(k *kit, spec rowSpec, ctl control) *settingRow {
	t := i18n.Settings()
	axis := widget.Row
	if spec.fullWidth {
		axis = widget.Column
	}
	r := &settingRow{kit: k, path: spec.path, ctl: ctl, Box: widget.NewBox(axis, 0, 0)}
	r.AddClass("setting-row")

	info := widget.NewBox(widget.Column, 0, 0)
	labelRow := widget.NewBox(widget.Row, 0, 0)
	labelRow.Append(k.label(t.Get(spec.labelKey()), "setting-label"), false)
	if spec.unit != "" {
		labelRow.Append(k.label("("+spec.unit+")", "setting-unit"), false)
	}
	r.badge = k.label("", "badge-subtle")
	labelRow.Append(r.badge, false)
	if d, ok := ctl.(interface{ dirtyBadge() *widget.Label }); ok {
		labelRow.Append(d.dirtyBadge(), false)
	}
	info.Append(labelRow, false)
	if desc := t.Attr(spec.labelKey(), "description"); desc != "" {
		dl := k.label(desc, "setting-description")
		dl.SetEllipsize(widget.EllipsizeEnd)
		dl.SetMaxWidthChars(descriptionMaxChars)
		if len(desc) > descriptionTooltipMinChars {
			dl.SetTooltip(desc)
		}
		info.Append(dl, false)
	}

	r.reset = k.button(k.icon("ld-rotate-ccw-symbolic"), func() { k.store.reset(spec.path) }, "setting-reset")
	slot := widget.NewBox(widget.Row, 0, 0)
	slot.AddClass("setting-control")
	slot.AppendAligned(ctl, spec.fullWidth, widget.AlignCenter)

	if spec.fullWidth {
		r.AddClass("vertical")
		labelRow.Append(widget.NewSpacer(0, 0), true)
		labelRow.Append(r.reset, false)
		r.Append(info, false)
		r.Append(slot, false)
	} else {
		r.AppendAligned(info, true, widget.AlignCenter)
		r.AppendAligned(r.reset, false, widget.AlignCenter)
		r.AppendAligned(slot, false, widget.AlignCenter)
	}
	r.refreshSource()
	return r
}

// refresh re-reads the source and the control's value.
func (r *settingRow) refresh() {
	r.refreshSource()
	r.ctl.refresh()
}

// refreshSource shows the badge and arms the reset button
// (update_source_info): the button keeps its place, invisible and
// inert, while there is no runtime override.
func (r *settingRow) refreshSource() {
	src := sourceOf(r.kit.store, r.path)
	r.badge.SetVisible(src.badge)
	r.badge.SetText(src.label)
	r.badge.SetTooltip(src.tooltip)
	r.badge.SetClasses("badge-subtle")
	if src.class != "" {
		r.badge.AddClass(src.class)
	}
	r.reset.SetEnabled(src.reset)
	if src.reset {
		r.reset.SetInlineStyle("")
	} else {
		r.reset.SetInlineStylePriority("opacity: 0;", hiddenPriority)
	}
}

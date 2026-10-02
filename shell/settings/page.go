package settings

import (
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// pageSpec is LeafEntry with its PageSpec: the sidebar entry and the
// page's sections.
type pageSpec struct {
	id, navKey, icon string
	// header titles the page; its breadcrumb attribute heads it.
	header   string
	sections []sectionSpec
}

// sectionSpec is SectionSpec: a titled group of rows.
type sectionSpec struct {
	title string
	rows  []rowSpec
}

// rowSpec is SettingRowInit, by path: the label key defaults to the
// field's (config.I18nKey), the editor to the one its type calls for.
type rowSpec struct {
	path      string
	key       string
	editor    editorFunc
	fullWidth bool
	unit      string
}

// labelKey is the row's label key.
func (r rowSpec) labelKey() string {
	if r.key != "" {
		return r.key
	}
	if key, ok := config.I18nKey(r.path); ok {
		return key
	}
	return "missing-i18n-key"
}

// rowOpt adjusts a row.
type rowOpt func(*rowSpec)

// field is a row for the config field at path.
func field(path string, opts ...rowOpt) rowSpec {
	r := rowSpec{path: path}
	for _, o := range opts {
		o(&r)
	}
	return r
}

// fields is a row per path, each with its auto editor.
func fields(paths ...string) []rowSpec {
	rows := make([]rowSpec, len(paths))
	for i, p := range paths {
		rows[i] = field(p)
	}
	return rows
}

// withEditor names the row's editor.
func withEditor(e editorFunc) rowOpt { return func(r *rowSpec) { r.editor = e } }

// settingsPage is SettingsPage: the header and the sections in a
// scroll, refreshing its rows when the config changes.
type settingsPage struct {
	*widget.Scroll
	rows []*settingRow
}

// buildPage lays out a page (pages/layout.rs).
func buildPage(k *kit, spec pageSpec) *settingsPage {
	t := i18n.Settings()
	content := widget.NewBox(widget.Column, 0, 0)
	content.AddClass("settings-page")
	header := widget.NewBox(widget.Column, 0, 0)
	header.AddClass("settings-page-header")
	header.Append(k.label(t.Attr(spec.header, "breadcrumb"), "settings-breadcrumb"), false)
	header.Append(k.label(t.Get(spec.header), "settings-page-title"), false)
	content.Append(header, false)
	p := &settingsPage{}
	for _, s := range spec.sections {
		section := widget.NewBox(widget.Column, 0, 0)
		section.AddClass("settings-section")
		section.Append(k.label(t.Get(s.title), "settings-section-title"), false)
		group := widget.NewBox(widget.Column, 0, 0)
		group.AddClass("settings-group")
		for _, rs := range s.rows {
			row := newSettingRow(k, rs, fieldControl(k, rs))
			group.Append(row, false)
			p.rows = append(p.rows, row)
		}
		section.Append(group, false)
		content.Append(section, false)
	}
	p.Scroll = widget.NewScroll(content)
	p.VerticalOnly, p.ShowBars, p.FillY = true, true, true
	return p
}

// refresh re-reads every row.
func (p *settingsPage) refresh() {
	for _, r := range p.rows {
		r.refresh()
	}
}

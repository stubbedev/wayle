package settings

import (
	"fmt"
	"log"
	"math"
	"path/filepath"
	"strings"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/icons"
	"github.com/stubbedev/wayle/styling"
)

// The editors that open a picker (editors/font, editors/color).

// pickers opens what an editor picks from: a popover beside an anchor
// and the color dialog. The window provides it; tests fake it.
type pickers interface {
	// popover shows content beside anchor; close dismisses it.
	popover(anchor widget.Widget, content widget.Widget) (close func())
	// color asks for a color, calling fn with the pick.
	color(initial render.Color, fn func(render.Color))
	// openFile asks the file chooser for one file, calling fn on the
	// loop with its path; a dismissed chooser calls nothing.
	openFile(fn func(path string))
}

// fontFamilies lists the families a font picker offers (a seam for
// tests).
var fontFamilies = app.FontFamilies

// fontPickerLabelChars caps the face's label (FontControl).
const fontPickerLabelChars = 20

// fontPickerListPx is the family list's height: .font-picker-scroll's
// 18rem at the settings window's 1.1 global scale, the scrolled
// window's size in Rust.
const fontPickerListPx = 18 * remBasePx * 1.1

// fontEditor is FontControl: a menu button showing the family that
// opens the searchable family list.
type fontEditor struct {
	*widget.Box
	k     *kit
	slot  slot
	label *widget.Label
	btn   *widget.Button
	// picker is the open (or last) picker.
	picker *searchPicker
}

func newFontEditor(k *kit, s slot) *fontEditor {
	c := &fontEditor{Box: widget.NewBox(widget.Row, 0, 0), k: k, slot: s}
	c.SetElement("menubutton")
	c.AddClass("font")
	c.label = k.label("", "font-picker-label")
	c.label.SetEllipsize(widget.EllipsizeEnd)
	c.label.SetMaxWidthChars(fontPickerLabelChars)
	c.btn = k.button(c.label, c.open)
	c.Append(c.btn, false)
	c.refresh()
	return c
}

func (c *fontEditor) refresh() {
	s, _ := c.slot.get().(string)
	c.label.SetText(s)
}

// open shows the picker (FontPicker): a search entry over the family
// list; a pick writes the family and closes it.
func (c *fontEditor) open() {
	if c.k.pickers == nil {
		return
	}
	families, err := fontFamilies()
	if err != nil {
		log.Printf("settings: font families: %v", err)
	}
	c.picker = openSearchPicker(c.k, c.btn, families, pickerSpec{
		class: "font-picker-popover", listClass: "font-picker-list", scrollClass: "font-picker-scroll",
		placeholder: "settings-font-search", maxH: int(math.Round(fontPickerListPx)), searchEntry: true,
		row: func(k *kit, name string) widget.Widget {
			l := k.label(name, "font-picker-item")
			l.SetEllipsize(widget.EllipsizeEnd)
			return l
		},
	}, func(family string) { _ = c.slot.set(family) })
}

// pickerSpec shapes a searchPicker: the CSS classes, the search
// placeholder key, the row (or grid cell, with cellW) for a name, the
// list's height cap and minimum width, and what sits beside the search
// entry.
type pickerSpec struct {
	class, listClass, scrollClass, placeholder string
	row                                        func(k *kit, name string) widget.Widget
	cellW, maxH, minW                          int
	// trailing builds what follows the search entry (the icon picker's
	// clear button), handed the pick.
	trailing func(k *kit, pick func(string)) widget.Widget
	// pickTyped makes Enter in the search entry pick the typed text.
	pickTyped bool
	// searchEntry makes the search a gtk::SearchEntry (the font and
	// module pickers): the search icon, the clear button, Esc clearing,
	// and the filter on search-changed after GTK's default delay. The
	// others are plain gtk::Entry filtering per keystroke.
	searchEntry bool
}

// searchPicker is the popovers' shared tree (FontPicker, the icon
// picker): a search row over a virtualized list filtered by a
// case-insensitive substring; one click on a row picks it, writes and
// closes the popover.
type searchPicker struct {
	root   *widget.Box
	search *widget.Entry
	list   *widget.List
	all    []string
	shown  []string
	k      *kit
	spec   pickerSpec
	pick   func(string)
}

// openSearchPicker builds the picker and opens it beside anchor.
func openSearchPicker(k *kit, anchor widget.Widget, names []string, spec pickerSpec, onPick func(string)) *searchPicker {
	var closePicker func()
	p := newSearchPicker(k, names, spec, func(name string) {
		onPick(name)
		if closePicker != nil {
			closePicker()
		}
	})
	closePicker = k.pickers.popover(anchor, p.root)
	return p
}

func newSearchPicker(k *kit, names []string, spec pickerSpec, pick func(string)) *searchPicker {
	p := &searchPicker{k: k, all: names, shown: names, spec: spec, pick: pick}
	p.root = widget.NewBox(widget.Column, 0, 0)
	p.root.AddClass(spec.class)
	contents := widget.NewBox(widget.Column, 8, 0)
	contents.SetElement("contents")
	var searchField widget.Widget
	if spec.searchEntry {
		se := widget.NewSearchEntry(k.face, 14, i18n.Settings().Get(spec.placeholder))
		// The stylesheet inks it, as k.entry's.
		se.SetColor(0)
		se.SetTextWidth(widget.GTKTextWidth)
		se.OnSearchChanged = p.filter
		p.search, searchField = se.Entry, se
	} else {
		p.search = k.entry()
		p.search.SetPlaceholder(i18n.Settings().Get(spec.placeholder))
		p.search.OnChanged = p.filter
		searchField = p.search
	}
	if spec.pickTyped {
		p.search.OnActivate = func(text string) {
			if text != "" {
				p.pick(text)
			}
		}
	}
	searchRow := widget.NewBox(widget.Row, 4, 0)
	searchRow.Append(searchField, true)
	if spec.trailing != nil {
		searchRow.AppendAligned(spec.trailing(k, p.pick), false, widget.AlignCenter)
	}
	contents.Append(searchRow, false)
	p.list = widget.NewList[widget.Widget](pickerRows{p}, 0)
	p.list.AddClass(spec.listClass)
	p.list.SetSingleClickActivate(true)
	if spec.cellW > 0 {
		p.list.SetCellWidth(spec.cellW)
	}
	if spec.maxH > 0 {
		p.list.SetMaxHeight(spec.maxH)
	}
	p.list.OnActivate = func(i int) {
		if i >= 0 && i < len(p.shown) {
			p.pick(p.shown[i])
		}
	}
	scroll := widget.NewBox(widget.Column, 0, 0)
	if spec.scrollClass != "" {
		scroll.AddClass(spec.scrollClass)
	}
	if spec.minW > 0 {
		scroll.SetInlineStyle(fmt.Sprintf("min-width: %dpx;", spec.minW))
	}
	scroll.Append(p.list, true)
	contents.Append(scroll, true)
	p.root.Append(contents, true)
	return p
}

// filter narrows the list to the names containing the query
// (StringFilter, Substring, ignore case).
func (p *searchPicker) filter(query string) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		p.shown = p.all
	} else {
		p.shown = nil
		for _, f := range p.all {
			if strings.Contains(strings.ToLower(f), q) {
				p.shown = append(p.shown, f)
			}
		}
	}
	p.list.Reset()
}

// pickerRows is the list model over the shown names.
type pickerRows struct{ p *searchPicker }

func (r pickerRows) Len() int { return len(r.p.shown) }

func (r pickerRows) Row(i int) widget.Widget { return r.p.spec.row(r.p.k, r.p.shown[i]) }

// colorEditor is ColorControl: a swatch button opening the color
// dialog; a pick writes the hex (rgba_to_hex: #rrggbb, #rrggbbaa
// with alpha).
type colorEditor struct {
	*widget.Button
	k      *kit
	slot   slot
	swatch *widget.Box
	value  render.Color
}

func newColorEditor(k *kit, s slot) *colorEditor {
	c := &colorEditor{k: k, slot: s, swatch: widget.NewBox(widget.Row, 0, 0)}
	c.swatch.AddClass("color-swatch")
	c.Button = k.button(c.swatch, c.open)
	c.SetElement("colorswatch")
	c.refresh()
	return c
}

func (c *colorEditor) refresh() {
	s, _ := c.slot.get().(string)
	col, ok := styling.ParseHex(s)
	if !ok {
		return
	}
	c.value = col
	c.swatch.SetInlineStyle("background-color: " + s + ";")
}

func (c *colorEditor) open() {
	if c.k.pickers == nil {
		return
	}
	c.k.pickers.color(c.value, func(col render.Color) {
		_ = c.slot.set(styling.HexRGBA(col))
	})
}

// fileEditor is FilePickerControl: the path entry (committing on
// Enter, with the unsaved badge) and a browse button asking the file
// chooser portal; a pick writes the path.
type fileEditor struct {
	*widget.Box
	entry *text
	k     *kit
	slot  slot
}

func newFileEditor(k *kit, s slot) *fileEditor {
	c := &fileEditor{Box: widget.NewBox(widget.Row, 0, 0), entry: newText(k, s, false), k: k, slot: s}
	c.AddClass("file-picker")
	browse := k.button(k.icon("ld-folder-open-symbolic"), c.browse, "icon")
	c.AppendAligned(c.entry, true, widget.AlignCenter)
	c.AppendAligned(browse, false, widget.AlignCenter)
	return c
}

func (c *fileEditor) dirtyBadge() *widget.Label { return c.entry.badge }

func (c *fileEditor) refresh() { c.entry.refresh() }

func (c *fileEditor) browse() {
	if c.k.pickers == nil {
		return
	}
	c.k.pickers.openFile(func(file string) {
		if c.slot.set(file) == nil {
			c.entry.badge.SetVisible(false)
		}
	})
}

// filePath is file_picker::file_path for a row.
var filePath = withEditor(func(k *kit, s slot, _ config.FieldMeta) control { return newFileEditor(k, s) })

// fontRow is font::font for a row.
var fontRow = withEditor(func(k *kit, s slot, _ config.FieldMeta) control { return newFontEditor(k, s) })

// Icon picker sizes (editors/icon: PREVIEW_SIZE, the scroller's
// minimum content size, and the 2rem cell at the 1.1 global scale plus
// its padding).
const (
	iconPreviewPx  = 24
	iconPickerH    = 420
	iconPickerW    = 440
	iconPickerCell = 40
)

// iconNames lists the installed icons the picker offers (a seam for
// tests; IconManager::list).
var iconNames = func() []string {
	m, err := icons.NewManager()
	if err != nil {
		log.Printf("settings: icons: %v", err)
		return nil
	}
	return m.List()
}

// iconEditor is icon_picker_widget: a trigger showing the icon and its
// name that opens the searchable grid of installed icons; Enter picks
// the typed name, the clear button none.
type iconEditor struct {
	*widget.Box
	k      *kit
	slot   slot
	btn    *widget.Button
	icon   *widget.Icon
	label  *widget.Label
	picker *searchPicker
}

func newIconEditor(k *kit, s slot) *iconEditor {
	c := &iconEditor{Box: widget.NewBox(widget.Row, 0, 0), k: k, slot: s}
	c.SetElement("menubutton")
	c.AddClass("icon-picker-trigger")
	c.icon = k.icon("ld-image-symbolic")
	c.label = k.label("")
	c.label.SetEllipsize(widget.EllipsizeEnd)
	face := widget.NewBox(widget.Row, 8, 0)
	face.AppendAligned(c.icon, false, widget.AlignCenter)
	face.AppendAligned(c.label, true, widget.AlignCenter)
	c.btn = k.button(face, c.open)
	c.Append(c.btn, false)
	c.refresh()
	return c
}

// refresh is update_display: the icon and its name, or the image
// placeholder and "None".
func (c *iconEditor) refresh() {
	name := c.slot.text()
	if name == "" {
		c.icon.SetThemeName("ld-image-symbolic")
		c.label.SetText(i18n.Settings().Get("settings-icon-none"))
		return
	}
	c.icon.SetThemeName(name)
	c.label.SetText(name)
}

func (c *iconEditor) open() {
	if c.k.pickers == nil {
		return
	}
	c.picker = openSearchPicker(c.k, c.btn, iconNames(), pickerSpec{
		class: "icon-picker-popover", placeholder: "settings-icon-search",
		cellW: iconPickerCell, maxH: iconPickerH, minW: iconPickerW, pickTyped: true,
		row: func(k *kit, name string) widget.Widget {
			cell := widget.NewBox(widget.Row, 0, 0)
			cell.AddClass("icon-picker-cell")
			cell.SetTooltip(name)
			cell.AppendAligned(widget.NewThemeIcon(name, iconPreviewPx), true, widget.AlignCenter)
			return cell
		},
		trailing: func(k *kit, pick func(string)) widget.Widget {
			clear := k.button(k.icon("ld-x-circle-symbolic"), func() { pick("") }, "flat", "icon-picker-clear")
			clear.SetTooltip(i18n.Settings().Get("settings-icon-clear"))
			return clear
		},
	}, func(name string) {
		_ = c.slot.set(name)
		c.refresh()
	})
}

// iconRow is icon::icon for a row.
var iconRow = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newIconEditor(k, s)
})

// Theme selector sizes (theme_selector: BASE_MAX_HEIGHT at the
// styling scale).
const themeListPx = 384

// paletteKeys are the [styling.palette] keys a theme sets, in the
// palette's order.
var paletteKeys = []string{"bg", "surface", "elevated", "fg", "fg-muted", "primary", "red", "yellow", "green", "blue"}

// availableThemes lists the selectable themes (styling.available):
// the built-ins and the config's themes/ files (a seam for tests).
var availableThemes = func(s store) []config.ThemeEntry {
	themes, skipped := config.LoadThemes(filepath.Join(s.svc.Dir(), "themes"))
	for _, err := range skipped {
		log.Printf("settings: theme: %v", err)
	}
	return themes
}

// themeSelector is ThemeSelectorControl: a button opening the preset
// list (each theme's swatches and name); applying one names it the
// palette's base theme and writes its colors, the badge showing it.
type themeSelector struct {
	*widget.Box
	k      *kit
	btn    *widget.Button
	badge  *widget.Label
	themes []config.ThemeEntry
	picker *searchPicker
}

func newThemeSelector(k *kit) *themeSelector {
	t := i18n.Settings()
	c := &themeSelector{Box: widget.NewBox(widget.Row, 0, 0), k: k}
	content := widget.NewBox(widget.Row, 0, 0)
	content.AppendAligned(k.icon("ld-palette-symbolic", "theme-preset-button-icon"), false, widget.AlignCenter)
	content.AppendAligned(k.label(t.Get("settings-theme-preset-apply"), "theme-preset-button-label"), false, widget.AlignCenter)
	content.AppendAligned(k.icon("ld-chevron-down-symbolic", "theme-preset-button-chevron"), false, widget.AlignCenter)
	c.btn = k.button(content, c.open, "theme-preset-button")
	c.Append(c.btn, false)
	c.badge = k.label("", "badge", "badge-subtle")
	c.refresh()
	return c
}

func (c *themeSelector) dirtyBadge() *widget.Label { return c.badge }

// refresh shows the base theme on the badge.
func (c *themeSelector) refresh() {
	name, _ := c.k.store.value("styling.palette_base_theme").(string)
	c.badge.SetText(name)
	c.badge.SetVisible(name != "")
}

func (c *themeSelector) open() {
	if c.k.pickers == nil {
		return
	}
	c.themes = availableThemes(c.k.store)
	names := make([]string, len(c.themes))
	for i, th := range c.themes {
		names[i] = th.Name
	}
	scale, _ := c.k.store.value("styling.scale").(float64)
	c.picker = openSearchPicker(c.k, c.btn, names, pickerSpec{
		class: "theme-preset-popover", listClass: "theme-preset-list", scrollClass: "theme-preset-scroll",
		placeholder: "settings-theme-preset", maxH: int(math.Round(themeListPx * max(scale, 0.25))),
		row: func(k *kit, name string) widget.Widget { return c.row(name) },
	}, c.apply)
}

// row is build_theme_row: the swatches (bg, primary, red, yellow,
// green, blue) and the name.
func (c *themeSelector) row(name string) widget.Widget {
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("theme-preset-entry")
	content := widget.NewBox(widget.Row, 0, 0)
	content.AddClass("theme-preset-row")
	swatches := widget.NewBox(widget.Row, 0, 0)
	swatches.AddClass("theme-preset-swatches")
	if th, ok := c.theme(name); ok {
		p := th.Palette
		for _, hex := range []string{p.Bg, p.Primary, p.Red, p.Yellow, p.Green, p.Blue} {
			sw := widget.NewBox(widget.Row, 0, 0)
			sw.AddClass("theme-preset-swatch")
			sw.SetInlineStyle("background-color: " + hex + "; background-image: none;")
			swatches.AppendAligned(sw, false, widget.AlignCenter)
		}
	}
	content.AppendAligned(swatches, false, widget.AlignCenter)
	content.AppendAligned(c.k.label(name, "theme-preset-name"), true, widget.AlignCenter)
	row.Append(content, true)
	return row
}

func (c *themeSelector) theme(name string) (config.ThemeEntry, bool) {
	for _, th := range c.themes {
		if th.Name == name {
			return th, true
		}
	}
	return config.ThemeEntry{}, false
}

// apply is on_apply: the base theme, then each valid palette color.
func (c *themeSelector) apply(name string) {
	th, ok := c.theme(name)
	if !ok {
		return
	}
	_ = c.k.store.set("styling.palette_base_theme", name)
	p := th.Palette
	for i, hex := range []string{p.Bg, p.Surface, p.Elevated, p.Fg, p.FgMuted, p.Primary, p.Red, p.Yellow, p.Green, p.Blue} {
		if _, err := config.ParseHexColor(hex); err == nil {
			_ = c.k.store.set("styling.palette."+paletteKeys[i], hex)
		}
	}
	c.refresh()
}

// themePreset is theme_selector's row: an action row under the preset
// key.
func themePreset() rowSpec {
	return rowSpec{
		path: "styling.palette_base_theme", key: "settings-theme-preset", action: true,
		editor: func(k *kit, _ slot, _ config.FieldMeta) control { return newThemeSelector(k) },
	}
}

package settings

import (
	"log"
	"math"
	"strings"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
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
	path  string
	label *widget.Label
	btn   *widget.Button
	// picker is the open (or last) picker.
	picker *fontPicker
}

func newFontEditor(k *kit, path string) *fontEditor {
	c := &fontEditor{Box: widget.NewBox(widget.Row, 0, 0), k: k, path: path}
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
	s, _ := c.k.store.value(c.path).(string)
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
	var closePicker func()
	p := newFontPicker(c.k, families, func(family string) {
		_ = c.k.store.set(c.path, family)
		if closePicker != nil {
			closePicker()
		}
	})
	c.picker = p
	closePicker = c.k.pickers.popover(c.btn, p.root)
}

// fontPicker is the popover's tree: the search entry and the list
// filtered by a case-insensitive substring.
type fontPicker struct {
	root   *widget.Box
	search *widget.Entry
	list   *widget.List
	all    []string
	shown  []string
	k      *kit
	onPick func(string)
}

func newFontPicker(k *kit, families []string, onPick func(string)) *fontPicker {
	p := &fontPicker{k: k, all: families, shown: families, onPick: onPick}
	p.root = widget.NewBox(widget.Column, 0, 0)
	p.root.AddClass("font-picker-popover")
	contents := widget.NewBox(widget.Column, 0, 0)
	contents.SetElement("contents")
	p.search = widget.NewEntry(k.face, 14, 0)
	p.search.SetPlaceholder(i18n.Settings().Get("settings-font-search"))
	p.search.OnChanged = p.filter
	contents.Append(p.search, false)
	p.list = widget.NewList[widget.Widget](fontRows{p}, 0)
	p.list.AddClass("font-picker-list")
	p.list.SetMaxHeight(int(math.Round(fontPickerListPx)))
	p.list.OnActivate = func(i int) {
		if i >= 0 && i < len(p.shown) {
			p.onPick(p.shown[i])
		}
	}
	scroll := widget.NewBox(widget.Column, 0, 0)
	scroll.AddClass("font-picker-scroll")
	scroll.Append(p.list, true)
	contents.Append(scroll, true)
	p.root.Append(contents, true)
	return p
}

// filter narrows the list to the families containing the query
// (StringFilter, Substring, ignore case).
func (p *fontPicker) filter(query string) {
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

// fontRows is the list model over the shown families.
type fontRows struct{ p *fontPicker }

func (r fontRows) Len() int { return len(r.p.shown) }

func (r fontRows) Row(i int) widget.Widget {
	row := widget.NewBox(widget.Row, 0, 0)
	row.SetElement("row")
	l := r.p.k.label(r.p.shown[i], "font-picker-item")
	l.SetEllipsize(widget.EllipsizeEnd)
	row.Append(l, true)
	return row
}

// colorEditor is ColorControl: a swatch button opening the color
// dialog; a pick writes the hex (rgba_to_hex: #rrggbb, #rrggbbaa
// with alpha).
type colorEditor struct {
	*widget.Button
	k      *kit
	path   string
	swatch *widget.Box
	value  render.Color
}

func newColorEditor(k *kit, path string) *colorEditor {
	c := &colorEditor{k: k, path: path, swatch: widget.NewBox(widget.Row, 0, 0)}
	c.swatch.AddClass("color-swatch")
	c.Button = k.button(c.swatch, c.open)
	c.SetElement("colorswatch")
	c.refresh()
	return c
}

func (c *colorEditor) refresh() {
	s, _ := c.k.store.value(c.path).(string)
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
		_ = c.k.store.set(c.path, styling.HexRGBA(col))
	})
}

// fileEditor is FilePickerControl: the path entry (committing on
// Enter, with the unsaved badge) and a browse button asking the file
// chooser portal; a pick writes the path.
type fileEditor struct {
	*widget.Box
	entry *text
	k     *kit
	path  string
}

func newFileEditor(k *kit, path string) *fileEditor {
	c := &fileEditor{Box: widget.NewBox(widget.Row, 0, 0), entry: newText(k, path, false), k: k, path: path}
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
		if c.k.store.set(c.path, file) == nil {
			c.entry.badge.SetVisible(false)
		}
	})
}

// filePath is file_picker::file_path for a row.
var filePath = withEditor(func(k *kit, path string, _ config.FieldMeta) control { return newFileEditor(k, path) })

// fontRow is font::font for a row.
var fontRow = withEditor(func(k *kit, path string, _ config.FieldMeta) control { return newFontEditor(k, path) })

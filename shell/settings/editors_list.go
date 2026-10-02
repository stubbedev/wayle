package settings

import (
	"maps"
	"reflect"
	"slices"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// The list editors (string_list, icon_list, enum_list): one row per
// item with move up, move down and remove, an add button below, every
// edit writing the whole list; an outside change rebuilds the rows.

// listItem is one row's control over one item.
type listItem interface {
	widget.Widget
	itemValue() any
}

// listSpec shapes a list editor: the add button's tooltip key, the
// row's class, whether rows reorder, the item control for a value
// (changed writes the list), and a new item's value.
type listSpec struct {
	addKey   string
	rowClass string
	reorder  bool
	item     func(k *kit, v any, changed func()) listItem
	blank    any
	classes  []string
	// decode turns the stored value into items, encode items into the
	// stored value (a map's pairs); nil for a plain list.
	decode func(v any) []any
	encode func(items []any) any
}

type listEditor struct {
	*widget.Box
	k     *kit
	slot  slot
	spec  listSpec
	rows  *widget.Box
	items []listItem
}

func newListEditor(k *kit, s slot, spec listSpec) *listEditor {
	c := &listEditor{Box: widget.NewBox(widget.Column, 8, 0), k: k, slot: s, spec: spec}
	c.AddClass(append([]string{"string-list-editor"}, spec.classes...)...)
	c.rows = widget.NewBox(widget.Column, 4, 0)
	c.rows.AddClass("string-list")
	c.Append(c.rows, false)
	add := listButton(k, "ld-plus-symbolic", spec.addKey, "list-control-add", func() {
		c.appendRow(spec.blank)
		c.commit()
	})
	c.AppendAligned(add, false, widget.AlignStart)
	c.rebuild(c.stored())
	return c
}

// listButton is list_controls' add/remove button.
func listButton(k *kit, icon, tooltipKey, class string, onClick func()) *widget.Button {
	b := k.button(k.icon(icon), onClick, "list-control-button", class)
	b.SetTooltip(i18n.Settings().Get(tooltipKey))
	return b
}

func (c *listEditor) stored() []any {
	if c.spec.decode != nil {
		return c.spec.decode(c.slot.get())
	}
	v, _ := c.slot.get().([]any)
	return v
}

// encoded is the rows as the stored value.
func (c *listEditor) encoded() any {
	if c.spec.encode != nil {
		return c.spec.encode(c.values())
	}
	return c.values()
}

func (c *listEditor) values() []any {
	out := make([]any, len(c.items))
	for i, it := range c.items {
		out[i] = it.itemValue()
	}
	return out
}

func (c *listEditor) commit() { _ = c.slot.set(c.encoded()) }

func (c *listEditor) rebuild(values []any) {
	c.rows.Clear()
	c.items = nil
	for _, v := range values {
		c.appendRow(v)
	}
}

func (c *listEditor) appendRow(v any) {
	item := c.spec.item(c.k, v, c.commit)
	row := widget.NewBox(widget.Row, 4, 0)
	row.AddClass(c.spec.rowClass)
	row.Append(item, true)
	if c.spec.reorder {
		row.AppendAligned(c.rowButton("ld-chevron-up-symbolic", func() { c.move(item, -1) }), false, widget.AlignCenter)
		row.AppendAligned(c.rowButton("ld-chevron-down-symbolic", func() { c.move(item, 1) }), false, widget.AlignCenter)
	}
	row.AppendAligned(c.rowButton("ld-trash-2-symbolic", func() { c.remove(item) }), false, widget.AlignCenter)
	c.rows.Append(row, false)
	c.items = append(c.items, item)
}

func (c *listEditor) rowButton(icon string, onClick func()) *widget.Button {
	return c.k.button(c.k.icon(icon), onClick, "string-list-button")
}

func (c *listEditor) remove(item listItem) {
	i := slices.Index(c.items, item)
	if i < 0 {
		return
	}
	values := c.values()
	c.rebuild(slices.Delete(values, i, i+1))
	c.commit()
}

func (c *listEditor) move(item listItem, delta int) {
	i := slices.Index(c.items, item)
	j := i + delta
	if i < 0 || j < 0 || j >= len(c.items) {
		return
	}
	values := c.values()
	values[i], values[j] = values[j], values[i]
	c.rebuild(values)
	c.commit()
}

// refresh rebuilds only when the stored list moved away from the rows
// (the watcher's check), so typing keeps its entry.
func (c *listEditor) refresh() {
	raw := c.slot.get()
	if sameStored(raw, c.encoded()) {
		return
	}
	c.rebuild(c.stored())
}

// listRow makes a full-width row with the list editor spec builds.
func listRow(spec func(meta config.FieldMeta) listSpec) rowOpt {
	return func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, meta config.FieldMeta) control {
			return newListEditor(k, s, spec(meta))
		}
	}
}

// textItem is a string_list row: an entry.
type textItem struct{ *widget.Entry }

func (t textItem) itemValue() any { return t.Text() }

// stringList is string_list::string_list.
var stringList = listRow(func(config.FieldMeta) listSpec {
	return listSpec{
		addKey: "settings-list-add", rowClass: "string-list-row", reorder: true, blank: "",
		item: func(k *kit, v any, changed func()) listItem {
			e := widget.NewEntry(k.face, 14, 0)
			s, _ := v.(string)
			e.SetText(s)
			e.OnChanged = func(string) { changed() }
			return textItem{e}
		},
	}
})

// iconItem is an icon_list row: the icon picker over the item.
type iconItem struct {
	*iconEditor
	value *string
}

func (i iconItem) itemValue() any { return *i.value }

// iconList is icon_list::icon_list.
var iconList = listRow(func(config.FieldMeta) listSpec {
	return listSpec{
		addKey: "settings-list-add", rowClass: "string-list-row", reorder: true, blank: "",
		item: func(k *kit, v any, changed func()) listItem {
			s, _ := v.(string)
			value := &s
			ed := newIconEditor(k, slot{
				get: func() any { return *value },
				set: func(nv any) error { *value, _ = nv.(string); changed(); return nil },
			})
			return iconItem{ed, value}
		},
	}
})

// enumItem is an enum_list row: the variants' dropdown.
type enumItem struct {
	*widget.Dropdown
	variants []string
}

func (e enumItem) itemValue() any { return e.variants[max(e.Selected(), 0)] }

// enumList is enum_list::enum_list over the list's element enum.
var enumList = listRow(func(meta config.FieldMeta) listSpec {
	var elem config.FieldMeta
	if meta.Elem != nil {
		elem = *meta.Elem
	}
	blank := ""
	if len(elem.Variants) > 0 {
		blank = elem.Variants[0]
	}
	return listSpec{
		addKey: "settings-list-add", rowClass: "string-list-row", reorder: true, blank: blank,
		item: func(k *kit, v any, changed func()) listItem {
			s, _ := v.(string)
			d := widget.NewDropdown(k.face, 14, enumLabels(elem), max(slices.Index(elem.Variants, s), 0))
			d.OnSelect = func(int) { changed() }
			return enumItem{d, elem.Variants}
		},
	}
})

// sameStored compares stored values, an empty list or map equal to
// none.
func sameStored(a, b any) bool {
	empty := func(v any) bool {
		rv := reflect.ValueOf(v)
		return v == nil || ((rv.Kind() == reflect.Slice || rv.Kind() == reflect.Map) && rv.Len() == 0)
	}
	if empty(a) && empty(b) {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// mapItem is a string_map row: the key and value entries.
type mapItem struct {
	*widget.Box
	key, value *widget.Entry
}

func (m mapItem) itemValue() any { return [2]string{m.key.Text(), m.value.Text()} }

// stringMap is string_map::string_map: key/value rows, an empty key
// dropped on write, the stored map shown sorted by key.
var stringMap = listRow(func(config.FieldMeta) listSpec {
	t := i18n.Settings()
	return listSpec{
		addKey: "settings-map-add", rowClass: "string-map-row", blank: [2]string{}, classes: []string{"string-map-editor"},
		item: func(k *kit, v any, changed func()) listItem {
			pair, _ := v.([2]string)
			row := mapItem{Box: widget.NewBox(widget.Row, 4, 0), key: widget.NewEntry(k.face, 14, 0), value: widget.NewEntry(k.face, 14, 0)}
			row.key.SetPlaceholder(t.Get("settings-map-key-placeholder"))
			row.value.SetPlaceholder(t.Get("settings-map-value-placeholder"))
			row.key.SetText(pair[0])
			row.value.SetText(pair[1])
			row.key.OnChanged = func(string) { changed() }
			row.value.OnChanged = func(string) { changed() }
			row.Append(row.key, true)
			row.Append(row.value, true)
			return row
		},
		decode: func(v any) []any {
			m, _ := v.(map[string]any)
			keys := slices.Sorted(maps.Keys(m))
			out := make([]any, len(keys))
			for i, key := range keys {
				s, _ := m[key].(string)
				out[i] = [2]string{key, s}
			}
			return out
		},
		encode: func(items []any) any {
			out := map[string]any{}
			for _, it := range items {
				if pair, _ := it.([2]string); pair[0] != "" {
					out[pair[0]] = pair[1]
				}
			}
			return out
		},
	}
})

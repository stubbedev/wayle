package settings

import (
	"maps"
	"math"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// The card lists (card_form, threshold_list, the preset and override
// lists): one card per list item, its fields the ordinary editors over
// slots into the item, a remove button in the card's header and an
// add button below.

// cardField is one field of a card: the item key, its label key, and
// the editor over the field's slot.
type cardField struct {
	key, label string
	editor     func(k *kit, s slot) control
}

// cardSpec shapes a card list: the card title for an item (1-based
// number), its fields, the new item, and the container classes.
type cardSpec struct {
	title    func(item map[string]any, number int) string
	fields   []cardField
	blank    func() map[string]any
	classes  []string
	listCls  string
	rowClass string
	labelCls string
}

type cardList struct {
	*widget.Box
	k     *kit
	slot  slot
	spec  cardSpec
	list  *widget.Box
	cards []*card
}

type card struct {
	title    *widget.Label
	controls []control
}

func newCardList(k *kit, s slot, spec cardSpec) *cardList {
	c := &cardList{Box: widget.NewBox(widget.Column, 8, 0), k: k, slot: s, spec: spec}
	c.AddClass(append([]string{"card-form-editor"}, spec.classes...)...)
	c.list = widget.NewBox(widget.Column, 8, 0)
	c.list.AddClass(spec.listCls)
	c.Append(c.list, false)
	add := listButton(k, "ld-plus-symbolic", "settings-list-add", "list-control-add", func() {
		_ = c.slot.set(append(c.items(), spec.blank()))
		c.rebuild()
	})
	c.AppendAligned(add, false, widget.AlignStart)
	c.rebuild()
	return c
}

// items are the list's items, plain.
func (c *cardList) items() []any {
	v, _ := c.slot.get().([]any)
	return v
}

func (c *cardList) item(i int) map[string]any {
	items := c.items()
	if i < 0 || i >= len(items) {
		return nil
	}
	m, _ := items[i].(map[string]any)
	return m
}

// fieldSlot is the slot of one key of item i: written back with the
// whole list, unset by removing the key.
func (c *cardList) fieldSlot(i int, key string) slot {
	write := func(v any, remove bool) error {
		items := c.items()
		if i >= len(items) {
			return nil
		}
		m := maps.Clone(c.item(i))
		if m == nil {
			m = map[string]any{}
		}
		if remove {
			delete(m, key)
		} else {
			m[key] = v
		}
		items[i] = m
		err := c.slot.set(items)
		c.retitle(i)
		return err
	}
	return slot{
		get:   func() any { return c.item(i)[key] },
		set:   func(v any) error { return write(v, false) },
		unset: func() { _ = write(nil, true) },
	}
}

func (c *cardList) rebuild() {
	c.list.Clear()
	c.cards = nil
	for i := range c.items() {
		c.appendCard(i)
	}
}

// appendCard is card_titled with the fields in its body.
func (c *cardList) appendCard(i int) {
	t := i18n.Settings()
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass("card-form-card")
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("card-form-header")
	cd := &card{title: c.k.label("", "card-form-title")}
	header.AppendAligned(cd.title, true, widget.AlignCenter)
	header.AppendAligned(listButton(c.k, "ld-trash-2-symbolic", "settings-list-remove", "list-control-remove", func() {
		items := c.items()
		if i < len(items) {
			items = append(items[:i:i], items[i+1:]...)
			_ = c.slot.set(items)
			c.rebuild()
		}
	}), false, widget.AlignCenter)
	body := widget.NewBox(widget.Column, 0, 0)
	body.AddClass("card-form-body")
	for _, f := range c.spec.fields {
		ctl := f.editor(c.k, c.fieldSlot(i, f.key))
		row := widget.NewBox(widget.Row, 8, 0)
		row.AddClass(c.spec.rowClass)
		row.AppendAligned(c.k.label(t.Get(f.label), c.spec.labelCls), true, widget.AlignCenter)
		row.AppendAligned(ctl, false, widget.AlignCenter)
		body.Append(row, false)
		cd.controls = append(cd.controls, ctl)
	}
	root.Append(header, false)
	root.Append(body, false)
	c.list.Append(root, false)
	c.cards = append(c.cards, cd)
	c.retitle(i)
}

func (c *cardList) retitle(i int) {
	if i < len(c.cards) {
		c.cards[i].title.SetText(c.spec.title(c.item(i), i+1))
	}
}

// refresh rebuilds when the count changed, else refreshes every
// field and title (the watcher).
func (c *cardList) refresh() {
	if len(c.items()) != len(c.cards) {
		c.rebuild()
		return
	}
	for i, cd := range c.cards {
		for _, ctl := range cd.controls {
			ctl.refresh()
		}
		c.retitle(i)
	}
}

// cardRow makes a full-width row with the card list spec builds.
func cardRow(spec cardSpec) rowOpt {
	return func(r *rowSpec) {
		r.fullWidth = true
		r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newCardList(k, s, spec) }
	}
}

// Threshold bounds (threshold_list: MAX_THRESHOLD, THRESHOLD_FALLBACK).
const (
	maxThreshold      = 100_000
	thresholdFallback = 50
)

// optionalColor is color_value_widget: the color-value control over an
// optional color, an unset one showing as auto and every pick setting
// it.
func optionalColor(k *kit, s slot) control {
	return newColorValueEditor(k, slot{
		get: func() any {
			if v := s.get(); v != nil {
				return v
			}
			return colorAuto
		},
		set: s.set, unset: s.unset,
	})
}

// thresholdList is threshold_list::threshold_list.
var thresholdList = cardRow(cardSpec{
	classes: []string{"threshold-editor"}, listCls: "threshold-list", rowClass: "threshold-row", labelCls: "threshold-label",
	blank: func() map[string]any { return map[string]any{} },
	title: thresholdTitle,
	fields: []cardField{
		{"above", "settings-threshold-above", thresholdBound},
		{"below", "settings-threshold-below", thresholdBound},
		{"icon-color", "settings-threshold-icon-color", optionalColor},
		{"label-color", "settings-threshold-label-color", optionalColor},
		{"icon-bg-color", "settings-threshold-icon-bg-color", optionalColor},
		{"button-bg-color", "settings-threshold-button-bg-color", optionalColor},
		{"border-color", "settings-threshold-border-color", optionalColor},
	},
})

// thresholdBound is the override switch and spin over an optional
// bound (optional_number_f64_widget at whole numbers).
func thresholdBound(k *kit, s slot) control {
	return newOptionalNumber(k, s, 0, maxThreshold, 1, thresholdFallback)
}

// thresholdTitle is card_title: the range the entry covers, or its
// number.
func thresholdTitle(item map[string]any, number int) string {
	num := func(v any) (string, bool) {
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case int64:
			f = float64(n)
		default:
			return "", false
		}
		if f == math.Trunc(f) {
			return strconv.FormatInt(int64(f), 10), true
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	above, hasAbove := num(item["above"])
	below, hasBelow := num(item["below"])
	switch {
	case hasAbove && hasBelow:
		return above + " – " + below
	case hasAbove:
		return "≥ " + above
	case hasBelow:
		return "≤ " + below
	}
	return i18n.Settings().Get("settings-threshold-card-title") + " " + strconv.Itoa(number)
}

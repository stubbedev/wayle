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
// the editor over the field's slot; or, for a control over several
// keys of the item, item over the item's field slots. fill gives the
// control the row's spare width instead of the label.
type cardField struct {
	key, label string
	editor     func(k *kit, s slot) control
	item       func(k *kit, field func(key string) slot) control
	fill       bool
}

// cardChrome is a card list's own card markup: the card, header and
// body classes and the remove button's (card_form's by default).
type cardChrome struct {
	card, header, body string
	remove             []string
	// removeKey is the remove button's tooltip ("" for none).
	removeKey string
}

var cardFormChrome = cardChrome{
	card: "card-form-card", header: "card-form-header", body: "card-form-body",
	remove: []string{"list-control-button", "list-control-remove"}, removeKey: "settings-list-remove",
}

// cardSpec shapes a card list: the card's header (a title for the
// item, or an identity field beside its label), its body fields, the
// new item, what of the cards is stored (keep filters, clean tidies
// each written item), the key duplicates of which are an error, and
// the container classes.
type cardSpec struct {
	title    func(item map[string]any, number int) string
	identity *cardField
	fields   []cardField
	blank    func() map[string]any
	keep     func(item map[string]any) bool
	clean    func(item map[string]any) map[string]any
	unique   string
	// addKey labels the add button's tooltip (settings-list-add).
	addKey   string
	chrome   *cardChrome
	classes  []string
	listCls  string
	rowClass string
	labelCls string
}

// cardList holds its cards as the source of truth (local), writing the
// kept, cleaned items; an outside change replaces them.
type cardList struct {
	*widget.Box
	k     *kit
	slot  slot
	spec  cardSpec
	list  *widget.Box
	local []map[string]any
	cards []*card
}

type card struct {
	title    *widget.Label
	identity control
	controls []control
}

func newCardList(k *kit, s slot, spec cardSpec) *cardList {
	c := &cardList{Box: widget.NewBox(widget.Column, 8, 0), k: k, slot: s, spec: spec}
	c.AddClass(append([]string{"card-form-editor"}, spec.classes...)...)
	c.list = widget.NewBox(widget.Column, 8, 0)
	c.list.AddClass(spec.listCls)
	c.Append(c.list, false)
	addKey := spec.addKey
	if addKey == "" {
		addKey = "settings-list-add"
	}
	add := listButton(k, "ld-plus-symbolic", addKey, "list-control-add", func() {
		c.local = append(c.local, spec.blank())
		c.commit()
		c.rebuild()
	})
	c.AppendAligned(add, false, widget.AlignStart)
	c.local = c.stored()
	c.rebuild()
	return c
}

// stored are the slot's items, plain.
func (c *cardList) stored() []map[string]any {
	v, _ := c.slot.get().([]any)
	out := make([]map[string]any, 0, len(v))
	for _, it := range v {
		m, _ := it.(map[string]any)
		out = append(out, maps.Clone(m))
	}
	return out
}

// written is what the cards store: the kept items, cleaned.
func (c *cardList) written() []any {
	out := make([]any, 0, len(c.local))
	for _, m := range c.local {
		if c.spec.keep != nil && !c.spec.keep(m) {
			continue
		}
		if c.spec.clean != nil {
			m = c.spec.clean(maps.Clone(m))
		}
		out = append(out, m)
	}
	return out
}

func (c *cardList) commit() {
	_ = c.slot.set(c.written())
	c.validate()
}

// fieldSlot is the slot of one key of card i: unset removes the key.
func (c *cardList) fieldSlot(i int, key string) slot {
	write := func(v any, remove bool) {
		if i >= len(c.local) {
			return
		}
		if c.local[i] == nil {
			c.local[i] = map[string]any{}
		}
		if remove {
			delete(c.local[i], key)
		} else {
			c.local[i][key] = v
		}
		c.commit()
		c.retitle(i)
	}
	return slot{
		get: func() any {
			if i < len(c.local) {
				return c.local[i][key]
			}
			return nil
		},
		set:   func(v any) error { write(v, false); return nil },
		unset: func() { write(nil, true) },
	}
}

func (c *cardList) rebuild() {
	c.list.Clear()
	c.cards = nil
	for i := range c.local {
		c.appendCard(i)
	}
	c.validate()
}

// appendCard is card or card_titled with the fields in its body.
func (c *cardList) appendCard(i int) {
	t := i18n.Settings()
	chrome := cardFormChrome
	if c.spec.chrome != nil {
		chrome = *c.spec.chrome
	}
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass(chrome.card)
	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass(chrome.header)
	cd := &card{}
	if id := c.spec.identity; id != nil {
		header.AppendAligned(c.k.label(t.Get(id.label), c.spec.labelCls), false, widget.AlignCenter)
		cd.identity = id.editor(c.k, c.fieldSlot(i, id.key))
		header.AppendAligned(cd.identity, true, widget.AlignCenter)
	} else {
		cd.title = c.k.label("", "card-form-title")
		header.AppendAligned(cd.title, true, widget.AlignCenter)
	}
	remove := c.k.button(c.k.icon("ld-trash-2-symbolic"), func() {
		if i < len(c.local) {
			c.local = append(c.local[:i:i], c.local[i+1:]...)
			c.commit()
			c.rebuild()
		}
	}, chrome.remove...)
	if chrome.removeKey != "" {
		remove.SetTooltip(t.Get(chrome.removeKey))
	}
	header.AppendAligned(remove, false, widget.AlignCenter)
	body := widget.NewBox(widget.Column, 0, 0)
	body.AddClass(chrome.body)
	for _, f := range c.spec.fields {
		var ctl control
		if f.item != nil {
			ctl = f.item(c.k, func(key string) slot { return c.fieldSlot(i, key) })
		} else {
			ctl = f.editor(c.k, c.fieldSlot(i, f.key))
		}
		row := widget.NewBox(widget.Row, 8, 0)
		row.AddClass(c.spec.rowClass)
		row.AppendAligned(c.k.label(t.Get(f.label), c.spec.labelCls), !f.fill, widget.AlignCenter)
		row.AppendAligned(ctl, f.fill, widget.AlignCenter)
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
	if i < len(c.cards) && c.cards[i].title != nil && c.spec.title != nil {
		c.cards[i].title.SetText(c.spec.title(c.local[i], i+1))
	}
}

// validate marks the identity of every card whose unique key repeats
// another's (the duplicate preset id): the error class and tooltip.
func (c *cardList) validate() {
	if c.spec.unique == "" {
		return
	}
	counts := map[string]int{}
	for _, m := range c.local {
		if s, _ := m[c.spec.unique].(string); s != "" {
			counts[s]++
		}
	}
	for i, cd := range c.cards {
		w, ok := cd.identity.(interface {
			AddClass(...string)
			RemoveClass(...string)
			SetTooltip(string)
		})
		if !ok || i >= len(c.local) {
			continue
		}
		s, _ := c.local[i][c.spec.unique].(string)
		if s != "" && counts[s] > 1 {
			w.AddClass("error")
			w.SetTooltip(i18n.Settings().Get("settings-toast-preset-id-duplicate"))
		} else {
			w.RemoveClass("error")
			w.SetTooltip("")
		}
	}
}

// refresh follows the store (the watcher): with as many items as the
// cards keep, those cards take the stored values in place; with
// another count the cards are replaced.
func (c *cardList) refresh() {
	stored := c.stored()
	var kept []int
	for i, m := range c.local {
		if c.spec.keep == nil || c.spec.keep(m) {
			kept = append(kept, i)
		}
	}
	if len(kept) != len(stored) {
		c.local = stored
		c.rebuild()
		return
	}
	for j, i := range kept {
		c.local[i] = stored[j]
	}
	for i, cd := range c.cards {
		if cd.identity != nil {
			cd.identity.refresh()
		}
		for _, ctl := range cd.controls {
			ctl.refresh()
		}
		c.retitle(i)
	}
	c.validate()
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
		{key: "above", label: "settings-threshold-above", editor: thresholdBound},
		{key: "below", label: "settings-threshold-below", editor: thresholdBound},
		{key: "icon-color", label: "settings-threshold-icon-color", editor: optionalColor},
		{key: "label-color", label: "settings-threshold-label-color", editor: optionalColor},
		{key: "icon-bg-color", label: "settings-threshold-icon-bg-color", editor: optionalColor},
		{key: "button-bg-color", label: "settings-threshold-button-bg-color", editor: optionalColor},
		{key: "border-color", label: "settings-threshold-border-color", editor: optionalColor},
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

// liveText is card_form::entry: an entry writing as it is typed.
type liveText struct {
	*widget.Entry
	slot    slot
	syncing bool
}

func newLiveText(k *kit, s slot, placeholder string) *liveText {
	c := &liveText{Entry: k.entry(), slot: s}
	c.SetPlaceholder(placeholder)
	c.OnChanged = func(text string) {
		if !c.syncing {
			_ = c.slot.set(text)
		}
	}
	c.refresh()
	return c
}

func (c *liveText) refresh() {
	if text := c.slot.text(); text != c.Text() {
		c.syncing = true
		c.SetText(text)
		c.syncing = false
	}
}

// toastPresetList is toast_preset_list: a card per preset with its id
// in the header (a duplicate one flagged), the label and the icon; a
// preset without an id is not stored, an empty label or icon is none.
var toastPresetList = cardRow(cardSpec{
	listCls: "card-form-list", rowClass: "card-form-row", labelCls: "card-form-label", unique: "id",
	identity: &cardField{key: "id", label: "settings-toast-preset-id", editor: func(k *kit, s slot) control { return newLiveText(k, s, "id") }},
	fields: []cardField{
		{key: "label", label: "settings-toast-preset-label", editor: func(k *kit, s slot) control { return newLiveText(k, s, "label") }},
		{key: "icon", label: "settings-toast-preset-icon", editor: func(k *kit, s slot) control { return newIconEditor(k, s) }},
	},
	blank: func() map[string]any { return map[string]any{"id": ""} },
	keep:  func(m map[string]any) bool { id, _ := m["id"].(string); return id != "" },
	clean: func(m map[string]any) map[string]any {
		for _, key := range []string{"label", "icon"} {
			if s, _ := m[key].(string); s == "" {
				delete(m, key)
			}
		}
		return m
	},
})

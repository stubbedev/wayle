package settings

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/transfer"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// The bar layout editor (editors/bar_layout): a card per monitor
// layout, its three zones holding module and group chips that drag
// between zones and cards, add from a searchable module picker, and
// write the whole layout list on every change.

// Layout zones, in the card's order.
const (
	zoneLeft = iota
	zoneCenter
	zoneRight
	zoneCount
)

// zoneNames are the zones' ids (ZoneId's Display), the drag payload's
// middle field.
var zoneNames = [zoneCount]string{"left", "center", "right"}

// zoneKeys are the zones' labels.
var zoneKeys = [zoneCount]string{"settings-layout-zone-left", "settings-layout-zone-center", "settings-layout-zone-right"}

// layoutDragMime is the chips' drag payload type (a GLib string).
const layoutDragMime = "text/plain;charset=utf-8"

// Group name entry width (chip/helpers: GROUP_NAME_WIDTH_CHARS,
// GROUP_NAME_MAX_WIDTH_CHARS).
const (
	groupNameChars    = 6
	groupNameMaxChars = 10
)

// modulePickerListPx caps the module picker's list.
const modulePickerListPx = 300

// layoutEditor is BarLayoutControl over bar.layout.
type layoutEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	list    *widget.Box
	layouts []config.BarLayout
}

func newLayoutEditor(k *kit, s slot) *layoutEditor {
	c := &layoutEditor{Box: widget.NewBox(widget.Column, 0, 0), k: k, slot: s}
	c.AddClass("bar-layout-control")
	c.list = widget.NewBox(widget.Column, 0, 0)
	c.list.AddClass("bar-layout-list")
	c.Append(c.list, false)
	add := listButton(k, "ld-plus-symbolic", "settings-layout-add", "list-control-add", func() {
		c.layouts = append(c.layouts, config.BarLayout{Show: true})
		c.commit()
		c.rebuild()
	})
	c.AppendAligned(add, false, widget.AlignStart)
	c.layouts = c.stored()
	c.rebuild()
	return c
}

// stored is the configured layout list, copied for editing.
func (c *layoutEditor) stored() []config.BarLayout {
	out := slices.Clone(c.k.store.svc.Config().Bar.Layout)
	for i := range out {
		out[i].Left = cloneItems(out[i].Left)
		out[i].Center = cloneItems(out[i].Center)
		out[i].Right = cloneItems(out[i].Right)
	}
	return out
}

func cloneItems(items []config.BarItem) []config.BarItem {
	out := slices.Clone(items)
	for i := range out {
		if g := out[i].Group; g != nil {
			out[i].Group = &config.BarGroup{Name: g.Name, Modules: slices.Clone(g.Modules)}
		}
	}
	return out
}

// commit writes the layouts.
func (c *layoutEditor) commit() { _ = c.slot.set(config.Encode(c.layouts)) }

// refresh (on_refresh) rebuilds when the stored layouts moved away
// from the cards.
func (c *layoutEditor) refresh() {
	if reflect.DeepEqual(config.Encode(c.layouts), config.Plain(c.slot.get())) {
		return
	}
	c.layouts = c.stored()
	c.rebuild()
}

// zone is layout i's zone z.
func (c *layoutEditor) zone(i, z int) *[]config.BarItem {
	l := &c.layouts[i]
	switch z {
	case zoneLeft:
		return &l.Left
	case zoneCenter:
		return &l.Center
	}
	return &l.Right
}

func (c *layoutEditor) rebuild() {
	c.list.Clear()
	for i := range c.layouts {
		c.list.Append(c.card(i), false)
	}
}

// card is LayoutCard: the monitor and extends entries, the show switch
// and the delete button over the zones (or the hidden note).
func (c *layoutEditor) card(i int) widget.Widget {
	t := i18n.Settings()
	l := &c.layouts[i]
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass("layout-card")
	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("layout-card-header")
	monitor := c.k.entry()
	monitor.AddClass("layout-monitor-input")
	monitor.SetPlaceholder("DP-1")
	monitor.SetText(l.Monitor)
	monitor.OnChanged = func(text string) {
		if c.layouts[i].Monitor != text {
			c.layouts[i].Monitor = text
			c.commit()
		}
	}
	extends := c.k.entry()
	extends.AddClass("layout-extends-input")
	extends.SetPlaceholder(t.Get("settings-layout-extends-none"))
	if l.Extends != nil {
		extends.SetText(*l.Extends)
	}
	extends.OnChanged = func(text string) {
		var v *string
		if text != "" {
			v = &text
		}
		if !reflect.DeepEqual(c.layouts[i].Extends, v) {
			c.layouts[i].Extends = v
			c.commit()
		}
	}
	show := widget.NewSwitch(l.Show)
	show.AddClass("layout-show-toggle")
	show.OnChanged = func(on bool) {
		if c.layouts[i].Show != on {
			c.layouts[i].Show = on
			c.commit()
			c.rebuild()
		}
	}
	remove := c.k.button(c.k.icon("ld-trash-2-symbolic"), func() {
		c.layouts = slices.Delete(c.layouts, i, i+1)
		c.commit()
		c.rebuild()
	}, "ghost-icon")
	header.AppendAligned(c.k.label(t.Get("settings-layout-monitor-label"), "layout-extends-label"), false, widget.AlignCenter)
	header.AppendAligned(monitor, false, widget.AlignCenter)
	header.AppendAligned(c.k.label(t.Get("settings-layout-extends-label"), "layout-extends-label"), false, widget.AlignCenter)
	header.AppendAligned(extends, false, widget.AlignCenter)
	header.Append(widget.NewSpacer(0, 0), true)
	header.AppendAligned(show, false, widget.AlignCenter)
	header.AppendAligned(remove, false, widget.AlignCenter)
	body := widget.NewBox(widget.Column, 0, 0)
	body.AddClass("layout-card-body")
	if !l.Show {
		body.Append(c.k.label(t.Get("settings-layout-hidden"), "layout-hidden-label"), false)
	} else {
		for z := range zoneCount {
			body.Append(c.zoneRow(i, z), false)
		}
	}
	root.Append(header, false)
	root.Append(body, false)
	return root
}

// zoneRow is build_zone_row: the label, the chips (the drop target),
// the add-module picker and the add-group button.
func (c *layoutEditor) zoneRow(i, z int) widget.Widget {
	t := i18n.Settings()
	row := widget.NewBox(widget.Row, 0, 0)
	row.AddClass("layout-zone")
	chips := &layoutZone{FlowBox: widget.NewFlowBox(4, 4), ed: c, card: i, zone: z}
	for k, item := range *c.zone(i, z) {
		chips.Append(c.chip(i, z, k, item))
	}
	frame := widget.NewBox(widget.Row, 0, 0)
	frame.AddClass("layout-zone-items")
	frame.AppendAligned(chips, false, widget.AlignCenter)
	var addModule *widget.Button
	addModule = c.k.button(c.k.icon("ld-plus-symbolic"), func() {
		c.pickModule(addModule, func(m config.BarModule) {
			items := c.zone(i, z)
			*items = append(*items, config.BarItem{Module: m})
			c.commit()
			c.rebuild()
		})
	}, "zone-add-btn")
	addModule.SetTooltip(t.Get("settings-layout-add-module"))
	addGroup := c.k.button(c.k.icon("ld-layers-symbolic"), func() {
		items := c.zone(i, z)
		*items = append(*items, config.BarItem{Group: &config.BarGroup{Name: t.Get("settings-layout-default-group")}})
		c.commit()
		c.rebuild()
	}, "zone-add-btn")
	addGroup.SetTooltip(t.Get("settings-layout-add-group"))
	row.AppendAligned(c.k.label(t.Get(zoneKeys[z]), "layout-zone-label"), false, widget.AlignCenter)
	row.Append(frame, true)
	row.AppendAligned(addModule, false, widget.AlignCenter)
	row.AppendAligned(addGroup, false, widget.AlignCenter)
	return row
}

// chip is Chip: a module's name, or a group's name entry with its
// modules, either removable and dragged by its body.
func (c *layoutEditor) chip(i, z, k int, item config.BarItem) widget.Widget {
	root := &layoutChip{Box: widget.NewBox(widget.Row, 0, 0), payload: dragPayload{i, z, k}}
	removeItem := func() {
		items := c.zone(i, z)
		*items = slices.Delete(*items, k, k+1)
		c.commit()
		c.rebuild()
	}
	if item.Group == nil {
		root.AddClass("module-chip")
		root.AppendAligned(c.k.label(string(item.Module)), false, widget.AlignCenter)
		root.AppendAligned(c.chipButton("ld-x-symbolic", "chip-remove", removeItem), false, widget.AlignCenter)
		return root
	}
	root.AddClass("group-chip")
	name := c.k.entry()
	name.AddClass("group-name-entry")
	name.SetWidthChars(groupNameChars, groupNameMaxChars)
	name.SetText(item.Group.Name)
	name.OnChanged = func(text string) {
		if g := (*c.zone(i, z))[k].Group; g != nil && g.Name != text {
			g.Name = text
			c.commit()
		}
	}
	root.AppendAligned(name, false, widget.AlignCenter)
	for m, mod := range item.Group.Modules {
		sub := widget.NewBox(widget.Row, 0, 0)
		sub.AddClass("module-chip")
		sub.AppendAligned(c.k.label(string(mod.Module)), false, widget.AlignCenter)
		sub.AppendAligned(c.chipButton("ld-x-symbolic", "chip-remove", func() {
			g := (*c.zone(i, z))[k].Group
			g.Modules = slices.Delete(g.Modules, m, m+1)
			c.commit()
			c.rebuild()
		}), false, widget.AlignCenter)
		root.AppendAligned(sub, false, widget.AlignCenter)
	}
	var add *widget.Button
	add = c.chipButton("ld-plus-symbolic", "chip-add", func() {
		c.pickModule(add, func(mod config.BarModule) {
			g := (*c.zone(i, z))[k].Group
			g.Modules = append(g.Modules, config.BarItem{Module: mod})
			c.commit()
			c.rebuild()
		})
	})
	root.AppendAligned(add, false, widget.AlignCenter)
	root.AppendAligned(c.chipButton("ld-x-symbolic", "chip-remove", removeItem), false, widget.AlignCenter)
	return root
}

func (c *layoutEditor) chipButton(icon, class string, onClick func()) *widget.Button {
	return c.k.button(c.k.icon(icon), onClick, class)
}

// moduleNames are the picker's modules: the built-ins, then a
// custom-<id> per custom module (all_module_names).
func (c *layoutEditor) moduleNames() []string {
	names := make([]string, 0, len(config.BuiltinModules))
	for _, m := range config.BuiltinModules {
		names = append(names, string(m))
	}
	for _, m := range c.k.store.svc.Config().Custom {
		names = append(names, "custom-"+m.Id)
	}
	return names
}

// pickModule opens the module picker (module_picker) beside anchor.
func (c *layoutEditor) pickModule(anchor widget.Widget, pick func(config.BarModule)) *searchPicker {
	if c.k.pickers == nil {
		return nil
	}
	return openSearchPicker(c.k, anchor, c.moduleNames(), pickerSpec{
		class: "module-picker-popover", listClass: "module-picker-list", scrollClass: "module-picker-scroll",
		placeholder: "settings-layout-search", maxH: modulePickerListPx,
		row: func(k *kit, name string) widget.Widget {
			return k.label(name, "module-picker-item")
		},
	}, func(name string) {
		var m config.BarModule
		if m.UnmarshalConfig(name) == nil {
			pick(m)
		}
	})
}

// dragPayload is DragPayload: where a dragged chip came from.
type dragPayload struct{ card, zone, item int }

// encode is DragPayload::encode: card:zone:item.
func (p dragPayload) encode() string {
	return fmt.Sprintf("%d:%s:%d", p.card, zoneNames[p.zone], p.item)
}

// decodePayload is DragPayload::decode.
func decodePayload(s string) (dragPayload, bool) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return dragPayload{}, false
	}
	card, err1 := strconv.Atoi(parts[0])
	item, err2 := strconv.Atoi(parts[2])
	zone := slices.Index(zoneNames[:], parts[1])
	if err1 != nil || err2 != nil || zone < 0 {
		return dragPayload{}, false
	}
	return dragPayload{card, zone, item}, true
}

// layoutChip is a chip's root: the drag source (attach_drag_source),
// marked chip-dragging while it travels.
type layoutChip struct {
	*widget.Box
	payload dragPayload
}

// DragContent implements widget.DragSource.
func (c *layoutChip) DragContent() *widget.DragContent {
	c.AddClass("chip-dragging")
	return &widget.DragContent{
		Mimes: []string{layoutDragMime},
		Write: func(_ string, w io.Writer) error {
			_, err := io.WriteString(w, c.payload.encode())
			return err
		},
		OnDone: func(transfer.Action) { c.RemoveClass("chip-dragging") },
	}
}

// layoutZone is a zone's chips, the drop target (attach_drop_target):
// hovering marks where the chip would land, dropping moves it there.
type layoutZone struct {
	*widget.FlowBox
	ed         *layoutEditor
	card, zone int
}

// DragEnter implements widget.DragEnterer: a chip's payload is taken.
func (z *layoutZone) DragEnter(mimes []string, p widget.Point) string {
	if !slices.Contains(mimes, layoutDragMime) {
		return ""
	}
	z.DragHover(p)
	return layoutDragMime
}

// DragHover implements widget.DragHoverer (highlight_drop_position).
func (z *layoutZone) DragHover(p widget.Point) {
	z.clearMarks()
	pos := z.dropPosition(p)
	if c := z.ChildAt(pos); c != nil {
		c.AddClass("drop-before")
	} else if c := z.ChildAt(z.Len() - 1); c != nil {
		c.AddClass("drop-after")
	}
}

// DragLeave implements widget.DragLeaver.
func (z *layoutZone) DragLeave() { z.clearMarks() }

func (z *layoutZone) clearMarks() {
	for i := range z.Len() {
		z.ChildAt(i).RemoveClass("drop-before", "drop-after")
	}
}

// Drop implements widget.Dropper.
func (z *layoutZone) Drop(_ string, data []byte, p widget.Point) {
	z.clearMarks()
	from, ok := decodePayload(string(data))
	if !ok {
		return
	}
	z.ed.move(from, z.card, z.zone, z.dropPosition(p))
}

// dropPosition is compute_drop_position: before or after the chip
// under p by its center, else after the last chip of p's line, else
// the start when p is above every line.
func (z *layoutZone) dropPosition(p widget.Point) int {
	if i := z.IndexAt(p); i >= 0 {
		b := z.ChildAt(i).Bounds()
		if p.X < b.X+b.W/2 {
			return i
		}
		return i + 1
	}
	// Lines run top to bottom: the last chip starting at or above p
	// ends p's line.
	last := -1
	for i := range z.Len() {
		if z.ChildAt(i).Bounds().Y <= p.Y {
			last = i
		}
	}
	return last + 1
}

// move is handle_drop: take the item out of its zone and insert it at
// position in the target zone, a later position in the same zone
// shifting for the removal.
func (c *layoutEditor) move(from dragPayload, card, zone, position int) {
	if from.card < 0 || from.card >= len(c.layouts) || card < 0 || card >= len(c.layouts) {
		return
	}
	src := c.zone(from.card, from.zone)
	if from.item < 0 || from.item >= len(*src) {
		return
	}
	item := (*src)[from.item]
	*src = slices.Delete(*src, from.item, from.item+1)
	if from.card == card && from.zone == zone && from.item < position {
		position--
	}
	dst := c.zone(card, zone)
	position = min(max(position, 0), len(*dst))
	*dst = slices.Insert(*dst, position, item)
	c.commit()
	c.rebuild()
}

// layoutRow is bar_layout for a row.
var layoutRow = func(r *rowSpec) {
	r.fullWidth = true
	r.editor = func(k *kit, s slot, _ config.FieldMeta) control { return newLayoutEditor(k, s) }
}

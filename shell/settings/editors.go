package settings

import (
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/shell/widgets"
)

// editorFunc builds the control for the field at path.
type editorFunc func(k *kit, s slot, meta config.FieldMeta) control

// autoEditor is the control a field's type calls for, the editor the
// Rust page picks for it: toggle, enum_select, number_*, text, size,
// color, and the optional (inherit) forms.
func autoEditor(k *kit, s slot, meta config.FieldMeta) (control, error) {
	switch {
	case meta.Type == "Size":
		return newSizeEditor(k, s, 1), nil
	case meta.Type == "HexColor":
		return newColorEditor(k, s), nil
	case meta.Type == "ColorValue":
		return newColorValueEditor(k, s), nil
	case meta.Optional && meta.Kind == config.FieldEnum:
		return newOptionalEnum(k, s, meta), nil
	case meta.Optional && meta.Kind == config.FieldInt:
		return newOptionalNumber(k, s, meta.Min, meta.Max, 1, 0), nil
	}
	switch meta.Kind {
	case config.FieldBool:
		return newToggle(k, s), nil
	case config.FieldEnum:
		return newEnumSelect(k, s, meta), nil
	case config.FieldInt:
		return newNumber(k, s, meta.Min, meta.Max, 1, 0), nil
	case config.FieldFloat:
		// scale(): the only float the Rust pages edit without explicit
		// bounds is a ScaleFactor.
		return newNumber(k, s, meta.Min, meta.Max, 0.05, 2), nil
	case config.FieldText:
		return newText(k, s, meta.Optional), nil
	}
	return nil, fmt.Errorf("settings: a %s needs its own editor", meta.Type)
}

// toggle is ToggleControl: a switch writing the bool.
type toggle struct {
	*widget.Switch
	k       *kit
	slot    slot
	syncing bool
}

func newToggle(k *kit, s slot) *toggle {
	c := &toggle{Switch: widget.NewSwitch(false), k: k, slot: s}
	c.OnChanged = func(on bool) {
		if !c.syncing {
			_ = c.slot.set(on)
		}
	}
	c.refresh()
	return c
}

func (c *toggle) refresh() {
	on, _ := c.slot.get().(bool)
	c.syncing = true
	c.SetOn(on)
	c.syncing = false
}

// enumSelect is EnumSelectControl: a dropdown of the variants'
// labels, falling back to the raw value where a label is missing.
type enumSelect struct {
	*widget.Dropdown
	k        *kit
	slot     slot
	variants []string
	syncing  bool
}

func newEnumSelect(k *kit, s slot, meta config.FieldMeta) *enumSelect {
	c := &enumSelect{Dropdown: widget.NewDropdown(k.face, 14, enumLabels(meta), 0), k: k, slot: s, variants: meta.Variants}
	c.OnSelect = func(i int) {
		if !c.syncing && i >= 0 && i < len(c.variants) {
			_ = c.slot.set(c.variants[i])
		}
	}
	c.refresh()
	return c
}

// refresh selects the current value; an unknown one shows the first
// variant (variant_index_of).
func (c *enumSelect) refresh() {
	v, _ := c.slot.get().(string)
	c.syncing = true
	c.SetSelected(max(slices.Index(c.variants, v), 0))
	c.syncing = false
}

// number is NumberControl: a spin button without its +/- buttons.
type number struct {
	*widget.SpinButton
	k    *kit
	slot slot
	// whole writes integers: the field's value reads as one (a float
	// field takes floats whatever the shown digits).
	whole bool
}

func newNumber(k *kit, s slot, lo, hi, step float64, digits int) *number {
	c := &number{SpinButton: widget.NewSpinButton(k.face, 14, 0, lo, hi, step, digits), k: k, slot: s, whole: digits == 0}
	c.OnValueChanged = func(v float64) {
		if c.whole {
			_ = c.slot.set(int64(math.Round(v)))
			return
		}
		_ = c.slot.set(v)
	}
	c.refresh()
	return c
}

func (c *number) refresh() {
	switch v := c.slot.get().(type) {
	case int64:
		c.whole = true
		c.SetValue(float64(v))
	case float64:
		c.whole = false
		c.SetValue(v)
	}
}

// text is TextControl: an entry committing on Enter, with the
// "unsaved" badge up while the typed text is not committed. An
// optional field's empty text is no value (store.unset).
type text struct {
	*widget.Entry
	k        *kit
	slot     slot
	optional bool
	badge    *widget.Label
	syncing  bool
}

func newText(k *kit, s slot, optional bool) *text {
	t := i18n.Settings()
	c := &text{Entry: widget.NewEntry(k.face, 14, 0), k: k, slot: s, optional: optional}
	c.AddClass("setting-text-entry")
	c.badge = k.label(t.Get("settings-source-unsaved"), "badge-subtle", "warning")
	c.badge.SetVisible(false)
	c.OnChanged = func(string) {
		if !c.syncing {
			c.badge.SetVisible(true)
		}
	}
	c.OnActivate = func(s string) {
		if c.commit(s) {
			c.badge.SetVisible(false)
		}
	}
	c.refresh()
	return c
}

func (c *text) dirtyBadge() *widget.Label { return c.badge }

// commit writes the typed text; false when the field refused it (the
// badge stays up).
func (c *text) commit(s string) bool {
	if c.optional && s == "" {
		c.slot.unset()
		return true
	}
	return c.slot.set(s) == nil
}

func (c *text) refresh() {
	s, _ := c.slot.get().(string)
	c.syncing = true
	c.SetText(s)
	c.syncing = false
}

// fieldControl builds a row's control: the page's editor, or the one
// the field's type calls for. A path naming no field is a page bug.
func fieldControl(k *kit, spec rowSpec) control {
	meta, ok := config.Field(spec.path)
	if !ok {
		log.Panicf("settings: page row %q names no config field", spec.path)
	}
	if spec.editor != nil {
		return spec.editor(k, pathSlot(k.store, spec.path), meta)
	}
	c, err := autoEditor(k, pathSlot(k.store, spec.path), meta)
	if err != nil {
		log.Panicf("settings: %s: %v", spec.path, err)
	}
	return c
}

// slider is SliderControl: a debounced slider with its value label,
// writing whole numbers when the format shows no decimals.
type slider struct {
	*widgets.DebouncedSlider
	k    *kit
	slot slot
}

func newSlider(k *kit, s slot, lo, hi float64, whole bool, format func(float64) string) *slider {
	c := &slider{DebouncedSlider: widgets.NewRangedSlider(lo, hi, lo, k.face, 14, 0, k.invoke), k: k, slot: s}
	c.SetFormat(format)
	c.OnCommit = func(v float64) {
		if whole {
			_ = c.slot.set(int64(math.Round(v)))
			return
		}
		_ = c.slot.set(v)
	}
	c.refresh()
	return c
}

func (c *slider) refresh() {
	switch v := c.slot.get().(type) {
	case int64:
		c.Set(float64(v))
	case float64:
		c.Set(v)
	}
}

// normalized is slider::normalized: a 0-1 NormalizedF64, labeled "{:.2}".
var normalized = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newSlider(k, s, 0, 1, false, func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) })
})

// signedNormalized is slider::signed_normalized: a -1-1 value, labeled
// "{:.2}".
var signedNormalized = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newSlider(k, s, -1, 1, false, func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) })
})

// percentage is slider::percentage: a 0-100 Percentage, labeled
// "{:.0}%".
var percentage = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newSlider(k, s, 0, 100, true, func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + "%" })
})

// mountPoints is text_like over a StorageMountPoint: one path, or
// several shown comma-joined; the typed text splits at commas back
// into one path ("/" when empty) or a list.
var mountPoints = withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
	return newText(k, slot{
		get: func() any {
			switch v := s.get().(type) {
			case string:
				return v
			case []any:
				parts := make([]string, 0, len(v))
				for _, p := range v {
					if str, ok := p.(string); ok {
						parts = append(parts, str)
					}
				}
				return strings.Join(parts, ", ")
			}
			return ""
		},
		set: func(v any) error {
			text, _ := v.(string)
			var paths []any
			for p := range strings.SplitSeq(text, ",") {
				if p = strings.TrimSpace(p); p != "" {
					paths = append(paths, p)
				}
			}
			switch len(paths) {
			case 0:
				return s.set("/")
			case 1:
				return s.set(paths[0])
			}
			return s.set(paths)
		},
		unset: s.unset,
	}, false)
})

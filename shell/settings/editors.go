package settings

import (
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/shell/widgets"
)

// editorFunc builds the control for the field at path.
type editorFunc func(k *kit, path string, meta config.FieldMeta) control

// autoEditor is the control a field's type calls for, the editor the
// Rust page picks for it: toggle, enum_select, number_*, text, size,
// color, and the optional (inherit) forms.
func autoEditor(k *kit, path string, meta config.FieldMeta) (control, error) {
	switch {
	case meta.Type == "Size":
		return newSizeEditor(k, path, 1), nil
	case meta.Type == "HexColor":
		return newColorEditor(k, path), nil
	case meta.Optional && meta.Kind == config.FieldEnum:
		return newOptionalEnum(k, path, meta), nil
	case meta.Optional && meta.Kind == config.FieldInt:
		return newOptionalNumber(k, path, meta.Min, meta.Max, 1, 0, 0), nil
	}
	switch meta.Kind {
	case config.FieldBool:
		return newToggle(k, path), nil
	case config.FieldEnum:
		return newEnumSelect(k, path, meta), nil
	case config.FieldInt:
		return newNumber(k, path, meta.Min, meta.Max, 1, 0), nil
	case config.FieldFloat:
		// scale(): the only float the Rust pages edit without explicit
		// bounds is a ScaleFactor.
		return newNumber(k, path, meta.Min, meta.Max, 0.05, 2), nil
	case config.FieldText:
		return newText(k, path, meta.Optional), nil
	}
	return nil, fmt.Errorf("settings: %s (%s) needs its own editor", path, meta.Type)
}

// toggle is ToggleControl: a switch writing the bool.
type toggle struct {
	*widget.Switch
	k       *kit
	path    string
	syncing bool
}

func newToggle(k *kit, path string) *toggle {
	c := &toggle{Switch: widget.NewSwitch(false), k: k, path: path}
	c.OnChanged = func(on bool) {
		if !c.syncing {
			_ = k.store.set(path, on)
		}
	}
	c.refresh()
	return c
}

func (c *toggle) refresh() {
	on, _ := c.k.store.value(c.path).(bool)
	c.syncing = true
	c.SetOn(on)
	c.syncing = false
}

// enumSelect is EnumSelectControl: a dropdown of the variants'
// labels, falling back to the raw value where a label is missing.
type enumSelect struct {
	*widget.Dropdown
	k        *kit
	path     string
	variants []string
	syncing  bool
}

func newEnumSelect(k *kit, path string, meta config.FieldMeta) *enumSelect {
	c := &enumSelect{Dropdown: widget.NewDropdown(k.face, 14, enumLabels(meta), 0), k: k, path: path, variants: meta.Variants}
	c.OnSelect = func(i int) {
		if !c.syncing && i >= 0 && i < len(c.variants) {
			_ = k.store.set(path, c.variants[i])
		}
	}
	c.refresh()
	return c
}

// refresh selects the current value; an unknown one shows the first
// variant (variant_index_of).
func (c *enumSelect) refresh() {
	v, _ := c.k.store.value(c.path).(string)
	c.syncing = true
	c.SetSelected(max(slices.Index(c.variants, v), 0))
	c.syncing = false
}

// number is NumberControl: a spin button without its +/- buttons.
type number struct {
	*widget.SpinButton
	k      *kit
	path   string
	digits int
}

func newNumber(k *kit, path string, lo, hi, step float64, digits int) *number {
	c := &number{SpinButton: widget.NewSpinButton(k.face, 14, 0, lo, hi, step, digits), k: k, path: path, digits: digits}
	c.OnValueChanged = func(v float64) {
		if c.digits == 0 {
			_ = k.store.set(path, int64(math.Round(v)))
			return
		}
		_ = k.store.set(path, v)
	}
	c.refresh()
	return c
}

func (c *number) refresh() {
	switch v := c.k.store.value(c.path).(type) {
	case int64:
		c.SetValue(float64(v))
	case float64:
		c.SetValue(v)
	}
}

// text is TextControl: an entry committing on Enter, with the
// "unsaved" badge up while the typed text is not committed. An
// optional field's empty text is no value (store.unset).
type text struct {
	*widget.Entry
	k        *kit
	path     string
	optional bool
	badge    *widget.Label
	syncing  bool
}

func newText(k *kit, path string, optional bool) *text {
	t := i18n.Settings()
	c := &text{Entry: widget.NewEntry(k.face, 14, 0), k: k, path: path, optional: optional}
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
		c.k.store.unset(c.path)
		return true
	}
	return c.k.store.set(c.path, s) == nil
}

func (c *text) refresh() {
	s, _ := c.k.store.value(c.path).(string)
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
		return spec.editor(k, spec.path, meta)
	}
	c, err := autoEditor(k, spec.path, meta)
	if err != nil {
		log.Panic(err)
	}
	return c
}

// slider is SliderControl: a debounced slider with its value label,
// writing whole numbers when the format shows no decimals.
type slider struct {
	*widgets.DebouncedSlider
	k    *kit
	path string
}

func newSlider(k *kit, path string, lo, hi float64, whole bool, format func(float64) string) *slider {
	c := &slider{DebouncedSlider: widgets.NewRangedSlider(lo, hi, lo, k.face, 14, 0, k.invoke), k: k, path: path}
	c.SetFormat(format)
	c.OnCommit = func(v float64) {
		if whole {
			_ = k.store.set(path, int64(math.Round(v)))
			return
		}
		_ = k.store.set(path, v)
	}
	c.refresh()
	return c
}

func (c *slider) refresh() {
	switch v := c.k.store.value(c.path).(type) {
	case int64:
		c.Set(float64(v))
	case float64:
		c.Set(v)
	}
}

// percentage is slider::percentage: a 0-100 Percentage, labeled
// "{:.0}%".
var percentage = withEditor(func(k *kit, path string, _ config.FieldMeta) control {
	return newSlider(k, path, 0, 100, true, func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + "%" })
})

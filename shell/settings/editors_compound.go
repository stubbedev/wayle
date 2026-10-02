package settings

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// The editors built from several controls (editors/optional,
// editors/size, editors/surface_animation).

// enumLabels are the variants' labels: the Fluent key's text, or the
// raw value where the key is missing (variant_label).
func enumLabels(meta config.FieldMeta) []string {
	t := i18n.Settings()
	labels := make([]string, len(meta.Variants))
	for i, v := range meta.Variants {
		labels[i] = v
		if key := config.EnumLabelKey(meta.Type, v); t.Has(key) {
			labels[i] = t.Get(key)
		}
	}
	return labels
}

// optionalEnum is optional_enum_widget: "Inherit" (unset) first, then
// the variants.
type optionalEnum struct {
	*widget.Dropdown
	k        *kit
	slot     slot
	variants []string
	syncing  bool
}

func newOptionalEnum(k *kit, s slot, meta config.FieldMeta) *optionalEnum {
	labels := append([]string{i18n.Settings().Get("settings-inherit")}, enumLabels(meta)...)
	c := &optionalEnum{Dropdown: widget.NewDropdown(k.face, 14, labels, 0), k: k, slot: s, variants: meta.Variants}
	c.OnSelect = func(i int) {
		switch {
		case c.syncing:
		case i == 0:
			c.slot.unset()
		case i <= len(c.variants):
			_ = c.slot.set(c.variants[i-1])
		}
	}
	c.refresh()
	return c
}

func (c *optionalEnum) refresh() {
	v, _ := c.slot.get().(string)
	c.syncing = true
	c.SetSelected(slices.Index(c.variants, v) + 1)
	c.syncing = false
}

// optionalNumber is optional_number_widget: the override switch, and a
// spin button live only while it is on. Turning it on writes the
// spin's value (the fallback until one was set).
type optionalNumber struct {
	*widget.Box
	k       *kit
	slot    slot
	on      *widget.Switch
	spin    *widget.SpinButton
	syncing bool
}

func newOptionalNumber(k *kit, s slot, lo, hi, step, fallback float64) *optionalNumber {
	c := &optionalNumber{Box: widget.NewBox(widget.Row, 8, 0), k: k, slot: s}
	c.on = widget.NewSwitch(false)
	c.on.AddClass("inherit-switch")
	c.on.SetTooltip(i18n.Settings().Get("settings-override"))
	c.spin = widget.NewSpinButton(k.face, 14, 0, lo, hi, step, 0)
	c.spin.SetValue(fallback)
	c.AppendAligned(c.on, false, widget.AlignCenter)
	c.AppendAligned(c.spin, false, widget.AlignCenter)
	c.on.OnChanged = func(on bool) {
		c.spin.SetEnabled(on)
		if c.syncing {
			return
		}
		if on {
			c.write(c.spin.Value())
		} else {
			c.slot.unset()
		}
	}
	// The spin is disabled while the override is off: only a live one
	// writes.
	c.spin.OnValueChanged = c.write
	c.refresh()
	return c
}

// write stores a whole number (float fields take it as one).
func (c *optionalNumber) write(v float64) { _ = c.slot.set(int64(math.Round(v))) }

func (c *optionalNumber) refresh() {
	v := c.slot.get()
	c.syncing = true
	c.on.SetOn(v != nil)
	c.spin.SetEnabled(v != nil)
	switch n := v.(type) {
	case int64:
		c.spin.SetValue(float64(n))
	case float64:
		c.spin.SetValue(n)
	}
	c.syncing = false
}

// optionalSpin is number_u32_optional's row: the override switch and a
// spin over [lo, hi] stepping by step, starting at fallback.
func optionalSpin(lo, hi, step, fallback float64) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newOptionalNumber(k, s, lo, hi, step, fallback)
	})
}

// Size modes (editors/size).
const (
	sizeScale = iota
	sizePx
	// remBasePx is REM_BASE_PX, the rem a scale multiplies.
	remBasePx = 16.0
)

// sizeEditor is size_with_base: a scale-or-pixels dropdown and the
// spin. Switching the mode converts the value through the base (a
// scale of base rem), so the size stays put.
type sizeEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	basePx  float64
	mode    *widget.Dropdown
	spin    *widget.SpinButton
	syncing bool
}

func newSizeEditor(k *kit, s slot, baseRem float64) *sizeEditor {
	t := i18n.Settings()
	c := &sizeEditor{Box: widget.NewBox(widget.Row, 8, 0), k: k, slot: s, basePx: baseRem * remBasePx}
	c.mode = widget.NewDropdown(k.face, 14, []string{t.Get("settings-size-scale"), t.Get("settings-size-px")}, sizeScale)
	c.spin = widget.NewSpinButton(k.face, 14, 0, 0, 10000, 0.05, 2)
	c.AppendAligned(c.mode, false, widget.AlignCenter)
	c.AppendAligned(c.spin, false, widget.AlignCenter)
	c.mode.OnSelect = func(i int) {
		if c.syncing {
			return
		}
		v := c.spin.Value()
		if i == sizePx {
			v = math.Round(v * c.basePx)
		} else if c.basePx > 0 {
			v /= c.basePx
		}
		c.configure(i == sizePx, v)
		c.commit()
	}
	c.spin.OnValueChanged = func(float64) { c.commit() }
	c.refresh()
	return c
}

// configure sets the spin for a mode (configure_spin_for_mode): whole
// pixels, or a scale in hundredths, then shows v.
func (c *sizeEditor) configure(px bool, v float64) {
	if px {
		c.spin.SetStep(1)
		c.spin.SetDigits(0)
	} else {
		c.spin.SetStep(0.05)
		c.spin.SetDigits(2)
	}
	c.spin.SetValue(v)
}

func (c *sizeEditor) commit() {
	v := c.spin.Value()
	if c.mode.Selected() == sizePx {
		_ = c.slot.set(strconv.FormatFloat(math.Round(v), 'f', 0, 64) + "px")
		return
	}
	_ = c.slot.set(v)
}

// refresh shows the stored size: a number is a scale, a "<n>px" string
// pixels.
func (c *sizeEditor) refresh() {
	c.syncing = true
	defer func() { c.syncing = false }()
	switch v := c.slot.get().(type) {
	case float64:
		c.mode.SetSelected(sizeScale)
		c.configure(false, v)
	case int64:
		c.mode.SetSelected(sizeScale)
		c.configure(false, float64(v))
	case string:
		px, err := strconv.ParseFloat(strings.TrimSuffix(v, "px"), 64)
		if err != nil {
			return
		}
		c.mode.SetSelected(sizePx)
		c.configure(true, math.Round(px))
	}
}

// sizeBase is size_with_base for a row: the base rem a scale of 1 is.
func sizeBase(rem float64) rowOpt {
	return withEditor(func(k *kit, s slot, _ config.FieldMeta) control {
		return newSizeEditor(k, s, rem)
	})
}

// surfaceAnimationRows are surface_animation_rows for the [animations]
// surface at prefix: the optional enter and exit transitions and
// durations, each its own field.
func surfaceAnimationRows(prefix string) []rowSpec {
	duration := optionalSpin(0, maxDurationMS, durationStepMS, durationFallbackMS)
	return []rowSpec{
		field(prefix+".enter", withKey("settings-animations-enter")),
		field(prefix+".exit", withKey("settings-animations-exit")),
		field(prefix+".enter-duration", withKey("settings-animations-enter-duration"), duration),
		field(prefix+".exit-duration", withKey("settings-animations-exit-duration"), duration),
	}
}

// Animation duration bounds (pages/animations, editors/surface_animation).
const (
	maxDurationMS      = 100_000
	durationStepMS     = 10
	durationFallbackMS = 200
)

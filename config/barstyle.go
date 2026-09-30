package config

// The bar styling enums and the CSS mappings their consumers read,
// ported from crates/wayle-config/src/schemas/bar/types (mod.rs,
// shadow.rs) and crates/wayle-config/src/schemas/styling/types
// (rounding.rs, typography.rs).

// ShadowPreset is the bar's shadow style.
type ShadowPreset string

// Shadow presets.
const (
	ShadowNone     ShadowPreset = "none"
	ShadowDrop     ShadowPreset = "drop"
	ShadowFloating ShadowPreset = "floating"
)

// UnmarshalText decodes and validates a shadow preset.
func (s *ShadowPreset) UnmarshalText(text []byte) error {
	v, err := parseEnum(text, "shadow", ShadowNone, ShadowDrop, ShadowFloating)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// MarginPx is the margin the shadow needs to render unclipped.
func (s ShadowPreset) MarginPx() int {
	if s == ShadowDrop || s == ShadowFloating {
		return 4
	}
	return 0
}

// CSSShadow is the box-shadow value for a bar docked at location: a
// drop shadow falls away from the anchor edge, floating always below.
func (s ShadowPreset) CSSShadow(location Location) string {
	switch s {
	case ShadowDrop:
		switch location {
		case LocationBottom:
			return "0 -1px 2px 1px rgba(0, 0, 0, 0.25)"
		case LocationLeft:
			return "1px 0 2px 1px rgba(0, 0, 0, 0.25)"
		case LocationRight:
			return "-1px 0 2px 1px rgba(0, 0, 0, 0.25)"
		default: // top
			return "0 1px 2px 1px rgba(0, 0, 0, 0.25)"
		}
	case ShadowFloating:
		return "0 1px 2px 1px rgba(0, 0, 0, 0.25)"
	default:
		return "none"
	}
}

// OppositeMargin is the margin on the edge opposite the anchor, where
// the shadow extends.
func (s ShadowPreset) OppositeMargin() int { return s.MarginPx() }

// BarButtonVariant is the module button chrome.
type BarButtonVariant string

// Button variants.
const (
	ButtonVariantBasic       BarButtonVariant = "basic"
	ButtonVariantBlockPrefix BarButtonVariant = "block-prefix"
	ButtonVariantIconSquare  BarButtonVariant = "icon-square"
)

// UnmarshalText decodes and validates a button variant.
func (v *BarButtonVariant) UnmarshalText(text []byte) error {
	parsed, err := parseEnum(text, "button-variant", ButtonVariantBasic, ButtonVariantBlockPrefix, ButtonVariantIconSquare)
	if err != nil {
		return err
	}
	*v = parsed
	return nil
}

// CSSClass is the variant's class on the button ("block-prefix").
func (v BarButtonVariant) CSSClass() string { return string(v) }

// IconPosition places a button's icon relative to its label.
type IconPosition string

// Icon positions.
const (
	IconStart IconPosition = "start"
	IconEnd   IconPosition = "end"
)

// UnmarshalText decodes and validates an icon position.
func (p *IconPosition) UnmarshalText(text []byte) error {
	v, err := parseEnum(text, "button-icon-position", IconStart, IconEnd)
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// CSSClass is the class the position adds: "icon-end" for end, none
// (ok false) for the default start.
func (p IconPosition) CSSClass() (string, bool) {
	if p == IconEnd {
		return "icon-end", true
	}
	return "", false
}

// FontWeightClass is a typography weight backed by the --weight-*
// tokens.
type FontWeightClass string

// Font weights.
const (
	WeightNormal   FontWeightClass = "normal"
	WeightMedium   FontWeightClass = "medium"
	WeightSemibold FontWeightClass = "semibold"
	WeightBold     FontWeightClass = "bold"
)

// UnmarshalText decodes and validates a font weight.
func (w *FontWeightClass) UnmarshalText(text []byte) error {
	v, err := parseEnum(text, "font weight", WeightNormal, WeightMedium, WeightSemibold, WeightBold)
	if err != nil {
		return err
	}
	*w = v
	return nil
}

// CSSClass is the weight class ("weight-medium").
func (w FontWeightClass) CSSClass() string { return "weight-" + string(w) }

// CSSVar is the weight's variable name ("--weight-medium").
func (w FontWeightClass) CSSVar() string { return "--weight-" + string(w) }

// CSSClass is the border placement class ("border-top", "border-all");
// none has no class (ok false).
func (b BorderLocation) CSSClass() (string, bool) {
	if b == BorderNone {
		return "", false
	}
	return "border-" + string(b), true
}

// RoundingCSSValues are the var() references for one rounding level:
// Element for interactive elements, Container for surfaces.
type RoundingCSSValues struct {
	Element   string
	Container string
}

// CSSValues maps the level to the global --radius-* tokens; containers
// round one step larger for perceptual consistency (to_css_values).
func (r RoundingLevel) CSSValues() RoundingCSSValues {
	switch r {
	case RoundingSm:
		return RoundingCSSValues{"var(--radius-sm)", "var(--radius-md)"}
	case RoundingMd:
		return RoundingCSSValues{"var(--radius-md)", "var(--radius-lg)"}
	case RoundingLg:
		return RoundingCSSValues{"var(--radius-lg)", "var(--radius-xl)"}
	case RoundingFull:
		return RoundingCSSValues{"var(--radius-full)", "var(--radius-xl)"}
	default: // none
		return RoundingCSSValues{"var(--radius-none)", "var(--radius-none)"}
	}
}

// BarCSSValues maps the level to the bar's --bar-radius-* tokens
// (to_bar_css_values).
func (r RoundingLevel) BarCSSValues() RoundingCSSValues {
	return r.scopedCSSValues("--bar-radius-")
}

// BarElementCSSValues maps the level to the bar buttons' and groups'
// --bar-button-radius-* tokens (to_bar_element_css_values).
func (r RoundingLevel) BarElementCSSValues() RoundingCSSValues {
	return r.scopedCSSValues("--bar-button-radius-")
}

// scopedCSSValues is the shared shape of the bar rounding tables: none
// and full use the global tokens, the steps use prefix's, containers
// one step larger.
func (r RoundingLevel) scopedCSSValues(prefix string) RoundingCSSValues {
	step := func(size string) string { return "var(" + prefix + size + ")" }
	switch r {
	case RoundingSm:
		return RoundingCSSValues{step("sm"), step("md")}
	case RoundingMd:
		return RoundingCSSValues{step("md"), step("lg")}
	case RoundingLg:
		return RoundingCSSValues{step("lg"), step("xl")}
	case RoundingFull:
		return RoundingCSSValues{"var(--radius-full)", "var(--radius-full)"}
	default: // none
		return RoundingCSSValues{"var(--radius-none)", "var(--radius-none)"}
	}
}

// ScaleValue is the multiplier of a scale size; ok is false for pixels
// (Size::scale_value).
func (s Size) ScaleValue() (float64, bool) {
	return s.Value, s.Unit == SizeMultiplier
}

// PxValue is the length of a pixel size; ok is false for a multiplier
// (Size::px_value).
func (s Size) PxValue() (float64, bool) {
	return s.Value, s.Unit == SizePixels
}

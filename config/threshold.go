package config

import (
	"errors"
	"fmt"
)

// ThresholdEntry maps a numeric range to color overrides
// (styling/types/threshold.rs ThresholdEntry): above and below are
// inclusive bounds, both set means AND, and at least one must be set;
// each color is optional and overrides one part of the button.
type ThresholdEntry struct {
	Above *float64
	Below *float64

	IconColor     *ColorValue
	LabelColor    *ColorValue
	IconBgColor   *ColorValue
	ButtonBgColor *ColorValue
	BorderColor   *ColorValue
}

// Matches reports whether value falls in the entry's range (value >=
// above and value <= below).
func (t ThresholdEntry) Matches(value float64) bool {
	if t.Above == nil && t.Below == nil {
		return false
	}
	if t.Above != nil && value < *t.Above {
		return false
	}
	if t.Below != nil && value > *t.Below {
		return false
	}
	return true
}

// ThresholdColors are the overrides evaluate_thresholds resolves; a
// nil color keeps the module's own.
type ThresholdColors struct {
	IconColor     *ColorValue
	LabelColor    *ColorValue
	IconBgColor   *ColorValue
	ButtonBgColor *ColorValue
	BorderColor   *ColorValue
}

// EvaluateThresholds is evaluate_thresholds: every matching entry in
// order overlays the colors it sets, so the last match wins per color.
func EvaluateThresholds(value float64, entries []ThresholdEntry) ThresholdColors {
	var out ThresholdColors
	for _, e := range entries {
		if !e.Matches(value) {
			continue
		}
		for _, c := range []struct {
			src *ColorValue
			dst **ColorValue
		}{
			{e.IconColor, &out.IconColor},
			{e.LabelColor, &out.LabelColor},
			{e.IconBgColor, &out.IconBgColor},
			{e.ButtonBgColor, &out.ButtonBgColor},
			{e.BorderColor, &out.BorderColor},
		} {
			if c.src != nil {
				cv := *c.src // a copy: callers may not alias the config
				*c.dst = &cv
			}
		}
	}
	return out
}

// UnmarshalTOML decodes one [[thresholds]] table. An entry with
// neither bound, a non-numeric bound, or an unknown color is a load
// error rather than an entry that silently never matches.
func (t *ThresholdEntry) UnmarshalTOML(value any) error {
	table, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("threshold: want a table, got %T", value)
	}
	*t = ThresholdEntry{}
	for key, raw := range table {
		switch key {
		case "above", "below":
			var f float64
			switch n := raw.(type) {
			case int64:
				f = float64(n)
			case float64:
				f = n
			default:
				return fmt.Errorf("threshold: %s must be a number, got %v", key, raw)
			}
			if key == "above" {
				t.Above = &f
			} else {
				t.Below = &f
			}
		case "icon-color", "label-color", "icon-bg-color", "button-bg-color", "border-color":
			s, ok := raw.(string)
			if !ok {
				return fmt.Errorf("threshold: %s must be a string", key)
			}
			cv, err := ParseColorValue(s)
			if err != nil {
				return fmt.Errorf("threshold: %s: %w", key, err)
			}
			switch key {
			case "icon-color":
				t.IconColor = &cv
			case "label-color":
				t.LabelColor = &cv
			case "icon-bg-color":
				t.IconBgColor = &cv
			case "button-bg-color":
				t.ButtonBgColor = &cv
			default:
				t.BorderColor = &cv
			}
		}
	}
	if t.Above == nil && t.Below == nil {
		return errors.New("threshold: needs above or below")
	}
	return nil
}

// IsEmpty reports whether no slot is overridden.
func (c ThresholdColors) IsEmpty() bool {
	return c.IconColor == nil && c.LabelColor == nil && c.IconBgColor == nil &&
		c.ButtonBgColor == nil && c.BorderColor == nil
}

// ResolveOr is one slot's CSS: the override's value when set, else the
// module's configured color already resolved for the theme
// (ThresholdColors::resolve_or).
func ResolveOr(override *ColorValue, configCSS string) string {
	if override != nil {
		return override.ToCSS()
	}
	return configCSS
}

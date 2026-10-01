package config

// ThresholdEntry maps a numeric range to color overrides
// (crates/wayle-config/src/schemas/styling/types/threshold.rs).
//
// A threshold entry that maps a numeric value range to color overrides.
//
// At least one of `above` or `below` must be set. When both are set,
// both conditions must be satisfied (AND logic).
//
// ## TOML Example
//
// ```toml
// [[modules.cpu.thresholds]]
// above = 70
// icon-color = "status-warning"
// label-color = "status-warning"
//
// [[modules.cpu.thresholds]]
// above = 90
// icon-color = "status-error"
// label-color = "status-error"
// ```
type ThresholdEntry struct {
	// Activate when metric value >= this threshold.
	Above *float64 `cfg:"above"`
	// Activate when metric value <= this threshold.
	Below *float64 `cfg:"below"`
	// Override icon color when threshold is active.
	IconColor *ColorValue `cfg:"icon-color"`
	// Override label color when threshold is active.
	LabelColor *ColorValue `cfg:"label-color"`
	// Override icon background color when threshold is active.
	IconBgColor *ColorValue `cfg:"icon-bg-color"`
	// Override button background color when threshold is active.
	ButtonBgColor *ColorValue `cfg:"button-bg-color"`
	// Override border color when threshold is active.
	BorderColor *ColorValue `cfg:"border-color"`
}

// Matches reports whether value falls in the entry's range: both set
// bounds hold (inclusive), and an entry with neither bound never
// matches.
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

// ThresholdColors is the merged override set of every matching entry;
// a nil slot keeps the module's configured color.
type ThresholdColors struct {
	IconColor     *ColorValue
	LabelColor    *ColorValue
	IconBgColor   *ColorValue
	ButtonBgColor *ColorValue
	BorderColor   *ColorValue
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

// EvaluateThresholds walks the entries in order; for each color slot
// the last matching entry that sets it wins (evaluate_thresholds). The
// slots are copies, so a caller cannot alias the config.
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
				cv := *c.src
				*c.dst = &cv
			}
		}
	}
	return out
}

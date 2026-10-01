package config

import "math"

// DropdownsConfig is the [dropdowns] section
// (crates/wayle-config/src/schemas/dropdowns/mod.rs).
//
// Per-dropdown foldout panel sizing.
//
// Each field overrides the size of one bar widget dropdown. Unset fields keep
// the built-in default (scaled by the global scale).
type DropdownsConfig struct {
	// Audio dropdown panel size.
	Audio DropdownSize `cfg:"audio"`
	// Battery dropdown panel size.
	Battery DropdownSize `cfg:"battery"`
	// Bluetooth dropdown panel size.
	Bluetooth DropdownSize `cfg:"bluetooth"`
	// Brightness dropdown panel size. Height grows to fit content.
	Brightness DropdownSize `cfg:"brightness"`
	// Calendar dropdown panel size. Height grows to fit content.
	Calendar DropdownSize `cfg:"calendar"`
	// Dashboard dropdown panel size. Height grows to fit content.
	Dashboard DropdownSize `cfg:"dashboard"`
	// Mail dropdown panel size. Height grows to fit content.
	Mail DropdownSize `cfg:"mail"`
	// Media dropdown panel size.
	Media DropdownSize `cfg:"media"`
	// Network dropdown panel size.
	Network DropdownSize `cfg:"network"`
	// Notification dropdown panel size.
	Notification DropdownSize `cfg:"notification"`
	// Treeman dropdown panel size.
	Treeman DropdownSize `cfg:"treeman"`
	// Weather dropdown panel size.
	Weather DropdownSize `cfg:"weather"`
}

// ByName returns the size override of the named dropdown (the
// dropdown:<name> identifiers); ok is false for a dropdown without a
// sizing key.
func (d DropdownsConfig) ByName(name string) (DropdownSize, bool) {
	switch name {
	case "audio":
		return d.Audio, true
	case "battery":
		return d.Battery, true
	case "bluetooth":
		return d.Bluetooth, true
	case "brightness":
		return d.Brightness, true
	case "calendar":
		return d.Calendar, true
	case "dashboard":
		return d.Dashboard, true
	case "mail":
		return d.Mail, true
	case "media":
		return d.Media, true
	case "network":
		return d.Network, true
	case "notification":
		return d.Notification, true
	case "treeman":
		return d.Treeman, true
	case "weather":
		return d.Weather, true
	}
	return DropdownSize{}, false
}

// DropdownSize is one panel's size override
// (crates/wayle-config/src/schemas/dropdowns/types.rs).
//
// Size override for a dropdown foldout panel.
//
// Each field is optional: when unset the dropdown keeps its built-in default
// size (scaled by the global scale). A [`Size`] may be a scale multiplier of
// the built-in base (e.g. `1.5`) or an absolute pixel length (e.g. `"480px"`).
type DropdownSize struct {
	// Panel width override. Unset uses the built-in default width.
	Width *Size `cfg:"width"`
	// Panel height override. Unset uses the built-in default height. Has no
	// effect on dropdowns whose height grows to fit their content.
	Height *Size `cfg:"height"`
}

func (DropdownSize) configValue() {}

// ResolveDimension applies an optional override to a built-in base
// size in pixels: unset scales the base, a multiplier scales it
// further, pixels replace it (wayle-shell-core bar::resolve_dimension).
func ResolveDimension(override *Size, base, scale float64) int {
	if override == nil {
		return int(math.Round(base * scale))
	}
	return int(math.Round(override.ResolvePx(base, scale)))
}

// ResolveContentHeight is the height of a dropdown that sizes to its
// content: -1 (natural size) unless an absolute pixel override is set,
// since a multiplier has no base to scale (resolve_content_height).
func ResolveContentHeight(override *Size) int {
	if override == nil || override.Unit != SizePixels {
		return -1
	}
	return int(math.Round(float64(override.Value)))
}

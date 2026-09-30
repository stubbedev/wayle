package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// SharePickerPage is the page the share picker opens on.
type SharePickerPage int

// The picker pages, in notebook order.
const (
	SharePickerWindows SharePickerPage = iota
	SharePickerOutputs
	SharePickerRegion
)

// String is the config spelling.
func (p SharePickerPage) String() string {
	switch p {
	case SharePickerOutputs:
		return "outputs"
	case SharePickerRegion:
		return "region"
	}
	return "windows"
}

// parseSharePickerPage reads the kebab-case page name.
func parseSharePickerPage(s string) (SharePickerPage, error) {
	switch s {
	case "windows":
		return SharePickerWindows, nil
	case "outputs":
		return SharePickerOutputs, nil
	case "region":
		return SharePickerRegion, nil
	}
	return 0, fmt.Errorf("share-picker: default-page %q is not windows, outputs, or region", s)
}

// The rem bases the share picker's Size multipliers resolve against
// (share_picker/mod.rs): Scale(1) is the default pixel size.
const (
	SharePickerWidthBaseRem          = 62.5
	SharePickerHeightBaseRem         = 31.25
	SharePickerWidgetBaseRem         = 9.375
	SharePickerWindowsSpacingBaseRem = 0.75
	SharePickerOutputsSpacingBaseRem = 0.375
)

// SharePickerConfig is [share-picker]: the screencast source picker the
// portal pops.
type SharePickerConfig struct {
	DefaultPage      SharePickerPage
	HideTokenRestore bool
	// Width and Height size the picker: multipliers of 1000x500 px, or
	// absolute pixels.
	Width, Height Size
	// ResizeSize caps every preview frame's smaller side, in pixels.
	ResizeSize int
	// WidgetSize is a card preview's height (multiplier of 150 px).
	WidgetSize Size
	// WindowsSpacing separates window cards (multiplier of 12 px).
	WindowsSpacing                     Size
	WindowsMinPerRow, WindowsMaxPerRow int
	// OutputsSpacing separates output cards, per side (multiplier of
	// 6 px).
	OutputsSpacing        Size
	OutputsShowLabel      bool
	OutputsRespectScaling bool
}

// DefaultsSharePicker returns the schema defaults.
func DefaultsSharePicker() SharePickerConfig {
	one := Size{Value: 1, Unit: SizeMultiplier}
	return SharePickerConfig{
		DefaultPage:           SharePickerWindows,
		Width:                 one,
		Height:                one,
		ResizeSize:            640,
		WidgetSize:            one,
		WindowsSpacing:        one,
		WindowsMinPerRow:      3,
		WindowsMaxPerRow:      4,
		OutputsSpacing:        one,
		OutputsRespectScaling: true,
	}
}

// applySharePicker overlays [share-picker].
func applySharePicker(md toml.MetaData, prim toml.Primitive) (SharePickerConfig, error) {
	cfg := DefaultsSharePicker()
	var doc struct {
		DefaultPage           *string    `toml:"default-page"`
		HideTokenRestore      *bool      `toml:"hide-token-restore"`
		Width                 *tomlValue `toml:"width"`
		Height                *tomlValue `toml:"height"`
		ResizeSize            *int64     `toml:"resize-size"`
		WidgetSize            *tomlValue `toml:"widget-size"`
		WindowsSpacing        *tomlValue `toml:"windows-spacing"`
		WindowsMinPerRow      *int64     `toml:"windows-min-per-row"`
		WindowsMaxPerRow      *int64     `toml:"windows-max-per-row"`
		OutputsSpacing        *tomlValue `toml:"outputs-spacing"`
		OutputsShowLabel      *bool      `toml:"outputs-show-label"`
		OutputsRespectScaling *bool      `toml:"outputs-respect-scaling"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.DefaultPage != nil {
		page, err := parseSharePickerPage(*doc.DefaultPage)
		if err != nil {
			return cfg, err
		}
		cfg.DefaultPage = page
	}
	if doc.HideTokenRestore != nil {
		cfg.HideTokenRestore = *doc.HideTokenRestore
	}
	for _, s := range []struct {
		raw    *tomlValue
		target *Size
		key    string
	}{
		{doc.Width, &cfg.Width, "width"},
		{doc.Height, &cfg.Height, "height"},
		{doc.WidgetSize, &cfg.WidgetSize, "widget-size"},
		{doc.WindowsSpacing, &cfg.WindowsSpacing, "windows-spacing"},
		{doc.OutputsSpacing, &cfg.OutputsSpacing, "outputs-spacing"},
	} {
		if s.raw == nil {
			continue
		}
		if err := s.target.unmarshal(s.raw.value, s.key); err != nil {
			return cfg, fmt.Errorf("share-picker: %w", err)
		}
	}
	for _, n := range []struct {
		raw    *int64
		target *int
		key    string
	}{
		{doc.ResizeSize, &cfg.ResizeSize, "resize-size"},
		{doc.WindowsMinPerRow, &cfg.WindowsMinPerRow, "windows-min-per-row"},
		{doc.WindowsMaxPerRow, &cfg.WindowsMaxPerRow, "windows-max-per-row"},
	} {
		if n.raw == nil {
			continue
		}
		if *n.raw < 0 || *n.raw > 1<<31-1 {
			return cfg, fmt.Errorf("share-picker: %s %d is out of range", n.key, *n.raw)
		}
		*n.target = int(*n.raw)
	}
	if doc.OutputsShowLabel != nil {
		cfg.OutputsShowLabel = *doc.OutputsShowLabel
	}
	if doc.OutputsRespectScaling != nil {
		cfg.OutputsRespectScaling = *doc.OutputsRespectScaling
	}
	return cfg, nil
}

package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/strftime"
)

// ScreenshotConfig is [modules.screenshot]
// (crates/wayle-config/src/schemas/modules/screenshot): the capture
// options the screenshot host reads per capture, and the bar button.
type ScreenshotConfig struct {
	Click ClickConfig
	// Icon is the button icon (the schema's `icon`, icon-show,
	// icon-color).
	Icon IconConfig
	// Label is the static label text beside the icon, shown when
	// LabelShow; LabelMaxLength truncates it (0 disables).
	Label          string
	LabelShow      bool
	LabelColor     ColorValue
	LabelMaxLength int
	// OutputDirectory is where shots land; empty means
	// $XDG_PICTURES_DIR, then ~/Pictures.
	OutputDirectory string
	// FilenameFormat names each saved file, compiled at load so a bad
	// specifier is a load error rather than a mangled name.
	FilenameFormat *strftime.Layout
	// CopyToClipboard copies the capture to the clipboard.
	CopyToClipboard bool
	// Notify fires a desktop notification naming the saved path.
	Notify bool
}

// defaultScreenshotFilename is the schema's filename-format default.
const defaultScreenshotFilename = "Screenshot_%Y-%m-%d_%H-%M-%S.png"

// DefaultsScreenshot returns the schema defaults.
func DefaultsScreenshot() ScreenshotConfig {
	layout, err := strftime.Compile(defaultScreenshotFilename)
	if err != nil {
		panic(err)
	}
	return ScreenshotConfig{
		Click: DefaultsClick(map[string]string{
			"left-click":   "wayle screenshot region",
			"right-click":  "wayle screenshot output",
			"middle-click": "wayle screenshot window",
		}),
		Icon:            DefaultsIcon(true, "ld-camera-symbolic"),
		LabelColor:      mustColor("accent"),
		FilenameFormat:  layout,
		CopyToClipboard: true,
		Notify:          true,
	}
}

// applyScreenshot overlays [modules.screenshot].
func applyScreenshot(md toml.MetaData, prim toml.Primitive) (ScreenshotConfig, error) {
	cfg := DefaultsScreenshot()
	var doc struct {
		Icon            *string     `toml:"icon"`
		IconShow        *bool       `toml:"icon-show"`
		IconColor       *ColorValue `toml:"icon-color"`
		Label           *string     `toml:"label"`
		LabelShow       *bool       `toml:"label-show"`
		LabelColor      *ColorValue `toml:"label-color"`
		LabelMaxLength  *int        `toml:"label-max-length"`
		OutputDirectory *string     `toml:"output-directory"`
		FilenameFormat  *string     `toml:"filename-format"`
		CopyToClipboard *bool       `toml:"copy-to-clipboard"`
		Notify          *bool       `toml:"notify"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.Icon != nil {
		cfg.Icon.Name = *doc.Icon
	}
	if doc.IconShow != nil {
		cfg.Icon.Show = *doc.IconShow
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
	}
	if doc.Label != nil {
		cfg.Label = *doc.Label
	}
	if doc.LabelShow != nil {
		cfg.LabelShow = *doc.LabelShow
	}
	if doc.LabelColor != nil {
		cfg.LabelColor = *doc.LabelColor
	}
	if doc.LabelMaxLength != nil {
		if *doc.LabelMaxLength < 0 {
			return cfg, fmt.Errorf("screenshot: label-max-length %d is negative", *doc.LabelMaxLength)
		}
		cfg.LabelMaxLength = *doc.LabelMaxLength
	}
	if doc.OutputDirectory != nil {
		cfg.OutputDirectory = *doc.OutputDirectory
	}
	if doc.FilenameFormat != nil {
		layout, err := strftime.Compile(*doc.FilenameFormat)
		if err != nil {
			return cfg, fmt.Errorf("screenshot: filename-format: %w", err)
		}
		if *doc.FilenameFormat == "" {
			return cfg, errors.New("screenshot: filename-format is empty")
		}
		cfg.FilenameFormat = layout
	}
	if doc.CopyToClipboard != nil {
		cfg.CopyToClipboard = *doc.CopyToClipboard
	}
	if doc.Notify != nil {
		cfg.Notify = *doc.Notify
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}

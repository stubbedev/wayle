package config

import (
	"strings"
	"testing"
	"time"
)

func TestScreenshotDefaults(t *testing.T) {
	cfg := Defaults().Screenshot
	if cfg.Icon.Name != "ld-camera-symbolic" || !cfg.Icon.Show || cfg.LabelShow || cfg.Label != "" {
		t.Errorf("chrome defaults %+v", cfg)
	}
	if !cfg.CopyToClipboard || !cfg.Notify || cfg.OutputDirectory != "" {
		t.Errorf("capture defaults %+v", cfg)
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := cfg.FilenameFormat.Format(at); got != "Screenshot_2026-01-02_03-04-05.png" {
		t.Errorf("default filename %q", got)
	}
	if cfg.Click.LeftClick.Command != "wayle screenshot region" ||
		cfg.Click.RightClick.Command != "wayle screenshot output" ||
		cfg.Click.MiddleClick.Command != "wayle screenshot window" {
		t.Errorf("click defaults %+v", cfg.Click)
	}
}

func TestScreenshotOverrides(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[modules.screenshot]
icon = "cam"
icon-show = false
label = "Shot"
label-show = true
label-max-length = 3
output-directory = "/shots"
filename-format = "shot-%H.png"
copy-to-clipboard = false
notify = false
left-click = "wayle screenshot output DP-1"
`))
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Screenshot
	if s.Icon.Name != "cam" || s.Icon.Show || s.Label != "Shot" || !s.LabelShow || s.LabelMaxLength != 3 {
		t.Errorf("chrome %+v", s)
	}
	if s.OutputDirectory != "/shots" || s.CopyToClipboard || s.Notify {
		t.Errorf("capture %+v", s)
	}
	if got := s.FilenameFormat.Format(time.Date(2026, 1, 1, 7, 0, 0, 0, time.UTC)); got != "shot-07.png" {
		t.Errorf("filename %q", got)
	}
	if s.Click.LeftClick.Command != "wayle screenshot output DP-1" || s.Click.RightClick.Command != "wayle screenshot output" {
		t.Errorf("clicks %+v", s.Click)
	}
}

func TestScreenshotBadValuesAreLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"unsupported specifier": `filename-format = "shot-%Q.png"`,
		"empty filename":        `filename-format = ""`,
		"negative max length":   `label-max-length = -1`,
		"wrong type":            `notify = "yes"`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadFile(writeConfig(t, "[modules.screenshot]\n"+body+"\n"))
			if err == nil {
				t.Fatal("loaded")
			}
			if cfg.Screenshot.FilenameFormat.String() != defaultScreenshotFilename {
				t.Error("a failed load kept a partial screenshot section")
			}
		})
	}
}

func TestSharePickerDefaults(t *testing.T) {
	sp := Defaults().SharePicker
	one := Size{Value: 1, Unit: SizeMultiplier}
	if sp.DefaultPage != SharePickerWindows || sp.HideTokenRestore || sp.ResizeSize != 640 ||
		sp.WindowsMinPerRow != 3 || sp.WindowsMaxPerRow != 4 || sp.OutputsShowLabel || !sp.OutputsRespectScaling {
		t.Errorf("defaults %+v", sp)
	}
	if sp.Width != one || sp.Height != one || sp.WidgetSize != one || sp.WindowsSpacing != one || sp.OutputsSpacing != one {
		t.Errorf("size defaults %+v", sp)
	}
	if px := sp.Width.ResolvePx(SharePickerWidthBaseRem*16, 1); px != 1000 {
		t.Errorf("default width resolves to %v px, want 1000", px)
	}
}

func TestSharePickerOverrides(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[share-picker]
default-page = "region"
hide-token-restore = true
width = "1200px"
height = 1.5
resize-size = 320
windows-min-per-row = 2
windows-max-per-row = 6
outputs-show-label = true
outputs-respect-scaling = false
`))
	if err != nil {
		t.Fatal(err)
	}
	sp := cfg.SharePicker
	if sp.DefaultPage != SharePickerRegion || !sp.HideTokenRestore || sp.ResizeSize != 320 ||
		sp.WindowsMinPerRow != 2 || sp.WindowsMaxPerRow != 6 || !sp.OutputsShowLabel || sp.OutputsRespectScaling {
		t.Errorf("overrides %+v", sp)
	}
	if sp.Width != (Size{Value: 1200, Unit: SizePixels}) || sp.Height != (Size{Value: 1.5, Unit: SizeMultiplier}) {
		t.Errorf("sizes %+v %+v", sp.Width, sp.Height)
	}
	if SharePickerOutputs.String() != "outputs" || SharePickerRegion.String() != "region" || SharePickerWindows.String() != "windows" {
		t.Error("page names")
	}
}

func TestSharePickerBadValuesAreLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"unknown page":   `default-page = "tabs"`,
		"bad size":       `width = "wide"`,
		"negative size":  `height = -1`,
		"negative count": `windows-min-per-row = -2`,
		"huge count":     `resize-size = 99999999999`,
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadFile(writeConfig(t, "[share-picker]\n"+body+"\n"))
			if err == nil {
				t.Fatal("loaded")
			}
			if !strings.Contains(err.Error(), "share-picker") {
				t.Errorf("error %q does not name the section", err)
			}
			if cfg.SharePicker != DefaultsSharePicker() {
				t.Error("a failed load kept a partial share-picker section")
			}
		})
	}
}

package config

import (
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/wallpaper"
	"github.com/stubbedev/wayle/service/wallpaper/extract"
)

func TestWallpaperDefaults(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Wallpaper
	if w.Wallpaper != "" || w.FitMode != wallpaper.FitFill || w.CyclingDirectory != "" ||
		w.CyclingMode != wallpaper.Sequential || w.CyclingIntervalMins != 15 || w.CyclingSameImage || len(w.Monitors) != 0 {
		t.Errorf("defaults = %+v", w)
	}
}

func TestWallpaperSection(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[wallpaper]
wallpaper = "/pics/global.png"
fit-mode = "center"
cycling-directory = "/pics/cycle"
cycling-mode = "shuffle"
cycling-interval-mins = 30
cycling-same-image = true

[[wallpaper.monitors]]
name = "DP-1"
wallpaper = "/pics/primary.png"
fit-mode = "fit"

[[wallpaper.monitors]]
name = "HDMI-1"
`))
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Wallpaper
	if w.Wallpaper != "/pics/global.png" || w.FitMode != wallpaper.FitCenter || w.CyclingDirectory != "/pics/cycle" ||
		w.CyclingMode != wallpaper.Shuffle || w.CyclingIntervalMins != 30 || !w.CyclingSameImage {
		t.Errorf("section = %+v", w)
	}
	if m, ok := w.Monitor("DP-1"); !ok || m.Wallpaper != "/pics/primary.png" || m.FitMode != wallpaper.FitFit {
		t.Errorf("DP-1 = %+v %v", m, ok)
	}
	// An entry without a fit mode defaults to fill, not the global mode.
	if m, ok := w.Monitor("HDMI-1"); !ok || m.FitMode != wallpaper.FitFill || m.Wallpaper != "" {
		t.Errorf("HDMI-1 = %+v %v", m, ok)
	}
	if _, ok := w.Monitor("eDP-1"); ok {
		t.Error("an unconfigured monitor has an entry")
	}
}

func TestWallpaperIntervalClampsToOneMinute(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, "[wallpaper]\ncycling-interval-mins = 0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Wallpaper.CyclingIntervalMins != 1 {
		t.Errorf("interval 0 = %d, want the clamp to 1", cfg.Wallpaper.CyclingIntervalMins)
	}
}

func TestWallpaperBadValuesAreLoadErrors(t *testing.T) {
	for _, body := range []string{
		`fit-mode = "Fill"`, // serde's lowercase is exact
		`fit-mode = "tile"`,
		`cycling-mode = "random"`,
		`cycling-interval-mins = -1`,
		`wallpaper = 5`,
		"[[wallpaper.monitors]]\nwallpaper = \"/x.png\"", // name is required
		"[[wallpaper.monitors]]\nname = \"DP-1\"\nfit-mode = \"cover\"",
	} {
		cfg, err := LoadFile(writeConfig(t, "[wallpaper]\n"+body+"\n"))
		if err == nil {
			t.Errorf("%q loaded without error", body)
			continue
		}
		if cfg.Wallpaper.FitMode != wallpaper.FitFill {
			t.Errorf("%q: a failed load left a non-default fit", body)
		}
	}
}

func TestColorExtractorDefaults(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	ce := cfg.ColorExtractor
	want := extract.DefaultConfig()
	want.Tool = extract.None
	if ce.ThemeProvider != ThemeWayle || ce.ThemingMonitor != "" || ce.Extractor != want {
		t.Errorf("defaults = %+v, want wayle / %+v", ce, want)
	}
}

func TestColorExtractorFromStyling(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[styling]
theme-provider = "matugen"
theming-monitor = "DP-2"
matugen-scheme = "fruit-salad"
matugen-contrast = -0.5
matugen-source-color = 2
matugen-light = true
wallust-palette = "harddark16"
wallust-saturation = 40
wallust-check-contrast = false
wallust-backend = "kmeans"
wallust-colorspace = "lch"
wallust-apply-globally = false
pywal-saturation = 0.5
pywal-contrast = 7
pywal-light = true
pywal-apply-globally = false
scale = 1.2
`))
	if err != nil {
		t.Fatal(err)
	}
	ce := cfg.ColorExtractor
	ex := ce.Extractor
	if ce.ThemeProvider != ThemeMatugen || ex.Tool != extract.Matugen || ce.ThemingMonitor != "DP-2" {
		t.Errorf("provider = %+v", ce)
	}
	if ex.MatugenScheme != extract.SchemeFruitSalad || ex.MatugenContrast != -0.5 || ex.MatugenSourceColor != 2 || !ex.MatugenLight {
		t.Errorf("matugen = %+v", ex)
	}
	if ex.WallustPalette != extract.PaletteHarddark16 || ex.WallustSaturation != 40 || ex.WallustCheckContrast ||
		ex.WallustBackend != extract.BackendKmeans || ex.WallustColorspace != extract.ColorspaceLch || ex.WallustApplyGlobally {
		t.Errorf("wallust = %+v", ex)
	}
	// An integer is accepted for a float key, as serde's f64 is.
	if ex.PywalSaturation != 0.5 || ex.PywalContrast != 7 || !ex.PywalLight || ex.PywalApplyGlobally {
		t.Errorf("pywal = %+v", ex)
	}
}

func TestThemeProviderTools(t *testing.T) {
	for p, want := range map[ThemeProvider]extract.Tool{
		ThemeWayle: extract.None, ThemeMatugen: extract.Matugen, ThemePywal: extract.Pywal, ThemeWallust: extract.Wallust,
	} {
		if got := p.Tool(); got != want {
			t.Errorf("%s.Tool() = %v, want %v", p, got, want)
		}
	}
}

func TestColorExtractorClampsRangedValues(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[styling]
matugen-contrast = 3.0
wallust-saturation = 150
pywal-saturation = -1.0
pywal-contrast = 30.0
`))
	if err != nil {
		t.Fatal(err)
	}
	ex := cfg.ColorExtractor.Extractor
	if ex.MatugenContrast != 1 || ex.WallustSaturation != 100 || ex.PywalSaturation != 0 || ex.PywalContrast != 21 {
		t.Errorf("clamps = %+v", ex)
	}
}

func TestColorExtractorBadValuesAreLoadErrors(t *testing.T) {
	for _, body := range []string{
		`theme-provider = "Matugen"`,
		`theme-provider = "none"`,
		`matugen-scheme = "tonal_spot"`,
		`matugen-source-color = 300`,
		`matugen-source-color = -1`,
		`wallust-palette = "dark17"`,
		`wallust-saturation = 256`,
		`wallust-backend = "fast"`,
		`wallust-colorspace = "rgb"`,
		`pywal-light = "yes"`,
	} {
		_, err := LoadFile(writeConfig(t, "[styling]\n"+body+"\n"))
		if err == nil {
			t.Errorf("%q loaded without error", body)
		} else if !strings.Contains(err.Error(), "config:") {
			t.Errorf("%q: error lacks the file context: %v", body, err)
		}
	}
}

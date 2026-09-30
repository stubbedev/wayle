package extract

import "testing"

func TestParseToolAcceptsTheRustAliases(t *testing.T) {
	for in, want := range map[string]Tool{
		"wallust": Wallust, "MATUGEN": Matugen, "pywal": Pywal, "wal": Pywal,
		"none": None, "Disabled": None,
	} {
		got, err := ParseTool(in)
		if err != nil || got != want {
			t.Errorf("ParseTool(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseTool("wayle"); err == nil || err.Error() != "Invalid color extractor: wayle" {
		t.Errorf("ParseTool(wayle) err = %v, want the Rust message", err)
	}
}

func TestMatugenSchemeValues(t *testing.T) {
	s, err := ParseMatugenScheme("fruit-salad")
	if err != nil || s != SchemeFruitSalad || s.CLIValue() != "scheme-fruit-salad" {
		t.Fatalf("fruit-salad = %v %q %v", s, s.CLIValue(), err)
	}
	if SchemeTonalSpot.CLIValue() != "scheme-tonal-spot" || SchemeContent.CLIValue() != "scheme-content" {
		t.Error("cli values drifted from MatugenScheme::cli_value")
	}
	// The config spelling is exact kebab-case, as serde reads it.
	for _, bad := range []string{"FruitSalad", "fruit_salad", "scheme-tonal-spot", ""} {
		if _, err := ParseMatugenScheme(bad); err == nil {
			t.Errorf("ParseMatugenScheme(%q) accepted", bad)
		}
	}
}

func TestWallustEnums(t *testing.T) {
	p, err := ParseWallustPalette("softlightcomp16")
	if err != nil || p != PaletteSoftlightcomp16 || p.String() != "softlightcomp16" {
		t.Fatalf("softlightcomp16 = %v %v", p, err)
	}
	if _, err := ParseWallustPalette("Dark16"); err == nil {
		t.Error("palette parse is case-insensitive; serde is not")
	}
	for _, light := range []WallustPalette{PaletteLight, PaletteLight16, PaletteSoftlight, PaletteSoftlightcomp16} {
		if !light.IsLight() {
			t.Errorf("%v is light", light)
		}
	}
	for _, dark := range []WallustPalette{PaletteDark16, PaletteHarddark, PaletteSoftdark, PaletteAnsidark} {
		if dark.IsLight() {
			t.Errorf("%v is dark", dark)
		}
	}
	if b, err := ParseWallustBackend("kmeans"); err != nil || b != BackendKmeans {
		t.Errorf("kmeans = %v %v", b, err)
	}
	if _, err := ParseWallustBackend("fast"); err == nil {
		t.Error("unknown backend accepted")
	}
	if c, err := ParseWallustColorspace("lchansi"); err != nil || c != ColorspaceLchansi {
		t.Errorf("lchansi = %v %v", c, err)
	}
	if _, err := ParseWallustColorspace("rgb"); err == nil {
		t.Error("unknown colorspace accepted")
	}
}

func TestFormatFloatMatchesRustDisplay(t *testing.T) {
	for in, want := range map[float64]string{3: "3", 0: "0", 0.05: "0.05", -0.25: "-0.25", 21: "21", 1e-7: "0.0000001"} {
		if got := formatFloat(in); got != want {
			t.Errorf("formatFloat(%v) = %q, want %q", in, got, want)
		}
	}
}

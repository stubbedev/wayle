package styling

import "testing"

// Expected values are the Rust derive_layers / hex_to_rgba output for
// the same inputs (the probe in crates/wayle-styling), so the float32
// port is pinned byte for byte, rounding included.
func TestDeriveLayersMatchesRust(t *testing.T) {
	for _, tc := range []struct {
		in         string
		dark, lite layers
	}{
		{"#11111b", layers{"#09090e", "#11111b", "#191928"}, layers{"#191928", "#11111b", "#09090e"}},
		{"#e0e0e0", layers{"#d6d6d6", "#e0e0e0", "#eaeaea"}, layers{"#eaeaea", "#e0e0e0", "#d6d6d6"}},
		{"#101112", layers{"#060707", "#101112", "#1a1b1d"}, layers{"#1a1b1d", "#101112", "#060707"}},
		{"#fafbfc", layers{"#edf1f4", "#fafbfc", "#ffffff"}, layers{"#ffffff", "#fafbfc", "#edf1f4"}},
		{"#b4befe", layers{"#a0adfe", "#b4befe", "#c8cffe"}, layers{"#c8cffe", "#b4befe", "#a0adfe"}},
		{"#ff0000", layers{"#eb0000", "#ff0000", "#ff1414"}, layers{"#ff1414", "#ff0000", "#eb0000"}},
		{"#00ff00", layers{"#00eb00", "#00ff00", "#14ff14"}, layers{"#14ff14", "#00ff00", "#00eb00"}},
		{"#0000ff", layers{"#0000eb", "#0000ff", "#1414ff"}, layers{"#1414ff", "#0000ff", "#0000eb"}},
		{"#808080", layers{"#767676", "#808080", "#8a8a8a"}, layers{"#8a8a8a", "#808080", "#767676"}},
		{"#000000", layers{"#000000", "#000000", "#0a0a0a"}, layers{"#0a0a0a", "#000000", "#000000"}},
		{"#ffffff", layers{"#f5f5f5", "#ffffff", "#ffffff"}, layers{"#ffffff", "#ffffff", "#f5f5f5"}},
		{"#141420", layers{"#0c0c13", "#141420", "#1c1c2d"}, layers{"#1c1c2d", "#141420", "#0c0c13"}},
		{"#1c1c2c", layers{"#141420", "#1c1c2c", "#242438"}, layers{"#242438", "#1c1c2c", "#141420"}},
		{"#e46870", layers{"#e15760", "#e46870", "#e77980"}, layers{"#e77980", "#e46870", "#e15760"}},
		{"#7f3fbf", layers{"#753ab0", "#7f3fbf", "#894ec5"}, layers{"#894ec5", "#7f3fbf", "#753ab0"}},
		{"#3fbf7f", layers{"#3ab075", "#3fbf7f", "#4ec589"}, layers{"#4ec589", "#3fbf7f", "#3ab075"}},
		{"#bf7f3f", layers{"#b0753a", "#bf7f3f", "#c5894e"}, layers{"#c5894e", "#bf7f3f", "#b0753a"}},
		// Unparsable channels read as 0 (unwrap_or(0)); the surface keeps
		// the raw string.
		{"#zzzzzz", layers{"#000000", "#zzzzzz", "#0a0a0a"}, layers{"#0a0a0a", "#zzzzzz", "#000000"}},
	} {
		if got := deriveLayers(tc.in, false); got != tc.dark {
			t.Errorf("deriveLayers(%s, dark) = %+v, want %+v", tc.in, got, tc.dark)
		}
		if got := deriveLayers(tc.in, true); got != tc.lite {
			t.Errorf("deriveLayers(%s, light) = %+v, want %+v", tc.in, got, tc.lite)
		}
	}
}

func TestHexToRGBAMatchesRust(t *testing.T) {
	for _, tc := range []struct {
		hex   string
		alpha float32
		want  string
	}{
		{"#1c1c2c", 1, "rgba(28, 28, 44, 1.00)"},
		{"#1c1c2c", 0.85, "rgba(28, 28, 44, 0.85)"},
		{"#abcdef", 0.33, "rgba(171, 205, 239, 0.33)"},
		{"#abcdef", 0, "rgba(171, 205, 239, 0.00)"},
		{"#abcdef", 0.07, "rgba(171, 205, 239, 0.07)"},
		{"#abcdef", 0.995, "rgba(171, 205, 239, 1.00)"},
		{"#abcdef", 0.125, "rgba(171, 205, 239, 0.12)"}, // ties round to even
	} {
		if got := hexToRGBA(tc.hex, tc.alpha); got != tc.want {
			t.Errorf("hexToRGBA(%s, %v) = %q, want %q", tc.hex, tc.alpha, got, tc.want)
		}
	}
}

func TestParseChannelsIsTotal(t *testing.T) {
	// Where the Rust slice would panic (fewer than six digits), the
	// missing channels read as 0 instead.
	for in, want := range map[string][3]uint8{
		"#abc":     {0xab, 0, 0},
		"":         {0, 0, 0},
		"##ff0080": {0xff, 0x00, 0x80}, // trim_start_matches strips every '#'
		"+f+f+f":   {0x0f, 0x0f, 0x0f}, // from_str_radix takes a leading '+'
		"#ff00zz":  {0xff, 0x00, 0},
	} {
		r, g, b := parseChannels(in)
		if [3]uint8{r, g, b} != want {
			t.Errorf("parseChannels(%q) = %v, want %v", in, [3]uint8{r, g, b}, want)
		}
	}
}

func TestStimulusConversionsRoundTrip(t *testing.T) {
	for c := range 256 {
		if got := f32ToU8(u8ToF32(uint8(c))); got != uint8(c) {
			t.Fatalf("u8 %d round-trips to %d", c, got)
		}
	}
	if f32ToU8(-0.5) != 0 || f32ToU8(2) != 255 {
		t.Error("out-of-range stimuli must saturate to 0 and 255")
	}
}

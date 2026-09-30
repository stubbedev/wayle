package config

import (
	"strings"
	"testing"
)

func cvp(s string) *ColorValue {
	cv := mustColor(s)
	return &cv
}

func iconEntry(above, below *float64, color string) ThresholdEntry {
	return ThresholdEntry{Above: above, Below: below, IconColor: cvp(color)}
}

// The threshold.rs test suite, ported.
func TestEvaluateThresholds(t *testing.T) {
	warning := func(above float64) ThresholdEntry {
		e := iconEntry(new(above), nil, "status-warning")
		e.LabelColor = cvp("status-warning")
		return e
	}
	errorAt := func(above float64) ThresholdEntry {
		e := iconEntry(new(above), nil, "status-error")
		e.LabelColor = cvp("status-error")
		return e
	}
	icon := func(c ThresholdColors) string {
		if c.IconColor == nil {
			return ""
		}
		return string(c.IconColor.Token)
	}
	if got := EvaluateThresholds(50, nil); !got.IsEmpty() {
		t.Errorf("no thresholds: %+v, want empty", got)
	}
	if got := EvaluateThresholds(50, []ThresholdEntry{warning(70)}); !got.IsEmpty() {
		t.Errorf("below the threshold: %+v, want empty", got)
	}
	if got := icon(EvaluateThresholds(70, []ThresholdEntry{warning(70)})); got != "status-warning" {
		t.Errorf("at the threshold: icon %q, want status-warning (inclusive)", got)
	}
	if got := icon(EvaluateThresholds(85, []ThresholdEntry{warning(70)})); got != "status-warning" {
		t.Errorf("above the threshold: icon %q", got)
	}
	both := []ThresholdEntry{warning(70), errorAt(90)}
	high := EvaluateThresholds(95, both)
	if icon(high) != "status-error" || high.LabelColor == nil || high.LabelColor.Token != TokenStatusError {
		t.Errorf("higher threshold must override the lower: %+v", high)
	}
	if got := icon(EvaluateThresholds(80, both)); got != "status-warning" {
		t.Errorf("between thresholds: icon %q, want the lower one", got)
	}
	below := []ThresholdEntry{iconEntry(nil, new(20.0), "status-error")}
	if got := icon(EvaluateThresholds(15, below)); got != "status-error" {
		t.Errorf("below condition, low value: icon %q", got)
	}
	if got := EvaluateThresholds(50, below); !got.IsEmpty() {
		t.Errorf("below condition, high value: %+v, want empty", got)
	}
	partial := EvaluateThresholds(80, []ThresholdEntry{iconEntry(new(70.0), nil, "status-warning")})
	if partial.IconColor == nil || partial.LabelColor != nil || partial.IconBgColor != nil {
		t.Errorf("a partial override must leave the unset slots nil: %+v", partial)
	}
	band := []ThresholdEntry{iconEntry(new(30.0), new(70.0), "status-success")}
	if icon(EvaluateThresholds(50, band)) == "" || icon(EvaluateThresholds(80, band)) != "" || icon(EvaluateThresholds(20, band)) != "" {
		t.Error("above and below together must both hold")
	}
	if got := EvaluateThresholds(50, []ThresholdEntry{{IconColor: cvp("red")}}); !got.IsEmpty() {
		t.Errorf("an entry without bounds never matches: %+v", got)
	}
}

func TestEvaluateThresholdsCoversEverySlot(t *testing.T) {
	e := ThresholdEntry{
		Above: new(0.0), LabelColor: cvp("red"), IconBgColor: cvp("green"),
		ButtonBgColor: cvp("blue"), BorderColor: cvp("yellow"),
	}
	got := EvaluateThresholds(1, []ThresholdEntry{e})
	if got.IconColor != nil {
		t.Error("an entry without icon-color must not override the icon")
	}
	for name, slot := range map[string]*ColorValue{
		"red": got.LabelColor, "green": got.IconBgColor, "blue": got.ButtonBgColor, "yellow": got.BorderColor,
	} {
		if slot == nil || string(slot.Token) != name {
			t.Errorf("slot %s = %+v", name, slot)
		}
	}
	// The result is a copy: mutating it leaves the config entry intact.
	*got.LabelColor = mustColor("accent")
	if e.LabelColor.Token != TokenRed {
		t.Error("EvaluateThresholds aliased the entry's color")
	}
}

func TestResolveOr(t *testing.T) {
	if got := ResolveOr(nil, "var(--fg-default)"); got != "var(--fg-default)" {
		t.Errorf("no override = %q, want the config css", got)
	}
	if got := ResolveOr(cvp("status-error"), "var(--fg-default)"); got != "var(--status-error)" {
		t.Errorf("override = %q, want its var", got)
	}
}

func TestThresholdDecodeEveryKey(t *testing.T) {
	path := writeConfig(t, `
[[modules.volume.thresholds]]
above = 70
below = 90.5
icon-color = "status-warning"
label-color = "#ff0000"
icon-bg-color = "transparent"
button-bg-color = "bg-hover"
border-color = "auto"
unknown = "ignored"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Volume.Thresholds) != 1 {
		t.Fatalf("thresholds = %+v", cfg.Volume.Thresholds)
	}
	e := cfg.Volume.Thresholds[0]
	if *e.Above != 70 || *e.Below != 90.5 || e.IconColor == nil || e.IconColor.Token != TokenStatusWarning {
		t.Errorf("bounds/icon = %+v", e)
	}
	if e.LabelColor.Hex != "#ff0000" || e.IconBgColor.Kind != ColorTransparent ||
		e.ButtonBgColor.Token != TokenBgHover || e.BorderColor.Kind != ColorAuto {
		t.Errorf("slots = %+v", e)
	}
}

func TestThresholdDecodeRejectsBadEntries(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"icon-color = \"red\"", "above or below"},
		{"above = 5\nlabel-color = \"nope\"", "label-color"},
		{"above = 5\nborder-color = 3", "border-color"},
		{"above = \"high\"", "above"},
	} {
		// Every module shares the one decoder; cpu goes through the
		// sysinfo path, battery through its own table.
		for _, module := range []string{"battery", "cpu", "volume", "brightness", "notifications"} {
			path := writeConfig(t, "[[modules."+module+".thresholds]]\n"+tc.body+"\n")
			_, err := LoadFile(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %q: err = %v, want %q", module, tc.body, err, tc.want)
			}
		}
	}
}

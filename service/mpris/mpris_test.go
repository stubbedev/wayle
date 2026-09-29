package mpris

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestStateFromString(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want PlaybackState
	}{
		{"Playing", StatePlaying},
		{"Paused", StatePaused},
		{"Stopped", StateStopped},
		{"\nPlaying\n", StatePlaying},
		{"garbage", StateStopped},
	} {
		if got := StateFromString(tc.raw); got != tc.want {
			t.Errorf("StateFromString(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestVariantHelpers(t *testing.T) {
	if got := variantString(dbus.MakeVariant("Title")); got != "Title" {
		t.Errorf("string variant = %q", got)
	}
	if got := variantString(dbus.MakeVariant(42)); got != "" {
		t.Errorf("int variant = %q, want empty", got)
	}
	if got := variantString(dbus.Variant{}); got != "" {
		t.Errorf("empty variant = %q, want empty", got)
	}
	if got := variantStringArray(dbus.MakeVariant([]string{"A", "B"})); len(got) != 2 || got[0] != "A" {
		t.Errorf("array variant = %v", got)
	}
	if got := variantStringArray(dbus.MakeVariant("Solo")); len(got) != 1 || got[0] != "Solo" {
		t.Errorf("bare string = %v, want one-element slice", got)
	}
}

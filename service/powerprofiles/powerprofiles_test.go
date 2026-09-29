package powerprofiles

import "testing"

func TestNextProfileCanonicalCycle(t *testing.T) {
	// The full canonical cycle.
	if got := NextProfile(ProfilePowerSaver, nil); got != ProfileBalanced {
		t.Errorf("power-saver -> %q", got)
	}
	if got := NextProfile(ProfileBalanced, nil); got != ProfilePerformance {
		t.Errorf("balanced -> %q", got)
	}
	if got := NextProfile(ProfilePerformance, nil); got != ProfilePowerSaver {
		t.Errorf("performance -> %q", got)
	}
	// An unknown current starts the cycle.
	if got := NextProfile("whatever", nil); got != ProfilePowerSaver {
		t.Errorf("unknown -> %q, want power-saver", got)
	}
}

func TestNextProfileRestricted(t *testing.T) {
	// The daemon offers only two profiles: performance wraps past the
	// missing power-saver.
	available := []string{ProfileBalanced, ProfilePerformance}
	if got := NextProfile(ProfilePerformance, available); got != ProfileBalanced {
		t.Errorf("performance -> %q, want balanced", got)
	}
	if got := NextProfile(ProfileBalanced, available); got != ProfilePerformance {
		t.Errorf("balanced -> %q, want performance", got)
	}
	// A cycle with no matching profiles falls back to balanced (the
	// Rust empty-cycle guard).
	if got := NextProfile(ProfileBalanced, []string{"turbo"}); got != ProfileBalanced {
		t.Errorf("no matches -> %q", got)
	}
}

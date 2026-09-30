package appicons

import "testing"

func TestLookup(t *testing.T) {
	// The name is lowercased before matching.
	if icon, ok := Lookup("Firefox"); !ok || icon != "si-firefox-symbolic" {
		t.Errorf("Firefox = %q, %v", icon, ok)
	}
	if icon, ok := Lookup("spotify"); !ok || icon != "si-spotify-symbolic" {
		t.Errorf("spotify = %q, %v", icon, ok)
	}
	// Wildcard entries match inside longer names.
	if icon, ok := Lookup("com.obsproject.Studio"); !ok || icon != "si-obsstudio-symbolic" {
		t.Errorf("obs = %q, %v", icon, ok)
	}
	if _, ok := Lookup("no-such-app-anywhere"); ok {
		t.Error("an unknown name resolved")
	}
}

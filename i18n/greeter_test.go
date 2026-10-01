package i18n

import (
	"testing"

	greeterlocales "github.com/stubbedev/wayle/crates/wayle-greeter/locales"
)

// TestGreeterDomain pins the greeter's file-per-locale domain: the
// French file answers a French desktop, en-US the rest, and an
// argument is isolated as fluent-bundle does.
func TestGreeterDomain(t *testing.T) {
	assets := NewFileAssets(greeterlocales.FS, "wayle-greeter.ftl")
	available, err := assets.Available()
	if err != nil || len(available) != 2 {
		t.Fatalf("available = %v, %v; want en-US and fr", available, err)
	}
	fr, _ := ParseLangID("fr-FR")
	l, err := Select(assets, []LangID{fr})
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Get("greeter-username"); got != "Nom d'utilisateur" {
		t.Errorf("fr username = %q", got)
	}
	en, _ := ParseLangID("de-DE")
	l, _ = Select(assets, []LangID{en})
	if got := l.Get("greeter-caps-lock"); got != "Caps Lock is on" {
		t.Errorf("fallback caps lock = %q", got)
	}
	if got := l.Get("greeter-greetd-unavailable", Str("error", "no socket")); got != "greetd unavailable: \u2068no socket\u2069" {
		t.Errorf("unavailable = %q", got)
	}
	if got := l.Get("lock-nothing"); got == "" {
		t.Error("a missing id came back empty, want the no-localization marker")
	}
	if Greeter() == nil {
		t.Error("no greeter loader")
	}
	// A tree without the file has no fallback to load.
	if _, err := Select(NewFileAssets(greeterlocales.FS, "nope.ftl"), []LangID{en}); err == nil {
		t.Error("a missing file loaded")
	}
}

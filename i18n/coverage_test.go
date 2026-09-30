package i18n

import (
	"io/fs"
	"strings"
	"testing"

	settingslocales "github.com/stubbedev/wayle/crates/wayle-i18n/locales"
	shelllocales "github.com/stubbedev/wayle/crates/wayle-shell-core/locales"
)

var trees = map[string]Assets{
	"wayle-shell-core": NewAssets(shelllocales.FS),
	"wayle-i18n":       NewAssets(settingslocales.FS),
}

// Every partial in both trees parses without junk, and every
// embedded locale has its own plural rule set.
func TestEmbeddedPartialsParseCleanly(t *testing.T) {
	for name, assets := range trees {
		locales, err := assets.Available()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(locales) < 2 {
			t.Fatalf("%s: want en-US and fr at least, got %v", name, locales)
		}
		for _, loc := range locales {
			files, err := assets.partials(loc.String())
			if err != nil {
				t.Fatal(err)
			}
			if len(files) == 0 {
				t.Errorf("%s/%s: no partials", name, loc)
			}
			for _, f := range files {
				data, err := fs.ReadFile(assets.fsys, f)
				if err != nil {
					t.Fatal(err)
				}
				res := parseResource(strings.ReplaceAll(string(data), "\r\n", "\n"))
				for _, j := range res.junk {
					t.Errorf("%s/%s: junk (%v): %q", name, f, j.err, j.content)
				}
			}
			if chosen, _ := pluralRuleFor(loc); chosen.language != loc.language {
				t.Errorf("%s: locale %s has no plural rule set (fell back to %s)", name, loc, chosen)
			}
		}
	}
}

// The package-level domains initialize from the process environment.
func TestDomainsInitialize(t *testing.T) {
	if Shell() == nil || Settings() == nil {
		t.Fatal("domain loaders are nil")
	}
	if got := Settings().Get("app-name"); got != "Wayle" && !strings.Contains(got, "Wayle") {
		t.Errorf("app-name = %q", got)
	}
}

// The embedded trees resolve real messages per locale, arguments,
// plurals, and attributes included.
func TestEmbeddedMessagesResolve(t *testing.T) {
	load := func(tree string, want ...string) *Loader {
		var req []LangID
		for _, w := range want {
			req = append(req, MustLangID(w))
		}
		l, err := Select(trees[tree], req)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	shellEN := load("wayle-shell-core", "en-US")
	shellFR := load("wayle-shell-core", "fr-FR")
	settings := load("wayle-i18n")
	cases := []struct {
		got, want string
	}{
		{shellEN.Get("osd-brightness"), "Brightness"},
		{shellEN.Get("bar-bluetooth-connected-count", Int("count", 3)), "\u20683\u2069 Connected"},
		{shellEN.Get("osd-toggle-on", Str("label", "Caps Lock")), "\u2068Caps Lock\u2069 On"},
		{shellFR.Get("osd-toggle-on", Str("label", "Verr. Maj")), "\u2068Verr. Maj\u2069 activé"},
		{shellFR.Get("bar-bluetooth-disabled"), "Désactivé"},
		{settings.Get("settings-bar-scale"), "Scale"},
		{settings.Attr("settings-bar-scale", "description"), "Bar-specific scale multiplier for spacing, radius, and other elements"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

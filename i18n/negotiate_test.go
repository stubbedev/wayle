package i18n

import (
	"strings"
	"testing"
)

func langs(ids []LangID) string {
	var out []string
	for _, id := range ids {
		out = append(out, id.String())
	}
	return strings.Join(out, ",")
}

func TestParseLangIDNormalizes(t *testing.T) {
	for in, want := range map[string]string{
		"en-US":           "en-US",
		"en_us":           "en-US",
		"EN-latn-us":      "en-Latn-US",
		"und":             "und",
		"de-DE-1996-1996": "de-DE-1996",
		"sl-rozaj-biske":  "sl-biske-rozaj",
		"es-419":          "es-419",
	} {
		id, err := ParseLangID(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if id.String() != want {
			t.Errorf("%q -> %q, want %q", in, id, want)
		}
	}
	for _, bad := range []string{"", "C", "POSIX-x", "en--US", "e", "en-US-u-ca-gregory", "abcd"} {
		if _, err := ParseLangID(bad); err == nil {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestRequestedLanguagesFromEnvironment(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{}, ""},
		{map[string]string{"LANG": "fr_FR.UTF-8"}, "fr-FR"},
		{map[string]string{"LANG": "de_DE.UTF-8@euro"}, "de-DE"},
		{map[string]string{"LANG": "C.UTF-8"}, ""},
		{map[string]string{"LANG": "POSIX"}, "posix"}, // five letters is a valid language subtag
		{map[string]string{"LC_ALL": "fr_FR", "LC_MESSAGES": "de_DE", "LANG": "en_US"}, "fr-FR,de-DE,en-US"},
		{map[string]string{"LANGUAGE": "fr:de_DE:fr", "LANG": "en_US.UTF-8"}, "fr,de-DE,en-US"},
		{map[string]string{"LC_ALL": "en_US", "LANG": "en_US.UTF-8"}, "en-US"},
		{map[string]string{"LANGUAGE": "fr::de", "LANG": ""}, "fr,de"},
	}
	for _, c := range cases {
		got := langs(requestedLanguages(func(k string) string { return c.env[k] }))
		if got != c.want {
			t.Errorf("%v -> %q, want %q", c.env, got, c.want)
		}
	}
}

func TestNegotiateFilteringAgainstShippedLocales(t *testing.T) {
	available := []LangID{MustLangID("en-US"), MustLangID("fr")}
	cases := map[string]string{
		"fr-FR":       "fr,en-US",
		"fr":          "fr,en-US",
		"fr-CA":       "fr,en-US",
		"de-DE":       "en-US",
		"en-GB":       "en-US",
		"en":          "en-US",
		"":            "en-US",
		"de-DE,fr-BE": "fr,en-US",
		"en-US,fr":    "en-US,fr",
	}
	for req, want := range cases {
		var requested []LangID
		if req != "" {
			for r := range strings.SplitSeq(req, ",") {
				requested = append(requested, MustLangID(r))
			}
		}
		got := langs(negotiateLanguages(requested, available, Fallback, filtering))
		if got != want {
			t.Errorf("%q -> %q, want %q", req, got, want)
		}
	}
}

func TestNegotiateStrategies(t *testing.T) {
	available := []LangID{MustLangID("de"), MustLangID("de-AT"), MustLangID("fr")}
	req := []LangID{MustLangID("de-DE"), MustLangID("fr")}
	if got := langs(filterMatches(req, available, filtering)); got != "de,de-AT,fr" {
		t.Errorf("filtering = %q", got)
	}
	if got := langs(filterMatches(req, available, matching)); got != "de,fr" {
		t.Errorf("matching = %q", got)
	}
	if got := langs(filterMatches(req, available, lookup)); got != "de" {
		t.Errorf("lookup = %q", got)
	}
	if got := langs(negotiateLanguages([]LangID{MustLangID("ja")}, available, MustLangID("en"), lookup)); got != "en" {
		t.Errorf("lookup default = %q", got)
	}
}

func TestPluralRuleSelection(t *testing.T) {
	for in, want := range map[string]string{"en-US": "en", "fr-FR": "fr", "fr": "fr", "de": "en"} {
		if got, _ := pluralRuleFor(MustLangID(in)); got.String() != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

func TestSelectOrdersBundlesByPreference(t *testing.T) {
	l := loaderFor(t, map[string]string{
		"en-US/_a.ftl": "x = en\n",
		"fr/_a.ftl":    "x = fr\n",
	}, "de-DE", "fr-FR")
	if got := langs(l.Languages()); got != "fr,en-US" {
		t.Errorf("languages = %q", got)
	}
	if l.Get("x") != "fr" {
		t.Errorf("x = %q", l.Get("x"))
	}
	en := loaderFor(t, map[string]string{
		"en-US/_a.ftl": "x = en\n",
		"fr/_a.ftl":    "x = fr\n",
	}, "de-DE")
	if en.Get("x") != "en" {
		t.Errorf("unsupported locale must fall back: %q", en.Get("x"))
	}
}

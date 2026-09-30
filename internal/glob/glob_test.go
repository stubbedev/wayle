package glob

import "testing"

func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		pattern, name string
		want          bool
	}{
		{"*", "anything", true},
		{"noisy*", "noisy-app", true},
		{"noisy*", "quiet-app", false},
		{"*slack*", "Slack Desktop", true},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		{"a*b*c", "a-x-b-y-c", true},
		{"a*b*c", "a-x-c", false},
	} {
		if got := Match(tc.pattern, tc.name); got != tc.want {
			t.Errorf("Match(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestWildcard(t *testing.T) {
	for _, tc := range []struct {
		pattern, text string
		want          bool
	}{
		// wayle-shell-core glob.rs's cases.
		{"*spotify*", "org.mpris.MediaPlayer2.spotify", true},
		{"*spotify*", "spotify.instance1", true},
		{"firefox*", "firefox.instance12345", true},
		{"firefox*", "org.firefox", false},
		{"*.vlc", "org.videolan.vlc", true},
		{"*.vlc", "vlc.player", false},
		{"vlc", "vlc", true},
		{"vlc", "vlc2", false},
		{"1?", "12", true},
		{"1?", "1", false},
		{"", "", true},
		{"", "x", false},
		{"*", "", true},
		// Case-sensitive, unlike Match.
		{"Firefox", "firefox", false},
		// Escapes make the metasymbols literal.
		{`\*\?`, "*?", true},
		{`\*\?`, "ab", false},
		{`a\\b`, `a\b`, true},
		// Malformed escapes never match.
		{`a\b`, "ab", false},
		{`abc\`, `abc\`, false},
		// Backtracking across stars.
		{"*a*b", "aXbYb", true},
		{"*a*b", "aXbYc", false},
	} {
		if got := Wildcard(tc.pattern, tc.text); got != tc.want {
			t.Errorf("Wildcard(%q, %q) = %v, want %v", tc.pattern, tc.text, got, tc.want)
		}
	}
}

func TestFindWildcardFirstMatchWins(t *testing.T) {
	patterns := []string{"*fox*", "firefox", "*"}
	values := []string{"first", "second", "fallback"}
	if got, ok := FindWildcard(patterns, values, "firefox"); !ok || got != "first" {
		t.Errorf("got %q %v, want the first matching pattern", got, ok)
	}
	if got, ok := FindWildcard(patterns[:2], values[:2], "chromium"); ok {
		t.Errorf("no pattern matches, got %q", got)
	}
}

func TestGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, text string
		want          bool
	}{
		{"1*", "10", true},
		{"1?", "1", false},
		{"[1-3]", "2", true},
		{"[1-3]", "4", false},
		{"[!1-3]", "4", true},
		{"[!1-3]", "2", false},
		{"[]a]", "]", true},
		{"[!]a]", "b", true},
		{"-*", "-99", true},
		{"**", "anything/at/all", true},
		{"*org*", "firefox.org.x", true},
		{"Fire", "fire", false},
		// Syntax errors never match.
		{"[abc", "a", false},
		{"***", "x", false},
		{"a**", "ab", false},
	} {
		if got := Glob(tc.pattern, tc.text); got != tc.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", tc.pattern, tc.text, got, tc.want)
		}
	}
}

func TestFoldLowersTheText(t *testing.T) {
	if !Fold("Firefox", "firefox") || !Fold("org.Mozilla.Firefox", "*firefox*") {
		t.Error("case-folded text did not match")
	}
	if Fold("chromium", "*firefox*") {
		t.Error("unrelated text matched")
	}
}

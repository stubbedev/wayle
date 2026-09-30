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

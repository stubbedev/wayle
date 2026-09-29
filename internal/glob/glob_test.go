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

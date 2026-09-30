package openconnect

import "testing"

func TestUnreservedCharactersPassThroughUntouched(t *testing.T) {
	if got := formEscape("aZ0-_.~"); got != "aZ0-_.~" {
		t.Errorf("escaped = %q", got)
	}
}

func TestSeparatorsAreEscapedSoPairsCannotRunTogether(t *testing.T) {
	if got := formEscape("p@ss&word=x"); got != "p%40ss%26word%3Dx" {
		t.Errorf("escaped = %q", got)
	}
	if got := formEscape(`EXAMPLE\alice`); got != "EXAMPLE%5Calice" {
		t.Errorf("escaped = %q", got)
	}
}

func TestASpaceIsPercentTwentyNotAPlus(t *testing.T) {
	// openconnect resolves percent escapes but not +, so a plus would
	// arrive as a literal plus inside the password.
	if got := formEscape("a b"); got != "a%20b" {
		t.Errorf("escaped = %q", got)
	}
}

func TestNonASCIIIsEncodedPerUTF8Byte(t *testing.T) {
	if got := formEscape("é"); got != "%C3%A9" {
		t.Errorf("escaped = %q", got)
	}
}

func TestPairsJoinWithAmpersands(t *testing.T) {
	if got := formEncode(pair{"a", "1"}, pair{"b", ""}); got != "a=1&b=" {
		t.Errorf("encoded = %q", got)
	}
	if got := formEncode(); got != "" {
		t.Errorf("encoded = %q", got)
	}
}

func TestEscapesResolveBackToTheOriginal(t *testing.T) {
	for _, original := range []string{"p@ss&word=x", `EXAMPLE\alice`, "a b", "é", "aZ0-_.~"} {
		if got := decodeComponent(formEscape(original)); got != original {
			t.Errorf("round trip of %q = %q", original, got)
		}
	}
}

func TestAPercentThatIsNotAnEscapeSurvives(t *testing.T) {
	// A literal percent, and a truncated escape at the end: dropping
	// either silently corrupts somebody's cookie.
	for _, raw := range []string{"100%", "50%off", "%zz", "%2", ""} {
		if got := decodeComponent(raw); got != raw {
			t.Errorf("decode(%q) = %q", raw, got)
		}
	}
}

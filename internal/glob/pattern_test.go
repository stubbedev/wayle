package glob

import "testing"

func TestPatternCaseFolding(t *testing.T) {
	p, ok := Compile("*fire*")
	if !ok {
		t.Fatal("*fire* must compile")
	}
	if !p.Matches("Firefox", false) {
		t.Error("case-insensitive match must fold ASCII case")
	}
	if p.Matches("Firefox", true) {
		t.Error("case-sensitive match must not fold")
	}
	if !p.Matches("campfire", true) {
		t.Error("the literal case still matches case-sensitively")
	}

	r, _ := Compile("[a-c]x")
	if !r.Matches("Bx", false) || r.Matches("Bx", true) {
		t.Error("a letter range folds only when case-insensitive")
	}
	d, _ := Compile("[0-9]")
	if d.Matches("a", false) {
		t.Error("a digit range never matches a letter")
	}
	// Non-ASCII is compared as-is even when folding, as the crate does.
	u, _ := Compile("ä*")
	if u.Matches("Äpfel", false) {
		t.Error("non-ASCII letters are not case folded")
	}
	if _, ok := Compile("***"); ok {
		t.Error("*** is a syntax error")
	}
}

package styling

import (
	"os"
	"strings"
	"testing"
)

// TestStaticCSSIsFresh pins the embedded stylesheet to the SCSS it was
// compiled from: an SCSS edit without `just go-css` fails here, naming
// the fix. WAYLE_UPDATE_STATIC_CSS=1 (set by the recipe, right after
// grass rewrote static.css) records the new tree hash.
func TestStaticCSSIsFresh(t *testing.T) {
	sum, err := scssTreeHash(scssDir)
	if err != nil {
		t.Fatalf("hash the SCSS tree: %v", err)
	}
	if os.Getenv("WAYLE_UPDATE_STATIC_CSS") == "1" {
		if err := os.WriteFile(staticSumFile, []byte(sum+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	recorded, err := os.ReadFile(staticSumFile)
	if err != nil {
		t.Fatalf("read %s: %v", staticSumFile, err)
	}
	if got := strings.TrimSpace(string(recorded)); got != sum {
		t.Fatalf("styling/static.css is stale: the SCSS under %s changed (hash %s, compiled from %s); run `just go-css`", scssDir, sum, got)
	}
}

func TestScssTreeHashTracksEdits(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.scss", ".a { color: red; }")
	write("notes.txt", "ignored")
	base, err := scssTreeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	write("notes.txt", "still ignored")
	if again, _ := scssTreeHash(dir); again != base {
		t.Error("a non-SCSS file moved the hash")
	}
	write("a.scss", ".a { color: blue; }")
	if edited, _ := scssTreeHash(dir); edited == base {
		t.Error("an SCSS edit did not move the hash")
	}
	if _, err := scssTreeHash(dir + "/missing"); err == nil {
		t.Error("a missing tree hashed")
	}
}

// TestStaticCSSIsTheCompiledBundle spot-checks the artifact: the global
// reset, the token block, and the bar rules the Go bar relies on.
func TestStaticCSSIsTheCompiledBundle(t *testing.T) {
	for _, want := range []string{
		"* {\n  all: unset;\n}",
		"--bg-surface-elevated: color-mix(in srgb, var(--palette-surface) 93%, var(--palette-fg));",
		"menubutton.bar-button > button.toggle {",
		".bar-group {",
	} {
		if !strings.Contains(StaticCSS, want) {
			t.Errorf("static.css lacks %q", want)
		}
	}
	if strings.Contains(StaticCSS, "$") || strings.Contains(StaticCSS, "@import") {
		t.Error("static.css still carries SCSS syntax: it is not compiled output")
	}
}

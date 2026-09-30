package styling

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/scss"
)

// captureLog redirects the standard logger for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	return &buf
}

func writeStyles(t *testing.T, files map[string]string) string {
	t.Helper()
	config := t.TempDir()
	for name, text := range files {
		path := filepath.Join(config, "styles", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return config
}

func TestTryUserCSSCompilesTheEntryWithPartials(t *testing.T) {
	config := writeStyles(t, map[string]string{
		"index.scss":   "@import \"colors\";\n.bar { color: $accent; &:hover { opacity: 0.5; } }\n",
		"_colors.scss": "$accent: #ff0000;\n",
	})
	got, err := TryUserCSS(config)
	if err != nil {
		t.Fatal(err)
	}
	want := ".bar {\n  color: #ff0000;\n}\n.bar:hover {\n  opacity: 0.5;\n}\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTryUserCSSAbsentEntryIsNoOverrides(t *testing.T) {
	for name, config := range map[string]string{
		"no styles dir":   t.TempDir(),
		"no entry in dir": writeStyles(t, map[string]string{"_partial.scss": ".x { a: b; }"}),
	} {
		got, err := TryUserCSS(config)
		if got != "" || err != nil {
			t.Errorf("%s: got (%q, %v), want (\"\", nil)", name, got, err)
		}
	}
}

func TestTryUserCSSSurfacesCompileErrors(t *testing.T) {
	config := writeStyles(t, map[string]string{"index.scss": ".a {\n  b: $undefined;\n}\n"})
	got, err := TryUserCSS(config)
	var e *scss.Error
	if got != "" || !errors.As(err, &e) || e.Line != 2 {
		t.Fatalf("got (%q, %v), want a line-2 *scss.Error", got, err)
	}
	if !strings.HasSuffix(e.File, filepath.Join("styles", "index.scss")) {
		t.Errorf("error names %s, want the entry file", e.File)
	}
}

func TestUserCSSLogsAndDropsAFailure(t *testing.T) {
	logs := captureLog(t)
	config := writeStyles(t, map[string]string{"index.scss": "@each $x in a { }\n"})
	if got := UserCSS(config); got != "" {
		t.Errorf("UserCSS of a broken stylesheet = %q, want \"\"", got)
	}
	if !strings.Contains(logs.String(), "user styles compilation failed") ||
		!strings.Contains(logs.String(), "unsupported SCSS feature: @each") {
		t.Errorf("log = %q, want the compile failure", logs.String())
	}
}

func TestUserCSSReturnsTheCompiledCSSSilently(t *testing.T) {
	logs := captureLog(t)
	config := writeStyles(t, map[string]string{"index.scss": ".a { b: c; }"})
	if got := UserCSS(config); got != ".a {\n  b: c;\n}\n" {
		t.Errorf("UserCSS = %q", got)
	}
	if logs.Len() != 0 {
		t.Errorf("a clean compile logged %q", logs.String())
	}
}

func TestUserStylesDir(t *testing.T) {
	config := writeStyles(t, map[string]string{"index.scss": ""})
	if dir, ok := UserStylesDir(config); !ok || dir != filepath.Join(config, "styles") {
		t.Errorf("present: got (%q, %v)", dir, ok)
	}
	if dir, ok := UserStylesDir(t.TempDir()); ok || dir != "" {
		t.Errorf("absent: got (%q, %v), want (\"\", false)", dir, ok)
	}
	notDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(notDir, "styles"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := UserStylesDir(notDir); ok {
		t.Error("a file named styles is not a styles directory")
	}
}

func TestEnsureUserStylesScaffoldCreatesTheTemplate(t *testing.T) {
	config := filepath.Join(t.TempDir(), "wayle")
	EnsureUserStylesScaffold(config)
	got, err := os.ReadFile(filepath.Join(config, "styles", "index.scss"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "// Custom Wayle styles. Anything here overrides the built-in styling.\n" +
		"// Use @import \"name\" to bring in _name.scss from this folder.\n"
	if string(got) != want {
		t.Errorf("template = %q, want %q", got, want)
	}
	// The template itself compiles to nothing.
	if css, err := TryUserCSS(config); css != "" || err != nil {
		t.Errorf("template compiles to (%q, %v)", css, err)
	}
}

func TestEnsureUserStylesScaffoldNeverOverwrites(t *testing.T) {
	config := writeStyles(t, map[string]string{"index.scss": ".mine { a: b; }"})
	EnsureUserStylesScaffold(config)
	EnsureUserStylesScaffold(config)
	got, err := os.ReadFile(filepath.Join(config, "styles", "index.scss"))
	if err != nil || string(got) != ".mine { a: b; }" {
		t.Errorf("index.scss = (%q, %v), want the user's file untouched", got, err)
	}

	// An existing, empty styles dir only gains the entry file.
	bare := t.TempDir()
	if err := os.Mkdir(filepath.Join(bare, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	EnsureUserStylesScaffold(bare)
	if got, err := os.ReadFile(filepath.Join(bare, "styles", "index.scss")); err != nil || string(got) != userStylesTemplate {
		t.Errorf("bare dir: index.scss = (%q, %v)", got, err)
	}
}

func TestEnsureUserStylesScaffoldLogsFailures(t *testing.T) {
	logs := captureLog(t)
	config := t.TempDir()
	// styles cannot be created where a file already sits.
	if err := os.WriteFile(filepath.Join(config, "styles"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	EnsureUserStylesScaffold(config)
	if !strings.Contains(logs.String(), "cannot create user styles directory") {
		t.Errorf("log = %q, want the directory failure", logs.String())
	}

	logs.Reset()
	// The entry cannot be written where a directory sits.
	config = t.TempDir()
	if err := os.MkdirAll(filepath.Join(config, "styles", "index.scss", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	EnsureUserStylesScaffold(config)
	if logs.Len() != 0 {
		t.Errorf("an existing index.scss path (even a directory) is left alone, but logged %q", logs.String())
	}
}

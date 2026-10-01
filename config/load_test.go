package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFileMissingCreatesTheStub(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayle", "config.toml")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if cfg.Bar.Location != LocationTop || !cfg.Bar.Exclusive {
		t.Errorf("missing file: want defaults, got %+v", cfg.Bar)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "# Wayle configuration file\n" {
		t.Errorf("stub = %q, %v", data, err)
	}
}

func TestYAMLAndTOMLLoadTheSameConfig(t *testing.T) {
	dir := t.TempDir()
	toml := writeFile(t, dir, "a/config.toml", "[bar]\nlocation = \"bottom\"\nexclusive = false\n\n[modules.clock]\nformat = \"%H\"\n")
	yml := writeFile(t, dir, "b/config.yaml", "bar:\n  location: bottom\n  exclusive: false\nmodules:\n  clock:\n    format: \"%H\"\n")
	a, errA := LoadFile(toml)
	b, errB := LoadFile(yml)
	if errA != nil || errB != nil {
		t.Fatalf("load: %v / %v", errA, errB)
	}
	if a.Bar.Location != LocationBottom || a.Bar.Exclusive || a.Clock.Format != "%H" {
		t.Errorf("toml = %+v", a.Bar)
	}
	if b.Bar.Location != a.Bar.Location || b.Bar.Exclusive != a.Bar.Exclusive || b.Clock.Format != a.Clock.Format {
		t.Errorf("yaml %+v differs from toml %+v", b.Bar, a.Bar)
	}
}

func TestYAMLNullAndSyntaxErrorsFailTheFile(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"null.yaml":   "bar:\n  location: ~\n",
		"empty.yaml":  "",
		"broken.yaml": "a: : :\n  - broken",
	} {
		cfg, err := LoadFile(writeFile(t, dir, name, content))
		if err == nil {
			t.Errorf("%s: want a file error", name)
		}
		if cfg.Bar.Location != LocationTop {
			t.Errorf("%s: want the defaults back", name)
		}
	}
}

func TestBadValueKeepsItsDefaultAndTheRestApplies(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", "[bar]\nlocation = \"sideways\"\nexclusive = false\n")
	cfg, err := LoadFile(path)
	if err == nil {
		t.Fatal("invalid location: want a diagnostic error")
	}
	var d Diagnostic
	if !errors.As(err, &d) || d.Field("Field") != "bar.location" ||
		!strings.Contains(d.Field("Error"), "unknown variant `sideways`, expected one of `top`, `bottom`, `left`, `right`") {
		t.Errorf("diagnostic = %+v", err)
	}
	if cfg.Bar.Location != LocationTop {
		t.Errorf("location = %q, want the default", cfg.Bar.Location)
	}
	if cfg.Bar.Exclusive {
		t.Error("exclusive = true: the valid sibling must still apply")
	}
}

func TestTypeMismatchUsesSerdeWording(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", "[bar]\nborder-width = 300\nscale = \"big\"\n")
	_, err := LoadFile(path)
	if err == nil {
		t.Fatal("want errors")
	}
	msg := err.Error()
	for _, want := range []string{"invalid value: integer `300`, expected u8", `invalid type: string "big", expected f32`} {
		if !strings.Contains(msg, want) {
			t.Errorf("errors %q lack %q", msg, want)
		}
	}
}

func TestUnknownKeysAndNonTableSectionsAreIgnored(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", "osd = 5\n[bar]\nnot-in-the-schema = 3\n[modules.clock]\nalso-unknown = 1\n")
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("unknown keys: %v", err)
	}
}

func TestDeprecatedAliasApplies(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", "[modules.notification]\nicon-name = \"legacy\"\n")
	cfg, err := LoadFile(path)
	if err != nil || cfg.Notification.IconName != "legacy" {
		t.Fatalf("[modules.notification] alias: %q, %v", cfg.Notification.IconName, err)
	}
	path = writeFile(t, t.TempDir(), "config.toml", "[modules.notifications]\nicon-name = \"plural\"\n")
	cfg, _ = LoadFile(path)
	if cfg.Notification.IconName != "plural" {
		t.Errorf("canonical key: %q", cfg.Notification.IconName)
	}
}

func TestImportsMergeInOrderAndTheFileWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "one.toml", "[modules.clock]\nformat = \"one\"\n[bar]\nlocation = \"left\"\n")
	writeFile(t, dir, "two.yaml", "modules:\n  clock:\n    format: two\n")
	main := writeFile(t, dir, "config.toml", "imports = [\"one.toml\", \"two\"]\n[bar]\nexclusive = false\n")
	cfg, err := LoadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Clock.Format != "two" || cfg.Bar.Location != LocationLeft || cfg.Bar.Exclusive {
		t.Errorf("merged: format %q location %q exclusive %v", cfg.Clock.Format, cfg.Bar.Location, cfg.Bar.Exclusive)
	}
}

func TestImportCycleAndMissingImportFail(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.toml", "imports = [\"b.toml\"]\n")
	writeFile(t, dir, "b.toml", "imports = [\"a.toml\"]\n")
	main := writeFile(t, dir, "config.toml", "imports = [\"a.toml\"]\n")
	_, err := LoadFile(main)
	if err == nil || !strings.Contains(err.Error(), "circular import detected: config.toml -> a.toml -> b.toml -> a.toml") {
		t.Errorf("cycle: %v", err)
	}
	missing := writeFile(t, dir, "m.toml", "imports = [\"ghost.toml\"]\n")
	_, err = LoadFile(missing)
	if err == nil || !strings.Contains(err.Error(), "cannot import '"+filepath.Join(dir, "ghost.toml")+"'") {
		t.Errorf("missing import: %v", err)
	}
}

func TestResolveImportPrefersYAMLForBareNames(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "config.toml")
	if got := resolveImport(base, "theme"); got != filepath.Join(dir, "theme.toml") {
		t.Errorf("no sibling: %q, want the .toml fallback", got)
	}
	writeFile(t, dir, "theme.yml", "a: 1\n")
	if got := resolveImport(base, "theme"); got != filepath.Join(dir, "theme.yml") {
		t.Errorf("yml sibling: %q", got)
	}
	writeFile(t, dir, "theme.yaml", "a: 1\n")
	if got := resolveImport(base, "theme"); got != filepath.Join(dir, "theme.yaml") {
		t.Errorf("yaml sibling: %q, want .yaml before .yml", got)
	}
	if got := resolveImport(base, "/abs/x.toml"); got != "/abs/x.toml" {
		t.Errorf("absolute import: %q", got)
	}
}

func TestDiscoverMainPrefersYAML(t *testing.T) {
	dir := t.TempDir()
	if got := DiscoverMain(dir); got != filepath.Join(dir, "config.toml") {
		t.Errorf("empty dir: %q", got)
	}
	writeFile(t, dir, "config.yml", "")
	if got := DiscoverMain(dir); got != filepath.Join(dir, "config.yml") {
		t.Errorf("yml: %q", got)
	}
	writeFile(t, dir, "config.yaml", "")
	if got := DiscoverMain(dir); got != filepath.Join(dir, "config.yaml") {
		t.Errorf("yaml first: %q", got)
	}
}

func TestDirFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, err := Dir(); err != nil || got != "/xdg/wayle" {
		t.Errorf("xdg: %q %v", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got, err := Dir(); err != nil || got != "/home/u/.config/wayle" {
		t.Errorf("home: %q %v", got, err)
	}
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Error("neither set: want an error")
	}
}

func TestClampedNewtypesWarnAndClamp(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml",
		"[bar]\nbackground-opacity = 150\nscale = 9\npadding = \"nope\"\n[modules.cava]\nbars = 0\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("clamping is not an error: %v", err)
	}
	if cfg.Bar.BackgroundOpacity != 100 || cfg.Bar.Scale != 3 || cfg.Cava.Bars != 1 {
		t.Errorf("clamped: opacity %d scale %v bars %d", cfg.Bar.BackgroundOpacity, cfg.Bar.Scale, cfg.Cava.Bars)
	}
	if cfg.Bar.Padding != Scale(1) {
		t.Errorf("unparseable size = %+v, want the 1.0 fallback", cfg.Bar.Padding)
	}
}

func TestBarLayoutItemsFollowTheUntaggedOrder(t *testing.T) {
	path := writeFile(t, t.TempDir(), "config.toml", `
[[bar.layout]]
monitor = "DP-1"
left = ["clock", { module = "clock", class = "primary" }, { name = "g", modules = ["battery"] }]
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	left := cfg.Bar.Layout[0].Left
	if len(left) != 3 || left[0].Module != ModuleClock || left[1].Class != "primary" || !left[2].IsGroup() {
		t.Fatalf("left = %+v", left)
	}
	// An unset section takes BarLayout's own default, as serde(default)
	// fills it.
	if right := cfg.Bar.Layout[0].Right; len(right) != 5 {
		t.Errorf("right = %+v, want the default layout's right section", right)
	}
	bad := writeFile(t, t.TempDir(), "config.toml", "[[bar.layout]]\nleft = [\"nope\"]\n")
	cfg, err = LoadFile(bad)
	if err == nil || !strings.Contains(err.Error(), "data did not match any variant of untagged enum BarItem") {
		t.Errorf("unknown module: %v", err)
	}
	if len(cfg.Bar.Layout) != 1 || cfg.Bar.Layout[0].Monitor != "*" {
		t.Errorf("a bad layout keeps the default: %+v", cfg.Bar.Layout)
	}
}

func TestLoadErrorUnwraps(t *testing.T) {
	_, err := LoadFile(writeFile(t, t.TempDir(), "config.toml", "[bar\n"))
	if _, ok := errors.AsType[*LoadError](err); !ok {
		t.Fatalf("syntax error = %T %v, want a LoadError", err, err)
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if _, err := LoadFile(filepath.Join(dir, "sub", "config.toml")); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unwritable dir: %v", err)
	}
}

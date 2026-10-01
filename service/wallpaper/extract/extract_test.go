package extract

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
)

// goldenCases mirror testdata/rust/generator.rs.txt: the Rust
// ColorExtractorConfig each golden directory was recorded from. The
// recordings are the Rust extractor run against testdata/fake-tool.sh
// (argv, generated wallust config and template, saved matugen cache),
// so a match here means the real tool sees an identical invocation and
// writes an identical palette.
var goldenCases = map[string]func(*Config){
	"wallust-default": func(c *Config) { c.Tool = Wallust },
	"wallust-custom": func(c *Config) {
		c.Tool = Wallust
		c.WallustPalette = config.WallustPaletteSoftlightcomp16
		c.WallustSaturation = 35
		c.WallustCheckContrast = false
		c.WallustBackend = config.WallustBackendKmeans
		c.WallustColorspace = config.WallustColorspaceLchansi
		c.WallustApplyGlobally = false
	},
	"matugen-default": func(c *Config) { c.Tool = Matugen },
	"matugen-custom": func(c *Config) {
		c.Tool = Matugen
		c.MatugenScheme = config.MatugenSchemeFruitSalad
		c.MatugenContrast = -0.25
		c.MatugenSourceColor = 9
		c.MatugenLight = true
	},
	"matugen-contrast-one": func(c *Config) {
		c.Tool = Matugen
		c.MatugenContrast = 1.0
		c.MatugenSourceColor = 2
	},
	"pywal-default": func(c *Config) { c.Tool = Pywal },
	"pywal-custom": func(c *Config) {
		c.Tool = Pywal
		c.PywalSaturation = 0.7
		c.PywalContrast = 4.5
		c.PywalLight = true
		c.PywalApplyGlobally = false
	},
	"none": func(c *Config) { c.Tool = None },
}

// fakeTools installs testdata/fake-tool.sh as wallust, matugen, and wal
// in front of PATH and points the XDG dirs at a fresh root.
func fakeTools(t *testing.T, out string) (root string) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to run the fake tools")
	}
	root = t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/fake-tool.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wallust", "matugen", "wal"} {
		if err := os.Symlink(script, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("GOLDEN_OUT", out)
	t.Setenv("FAKE_FAIL", "")
	return root
}

func resultLine(err error) string {
	if err != nil {
		return "err: " + err.Error() + "\n"
	}
	return "ok\n"
}

func TestExtractMatchesRustGolden(t *testing.T) {
	for name, apply := range goldenCases {
		t.Run(name, func(t *testing.T) {
			out := t.TempDir()
			root := fakeTools(t, out)
			cfg := DefaultConfig()
			apply(&cfg)
			err := Extract(context.Background(), cfg, "/walls/my image.png")
			if err := os.WriteFile(filepath.Join(out, "result"), []byte(resultLine(err)), 0o600); err != nil {
				t.Fatal(err)
			}
			if cache := filepath.Join(root, "cache", "wayle", "matugen-colors.json"); fileExists(cache) {
				copyFile(t, cache, filepath.Join(out, "matugen-colors.json"))
			}
			compareDir(t, filepath.Join("testdata", "rust", name), out, root)
		})
	}
}

func TestExtractFailureCarriesTrimmedStderr(t *testing.T) {
	out := t.TempDir()
	fakeTools(t, out)
	t.Setenv("FAKE_FAIL", "1")
	cfg := DefaultConfig()
	cfg.Tool = Pywal
	err := Extract(context.Background(), cfg, "/walls/a.png")
	if _, ok := errors.AsType[*FailedError](err); !ok {
		t.Fatalf("err = %v, want a FailedError", err)
	}
	want, _ := os.ReadFile("testdata/rust/pywal-fail/result")
	if got := resultLine(err); got != string(want) {
		t.Errorf("error = %q, want the Rust %q", got, want)
	}
}

func TestExtractMissingToolIsACommandError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Tool = Matugen
	err := Extract(context.Background(), cfg, "/walls/a.png")
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Tool != Matugen {
		t.Fatalf("err = %v, want a matugen CommandError", err)
	}
	want, _ := os.ReadFile("testdata/rust/matugen-missing/result")
	if got := resultLine(err); got != string(want) {
		t.Errorf("error = %q, want the Rust %q", got, want)
	}
	if cmdErr.Unwrap() == nil {
		t.Error("the exec cause is dropped")
	}
}

func TestExtractNoneRunsNothing(t *testing.T) {
	out := t.TempDir()
	fakeTools(t, out)
	cfg := DefaultConfig()
	cfg.Tool = None
	if err := Extract(context.Background(), cfg, "/walls/a.png"); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 0 {
		t.Errorf("none ran a tool: %v", entries)
	}
}

func TestWallustPathErrorWithoutHome(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	cfg := DefaultConfig()
	err := Extract(context.Background(), cfg, "/walls/a.png")
	var pathErr *PathError
	if !errors.As(err, &pathErr) || err.Error() != "cannot access wayle data directory" {
		t.Fatalf("err = %v, want the data-directory PathError", err)
	}
}

func TestColorsPaths(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	for tool, want := range map[Tool]string{
		Matugen: filepath.Join(cache, "wayle", "matugen-colors.json"),
		Wallust: filepath.Join(cache, "wayle", "wallust-colors.json"),
		Pywal:   filepath.Join(cache, "wal", "colors.json"),
	} {
		got, ok, err := ColorsPath(tool)
		if err != nil || !ok || got != want {
			t.Errorf("ColorsPath(%v) = %q %v %v, want %q", tool, got, ok, err, want)
		}
	}
	if _, ok, err := ColorsPath(None); ok || err != nil {
		t.Errorf("ColorsPath(None) = ok %v err %v, want no path", ok, err)
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if _, ok, err := ColorsPath(Pywal); ok || err == nil {
		t.Error("a path resolved with neither XDG_CACHE_HOME nor HOME")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// compareDir asserts got holds exactly want's files with identical
// content once the recorded @ROOT@ placeholder is this run's root.
func compareDir(t *testing.T, want, got, root string) {
	t.Helper()
	wantEntries, err := os.ReadDir(want)
	if err != nil {
		t.Fatal(err)
	}
	gotEntries, err := os.ReadDir(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(wantEntries) != len(gotEntries) {
		t.Errorf("files = %v, want %v", names(gotEntries), names(wantEntries))
	}
	for _, e := range wantEntries {
		w, err := os.ReadFile(filepath.Join(want, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		g, err := os.ReadFile(filepath.Join(got, e.Name()))
		if err != nil {
			t.Errorf("%s missing: %v", e.Name(), err)
			continue
		}
		if expect := strings.ReplaceAll(string(w), "@ROOT@", root); string(g) != expect {
			t.Errorf("%s differs from the Rust recording:\n got: %q\nwant: %q", e.Name(), g, expect)
		}
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

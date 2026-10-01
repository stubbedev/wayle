package icons

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/icons/usvg"
)

// TestToSymbolicMatchesRust holds ToSymbolic and usvg.Parse to what the
// Rust transform makes of every SVG in testdata/symbolic (see
// testdata/README.md): the same bytes out, or no icon (NONE) where Rust
// gives up, and the same files rejected by the parser. SYMBOLIC_CORPUS
// adds a directory laid out the same way.
func TestToSymbolicMatchesRust(t *testing.T) {
	dirs := []string{filepath.Join("testdata", "symbolic")}
	if extra := os.Getenv("SYMBOLIC_CORPUS"); extra != "" {
		dirs = append(dirs, extra)
	}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.svg"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no corpus in %s: %v", dir, err)
		}
		for _, file := range files {
			t.Run(filepath.Base(file), func(t *testing.T) {
				src := readFile(t, file)
				golden := readFile(t, file+".golden")
				got, ok := ToSymbolic(src)
				switch {
				case golden == "NONE" && ok:
					t.Errorf("got an icon where Rust gives up:\n%s", got)
				case golden != "NONE" && !ok:
					t.Error("no icon where Rust makes one")
				case ok && got != golden:
					t.Errorf("output differs from Rust:\n%s", firstDifference(got, golden))
				}
				_, perr := usvg.Parse(src)
				if want := readFile(t, file+".parse"); (perr == nil) != (want == "OK") {
					t.Errorf("parse error = %v, Rust: %q", perr, want)
				}
			})
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// firstDifference shows the first differing line with context around
// the first differing byte.
func firstDifference(got, want string) string {
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range min(len(gl), len(wl)) {
		if gl[i] == wl[i] {
			continue
		}
		g, w := gl[i], wl[i]
		j := 0
		for j < len(g) && j < len(w) && g[j] == w[j] {
			j++
		}
		lo := max(0, j-40)
		return fmt.Sprintf("line %d:\n got  ...%s\n want ...%s", i+1, g[lo:min(len(g), j+40)], w[lo:min(len(w), j+40)])
	}
	return fmt.Sprintf("%d lines, want %d", len(gl), len(wl))
}

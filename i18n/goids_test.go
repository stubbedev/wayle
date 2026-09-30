package i18n

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// goUsage is what the repo's Go code asks of the shell domain: the ids
// passed literally to i18n.T/Attr, and every string literal in a file
// that imports this package (ids kept in tables, like the weather
// conditions).
type goUsage struct {
	calls    map[string]bool
	literals map[string]bool
}

func scanGoUsage(t *testing.T) goUsage {
	t.Helper()
	use := goUsage{calls: map[string]bool{}, literals: map[string]bool{}}
	root := ".."
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "target", "node_modules", "i18n":
				if p != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		file, err := goparser.ParseFile(token.NewFileSet(), p, nil, 0)
		if err != nil {
			return err
		}
		imports := false
		for _, imp := range file.Imports {
			if imp.Path.Value == `"github.com/stubbedev/wayle/i18n"` {
				imports = true
			}
		}
		if !imports {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BasicLit:
				if s, err := strconv.Unquote(x.Value); err == nil && x.Kind == token.STRING {
					use.literals[s] = true
				}
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok || len(x.Args) == 0 {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || pkg.Name != "i18n" || (sel.Sel.Name != "T" && sel.Sel.Name != "Attr") {
					return true
				}
				if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if id, err := strconv.Unquote(lit.Value); err == nil {
						use.calls[id] = true
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return use
}

// Every shell message id the Go code uses resolves in en-US and in
// fr, so a missing translation fails here instead of rendering the
// "No localization" marker.
func TestGoMessageIDsResolveInEveryLocale(t *testing.T) {
	use := scanGoUsage(t)
	if len(use.calls) == 0 {
		t.Fatal("found no i18n.T call sites; the scanner is broken")
	}
	assets := trees["wayle-shell-core"]
	bundleFor := func(lang string) *bundle {
		src, err := assets.Source(lang)
		if err != nil {
			t.Fatal(err)
		}
		return newBundle(MustLangID(lang), parseResource(src))
	}
	en, fr := bundleFor("en-US"), bundleFor("fr")
	has := func(b *bundle, id string) bool {
		msg := b.messages[id]
		return msg != nil && msg.value != nil
	}
	ids := map[string]bool{}
	for id := range use.calls {
		ids[id] = true
		if !has(en, id) {
			t.Errorf("en-US: message %q (called from Go) missing", id)
		}
	}
	for lit := range use.literals {
		if has(en, lit) {
			ids[lit] = true
		}
	}
	for id := range ids {
		if !has(fr, id) {
			t.Errorf("fr: message %q missing", id)
		}
	}
	// The table-held weather ids are found through the literal scan.
	if !ids["weather-light-rain"] {
		t.Error("the literal scan missed the weather condition table")
	}
}

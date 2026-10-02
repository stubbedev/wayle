// Package treetest walks gelm widget trees for the shell's surface
// tests: find the button a user would click, read what the labels say.
package treetest

import (
	"testing"

	"github.com/stubbedev/gelm/widget"
)

// Walk visits w and every descendant, depth first.
func Walk(w widget.Widget, visit func(widget.Widget)) {
	if w == nil {
		return
	}
	visit(w)
	if c, ok := w.(interface{ Children() []widget.Widget }); ok {
		for _, k := range c.Children() {
			Walk(k, visit)
		}
	}
}

// All is every widget of type T under root, in walk order.
func All[T widget.Widget](root widget.Widget) []T {
	var out []T
	Walk(root, func(w widget.Widget) {
		if t, ok := w.(T); ok {
			out = append(out, t)
		}
	})
	return out
}

// First is the first widget of type T under root.
func First[T widget.Widget](t *testing.T, root widget.Widget) T {
	t.Helper()
	all := All[T](root)
	if len(all) == 0 {
		var zero T
		t.Fatalf("no %T in the tree", zero)
		return zero
	}
	return all[0]
}

// Button is the first button whose accessible name is name.
func Button(t *testing.T, root widget.Widget, name string) *widget.Button {
	t.Helper()
	for _, b := range All[*widget.Button](root) {
		if widget.Describe(b).Name == name {
			return b
		}
	}
	t.Fatalf("no %q button", name)
	return nil
}

// WithClass is every widget under root carrying class.
func WithClass(root widget.Widget, class string) []widget.Widget {
	var out []widget.Widget
	Walk(root, func(w widget.Widget) {
		if widget.HasClass(w, class) {
			out = append(out, w)
		}
	})
	return out
}

// Labels is the text of every label under root, in walk order.
func Labels(root widget.Widget) []string {
	var out []string
	for _, l := range All[*widget.Label](root) {
		out = append(out, l.Text())
	}
	return out
}

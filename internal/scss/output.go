package scss

import "strings"

// cssBlock is one block of output CSS: a style rule (head is its
// selector list), an at-rule (head starts with @), or the root (no
// head). children are *cssBlock and cssDecl in source order.
type cssBlock struct {
	head     string
	children []any
	// bodyless is an at-rule statement such as `@layer base;`.
	bodyless bool
}

type cssDecl struct{ name, value string }

func (b *cssBlock) add(child *cssBlock) *cssBlock {
	b.children = append(b.children, child)
	return child
}

func (b *cssBlock) addDecl(name, value string) {
	b.children = append(b.children, cssDecl{name, value})
}

// empty reports a block that renders nothing; Sass drops those.
func (b *cssBlock) empty() bool {
	if b.bodyless {
		return false
	}
	for _, child := range b.children {
		switch x := child.(type) {
		case cssDecl:
			return false
		case *cssBlock:
			if !x.empty() {
				return false
			}
		}
	}
	return true
}

func (b *cssBlock) write(w *strings.Builder, indent string) {
	if b.empty() {
		return
	}
	w.WriteString(indent)
	w.WriteString(b.head)
	if b.bodyless {
		w.WriteString(";\n")
		return
	}
	w.WriteString(" {\n")
	b.writeChildren(w, indent+"  ")
	w.WriteString(indent)
	w.WriteString("}\n")
}

func (b *cssBlock) writeChildren(w *strings.Builder, indent string) {
	for _, child := range b.children {
		switch x := child.(type) {
		case cssDecl:
			w.WriteString(indent)
			w.WriteString(x.name)
			w.WriteString(": ")
			w.WriteString(x.value)
			w.WriteString(";\n")
		case *cssBlock:
			x.write(w, indent)
		}
	}
}

// output renders the compiled CSS: plain CSS @imports hoisted to the
// top, as Sass does, then the blocks in expanded style.
func (c *compiler) output() string {
	var w strings.Builder
	for _, imp := range c.cssImports {
		w.WriteString(imp)
		w.WriteByte('\n')
	}
	c.root.writeChildren(&w, "")
	return w.String()
}

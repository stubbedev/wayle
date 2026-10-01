// Package usvg is a port of the subset of usvg 0.46 (with svgtypes,
// simplecss and kurbo's arc code) that turns an SVG document into the
// tree of groups and paths the icon transform walks: element and
// attribute resolution (presentation attributes, CSS, style, inherit),
// use/switch/nested svg expansion, markers, shapes to paths, and
// fill/stroke resolution. Rendering-only features (text, images,
// filters, masks, clip geometry) are resolved only as far as they
// decide whether an element is kept.
package usvg

import (
	"errors"
	"strings"
)

// Namespaces usvg recognizes.
const (
	svgNS   = "http://www.w3.org/2000/svg"
	xlinkNS = "http://www.w3.org/1999/xlink"
	xmlNS   = "http://www.w3.org/XML/1998/namespace"
)

// xmlNode is a parsed XML element (roxmltree's element node).
type xmlNode struct {
	// space is the namespace URI when bound; an element or attribute
	// outside any namespace is unbound (a bound URI can be empty).
	space    string
	bound    bool
	name     string
	attrs    []xmlAttr
	children []*xmlNode // element children only
	parent   *xmlNode
	// text is Node::text: the first child's text, when the first child
	// is text.
	text  strings.Builder
	index int // position among the parent's element children

	ns    []nsBinding // the namespaces in scope
	nkids int         // child nodes of every kind
}

type xmlAttr struct {
	space string
	bound bool
	name  string
	value string
}

func (n *xmlNode) attrNS(space, name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name && a.bound && a.space == space {
			return a.value, true
		}
	}
	return "", false
}

func (n *xmlNode) prevSiblingElement() *xmlNode {
	if n.parent == nil || n.index == 0 {
		return nil
	}
	return n.parent.children[n.index-1]
}

// descendants walks the subtree in document order, n included.
func (n *xmlNode) descendants(visit func(*xmlNode)) {
	visit(n)
	for _, c := range n.children {
		c.descendants(visit)
	}
}

// errNoRoot is roxmltree's NoRootNode.
var errNoRoot = errors.New("the document does not have a root node")

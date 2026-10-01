package usvg

import (
	"errors"
	"strings"
)

// This file ports usvg's svgtree: the XML turned into SVG elements with
// every presentation attribute resolved (XML attributes, then CSS by
// specificity, then the style attribute, with inherit resolved), use
// elements expanded in place, unknown elements dropped.

// elements usvg knows (svgtree/names.rs); anything else is skipped with
// its subtree.
var knownElements = setOf(`feFlood radialGradient feImage stop fePointLight feConvolveMatrix
feComposite clipPath feMerge defs mask svg symbol linearGradient feSpecularLighting feFuncB filter
feFuncG circle g tref feFuncA image text line pattern use feDropShadow feSpotLight marker style switch
tspan feColorMatrix feOffset path feGaussianBlur feTile feTurbulence feMergeNode feMorphology a
textPath ellipse feComponentTransfer feDistantLight polyline polygon feBlend feDisplacementMap
feDiffuseLighting rect feFuncR`)

var presentationAttrs = setOf(`alignment-baseline baseline-shift background-color clip-path
clip-rule color color-interpolation color-interpolation-filters color-rendering direction display
dominant-baseline fill fill-opacity fill-rule filter flood-color flood-opacity font-family
font-kerning font-size font-size-adjust font-stretch font-style font-variant font-weight
glyph-orientation-horizontal glyph-orientation-vertical image-rendering isolation letter-spacing
lighting-color marker-end marker-mid marker-start mask mask-type mix-blend-mode opacity overflow
paint-order shape-rendering stop-color stop-opacity stroke stroke-dasharray stroke-dashoffset
stroke-linecap stroke-linejoin stroke-miterlimit stroke-opacity stroke-width text-anchor
text-decoration text-overflow text-rendering transform transform-origin unicode-bidi vector-effect
visibility white-space word-spacing writing-mode`)

var nonInheritableAttrs = setOf(`alignment-baseline baseline-shift clip-path display
dominant-baseline filter flood-color flood-opacity mask opacity overflow lighting-color stop-color
stop-opacity text-decoration transform transform-origin`)

var allowsInheritAttrs = setOf(`alignment-baseline baseline-shift clip-path clip-rule color
color-interpolation-filters direction display dominant-baseline fill fill-opacity fill-rule filter
flood-color flood-opacity font-family font-kerning font-size font-stretch font-style font-variant
font-weight image-rendering kerning letter-spacing marker-end marker-mid marker-start mask opacity
overflow shape-rendering stop-color stop-opacity stroke stroke-dasharray stroke-dashoffset
stroke-linecap stroke-linejoin stroke-miterlimit stroke-opacity stroke-width text-anchor
text-decoration text-rendering visibility word-spacing writing-mode`)

// possiblyNoneAttrs read as absent when set to "none".
var possiblyNoneAttrs = setOf(`mask marker-start marker-mid marker-end clip-path filter
font-size-adjust text-decoration stroke stroke-dasharray`)

// inheritDefaults are the fallbacks resolve_inherit uses.
var inheritDefaults = map[string]string{
	"image-rendering": "auto", "shape-rendering": "auto", "text-rendering": "auto",
	"clip-path": "none", "filter": "none", "marker-end": "none", "marker-mid": "none",
	"marker-start": "none", "mask": "none", "stroke": "none", "stroke-dasharray": "none",
	"text-decoration": "none", "font-stretch": "normal", "font-style": "normal",
	"font-variant": "normal", "font-weight": "normal", "letter-spacing": "normal",
	"word-spacing": "normal", "fill": "black", "flood-color": "black", "stop-color": "black",
	"fill-opacity": "1", "flood-opacity": "1", "opacity": "1", "stop-opacity": "1",
	"stroke-opacity": "1", "clip-rule": "nonzero", "fill-rule": "nonzero",
	"baseline-shift": "baseline", "color-interpolation-filters": "linearRGB", "direction": "ltr",
	"display": "inline", "font-size": "medium", "overflow": "visible", "stroke-dashoffset": "0",
	"stroke-linecap": "butt", "stroke-linejoin": "miter", "stroke-miterlimit": "4",
	"stroke-width": "1", "text-anchor": "start", "visibility": "visible", "writing-mode": "lr-tb",
}

func setOf(names string) map[string]bool {
	m := map[string]bool{}
	for n := range strings.FieldsSeq(names) {
		m[n] = true
	}
	return m
}

func isInheritable(name string) bool {
	return presentationAttrs[name] && !nonInheritableAttrs[name]
}

type attribute struct {
	name      string
	value     string
	important bool
}

// node is an svgtree element (or the root when tag is "").
type node struct {
	tag      string
	attrs    []attribute
	parent   *node
	children []*node
	doc      *document
}

type document struct {
	root  *node
	links map[string]*node
}

func (n *node) attribute(name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			if possiblyNoneAttrs[name] && a.value == "none" {
				return "", false
			}
			return a.value, true
		}
	}
	return "", false
}

func (n *node) hasAttribute(name string) bool {
	for _, a := range n.attrs {
		if a.name == name {
			return true
		}
	}
	return false
}

func (n *node) rawAttribute(name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			return a.value, true
		}
	}
	return "", false
}

func (n *node) elementID() string {
	v, _ := n.attribute("id")
	return v
}

func (n *node) parentElement() *node {
	if n.parent == nil || n.parent.tag == "" {
		return nil
	}
	return n.parent
}

// ancestors is the node and every parent up to the root.
func (n *node) ancestors() []*node {
	var out []*node
	for c := n; c != nil; c = c.parent {
		out = append(out, c)
	}
	return out
}

func (n *node) findAncestorWith(name string) *node {
	for _, a := range n.ancestors() {
		if a.hasAttribute(name) {
			return a
		}
	}
	return nil
}

// findAttributeNode is find_attribute_impl.
func (n *node) findAttributeNode(name string) *node {
	if isInheritable(name) {
		return n.findAncestorWith(name)
	}
	if n.hasAttribute(name) {
		return n
	}
	if p := n.parentElement(); p != nil && p.hasAttribute(name) {
		return p
	}
	return nil
}

func (n *node) findAttribute(name string) (string, bool) {
	a := n.findAttributeNode(name)
	if a == nil {
		return "", false
	}
	return a.attribute(name)
}

// linkedNode resolves a FuncIRI attribute (an IRI for href).
func (n *node) linkedNode(name string) *node {
	v, ok := n.attribute(name)
	if !ok {
		return nil
	}
	var id string
	if name == "href" {
		id, ok = parseIRI(v)
	} else {
		id, ok = parseFuncIRIStr(v)
	}
	if !ok {
		return nil
	}
	return n.doc.links[id]
}

func (n *node) firstChild() *node {
	if len(n.children) == 0 {
		return nil
	}
	return n.children[0]
}

// builder carries parse state.
type builder struct {
	doc   *document
	sheet []cssRule
	idMap map[string]*xmlNode
	nodes int
}

var errNodesLimit = errors.New("nodes limit reached")

// parseDocument is svgtree::Document::parse_tree.
func parseDocument(root *xmlNode) (*document, error) {
	doc := &document{root: &node{}, links: map[string]*node{}}
	doc.root.doc = doc
	b := &builder{doc: doc, idMap: map[string]*xmlNode{}}
	var styles []string
	root.descendants(func(x *xmlNode) {
		if x == root {
			return
		}
		if id, ok := x.attrAny("id"); ok {
			if _, dup := b.idMap[id]; !dup {
				b.idMap[id] = x
			}
		}
		if x.name == "style" {
			if t, ok := x.attrAny("type"); ok && t != "text/css" {
				return
			}
			if text := x.text.String(); text != "" {
				styles = append(styles, text)
			}
		}
	})
	b.sheet = parseStyleSheet(styles)
	for _, c := range root.children {
		if err := b.parseNode(c, nil, doc.root, false, 0); err != nil {
			return nil, err
		}
	}
	first := doc.root.firstChild()
	if first == nil || first.tag != "svg" {
		return nil, errNoRoot
	}
	var walk func(*node)
	walk = func(n *node) {
		if id, ok := n.attribute("id"); ok {
			doc.links[id] = n
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(doc.root)
	return doc, nil
}

func tagName(x *xmlNode) (string, bool) {
	if x.space != "" && x.space != svgNS {
		return "", false
	}
	if !knownElements[x.name] {
		return "", false
	}
	return x.name, true
}

func (b *builder) parseNode(x, origin *xmlNode, parent *node, ignoreIDs bool, depth int) error {
	if depth > 1024 {
		return errNodesLimit
	}
	tag, ok := tagName(x)
	if !ok || tag == "style" {
		return nil
	}
	if tag == "a" {
		tag = "g"
	}
	n, err := b.parseElement(x, parent, tag, ignoreIDs)
	if err != nil {
		return err
	}
	switch tag {
	case "text":
		// Text layout is not ported: the element's glyph outlines are
		// never produced, so its children are not needed.
	case "use":
		return b.parseUse(x, origin, n, depth+1)
	default:
		for _, c := range x.children {
			if err := b.parseNode(c, origin, n, ignoreIDs, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *builder) parseElement(x *xmlNode, parent *node, tag string, ignoreIDs bool) (*node, error) {
	n := &node{tag: tag, parent: parent, doc: b.doc}
	for _, a := range x.attrs {
		switch a.space {
		case "", svgNS, xlinkNS, xmlNS:
		default:
			continue
		}
		if ignoreIDs && a.name == "id" {
			continue
		}
		switch a.name {
		case "mix-blend-mode", "isolation", "font-kerning":
			continue
		case "image-rendering":
			switch a.value {
			case "smooth", "high-quality", "crisp-edges", "pixelated":
				continue
			}
		}
		b.appendAttribute(n, parent, a.name, a.value, false)
	}
	insert := func(name, value string, important bool) {
		idx := -1
		for i, a := range n.attrs {
			if a.name == name {
				idx = i
				break
			}
		}
		if !b.appendAttribute(n, parent, name, value, important) {
			return
		}
		if idx < 0 {
			return
		}
		last := len(n.attrs) - 1
		if !n.attrs[idx].important {
			n.attrs[idx], n.attrs[last] = n.attrs[last], n.attrs[idx]
		}
		n.attrs = n.attrs[:last]
	}
	write := func(d declaration) {
		switch {
		case d.name == "marker":
			insert("marker-start", d.value, d.important)
			insert("marker-mid", d.value, d.important)
			insert("marker-end", d.value, d.important)
		case d.name == "font":
			// The font shorthand only feeds text layout, which is not ported.
		case presentationAttrs[d.name]:
			insert(d.name, d.value, d.important)
		}
	}
	for _, rule := range b.sheet {
		if rule.sel.matches(x) {
			for _, d := range rule.decls {
				write(d)
			}
		}
	}
	if style, ok := x.attrAny("style"); ok {
		for _, d := range parseStyleAttr(style) {
			write(d)
		}
	}
	b.nodes++
	if b.nodes > 1_000_000 {
		return nil, errNodesLimit
	}
	parent.children = append(parent.children, n)
	return n, nil
}

// appendAttribute is append_attribute: style and class are dropped (they
// were resolved already) and inherit is resolved against the parent.
func (b *builder) appendAttribute(n, parent *node, name, value string, important bool) bool {
	switch name {
	case "style", "class":
		return false
	}
	if n.tag == "tspan" && name == "href" {
		return false
	}
	if allowsInheritAttrs[name] && value == "inherit" {
		return resolveInherit(n, parent, name)
	}
	n.attrs = append(n.attrs, attribute{name: name, value: value, important: important})
	return true
}

func resolveInherit(n, parent *node, name string) bool {
	var src *node
	if isInheritable(name) {
		src = parent.findAncestorWith(name)
	} else if parent.hasAttribute(name) {
		src = parent
	}
	if src != nil {
		for _, a := range src.attrs {
			if a.name == name {
				n.attrs = append(n.attrs, a)
				return true
			}
		}
	}
	def, ok := inheritDefaults[name]
	if !ok {
		return false
	}
	n.attrs = append(n.attrs, attribute{name: name, value: def})
	return true
}

func (b *builder) resolveHref(x *xmlNode) *xmlNode {
	v, ok := x.attrNS(xlinkNS, "href")
	if !ok {
		v, ok = x.attrAny("href")
	}
	if !ok {
		return nil
	}
	id, ok := parseIRI(v)
	if !ok {
		return nil
	}
	return b.idMap[id]
}

// parseUse is parse_svg_use_element: the referenced element parsed
// again as the use node's only child, ids dropped.
func (b *builder) parseUse(x, origin *xmlNode, n *node, depth int) error {
	link := b.resolveHref(x)
	if link == nil || link == x || link == origin {
		return nil
	}
	if _, ok := tagName(link); !ok {
		return nil
	}
	recursive := false
	link.descendants(func(d *xmlNode) {
		if recursive || d == link || d.name != "use" || d.space != svgNS {
			return
		}
		if l2 := b.resolveHref(d); l2 == x || l2 == link {
			recursive = true
		}
	})
	if recursive {
		return nil
	}
	return b.parseNode(link, x, n, true, depth+1)
}

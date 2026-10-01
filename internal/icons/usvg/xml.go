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
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
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
	space    string
	name     string
	attrs    []xmlAttr
	children []*xmlNode
	parent   *xmlNode
	text     strings.Builder
	index    int // position among the parent's element children
}

type xmlAttr struct {
	space string
	name  string
	value string
}

func (n *xmlNode) attrNS(space, name string) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name && a.space == space {
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

var entityDecl = regexp.MustCompile(`<!ENTITY\s+([A-Za-z_:][-A-Za-z0-9._:]*)\s+(?:"([^"]*)"|'([^']*)')\s*>`)

// errNoRoot is roxmltree's NoRootNode.
var errNoRoot = errors.New("the document does not have a root node")

// parseXML builds the element tree. Internal DTD entities are expanded
// (roxmltree's allow_dtd); attribute values get XML's whitespace
// normalization.
func parseXML(text string) (*xmlNode, error) {
	dec := xml.NewDecoder(strings.NewReader(text))
	dec.Strict = true
	dec.Entity = map[string]string{}
	// The text is already a string: roxmltree ignores the declared
	// encoding, and so does this.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	root := &xmlNode{}
	cur := root
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.Directive:
			for _, m := range entityDecl.FindAllStringSubmatch(string(t), -1) {
				value := m[2]
				if value == "" {
					value = m[3]
				}
				dec.Entity[m[1]] = value
			}
		case xml.StartElement:
			n := &xmlNode{space: t.Name.Space, name: t.Name.Local, parent: cur, index: len(cur.children)}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				n.attrs = append(n.attrs, xmlAttr{space: a.Name.Space, name: a.Name.Local, value: normalizeAttr(a.Value)})
			}
			cur.children = append(cur.children, n)
			cur = n
		case xml.EndElement:
			cur = cur.parent
		case xml.CharData:
			if cur != root {
				cur.text.Write(bytes.Clone(t))
			}
		}
	}
	if len(root.children) == 0 {
		return nil, errNoRoot
	}
	if cur != root {
		return nil, errors.New("unexpected end of stream")
	}
	return root, nil
}

func normalizeAttr(v string) string {
	if !strings.ContainsAny(v, "\t\n\r") {
		return v
	}
	v = strings.ReplaceAll(v, "\r\n", "\n")
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(v)
}

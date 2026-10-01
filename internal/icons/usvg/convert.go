package usvg

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/stubbedev/wayle/internal/icons/skpath"
)

// Tree is the converted document (usvg's Tree, trimmed to what the icon
// transform reads).
type Tree struct {
	Width, Height float32
	Root          *Group
}

// Group is a container with a transform relative to its parent.
type Group struct {
	Transform skpath.Transform
	Children  []any // *Group or *Path
}

// Path is one shape: its local geometry and how it is painted.
type Path struct {
	Data    *skpath.Path
	HasFill bool
	Stroke  *Stroke
}

// Stroke is a resolved stroke.
type Stroke struct {
	Width      float32
	MiterLimit float32
	LineCap    skpath.LineCap
	LineJoin   skpath.LineJoin
	DashArray  []float32 // nil for a solid stroke
	DashOffset float32
}

// ToSkia is Stroke::to_tiny_skia.
func (s *Stroke) ToSkia() skpath.Stroke {
	st := skpath.Stroke{Width: s.Width, MiterLimit: s.MiterLimit, LineCap: s.LineCap, LineJoin: s.LineJoin}
	if s.DashArray != nil {
		if d, ok := skpath.NewStrokeDash(s.DashArray, s.DashOffset); ok {
			st.Dash = d
		}
	}
	return st
}

// Error kinds, with usvg's messages.
var (
	ErrInvalidSize = errors.New("SVG has an invalid size")
)

// ParseError is usvg's Error::ParsingFailed.
type ParseError struct{ Cause error }

func (e *ParseError) Error() string { return "SVG data parsing failed cause " + e.Cause.Error() }

func (e *ParseError) Unwrap() error { return e.Cause }

// Parse is Tree::from_str with default options.
func Parse(text string) (*Tree, error) {
	x, err := parseXML(text)
	if err != nil {
		return nil, &ParseError{Cause: err}
	}
	doc, err := parseDocument(x)
	if err != nil {
		return nil, &ParseError{Cause: err}
	}
	return convertDoc(doc)
}

const (
	optDPI      float32 = 96
	optFontSize float32 = 12
)

type rect struct{ x, y, w, h float32 }

// state is converter::State.
type state struct {
	viewBox rect
	useSize [2]*float32
	context *contextPaint
	// parentMarkers are the markers being instantiated, outermost first.
	parentMarkers []*node
}

type contextPaint struct {
	fill   *fillInfo
	stroke *Stroke
}

type fillInfo struct{}

func convertDoc(doc *document) (*Tree, error) {
	svg := doc.root.firstChild()
	w, h, restore, ok := resolveSVGSize(svg)
	if !ok {
		return nil, ErrInvalidSize
	}
	vb, hasVB := svg.parseViewBox()
	if !hasVB {
		vb = rect{0, 0, w, h}
	}
	aspect := svg.aspectAttr()
	tree := &Tree{Width: w, Height: h, Root: &Group{Transform: skpath.Identity}}
	if !svg.isVisibleElement() {
		return tree, nil
	}
	st := &state{viewBox: vb}
	rootTS := viewBoxTransform(vb, aspect, w, h)
	background := false
	if v, ok := svg.attribute("background-color"); ok {
		if p, ok := parsePaint(v); ok && p.kind == paintColor {
			background = true
		}
	}
	if rootTS.IsIdentity() && !background {
		convertChildren(doc.root, st, tree.Root)
	} else {
		g := &Group{Transform: rootTS}
		if background {
			if r, ok := skpath.RectFromXYWH(vb.x, vb.y, vb.w, vb.h); ok {
				g.Children = append(g.Children, &Path{Data: skpath.FromRect(r), HasFill: true})
			}
		}
		convertChildren(doc.root, st, g)
		tree.Root.Children = append(tree.Root.Children, g)
	}
	if restore {
		if r, ok := absBoundingBox(tree.Root, skpath.Identity); ok && r.Right > 0 && r.Bottom > 0 {
			tree.Width, tree.Height = r.Right, r.Bottom
		}
	}
	return tree, nil
}

// absBoundingBox is the union of every path's object bbox in root
// coordinates (Group::abs_bounding_box).
func absBoundingBox(g *Group, parent skpath.Transform) (skpath.Rect, bool) {
	ts := parent.PreConcat(g.Transform)
	var out skpath.Rect
	have := false
	add := func(r skpath.Rect) {
		if !have {
			out, have = r, true
			return
		}
		out.Left = min(out.Left, r.Left)
		out.Top = min(out.Top, r.Top)
		out.Right = max(out.Right, r.Right)
		out.Bottom = max(out.Bottom, r.Bottom)
	}
	for _, c := range g.Children {
		switch c := c.(type) {
		case *Group:
			if r, ok := absBoundingBox(c, ts); ok {
				add(r)
			}
		case *Path:
			if p, ok := c.Data.Transform(ts); ok {
				if r, ok := p.ComputeTightBounds(); ok {
					add(r)
				}
			}
		}
	}
	return out, have
}

func viewBoxTransform(vb rect, aspect aspectRatio, w, h float32) skpath.Transform {
	sx := w / vb.w
	sy := h / vb.h
	if aspect.align != alignNone {
		var s float32
		if aspect.slice {
			s = sx
			if sx < sy {
				s = sy
			}
		} else {
			s = sx
			if sx > sy {
				s = sy
			}
		}
		sx, sy = s, s
	}
	x := -vb.x * sx
	y := -vb.y * sy
	ww := w - vb.w*sx
	hh := h - vb.h*sy
	tx, ty := alignedPos(aspect.align, x, y, ww, hh)
	return skpath.FromRow(sx, 0, 0, sy, tx, ty)
}

func alignedPos(a align, x, y, w, h float32) (float32, float32) {
	switch a {
	case alignXMidYMin:
		return x + w/2, y
	case alignXMaxYMin:
		return x + w, y
	case alignXMinYMid:
		return x, y + h/2
	case alignXMidYMid:
		return x + w/2, y + h/2
	case alignXMaxYMid:
		return x + w, y + h/2
	case alignXMinYMax:
		return x, y + h
	case alignXMidYMax:
		return x + w/2, y + h
	case alignXMaxYMax:
		return x + w, y + h
	default:
		return x, y
	}
}

// aspectAttr is attribute::<AspectRatio>(PreserveAspectRatio), the
// default when absent or invalid.
func (n *node) aspectAttr() aspectRatio {
	if v, ok := n.attribute("preserveAspectRatio"); ok {
		if a, ok := parseAspectRatio(v); ok {
			return a
		}
	}
	return defaultAspect
}

func (n *node) parseViewBox() (rect, bool) {
	v, ok := n.attribute("viewBox")
	if !ok {
		return rect{}, false
	}
	vb, ok := parseViewBox(v)
	if !ok {
		return rect{}, false
	}
	r := rect{float32(vb.x), float32(vb.y), float32(vb.w), float32(vb.h)}
	if !(r.w > 0 && r.h > 0) || !finite(r.x) || !finite(r.y) || !finite(r.w) || !finite(r.h) {
		return rect{}, false
	}
	return r, true
}

func finite(f float32) bool { return !math.IsInf(float64(f), 0) && !math.IsNaN(float64(f)) }

func resolveSVGSize(svg *node) (float32, float32, bool, bool) {
	st := &state{viewBox: rect{0, 0, 100, 100}}
	def := length{100, unitPercent}
	width := svg.lengthAttr("width", def)
	height := svg.lengthAttr("height", def)
	vb, hasVB := svg.parseViewBox()
	restore := (width.unit == unitPercent || height.unit == unitPercent) && !hasVB
	var w, h float32
	if hasVB {
		st.viewBox = vb
		if width.unit == unitPercent {
			w = vb.w * (float32(width.number) / 100)
		} else {
			w = svg.userLength("width", st, def)
		}
		if height.unit == unitPercent {
			h = vb.h * (float32(height.number) / 100)
		} else {
			h = svg.userLength("height", st, def)
		}
	} else {
		// usvg re-reads the attributes here, so a percentage resolves
		// against the 100x100 fallback view box.
		w = svg.userLength("width", st, def)
		h = svg.userLength("height", st, def)
	}
	ok := w > 0 && h > 0 && finite(w) && finite(h)
	return w, h, restore, ok
}

// lengthAttr is attribute::<Length> with a default for absent or
// unparseable values.
func (n *node) lengthAttr(name string, def length) length {
	v, ok := n.attribute(name)
	if !ok {
		return def
	}
	l, ok := parseLengthStr(v)
	if !ok {
		return def
	}
	return l
}

func (n *node) userLength(name string, st *state, def length) float32 {
	return convertLength(n.lengthAttr(name, def), n, name, false, st)
}

var (
	xLike = setOf("cx dx fx markerWidth refX rx width x x1 x2")
	yLike = setOf("cy dy fy height markerHeight refY ry y y1 y2")
)

// convertLength is units::convert_length.
func convertLength(l length, n *node, name string, objectBBox bool, st *state) float32 {
	num := float32(l.number)
	switch l.unit {
	case unitEm:
		return num * resolveFontSize(n)
	case unitEx:
		return num * resolveFontSize(n) / 2
	case unitIn:
		return num * optDPI
	case unitCm:
		return num * optDPI / 2.54
	case unitMm:
		return num * optDPI / 25.4
	case unitPt:
		return num * optDPI / 72
	case unitPc:
		return num * optDPI / 6
	case unitPercent:
		if objectBBox {
			return num / 100
		}
		switch {
		case xLike[name]:
			return st.viewBox.w * num / 100
		case yLike[name]:
			return st.viewBox.h * num / 100
		default:
			vbLen := st.viewBox.w*st.viewBox.w + st.viewBox.h*st.viewBox.h
			vbLen = float32(math.Sqrt(float64(vbLen / 2)))
			return vbLen * num / 100
		}
	default:
		return num
	}
}

func resolveFontSize(n *node) float32 {
	anc := n.ancestors()
	fontSize := optFontSize
	for i := len(anc) - 2; i >= 0; i-- {
		a := anc[i]
		v, ok := a.rawAttribute("font-size")
		if !ok {
			continue
		}
		if l, ok := parseLengthStr(v); ok {
			num := float32(l.number)
			switch l.unit {
			case unitNone, unitPx:
				fontSize = num
			case unitEm:
				fontSize = num * fontSize
			case unitEx:
				fontSize = num * fontSize / 2
			case unitIn:
				fontSize = num * optDPI
			case unitCm:
				fontSize = num * optDPI / 2.54
			case unitMm:
				fontSize = num * optDPI / 25.4
			case unitPt:
				fontSize = num * optDPI / 72
			case unitPc:
				fontSize = num * optDPI / 6
			case unitPercent:
				fontSize = float32(l.number) * fontSize * 0.01
			}
			continue
		}
		factors := map[string]int{"xx-small": -3, "x-small": -2, "small": -1, "medium": 0, "large": 1, "x-large": 2, "xx-large": 3, "smaller": -1, "larger": 1}
		fontSize *= float32(math.Pow(float64(float32(1.2)), float64(factors[v])))
	}
	return fontSize
}

// resolveLength is SvgNode::resolve_length: the nearest ancestor's
// value, the default when it does not parse.
func (n *node) resolveLength(name string, st *state, def float32) float32 {
	a := n.findAncestorWith(name)
	if a == nil {
		return def
	}
	v, ok := a.attribute(name)
	if !ok {
		return def
	}
	l, ok := parseLengthStr(v)
	if !ok {
		return def
	}
	return convertLength(l, a, name, false, st)
}

func (n *node) isVisibleElement() bool {
	if v, ok := n.attribute("display"); ok && v == "none" {
		return false
	}
	if !n.hasValidTransform() {
		return false
	}
	return isConditionPassed(n)
}

func (n *node) hasValidTransform() bool {
	v, ok := n.attribute("transform")
	if !ok {
		return true
	}
	ts, ok := parseTransform(v)
	if !ok {
		return true
	}
	return skpath.FromRow(float32(ts.a), float32(ts.b), float32(ts.c), float32(ts.d), float32(ts.e), float32(ts.f)).IsValid()
}

// transformAttr is attribute::<Transform>: an invalid matrix reads as
// identity, an unparseable one as absent.
func (n *node) transformAttr(name string) skpath.Transform {
	v, ok := n.attribute(name)
	if !ok {
		return skpath.Identity
	}
	ts, ok := parseTransform(v)
	if !ok {
		return skpath.Identity
	}
	t := skpath.FromRow(float32(ts.a), float32(ts.b), float32(ts.c), float32(ts.d), float32(ts.e), float32(ts.f))
	if !t.IsValid() {
		return skpath.Identity
	}
	return t
}

// resolveTransform is SvgNode::resolve_transform with transform-origin.
func (n *node) resolveTransform(st *state) skpath.Transform {
	ts := n.transformAttr("transform")
	v, ok := n.attribute("transform-origin")
	if !ok {
		return ts
	}
	ox, oy, ok := parseTransformOrigin(v)
	if !ok {
		return ts
	}
	dx := convertLength(ox, n, "width", false, st)
	dy := convertLength(oy, n, "height", false, st)
	return skpath.Identity.PreTranslate(dx, dy).PreConcat(ts).PreTranslate(-dx, -dy)
}

// parseTransformOrigin is TransformOrigin::from_str for one or two
// position tokens (a length/percent or a keyword).
func parseTransformOrigin(text string) (length, length, bool) {
	fields := strings.Fields(strings.ReplaceAll(text, ",", " "))
	if len(fields) == 0 || len(fields) > 3 {
		return length{}, length{}, false
	}
	keyword := func(f string) (length, bool) {
		switch f {
		case "left", "top":
			return length{0, unitPercent}, true
		case "center":
			return length{50, unitPercent}, true
		case "right", "bottom":
			return length{100, unitPercent}, true
		}
		return parseLengthStr(f)
	}
	x, ok := keyword(fields[0])
	if !ok {
		return length{}, length{}, false
	}
	y := length{50, unitPercent}
	if len(fields) >= 2 {
		if y, ok = keyword(fields[1]); !ok {
			return length{}, length{}, false
		}
	}
	if fields[0] == "top" || fields[0] == "bottom" {
		x, y = y, x
		if len(fields) == 1 {
			x = length{50, unitPercent}
		}
	}
	return x, y, true
}

var switchFeatures = setOf(`http://www.w3.org/TR/SVG11/feature#SVGDOM-static
http://www.w3.org/TR/SVG11/feature#SVG-static http://www.w3.org/TR/SVG11/feature#CoreAttribute
http://www.w3.org/TR/SVG11/feature#Structure http://www.w3.org/TR/SVG11/feature#BasicStructure
http://www.w3.org/TR/SVG11/feature#ContainerAttribute http://www.w3.org/TR/SVG11/feature#ConditionalProcessing
http://www.w3.org/TR/SVG11/feature#Image http://www.w3.org/TR/SVG11/feature#Style
http://www.w3.org/TR/SVG11/feature#Shape http://www.w3.org/TR/SVG11/feature#Text
http://www.w3.org/TR/SVG11/feature#BasicText http://www.w3.org/TR/SVG11/feature#PaintAttribute
http://www.w3.org/TR/SVG11/feature#BasicPaintAttribute http://www.w3.org/TR/SVG11/feature#OpacityAttribute
http://www.w3.org/TR/SVG11/feature#GraphicsAttribute http://www.w3.org/TR/SVG11/feature#BasicGraphicsAttribute
http://www.w3.org/TR/SVG11/feature#Marker http://www.w3.org/TR/SVG11/feature#Gradient
http://www.w3.org/TR/SVG11/feature#Pattern http://www.w3.org/TR/SVG11/feature#Clip
http://www.w3.org/TR/SVG11/feature#BasicClip http://www.w3.org/TR/SVG11/feature#Mask
http://www.w3.org/TR/SVG11/feature#Filter http://www.w3.org/TR/SVG11/feature#BasicFilter
http://www.w3.org/TR/SVG11/feature#XlinkAttribute`)

// isConditionPassed is switch::is_condition_passed with usvg's default
// languages (["en"]).
func isConditionPassed(n *node) bool {
	if n.hasAttribute("requiredExtensions") {
		return false
	}
	if v, ok := n.attribute("requiredFeatures"); ok {
		for f := range strings.SplitSeq(v, " ") {
			if !switchFeatures[f] {
				return false
			}
		}
	}
	if v, ok := n.attribute("systemLanguage"); ok {
		for lang := range strings.SplitSeq(v, ",") {
			lang = strings.TrimSpace(lang)
			if lang == "en" {
				return true
			}
			if i := strings.IndexByte(lang, '-'); i >= 0 && lang[:i] == "en" {
				return true
			}
		}
		return false
	}
	return true
}

var graphicElements = setOf("circle ellipse image line path polygon polyline rect text use")

func convertChildren(parent *node, st *state, g *Group) {
	for _, c := range parent.children {
		convertElement(c, st, g)
	}
}

func convertElement(n *node, st *state, parent *Group) {
	if !graphicElements[n.tag] && n.tag != "g" && n.tag != "switch" && n.tag != "svg" {
		return
	}
	if !n.isVisibleElement() {
		return
	}
	switch n.tag {
	case "use":
		convertUse(n, st, parent)
		return
	case "switch":
		convertSwitch(n, st, parent)
		return
	}
	convertGroup(n, st, parent, func(g *Group) { convertElementImpl(n, st, g) })
}

func convertElementImpl(n *node, st *state, g *Group) {
	switch n.tag {
	case "rect", "circle", "ellipse", "line", "polyline", "polygon", "path":
		if p, ok := convertShape(n, st); ok {
			convertPath(n, p, st, g)
		}
	case "svg":
		if n.parentElement() != nil {
			convertNestedSVG(n, st, g)
		} else {
			convertChildren(n, st, g)
		}
	case "g":
		convertChildren(n, st, g)
	}
}

// convertGroup is converter::convert_group. usvg only materializes a
// group when it is needed; since an identity transform composes to the
// same numbers, a group is always emitted here, and what remains of
// the "required" logic is the elements usvg drops outright: a
// clip-path or mask linking to an element of the wrong kind, and an
// invalid filter.
func convertGroup(n *node, st *state, parent *Group, collect func(*Group)) {
	g := &Group{Transform: n.resolveTransform(st)}
	collect(g)
	if keepGroup(n, st, g) {
		parent.Children = append(parent.Children, g)
	}
}

// keepGroup is the tail of convert_group: with the children collected,
// a clip-path, mask, or filter that fails to convert drops the element.
func keepGroup(n *node, st *state, g *Group) bool {
	hasBBox := hasObjectBBox(g)
	if link := n.linkedNode("clip-path"); link != nil && !clipPathValid(link, st, hasBBox, 0) {
		return false
	}
	if link := n.linkedNode("mask"); link != nil && !maskValid(link, st, hasBBox, 0) {
		return false
	}
	if v, ok := n.attribute("filter"); ok && !filtersValid(n, v, hasBBox) {
		return false
	}
	return true
}

// hasObjectBBox is whether Group::calculate_object_bbox is Some: the
// union of the children's object boxes in the group's space is not
// zero-sized.
func hasObjectBBox(g *Group) bool {
	r, ok := absBoundingBox(&Group{Transform: skpath.Identity, Children: g.Children}, skpath.Identity)
	return ok && r.Width() > 0 && r.Height() > 0
}

// clipPathValid is clippath::convert's success.
func clipPathValid(n *node, st *state, hasBBox bool, depth int) bool {
	if n.tag != "clipPath" || depth > 32 {
		return false
	}
	if v, ok := n.attribute("transform"); ok {
		if _, ok := parseTransform(v); !ok {
			return false
		}
	}
	if v, ok := n.attribute("clipPathUnits"); ok && v == "objectBoundingBox" && !hasBBox {
		return false
	}
	if link := n.linkedNode("clip-path"); link != nil && !clipPathValid(link, st, hasBBox, depth+1) {
		return false
	}
	if n.elementID() == "" {
		return false
	}
	return clipHasContent(n, st)
}

// clipHasContent is convert_clip_path_elements producing any node.
func clipHasContent(clip *node, st *state) bool {
	for _, c := range clip.children {
		if !graphicElements[c.tag] || !c.isVisibleElement() {
			continue
		}
		if c.tag == "use" {
			if c.firstChild() != nil {
				return true
			}
			continue
		}
		if !c.resolveTransform(st).IsIdentity() {
			return true // a required group is kept even when empty
		}
		switch c.tag {
		case "rect", "circle", "ellipse", "polyline", "polygon", "path":
			if p, ok := convertShape(c, st); ok && p.Len() >= 2 {
				return true
			}
		case "text":
			return true
		}
	}
	return false
}

// maskValid is mask::convert's success.
func maskValid(n *node, st *state, hasBBox bool, depth int) bool {
	if n.tag != "mask" || depth > 32 {
		return false
	}
	obb := true
	if v, ok := n.attribute("maskUnits"); ok && v == "userSpaceOnUse" {
		obb = false
	}
	x := convertLength(n.lengthAttr("x", length{-10, unitPercent}), n, "x", obb, st)
	y := convertLength(n.lengthAttr("y", length{-10, unitPercent}), n, "y", obb, st)
	w := convertLength(n.lengthAttr("width", length{120, unitPercent}), n, "width", obb, st)
	h := convertLength(n.lengthAttr("height", length{120, unitPercent}), n, "height", obb, st)
	if !(w > 0 && h > 0) || !finite(x) || !finite(y) || !finite(w) || !finite(h) {
		return false
	}
	if n.elementID() == "" {
		return false
	}
	if obb && !hasBBox {
		return true // the whole element is masked
	}
	if link := n.linkedNode("mask"); link != nil && !maskValid(link, st, hasBBox, depth+1) {
		return false
	}
	if v, ok := n.attribute("maskContentUnits"); ok && v == "objectBoundingBox" && !hasBBox {
		return false
	}
	g := &Group{Transform: skpath.Identity}
	convertChildren(n, st, g)
	return len(g.Children) > 0
}

// filtersValid approximates filter::convert's success: a filter list
// fails only when it has url() references to missing or non-filter
// elements and yields no filter at all (filter functions yield one
// when the element has a non-zero bounding box).
func filtersValid(n *node, v string, hasBBox bool) bool {
	s := stream{text: v}
	valid, invalidURL := 0, false
	for {
		s.skipSpaces()
		if s.atEnd() {
			break
		}
		if s.startsWith("url(") {
			link, ok := s.parseFuncIRI()
			if !ok {
				return true // an unparseable list is ignored
			}
			if t := n.doc.links[link]; t != nil && filterElementValid(t, hasBBox) {
				valid++
			} else {
				invalidURL = true
			}
			continue
		}
		name := s.consumeASCIIIdent()
		if name == "" || s.consumeByte('(') != nil {
			return true
		}
		for !s.atEnd() && s.text[s.pos] != ')' {
			s.pos++
		}
		if s.consumeByte(')') != nil {
			return true
		}
		if hasBBox {
			valid++
		}
	}
	return valid != 0 || !invalidURL
}

func convertPath(n *node, data *skpath.Path, st *state, g *Group) {
	if data.Len() < 2 {
		return
	}
	b := data.Bounds()
	hasBBox := b.Width() > 0 && b.Height() > 0
	hasFill := resolveFill(n, hasBBox, st)
	stroke := resolveStroke(n, hasBBox, st)
	var markers *Group
	if markersValid(n) && n.visible() {
		inner := *st
		inner.context = &contextPaint{stroke: stroke}
		if hasFill {
			inner.context.fill = &fillInfo{}
		}
		markers = &Group{Transform: skpath.Identity}
		convertMarkers(n, data, &inner, markers)
	}
	if _, ok := data.ComputeTightBounds(); !ok {
		return
	}
	path := &Path{Data: data, HasFill: hasFill, Stroke: stroke}
	if markers == nil {
		g.Children = append(g.Children, path)
		return
	}
	// The markers sit where paint-order puts them; markers between fill
	// and stroke split the path into its two paints.
	paint, _ := n.findAttribute("paint-order")
	switch order := parsePaintOrder(paint); orderMarkers {
	case order[0]:
		g.Children = append(g.Children, markers, path)
	case order[1]:
		g.Children = appendPaint(g.Children, order[0], path)
		g.Children = append(g.Children, markers)
		g.Children = appendPaint(g.Children, order[2], path)
	default:
		g.Children = append(g.Children, path, markers)
	}
}

// appendPaint is append_single_paint_path: the path with only its fill,
// or only its stroke, when it has that paint.
func appendPaint(children []any, kind paintOrderKind, p *Path) []any {
	switch {
	case kind == orderFill && p.HasFill:
		return append(children, &Path{Data: p.Data, HasFill: true})
	case kind == orderStroke && p.Stroke != nil:
		return append(children, &Path{Data: p.Data, Stroke: p.Stroke})
	}
	return children
}

// visible is find_attribute::<Visibility> being visible (the default
// for an absent or unknown value).
func (n *node) visible() bool {
	v, ok := n.findAttribute("visibility")
	return !ok || (v != "hidden" && v != "collapse")
}

func convertSwitch(n *node, st *state, parent *Group) {
	var child *node
	for _, c := range n.children {
		if isConditionPassed(c) {
			child = c
			break
		}
	}
	if child == nil {
		return
	}
	convertGroup(n, st, parent, func(g *Group) { convertElement(child, st, g) })
}

// convertUse is use_node::convert.
func convertUse(n *node, st *state, parent *Group) {
	child := n.firstChild()
	if child == nil {
		return
	}
	useState := *st
	useState.context = &contextPaint{stroke: resolveStroke(n, true, st)}
	if resolveFill(n, true, st) {
		useState.context.fill = &fillInfo{}
	}
	origTS := n.resolveTransform(st)
	newTS := skpath.Identity.PreTranslate(n.userLength("x", &useState, length{}), n.userLength("y", &useState, length{}))
	if child.tag == "symbol" {
		def := length{100, unitPercent}
		vb := useState.viewBox
		w := vb.w
		if n.hasAttribute("width") {
			w = n.userLength("width", &useState, def)
		}
		h := vb.h
		if n.hasAttribute("height") {
			h = n.userLength("height", &useState, def)
		}
		if w > 0 && h > 0 && finite(w) && finite(h) {
			useState.viewBox = rect{vb.x, vb.y, w, h}
		}
		if ts, ok := useViewBoxTransform(n, child, &useState); ok {
			newTS = newTS.PreConcat(ts)
		}
		// The use group's own transform is reset: orig_ts moves onto the
		// symbol's content group.
		ts := origTS.PreConcat(newTS)
		g := &Group{Transform: skpath.Identity}
		useChildren(child, ts, &useState, g)
		if keepGroup(n, &useState, g) {
			parent.Children = append(parent.Children, g)
		}
		return
	}
	ts := origTS.PreConcat(newTS)
	if child.tag == "svg" {
		useState.useSize = [2]*float32{}
		def := length{100, unitPercent}
		if n.hasAttribute("width") {
			w := n.userLength("width", &useState, def)
			useState.useSize[0] = &w
		}
		if n.hasAttribute("height") {
			h := n.userLength("height", &useState, def)
			useState.useSize[1] = &h
		}
	}
	useChildren(n, ts, &useState, parent)
}

// useChildren is use_node::convert_children: the node's children under
// a group carrying ts.
func useChildren(n *node, ts skpath.Transform, st *state, parent *Group) {
	g := &Group{Transform: ts}
	convertChildren(n, st, g)
	if keepGroup(n, st, g) {
		parent.Children = append(parent.Children, g)
	}
}

func useViewBoxTransform(n, linked *node, st *state) (skpath.Transform, bool) {
	def := length{100, unitPercent}
	w := n.userLength("width", st, def)
	h := n.userLength("height", st, def)
	if n.tag == "svg" {
		if st.useSize[0] != nil {
			w = *st.useSize[0]
		}
		if st.useSize[1] != nil {
			h = *st.useSize[1]
		}
	}
	if !(w > 0 && h > 0 && finite(w) && finite(h)) {
		return skpath.Transform{}, false
	}
	vb, ok := linked.parseViewBox()
	if !ok {
		return skpath.Transform{}, false
	}
	return viewBoxTransform(vb, linked.aspectAttr(), w, h), true
}

// convertNestedSVG is use_node::convert_svg.
func convertNestedSVG(n *node, st *state, parent *Group) {
	origTS := n.resolveTransform(st)
	x := n.userLength("x", st, length{})
	y := n.userLength("y", st, length{})
	newTS := skpath.Identity.PreTranslate(x, y)
	if ts, ok := useViewBoxTransform(n, n, st); ok {
		newTS = newTS.PreConcat(ts)
	}
	newState := *st
	if vb, ok := n.parseViewBox(); ok {
		newState.viewBox = vb
	} else {
		def := length{100, unitPercent}
		w := n.userLength("width", st, def)
		h := n.userLength("height", st, def)
		if st.useSize[0] != nil {
			w = *st.useSize[0]
		}
		if st.useSize[1] != nil {
			h = *st.useSize[1]
		}
		if w > 0 && h > 0 && finite(w) && finite(h) {
			newState.viewBox = rect{x, y, w, h}
		}
	}
	useChildren(n, origTS.PreConcat(newTS), &newState, parent)
}

// resolveFill is style::resolve_fill, reduced to whether there is one.
func resolveFill(n *node, hasBBox bool, st *state) bool {
	a := n.findAncestorWith("fill")
	if a == nil {
		return true // black
	}
	v, ok := a.attribute("fill")
	if !ok {
		return false
	}
	p, ok := parsePaint(v)
	if !ok {
		return true // unparseable fill falls back to black
	}
	return paintPresent(n, p, hasBBox, st)
}

// resolveStroke is style::resolve_stroke.
func resolveStroke(n *node, hasBBox bool, st *state) *Stroke {
	a := n.findAncestorWith("stroke")
	if a == nil {
		return nil
	}
	v, ok := a.attribute("stroke")
	if !ok {
		return nil
	}
	p, ok := parsePaint(v)
	if !ok {
		return nil
	}
	if !paintPresent(n, p, hasBBox, st) {
		return nil
	}
	width := n.resolveLength("stroke-width", st, 1)
	if !(width > 0 && finite(width)) {
		return nil
	}
	miter := float32(4)
	if v, ok := n.findAttribute("stroke-miterlimit"); ok {
		if f, ok := parseNumberStr(v); ok {
			miter = float32(f)
		}
	}
	if miter < 1 {
		miter = 1
	}
	s := &Stroke{
		Width:      width,
		MiterLimit: miter,
		DashArray:  dashArray(n, st),
		DashOffset: n.resolveLength("stroke-dashoffset", st, 0),
	}
	if v, ok := n.findAttribute("stroke-linecap"); ok {
		switch v {
		case "round":
			s.LineCap = skpath.CapRound
		case "square":
			s.LineCap = skpath.CapSquare
		}
	}
	if v, ok := n.findAttribute("stroke-linejoin"); ok {
		switch v {
		case "miter-clip":
			s.LineJoin = skpath.JoinMiterClip
		case "round":
			s.LineJoin = skpath.JoinRound
		case "bevel":
			s.LineJoin = skpath.JoinBevel
		}
	}
	return s
}

// paintPresent is convert_paint reduced to Some/None.
func paintPresent(n *node, p paint, hasBBox bool, st *state) bool {
	switch p.kind {
	case paintNone, paintInherit:
		return false
	case paintContextFill:
		return st.context != nil && st.context.fill != nil
	case paintContextStroke:
		return st.context != nil && st.context.stroke != nil
	case paintCurrentColor, paintColor:
		return true
	}
	link := n.doc.links[p.link]
	if link == nil {
		return p.fallback == fallbackCurrentColor || p.fallback == fallbackColor
	}
	switch link.tag {
	case "linearGradient", "radialGradient", "pattern":
	default:
		return false
	}
	server, color := paintServer(link)
	switch {
	case color:
		return true
	case server:
		if !hasBBox && serverUnitsBBox(link) {
			return p.fallback == fallbackCurrentColor || p.fallback == fallbackColor
		}
		return true
	default:
		return p.fallback == fallbackCurrentColor || p.fallback == fallbackColor
	}
}

// hrefChain is the node and the elements its href links through.
func hrefChain(n *node) []*node {
	chain := []*node{n}
	seen := map[*node]bool{n: true}
	for cur := n; ; {
		next := cur.linkedNode("href")
		if next == nil || seen[next] {
			return chain
		}
		seen[next] = true
		chain = append(chain, next)
		cur = next
	}
}

// paintServer approximates paint_server::convert: a gradient with two
// or more stops is a server, one stop is a plain color, none is
// invalid; a pattern needs a positive size and children.
func paintServer(n *node) (server, color bool) {
	chain := hrefChain(n)
	if n.tag == "pattern" {
		var w, h float32
		for _, c := range chain {
			if v, ok := c.attribute("width"); ok {
				if l, ok := parseLengthStr(v); ok {
					w = float32(l.number)
					break
				}
			}
		}
		for _, c := range chain {
			if v, ok := c.attribute("height"); ok {
				if l, ok := parseLengthStr(v); ok {
					h = float32(l.number)
					break
				}
			}
		}
		if !(w > 0 && h > 0) {
			return false, false
		}
		for _, c := range chain {
			if len(c.children) > 0 {
				return true, false
			}
		}
		return false, false
	}
	for _, c := range chain {
		if c.tag != "linearGradient" && c.tag != "radialGradient" {
			continue
		}
		stops := 0
		for _, s := range c.children {
			if s.tag == "stop" {
				stops++
			}
		}
		switch {
		case stops >= 2:
			return true, false
		case stops == 1:
			return false, true
		}
	}
	return false, false
}

func serverUnitsBBox(n *node) bool {
	name := "gradientUnits"
	if n.tag == "pattern" {
		name = "patternUnits"
	}
	for _, c := range hrefChain(n) {
		if v, ok := c.attribute(name); ok {
			return v != "userSpaceOnUse"
		}
	}
	return true
}

// filterElementValid is filter::convert_url's success: a filter element
// with a positive region, a bounding box when the region is relative to
// it, and primitives somewhere along its href chain.
func filterElementValid(n *node, hasBBox bool) bool {
	if n.tag != "filter" {
		return false
	}
	chain := hrefChain(n)
	attr := func(name string) (string, bool) {
		for _, c := range chain {
			if v, ok := c.attribute(name); ok {
				return v, true
			}
		}
		return "", false
	}
	obb := true
	if v, ok := attr("filterUnits"); ok && v == "userSpaceOnUse" {
		obb = false
	}
	num := func(name string, def length) float32 {
		l := def
		if v, ok := attr(name); ok {
			if p, ok := parseLengthStr(v); ok {
				l = p
			}
		}
		return convertLength(l, n, name, obb, &state{viewBox: rect{0, 0, 100, 100}})
	}
	w := num("width", length{120, unitPercent})
	h := num("height", length{120, unitPercent})
	if !(w > 0 && h > 0) {
		return false
	}
	if obb && !hasBBox {
		return false
	}
	for _, c := range chain {
		if c.tag == "filter" && len(c.children) > 0 {
			return true
		}
	}
	return false
}

// dashArray is style::conv_dasharray.
func dashArray(n *node, st *state) []float32 {
	a := n.findAncestorWith("stroke-dasharray")
	if a == nil {
		return nil
	}
	v, ok := a.attribute("stroke-dasharray")
	if !ok {
		return nil
	}
	var list []float32
	for _, l := range parseLengthList(v) {
		list = append(list, convertLength(l, a, "stroke-dasharray", false, st))
	}
	var sum float32
	for _, x := range list {
		if math.Signbit(float64(x)) {
			return nil
		}
		sum += x
	}
	if approxZeroUlps(sum) {
		return nil
	}
	if len(list)%2 != 0 {
		list = append(list, list...)
	}
	return list
}

// approxZeroUlps is usvg's approx_zero_ulps(4): approx_eq_ulps(&0.0, 4),
// so a tiny negative is not zero.
func approxZeroUlps(f float32) bool { return approxEqUlps(f, 0) }

// convertShape is shapes::convert.
func convertShape(n *node, st *state) (*skpath.Path, bool) {
	switch n.tag {
	case "rect":
		return convertRect(n, st)
	case "circle":
		cx := n.userLength("cx", st, length{})
		cy := n.userLength("cy", st, length{})
		r := n.userLength("r", st, length{})
		if !(r > 0 && finite(r)) {
			return nil, false
		}
		return ellipseToPath(cx, cy, r, r)
	case "ellipse":
		cx := n.userLength("cx", st, length{})
		cy := n.userLength("cy", st, length{})
		rx, ry := resolveRxRy(n, st)
		if !(rx > 0 && finite(rx)) || !(ry > 0 && finite(ry)) {
			return nil, false
		}
		return ellipseToPath(cx, cy, rx, ry)
	case "line":
		b := skpath.NewBuilder()
		b.MoveTo(n.userLength("x1", st, length{}), n.userLength("y1", st, length{}))
		b.LineTo(n.userLength("x2", st, length{}), n.userLength("y2", st, length{}))
		return b.Finish()
	case "polyline", "polygon":
		v, ok := n.attribute("points")
		if !ok {
			return nil, false
		}
		b := skpath.NewBuilder()
		for _, p := range parsePoints(v) {
			if b.IsEmpty() {
				b.MoveTo(float32(p[0]), float32(p[1]))
			} else {
				b.LineTo(float32(p[0]), float32(p[1]))
			}
		}
		if b.Len() < 2 {
			return nil, false
		}
		if n.tag == "polygon" {
			b.Close()
		}
		return b.Finish()
	case "path":
		v, ok := n.attribute("d")
		if !ok {
			return nil, false
		}
		b := skpath.NewBuilder()
		for _, s := range simplifyPath(v) {
			switch s.kind {
			case 'M':
				b.MoveTo(float32(s.x), float32(s.y))
			case 'L':
				b.LineTo(float32(s.x), float32(s.y))
			case 'Q':
				b.QuadTo(float32(s.x1), float32(s.y1), float32(s.x), float32(s.y))
			case 'C':
				b.CubicTo(float32(s.x1), float32(s.y1), float32(s.x2), float32(s.y2), float32(s.x), float32(s.y))
			case 'Z':
				b.Close()
			}
		}
		return b.Finish()
	}
	return nil, false
}

func convertRect(n *node, st *state) (*skpath.Path, bool) {
	w := n.userLength("width", st, length{})
	h := n.userLength("height", st, length{})
	if !(w > 0 && finite(w)) || !(h > 0 && finite(h)) {
		return nil, false
	}
	x := n.userLength("x", st, length{})
	y := n.userLength("y", st, length{})
	rx, ry := resolveRxRy(n, st)
	if rx > w/2 {
		rx = w / 2
	}
	if ry > h/2 {
		ry = h / 2
	}
	if approxZeroUlps(rx) {
		r, ok := skpath.RectFromXYWH(x, y, w, h)
		if !ok {
			return nil, false
		}
		return skpath.FromRect(r), true
	}
	b := skpath.NewBuilder()
	b.MoveTo(x+rx, y)
	b.LineTo(x+w-rx, y)
	arcTo(b, rx, ry, x+w, y+ry)
	b.LineTo(x+w, y+h-ry)
	arcTo(b, rx, ry, x+w-rx, y+h)
	b.LineTo(x+rx, y+h)
	arcTo(b, rx, ry, x, y+h-ry)
	b.LineTo(x, y+ry)
	arcTo(b, rx, ry, x+rx, y)
	b.Close()
	return b.Finish()
}

func resolveRxRy(n *node, st *state) (float32, float32) {
	get := func(name string) (length, bool) {
		v, ok := n.attribute(name)
		if !ok {
			return length{}, false
		}
		l, ok := parseLengthStr(v)
		if !ok || math.Signbit(l.number) {
			return length{}, false
		}
		return l, true
	}
	rx, hasRx := get("rx")
	ry, hasRy := get("ry")
	switch {
	case !hasRx && !hasRy:
		return 0, 0
	case hasRx && !hasRy:
		v := convertLength(rx, n, "rx", false, st)
		return v, v
	case !hasRx && hasRy:
		v := convertLength(ry, n, "ry", false, st)
		return v, v
	default:
		return convertLength(rx, n, "rx", false, st), convertLength(ry, n, "ry", false, st)
	}
}

func ellipseToPath(cx, cy, rx, ry float32) (*skpath.Path, bool) {
	b := skpath.NewBuilder()
	b.MoveTo(cx+rx, cy)
	arcTo(b, rx, ry, cx, cy+ry)
	arcTo(b, rx, ry, cx-rx, cy)
	arcTo(b, rx, ry, cx, cy-ry)
	arcTo(b, rx, ry, cx+rx, cy)
	b.Close()
	return b.Finish()
}

// arcTo is shapes.rs's PathBuilderExt::arc_to (no rotation, small arc,
// positive sweep).
func arcTo(b *skpath.Builder, rx, ry, x, y float32) {
	prev, ok := b.LastPoint()
	if !ok {
		return
	}
	arc, ok := arcFromSVG(float64(prev.X), float64(prev.Y), float64(x), float64(y), float64(rx), float64(ry), 0, false, true)
	if !ok {
		b.LineTo(x, y)
		return
	}
	arc.toCubics(0.1, func(x1, y1, x2, y2, x, y float64) {
		b.CubicTo(float32(x1), float32(y1), float32(x2), float32(y2), float32(x), float32(y))
	})
}

// String is a debug rendering of the tree.
func (t *Tree) String() string { return fmt.Sprintf("Tree{%gx%g}", t.Width, t.Height) }

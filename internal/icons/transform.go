package icons

import (
	"fmt"
	"strings"

	"github.com/stubbedev/wayle/internal/icons/skpath"
	"github.com/stubbedev/wayle/internal/icons/usvg"
)

// This file ports crates/wayle-icons/src/transform.rs: re-emit an
// arbitrary symbolic-style SVG as a filled-path-only GTK Grappa
// symbolic icon, outlining strokes into filled polygons.

const (
	targetSize float32 = 16
	// resolutionScale is the stroker/dasher precision target.
	resolutionScale float32 = 4
	// FormatVersion is stamped onto every emitted SVG via gpa:version.
	FormatVersion = 2
)

type scaledPath struct {
	d             string
	isBoundingBox bool
}

// ToSymbolic is transform::to_symbolic; false when nothing can be
// extracted.
func ToSymbolic(svg string) (string, bool) {
	tree, err := usvg.Parse(svg)
	if err != nil {
		return buildFallbackSVG(svg)
	}
	sourceSize := max(tree.Width, tree.Height)
	scale := float32(1)
	if sourceSize > 0 {
		scale = targetSize / sourceSize
	}
	var paths []scaledPath
	collectPaths(tree.Root, skpath.FromScale(scale, scale), &paths)
	if len(paths) == 0 {
		return buildFallbackSVG(svg)
	}
	return buildGTKSVG(paths), true
}

func collectPaths(g *usvg.Group, parent skpath.Transform, paths *[]scaledPath) {
	for _, child := range g.Children {
		switch c := child.(type) {
		case *usvg.Path:
			processPath(c, parent, paths)
		case *usvg.Group:
			collectPaths(c, parent.PreConcat(c.Transform), paths)
		}
	}
}

func processPath(p *usvg.Path, parent skpath.Transform, paths *[]scaledPath) {
	transformed, ok := p.Data.Transform(parent)
	if !ok {
		return
	}
	if p.HasFill {
		pushPath(transformed, false, paths)
	}
	if p.Stroke != nil {
		if outlined, ok := outlineStroke(transformed, p.Stroke, parent); ok {
			pushPath(outlined, false, paths)
		}
	}
	if !p.HasFill && p.Stroke == nil {
		pushPath(transformed, isBoundingBoxPath(transformed, targetSize), paths)
	}
}

func pushPath(p *skpath.Path, bbox bool, paths *[]scaledPath) {
	if d := pathData(p); d != "" {
		*paths = append(*paths, scaledPath{d: d, isBoundingBox: bbox})
	}
}

func outlineStroke(geometry *skpath.Path, s *usvg.Stroke, parent skpath.Transform) (*skpath.Path, bool) {
	st := s.ToSkia()
	st.Width *= skpath.ComputeResolutionScale(parent)
	if st.Dash != nil {
		dash := st.Dash
		st.Dash = nil
		dashed, ok := geometry.DashPath(dash, resolutionScale)
		if !ok {
			return nil, false
		}
		return dashed.StrokePath(st, resolutionScale)
	}
	return geometry.StrokePath(st, resolutionScale)
}

// isBoundingBoxPath reports a four-corner rectangle covering the whole
// 16x16 canvas (the invisible sizing rect icon sets carry).
func isBoundingBoxPath(p *skpath.Path, size float32) bool {
	it := p.Segments()
	var segs []skpath.Segment
	for {
		s, ok := it.Next()
		if !ok {
			break
		}
		segs = append(segs, s)
	}
	if len(segs) != 5 {
		return false
	}
	const eps float32 = 0.5
	abs := func(f float32) float32 {
		if f < 0 {
			return -f
		}
		return f
	}
	var corners []skpath.Point
	for _, s := range segs {
		if s.Kind == skpath.SegMove || s.Kind == skpath.SegLine {
			corners = append(corners, s.P[0])
		}
	}
	if len(corners) != 4 {
		return false
	}
	var origin, topRight, bottomRight, bottomLeft bool
	for _, c := range corners {
		left := abs(c.X) < eps
		right := abs(c.X-size) < eps
		top := abs(c.Y) < eps
		bottom := abs(c.Y-size) < eps
		switch {
		case left && top:
			origin = true
		case right && top:
			topRight = true
		case right && bottom:
			bottomRight = true
		case left && bottom:
			bottomLeft = true
		}
	}
	return origin && topRight && bottomRight && bottomLeft
}

func pathData(p *skpath.Path) string {
	var b strings.Builder
	it := p.Segments()
	for {
		s, ok := it.Next()
		if !ok {
			break
		}
		switch s.Kind {
		case skpath.SegMove:
			writeCommand(&b, 'M', s.P[0].X, s.P[0].Y)
		case skpath.SegLine:
			writeCommand(&b, 'L', s.P[0].X, s.P[0].Y)
		case skpath.SegQuad:
			writeCommand(&b, 'Q', s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y)
		case skpath.SegCubic:
			writeCommand(&b, 'C', s.P[0].X, s.P[0].Y, s.P[1].X, s.P[1].Y, s.P[2].X, s.P[2].Y)
		case skpath.SegClose:
			b.WriteByte('Z')
		}
	}
	return b.String()
}

func writeCommand(b *strings.Builder, cmd byte, coords ...float32) {
	b.WriteByte(cmd)
	for i, c := range coords {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(formatCoord(c))
	}
}

// formatCoord is Rust's `{:.2}` for an f32: the exact binary value
// rounded half-to-even at two decimals, "-0.00" for small negatives,
// "NaN"/"inf" spelled as Rust does.
func formatCoord(f float32) string {
	switch {
	case f != f:
		return "NaN"
	case f > 3.4e38:
		return "inf"
	case f < -3.4e38:
		return "-inf"
	}
	return fmt.Sprintf("%.2f", float64(f))
}

func buildGTKSVG(paths []scaledPath) string {
	var b strings.Builder
	b.WriteString("<svg width='16' height='16'\n")
	b.WriteString("     xmlns:gpa='https://www.gtk.org/grappa'\n")
	fmt.Fprintf(&b, "     gpa:version='%d'>\n", FormatVersion)
	for _, p := range paths {
		if !p.isBoundingBox {
			b.WriteString(buildFillPathElement(p.d))
		}
	}
	b.WriteString("</svg>\n")
	return b.String()
}

func buildFillPathElement(d string) string {
	return "  <path d='" + d + "'\nstroke='none'\nfill='rgb(0,0,0)'\ngpa:fill='foreground'/>\n"
}

func buildFallbackSVG(original string) (string, bool) {
	d, ok := extractPathDFallback(original)
	if !ok {
		return "", false
	}
	return buildGTKSVG([]scaledPath{{d: d}}), true
}

func extractPathDFallback(content string) (string, bool) {
	if d, ok := extractQuotedAttr(content, `d="`, '"'); ok {
		return d, true
	}
	return extractQuotedAttr(content, "d='", '\'')
}

func extractQuotedAttr(content, prefix string, quote byte) (string, bool) {
	start := strings.Index(content, prefix)
	if start < 0 {
		return "", false
	}
	start += len(prefix)
	end := strings.IndexByte(content[start:], quote)
	if end < 0 {
		return "", false
	}
	return content[start : start+end], true
}

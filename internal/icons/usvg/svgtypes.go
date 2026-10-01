package usvg

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// This file ports the svgtypes 0.16 parsers the converter uses.

var errParse = errors.New("parse error")

// stream is svgtypes' Stream: a byte cursor over the text.
type stream struct {
	text string
	pos  int
}

func (s *stream) atEnd() bool { return s.pos >= len(s.text) }

func (s *stream) curr() (byte, bool) {
	if s.atEnd() {
		return 0, false
	}
	return s.text[s.pos], true
}

func (s *stream) isCurr(c byte) bool { b, ok := s.curr(); return ok && b == c }

func (s *stream) next() (byte, bool) {
	if s.pos+1 >= len(s.text) {
		return 0, false
	}
	return s.text[s.pos+1], true
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func (s *stream) skipSpaces() {
	for !s.atEnd() && isSpace(s.text[s.pos]) {
		s.pos++
	}
}

func (s *stream) startsWith(p string) bool { return strings.HasPrefix(s.text[s.pos:], p) }

func (s *stream) consumeByte(c byte) error {
	if b, ok := s.curr(); !ok || b != c {
		return errParse
	}
	s.pos++
	return nil
}

func (s *stream) skipDigits() {
	for !s.atEnd() && s.text[s.pos] >= '0' && s.text[s.pos] <= '9' {
		s.pos++
	}
}

func (s *stream) parseListSeparator() {
	if s.isCurr(',') {
		s.pos++
	}
}

func isASCIIIdent(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-' || c == '_'
}

func (s *stream) consumeASCIIIdent() string {
	start := s.pos
	for !s.atEnd() && isASCIIIdent(s.text[s.pos]) {
		s.pos++
	}
	return s.text[start:s.pos]
}

// parseNumber is Stream::parse_number: an SVG number, finite only.
func (s *stream) parseNumber() (float64, error) {
	s.skipSpaces()
	if s.atEnd() {
		return 0, errParse
	}
	start := s.pos
	n, err := s.parseNumberImpl()
	if err != nil {
		s.pos = start
		return 0, errParse
	}
	return n, nil
}

func (s *stream) parseNumberImpl() (float64, error) {
	start := s.pos
	c, ok := s.curr()
	if !ok {
		return 0, errParse
	}
	if c == '+' || c == '-' {
		s.pos++
		if c, ok = s.curr(); !ok {
			return 0, errParse
		}
	}
	switch {
	case c >= '0' && c <= '9':
		s.skipDigits()
	case c == '.':
	default:
		return 0, errParse
	}
	if s.isCurr('.') {
		s.pos++
		s.skipDigits()
	}
	if c, ok := s.curr(); ok && (c == 'e' || c == 'E') {
		c2, ok := s.next()
		if !ok {
			return 0, errParse
		}
		if c2 != 'm' && c2 != 'x' {
			s.pos++
			c3, _ := s.curr()
			switch {
			case c3 == '+' || c3 == '-':
				s.pos++
				s.skipDigits()
			case c3 >= '0' && c3 <= '9':
				s.skipDigits()
			default:
				return 0, errParse
			}
		}
	}
	n, err := parseRustFloat(s.text[start:s.pos])
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, errParse
	}
	return n, nil
}

// parseRustFloat is f64::from_str over the already-validated number
// grammar (strconv agrees with it there, except that Rust takes a
// trailing-dot "1." and a leading-dot ".5", which strconv also does).
func parseRustFloat(t string) (float64, error) {
	body := strings.TrimLeft(t, "+-")
	if body == "" || body == "." || strings.HasPrefix(body, ".e") || strings.HasPrefix(body, ".E") {
		return 0, errParse
	}
	v, err := strconv.ParseFloat(t, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, err
	}
	return v, nil
}

func (s *stream) parseListNumber() (float64, error) {
	if s.atEnd() {
		return 0, errParse
	}
	n, err := s.parseNumber()
	if err != nil {
		return 0, err
	}
	s.skipSpaces()
	s.parseListSeparator()
	return n, nil
}

func (s *stream) parseNumberOrPercent() (float64, error) {
	s.skipSpaces()
	n, err := s.parseNumber()
	if err != nil {
		return 0, err
	}
	if s.startsWith("%") {
		s.pos++
		return n / 100, nil
	}
	return n, nil
}

// skipListNumberOrPercent consumes one list number or percentage.
func (s *stream) skipListNumberOrPercent() error {
	if s.atEnd() {
		return errParse
	}
	if _, err := s.parseNumberOrPercent(); err != nil {
		return err
	}
	s.skipSpaces()
	s.parseListSeparator()
	return nil
}

// parseNumberStr is Number::from_str: a lone number.
func parseNumberStr(text string) (float64, bool) {
	s := stream{text: text}
	n, err := s.parseNumber()
	if err != nil {
		return 0, false
	}
	s.skipSpaces()
	return n, s.atEnd()
}

// lengthUnit is svgtypes' LengthUnit.
type lengthUnit uint8

const (
	unitNone lengthUnit = iota
	unitEm
	unitEx
	unitPx
	unitIn
	unitCm
	unitMm
	unitPt
	unitPc
	unitPercent
)

type length struct {
	number float64
	unit   lengthUnit
}

func (s *stream) parseLength() (length, error) {
	s.skipSpaces()
	n, err := s.parseNumber()
	if err != nil {
		return length{}, err
	}
	if s.atEnd() {
		return length{n, unitNone}, nil
	}
	units := []struct {
		text string
		unit lengthUnit
	}{{"%", unitPercent}, {"em", unitEm}, {"ex", unitEx}, {"px", unitPx}, {"in", unitIn}, {"cm", unitCm}, {"mm", unitMm}, {"pt", unitPt}, {"pc", unitPc}}
	for _, u := range units {
		if s.startsWith(u.text) {
			s.pos += len(u.text)
			return length{n, u.unit}, nil
		}
	}
	return length{n, unitNone}, nil
}

// parseLengthStr is Length::from_str.
func parseLengthStr(text string) (length, bool) {
	s := stream{text: text}
	l, err := s.parseLength()
	if err != nil || !s.atEnd() {
		return length{}, false
	}
	return l, true
}

// parseLengthList is LengthListParser with .flatten(): values up to the
// first error.
func parseLengthList(text string) []length {
	s := stream{text: text}
	var out []length
	for !s.atEnd() {
		if s.atEnd() {
			break
		}
		l, err := s.parseLength()
		if err != nil {
			break
		}
		s.skipSpaces()
		s.parseListSeparator()
		out = append(out, l)
	}
	return out
}

// parsePoints is PointsParser.
func parsePoints(text string) [][2]float64 {
	s := stream{text: text}
	var out [][2]float64
	for !s.atEnd() {
		x, err := s.parseListNumber()
		if err != nil {
			break
		}
		y, err := s.parseListNumber()
		if err != nil {
			break
		}
		out = append(out, [2]float64{x, y})
	}
	return out
}

// viewBox is svgtypes' ViewBox.
type viewBox struct{ x, y, w, h float64 }

func parseViewBox(text string) (viewBox, bool) {
	s := stream{text: text}
	var v [4]float64
	for i := range v {
		n, err := s.parseListNumber()
		if err != nil {
			return viewBox{}, false
		}
		v[i] = n
	}
	if v[2] <= 0 || v[3] <= 0 {
		return viewBox{}, false
	}
	return viewBox{v[0], v[1], v[2], v[3]}, true
}

// align is svgtypes' Align.
type align uint8

const (
	alignNone align = iota
	alignXMinYMin
	alignXMidYMin
	alignXMaxYMin
	alignXMinYMid
	alignXMidYMid
	alignXMaxYMid
	alignXMinYMax
	alignXMidYMax
	alignXMaxYMax
)

type aspectRatio struct {
	align align
	slice bool
}

var defaultAspect = aspectRatio{align: alignXMidYMid}

func parseAspectRatio(text string) (aspectRatio, bool) {
	s := stream{text: text}
	s.skipSpaces()
	if s.startsWith("defer") {
		s.pos += 5
		if s.consumeByte(' ') != nil {
			return aspectRatio{}, false
		}
		s.skipSpaces()
	}
	names := map[string]align{
		"none": alignNone, "xMinYMin": alignXMinYMin, "xMidYMin": alignXMidYMin, "xMaxYMin": alignXMaxYMin,
		"xMinYMid": alignXMinYMid, "xMidYMid": alignXMidYMid, "xMaxYMid": alignXMaxYMid,
		"xMinYMax": alignXMinYMax, "xMidYMax": alignXMidYMax, "xMaxYMax": alignXMaxYMax,
	}
	a, ok := names[s.consumeASCIIIdent()]
	if !ok {
		return aspectRatio{}, false
	}
	s.skipSpaces()
	slice := false
	if !s.atEnd() {
		switch s.consumeASCIIIdent() {
		case "meet", "":
		case "slice":
			slice = true
		default:
			return aspectRatio{}, false
		}
	}
	return aspectRatio{align: a, slice: slice}, true
}

// transform is svgtypes' f64 Transform.
type transform struct{ a, b, c, d, e, f float64 }

var identityTS = transform{1, 0, 0, 1, 0, 0}

func multiply(t1, t2 transform) transform {
	return transform{
		a: t1.a*t2.a + t1.c*t2.b,
		b: t1.b*t2.a + t1.d*t2.b,
		c: t1.a*t2.c + t1.c*t2.d,
		d: t1.b*t2.c + t1.d*t2.d,
		e: t1.a*t2.e + t1.c*t2.f + t1.e,
		f: t1.b*t2.e + t1.d*t2.f + t1.f,
	}
}

func toRadians(deg float64) float64 { return deg * (math.Pi / 180) }

// parseTransform is Transform::from_str over TransformListParser.
func parseTransform(text string) (transform, bool) {
	s := stream{text: text}
	ts := identityTS
	for {
		s.skipSpaces()
		if s.atEnd() {
			return ts, true
		}
		name := s.consumeASCIIIdent()
		s.skipSpaces()
		if s.consumeByte('(') != nil {
			return transform{}, false
		}
		nums := func(n int) ([]float64, bool) {
			out := make([]float64, n)
			for i := range out {
				v, err := s.parseListNumber()
				if err != nil {
					return nil, false
				}
				out[i] = v
			}
			return out, true
		}
		switch name {
		case "matrix":
			v, ok := nums(6)
			if !ok {
				return transform{}, false
			}
			ts = multiply(ts, transform{v[0], v[1], v[2], v[3], v[4], v[5]})
		case "translate", "scale":
			v, ok := nums(1)
			if !ok {
				return transform{}, false
			}
			s.skipSpaces()
			second := 0.0
			if name == "scale" {
				second = v[0]
			}
			if !s.isCurr(')') {
				w, ok := nums(1)
				if !ok {
					return transform{}, false
				}
				second = w[0]
			}
			if name == "translate" {
				ts = multiply(ts, transform{1, 0, 0, 1, v[0], second})
			} else {
				ts = multiply(ts, transform{v[0], 0, 0, second, 0, 0})
			}
		case "rotate":
			v, ok := nums(1)
			if !ok {
				return transform{}, false
			}
			s.skipSpaces()
			rot := func(angle float64) transform {
				r := toRadians(angle)
				b, a := sincos(r)
				return transform{a, b, -b, a, 0, 0}
			}
			if !s.isCurr(')') {
				c, ok := nums(2)
				if !ok {
					return transform{}, false
				}
				ts = multiply(ts, transform{1, 0, 0, 1, c[0], c[1]})
				ts = multiply(ts, rot(v[0]))
				ts = multiply(ts, transform{1, 0, 0, 1, -c[0], -c[1]})
			} else {
				ts = multiply(ts, rot(v[0]))
			}
		case "skewX":
			v, ok := nums(1)
			if !ok {
				return transform{}, false
			}
			ts = multiply(ts, transform{1, 0, tanR(toRadians(v[0])), 1, 0, 0})
		case "skewY":
			v, ok := nums(1)
			if !ok {
				return transform{}, false
			}
			ts = multiply(ts, transform{1, tanR(toRadians(v[0])), 0, 1, 0, 0})
		default:
			return transform{}, false
		}
		s.skipSpaces()
		if s.consumeByte(')') != nil {
			return transform{}, false
		}
		s.skipSpaces()
		if s.isCurr(',') {
			s.pos++
		}
	}
}

// parseIRI is IRI::from_str: "#id".
func parseIRI(text string) (string, bool) {
	s := stream{text: text}
	s.skipSpaces()
	if s.consumeByte('#') != nil {
		return "", false
	}
	start := s.pos
	for !s.atEnd() && s.text[s.pos] != ' ' {
		s.pos++
	}
	link := s.text[start:s.pos]
	if link == "" {
		return "", false
	}
	s.skipSpaces()
	return link, s.atEnd()
}

// parseFuncIRI parses "url(#id)" at the cursor.
func (s *stream) parseFuncIRI() (string, bool) {
	s.skipSpaces()
	if !s.startsWith("url(") {
		return "", false
	}
	s.pos += 4
	s.skipSpaces()
	var quote byte
	if c, ok := s.curr(); ok && (c == '\'' || c == '"') {
		quote = c
		s.pos++
		s.skipSpaces()
	}
	if s.consumeByte('#') != nil {
		return "", false
	}
	start := s.pos
	var link string
	if quote != 0 {
		for !s.atEnd() && s.text[s.pos] != quote {
			s.pos++
		}
		link = strings.TrimRight(s.text[start:s.pos], " \t\n\r")
	} else {
		for !s.atEnd() && s.text[s.pos] != ' ' && s.text[s.pos] != ')' {
			s.pos++
		}
		link = s.text[start:s.pos]
	}
	if link == "" || strings.ContainsAny(link, `'"`) {
		return "", false
	}
	s.skipSpaces()
	if quote != 0 {
		if s.consumeByte(quote) != nil {
			return "", false
		}
		s.skipSpaces()
	}
	if s.consumeByte(')') != nil {
		return "", false
	}
	return link, true
}

// parseFuncIRIStr is FuncIRI::from_str.
func parseFuncIRIStr(text string) (string, bool) {
	s := stream{text: text}
	link, ok := s.parseFuncIRI()
	if !ok {
		return "", false
	}
	s.skipSpaces()
	return link, s.atEnd()
}

// validColor is whether text is a whole color (the converter.s outcome
// depends on nothing else about it).
func validColor(text string) bool {
	s := stream{text: text}
	if _, ok := s.parseColor(); !ok {
		return false
	}
	s.skipSpaces()
	return s.atEnd()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func roundU8(v float64) uint8 {
	r := math.Round(v)
	switch {
	case r != r || r <= 0:
		return 0
	case r >= 255:
		return 255
	default:
		return uint8(r)
	}
}

// parseColor is Stream::parse_color; it returns the alpha.
func (s *stream) parseColor() (uint8, bool) {
	s.skipSpaces()
	c, ok := s.curr()
	if !ok {
		return 0, false
	}
	alpha := uint8(255)
	if c == '#' {
		s.pos++
		start := s.pos
		for !s.atEnd() && isHex(s.text[s.pos]) {
			s.pos++
		}
		hex := s.text[start:s.pos]
		switch len(hex) {
		case 3, 6:
		case 4:
			v, _ := strconv.ParseUint(hex[3:4], 16, 8)
			alpha = uint8(v<<4 | v)
		case 8:
			v, _ := strconv.ParseUint(hex[6:8], 16, 8)
			alpha = uint8(v)
		default:
			return 0, false
		}
		return alpha, true
	}
	name := strings.ToLower(s.consumeASCIIIdent())
	switch name {
	case "rgb", "rgba":
		if s.consumeByte('(') != nil {
			return 0, false
		}
		if _, err := s.parseNumber(); err != nil {
			return 0, false
		}
		percent := false
		if s.startsWith("%") {
			s.pos++
			percent = true
		}
		s.skipSpaces()
		s.parseListSeparator()
		for range 2 {
			var err error
			if percent {
				err = s.skipListNumberOrPercent()
			} else {
				_, err = s.parseListNumber()
			}
			if err != nil {
				return 0, false
			}
		}
		s.skipSpaces()
		if !s.startsWith(")") {
			v, err := s.parseListNumber()
			if err != nil {
				return 0, false
			}
			alpha = roundU8(v * 255)
		}
		s.skipSpaces()
		return alpha, s.consumeByte(')') == nil
	case "hsl", "hsla":
		if s.consumeByte('(') != nil {
			return 0, false
		}
		if _, err := s.parseListNumber(); err != nil {
			return 0, false
		}
		for range 2 {
			if err := s.skipListNumberOrPercent(); err != nil {
				return 0, false
			}
		}
		s.skipSpaces()
		if !s.startsWith(")") {
			v, err := s.parseListNumber()
			if err != nil {
				return 0, false
			}
			alpha = roundU8(v * 255)
		}
		s.skipSpaces()
		return alpha, s.consumeByte(')') == nil
	default:
		if _, ok := namedColors[name]; !ok {
			return 0, false
		}
		if name == "transparent" {
			return 0, true
		}
		return 255, true
	}
}

// paintKind is svgtypes' Paint.
type paintKind uint8

const (
	paintNone paintKind = iota
	paintInherit
	paintCurrentColor
	paintColor
	paintFuncIRI
	paintContextFill
	paintContextStroke
)

type fallbackKind uint8

const (
	fallbackAbsent fallbackKind = iota
	fallbackNone
	fallbackCurrentColor
	fallbackColor
)

type paint struct {
	kind     paintKind
	link     string
	fallback fallbackKind
}

func parsePaint(text string) (paint, bool) {
	text = strings.TrimSpace(text)
	switch text {
	case "none":
		return paint{kind: paintNone}, true
	case "inherit":
		return paint{kind: paintInherit}, true
	case "currentColor":
		return paint{kind: paintCurrentColor}, true
	case "context-fill":
		return paint{kind: paintContextFill}, true
	case "context-stroke":
		return paint{kind: paintContextStroke}, true
	}
	s := stream{text: text}
	if s.startsWith("url(") {
		link, ok := s.parseFuncIRI()
		if !ok {
			return paint{}, false
		}
		s.skipSpaces()
		if s.atEnd() {
			return paint{kind: paintFuncIRI, link: link}, true
		}
		switch rest := s.text[s.pos:]; rest {
		case "none":
			return paint{kind: paintFuncIRI, link: link, fallback: fallbackNone}, true
		case "currentColor":
			return paint{kind: paintFuncIRI, link: link, fallback: fallbackCurrentColor}, true
		default:
			if !validColor(rest) {
				return paint{}, false
			}
			return paint{kind: paintFuncIRI, link: link, fallback: fallbackColor}, true
		}
	}
	if !validColor(text) {
		return paint{}, false
	}
	return paint{kind: paintColor}, true
}

var namedColors = func() map[string]struct{} {
	names := strings.Fields(`lightgrey lavenderblush deeppink seashell lightsalmon green lightgreen black
deepskyblue mistyrose silver dimgray navajowhite royalblue peru darkgrey steelblue teal orangered
mediumslateblue blueviolet cornflowerblue cyan beige goldenrod rosybrown yellow blue darkblue aliceblue
white mediumblue dodgerblue limegreen purple lightsteelblue lightslategray seagreen mediumvioletred
slategrey darkslategrey turquoise paleturquoise lightgoldenrodyellow magenta darkseagreen lightcyan
lightcoral mediumseagreen palegoldenrod palegreen darkslateblue moccasin forestgreen darkkhaki chartreuse
floralwhite snow fuchsia orchid darkorchid darkred darksalmon crimson lime palevioletred lightseagreen
ivory powderblue aquamarine darkturquoise lavender azure mediumturquoise lightgray transparent gainsboro
olivedrab papayawhip tomato midnightblue pink yellowgreen slategray red indigo orange grey wheat
darkgoldenrod lawngreen lightslategrey burlywood aqua saddlebrown oldlace lightskyblue violet dimgrey
darkorange lightblue khaki coral brown mediumpurple linen mediumorchid indianred maroon firebrick skyblue
darkgray hotpink olive sienna cadetblue darkslategray slateblue plum mediumspringgreen thistle mintcream
darkmagenta lemonchiffon bisque antiquewhite darkgreen whitesmoke lightpink darkcyan tan blanchedalmond
honeydew salmon lightyellow springgreen cornsilk sandybrown mediumaquamarine darkviolet darkolivegreen
gold peachpuff greenyellow gray navy ghostwhite chocolate`)
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}()

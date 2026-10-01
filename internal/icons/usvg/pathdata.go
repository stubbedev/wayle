package usvg

import "math"

// This file ports svgtypes' PathParser and SimplifyingPathParser plus
// kurbo 0.13's SvgArc -> Arc -> cubic conversion they use.

// simpleSeg is SimplePathSegment: absolute move/line/quad/cubic/close.
type simpleSeg struct {
	kind                 byte // 'M', 'L', 'Q', 'C', 'Z'
	x1, y1, x2, y2, x, y float64
}

// rawSeg is a parsed PathSegment.
type rawSeg struct {
	cmd                  byte // relative (lower-case) command letter
	abs                  bool
	x1, y1, x2, y2, x, y float64
	rx, ry, rot          float64
	largeArc, sweep      bool
}

func isCmd(c byte) bool {
	switch c {
	case 'M', 'm', 'Z', 'z', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'S', 's', 'Q', 'q', 'T', 't', 'A', 'a':
		return true
	}
	return false
}

func isAbsolute(c byte) bool { return c >= 'A' && c <= 'Z' }

func toRelative(c byte) byte {
	if isAbsolute(c) {
		return c + ('a' - 'A')
	}
	return c
}

func isNumberStart(c byte) bool { return (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' }

type pathParser struct {
	s       stream
	prevCmd byte // 0 for none
}

func (p *pathParser) next() (rawSeg, bool, error) {
	p.s.skipSpaces()
	if p.s.atEnd() {
		return rawSeg{}, false, nil
	}
	seg, err := p.nextImpl()
	if err != nil {
		p.s.pos = len(p.s.text)
		return rawSeg{}, false, err
	}
	return seg, true, nil
}

func (p *pathParser) nextImpl() (rawSeg, error) {
	s := &p.s
	first := s.text[s.pos]
	hasPrev := p.prevCmd != 0
	if !hasPrev && !isCmd(first) {
		return rawSeg{}, errParse
	}
	if !hasPrev && first != 'M' && first != 'm' {
		return rawSeg{}, errParse
	}
	implicitMove := false
	var cmd byte
	switch {
	case isCmd(first):
		cmd = first
		s.pos++
	case isNumberStart(first) && hasPrev:
		switch p.prevCmd {
		case 'Z', 'z':
			return rawSeg{}, errParse
		case 'M', 'm':
			implicitMove = true
			if isAbsolute(p.prevCmd) {
				cmd = 'L'
			} else {
				cmd = 'l'
			}
		default:
			cmd = p.prevCmd
		}
	default:
		return rawSeg{}, errParse
	}
	seg := rawSeg{cmd: toRelative(cmd), abs: isAbsolute(cmd)}
	num := func(dst *float64) error {
		v, err := s.parseListNumber()
		*dst = v
		return err
	}
	var err error
	nums := func(dsts ...*float64) {
		for _, d := range dsts {
			if err == nil {
				err = num(d)
			}
		}
	}
	switch seg.cmd {
	case 'm', 'l', 't':
		nums(&seg.x, &seg.y)
	case 'h':
		nums(&seg.x)
	case 'v':
		nums(&seg.y)
	case 'c':
		nums(&seg.x1, &seg.y1, &seg.x2, &seg.y2, &seg.x, &seg.y)
	case 's':
		nums(&seg.x2, &seg.y2, &seg.x, &seg.y)
	case 'q':
		nums(&seg.x1, &seg.y1, &seg.x, &seg.y)
	case 'a':
		nums(&seg.rx, &seg.ry, &seg.rot)
		if err == nil {
			seg.largeArc, err = parseFlag(s)
		}
		if err == nil {
			seg.sweep, err = parseFlag(s)
		}
		nums(&seg.x, &seg.y)
	}
	if err != nil {
		return rawSeg{}, err
	}
	if implicitMove {
		if seg.abs {
			p.prevCmd = 'M'
		} else {
			p.prevCmd = 'm'
		}
	} else {
		p.prevCmd = cmd
	}
	return seg, nil
}

func parseFlag(s *stream) (bool, error) {
	s.skipSpaces()
	c, ok := s.curr()
	if !ok || (c != '0' && c != '1') {
		return false, errParse
	}
	s.pos++
	if s.isCurr(',') {
		s.pos++
	}
	s.skipSpaces()
	return c == '1', nil
}

// simplifyPath is SimplifyingPathParser: every segment made absolute,
// H/V to lines, S/T to explicit curves, arcs to cubics. Parsing stops
// at the first error, keeping what came before.
func simplifyPath(text string) []simpleSeg {
	p := pathParser{s: stream{text: text}}
	var out []simpleSeg
	var prevMX, prevMY, prevTX, prevTY, prevX, prevY float64
	prevSeg := rawSeg{cmd: 'm', abs: true}
	var prevSimple *simpleSeg
	for {
		seg, ok, err := p.next()
		if err != nil || !ok {
			return out
		}
		var buffer []simpleSeg
		if prevSimple != nil && prevSimple.kind == 'Z' && seg.cmd != 'm' && seg.cmd != 'z' {
			m := simpleSeg{kind: 'M', x: prevMX, y: prevMY}
			buffer = append(buffer, m)
			prevSimple = &m
		}
		switch seg.cmd {
		case 'm':
			x, y := seg.x, seg.y
			if !seg.abs {
				if prevSimple != nil && prevSimple.kind == 'Z' {
					x += prevMX
					y += prevMY
				} else {
					x += prevX
					y += prevY
				}
			}
			buffer = append(buffer, simpleSeg{kind: 'M', x: x, y: y})
			prevSeg = seg
		case 'l':
			x, y := seg.x, seg.y
			if !seg.abs {
				x += prevX
				y += prevY
			}
			buffer = append(buffer, simpleSeg{kind: 'L', x: x, y: y})
			prevSeg = seg
		case 'h':
			x := seg.x
			if !seg.abs {
				x += prevX
			}
			buffer = append(buffer, simpleSeg{kind: 'L', x: x, y: prevY})
			prevSeg = seg
		case 'v':
			y := seg.y
			if !seg.abs {
				y += prevY
			}
			buffer = append(buffer, simpleSeg{kind: 'L', x: prevX, y: y})
			prevSeg = seg
		case 'c':
			if !seg.abs {
				seg.x1 += prevX
				seg.y1 += prevY
				seg.x2 += prevX
				seg.y2 += prevY
				seg.x += prevX
				seg.y += prevY
			}
			buffer = append(buffer, simpleSeg{kind: 'C', x1: seg.x1, y1: seg.y1, x2: seg.x2, y2: seg.y2, x: seg.x, y: seg.y})
			seg.abs = true
			prevSeg = seg
		case 's':
			x1, y1 := prevX, prevY
			if prevSeg.cmd == 'c' || prevSeg.cmd == 's' {
				x1, y1 = prevSeg.x*2-prevSeg.x2, prevSeg.y*2-prevSeg.y2
			}
			if !seg.abs {
				seg.x2 += prevX
				seg.y2 += prevY
				seg.x += prevX
				seg.y += prevY
			}
			buffer = append(buffer, simpleSeg{kind: 'C', x1: x1, y1: y1, x2: seg.x2, y2: seg.y2, x: seg.x, y: seg.y})
			seg.abs = true
			prevSeg = seg
		case 'q':
			if !seg.abs {
				seg.x1 += prevX
				seg.y1 += prevY
				seg.x += prevX
				seg.y += prevY
			}
			buffer = append(buffer, simpleSeg{kind: 'Q', x1: seg.x1, y1: seg.y1, x: seg.x, y: seg.y})
			seg.abs = true
			prevSeg = seg
		case 't':
			x1, y1 := prevX, prevY
			switch prevSeg.cmd {
			case 'q':
				x1, y1 = prevSeg.x*2-prevSeg.x1, prevSeg.y*2-prevSeg.y1
			case 't':
				x1, y1 = prevSeg.x*2-prevTX, prevSeg.y*2-prevTY
			}
			prevTX, prevTY = x1, y1
			if !seg.abs {
				seg.x += prevX
				seg.y += prevY
			}
			buffer = append(buffer, simpleSeg{kind: 'Q', x1: x1, y1: y1, x: seg.x, y: seg.y})
			seg.abs = true
			prevSeg = seg
		case 'a':
			x, y := seg.x, seg.y
			if !seg.abs {
				x += prevX
				y += prevY
			}
			arc, ok := arcFromSVG(prevX, prevY, x, y, seg.rx, seg.ry, toRadians(seg.rot), seg.largeArc, seg.sweep)
			if ok {
				arc.toCubics(0.1, func(x1, y1, x2, y2, x, y float64) {
					buffer = append(buffer, simpleSeg{kind: 'C', x1: x1, y1: y1, x2: x2, y2: y2, x: x, y: y})
				})
			} else {
				buffer = append(buffer, simpleSeg{kind: 'L', x: x, y: y})
			}
			prevSeg = seg
		case 'z':
			if prevSimple == nil || prevSimple.kind != 'Z' {
				buffer = append(buffer, simpleSeg{kind: 'Z'})
			}
			prevSeg = seg
		}
		if n := len(buffer); n > 0 {
			last := buffer[n-1]
			prevSimple = &last
			switch last.kind {
			case 'M':
				prevX, prevY = last.x, last.y
				prevMX, prevMY = prevX, prevY
			case 'L', 'C', 'Q':
				prevX, prevY = last.x, last.y
			case 'Z':
				prevX, prevY = prevMX, prevMY
			}
		}
		out = append(out, buffer...)
	}
}

// kArc is kurbo's Arc.
type kArc struct {
	cx, cy     float64
	rx, ry     float64
	startAngle float64
	sweepAngle float64
	xRotation  float64
}

// arcFromSVG is kurbo::Arc::from_svg_arc; false for a straight line.
func arcFromSVG(fromX, fromY, toX, toY, radX, radY, xRotation float64, largeArc, sweep bool) (kArc, bool) {
	if math.Abs(radX) <= 1e-5 || math.Abs(radY) <= 1e-5 || (fromX == toX && fromY == toY) {
		return kArc{}, false
	}
	rx := math.Abs(radX)
	ry := math.Abs(radY)
	xr := math.Mod(xRotation, 2*math.Pi)
	sinPhi, cosPhi := sincos(xr)
	hdX := (fromX - toX) * 0.5
	hdY := (fromY - toY) * 0.5
	hsX := (fromX + toX) * 0.5
	hsY := (fromY + toY) * 0.5
	px := cosPhi*hdX + sinPhi*hdY
	py := -sinPhi*hdX + cosPhi*hdY
	rf := px*px/(rx*rx) + py*py/(ry*ry)
	if rf > 1 {
		scale := math.Sqrt(rf)
		rx *= scale
		ry *= scale
	}
	rxry := rx * ry
	rxpy := rx * py
	rypx := ry * px
	sumOfSq := rxpy*rxpy + rypx*rypx
	signCoe := 1.0
	if largeArc == sweep {
		signCoe = -1
	}
	coe := signCoe * math.Sqrt(math.Abs((rxry*rxry-sumOfSq)/sumOfSq))
	tcx := coe * rxpy / ry
	tcy := -coe * rypx / rx
	cx := cosPhi*tcx - sinPhi*tcy + hsX
	cy := sinPhi*tcx + cosPhi*tcy + hsY
	startAngle := atan2R((py-tcy)/ry, (px-tcx)/rx)
	endAngle := atan2R((-py-tcy)/ry, (-px-tcx)/rx)
	sweepAngle := math.Mod(endAngle-startAngle, 2*math.Pi)
	if sweep && sweepAngle < 0 {
		sweepAngle += 2 * math.Pi
	} else if !sweep && sweepAngle > 0 {
		sweepAngle -= 2 * math.Pi
	}
	return kArc{cx: cx, cy: cy, rx: rx, ry: ry, startAngle: startAngle, sweepAngle: sweepAngle, xRotation: xRotation}, true
}

func signum(f float64) float64 {
	switch {
	case math.IsNaN(f):
		return f
	case math.Signbit(f):
		return -1
	default:
		return 1
	}
}

func sampleEllipse(rx, ry, xRotation, angle float64) (float64, float64) {
	s, c := sincos(angle)
	u := rx * c
	v := ry * s
	rs, rc := sincos(xRotation)
	return u*rc - v*rs, u*rs + v*rc
}

// toCubics is Arc::to_cubic_beziers via append_iter.
func (a kArc) toCubics(tolerance float64, emit func(x1, y1, x2, y2, x, y float64)) {
	sign := signum(a.sweepAngle)
	scaledErr := math.Max(a.rx, a.ry) / tolerance
	nErr := math.Max(math.Pow(1.1163*scaledErr, 1.0/6.0), 3.999_999)
	nf := math.Ceil(nErr * math.Abs(a.sweepAngle) * (1.0 / (2 * math.Pi)))
	angleStep := a.sweepAngle / nf
	n := int(nf)
	if nf < 0 || math.IsNaN(nf) {
		n = 0
	}
	armLen := (4.0 / 3.0) * tanR(math.Abs(0.25*angleStep)) * sign
	angle0 := a.startAngle
	p0x, p0y := sampleEllipse(a.rx, a.ry, a.xRotation, angle0)
	for range n {
		angle1 := angle0 + angleStep
		dx, dy := sampleEllipse(a.rx, a.ry, a.xRotation, angle0+math.Pi/2)
		p1x, p1y := p0x+armLen*dx, p0y+armLen*dy
		p3x, p3y := sampleEllipse(a.rx, a.ry, a.xRotation, angle1)
		ex, ey := sampleEllipse(a.rx, a.ry, a.xRotation, angle1+math.Pi/2)
		p2x, p2y := p3x-armLen*ex, p3y-armLen*ey
		angle0 = angle1
		p0x, p0y = p3x, p3y
		emit(a.cx+p1x, a.cy+p1y, a.cx+p2x, a.cy+p2y, a.cx+p3x, a.cy+p3y)
	}
}

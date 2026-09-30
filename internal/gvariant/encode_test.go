package gvariant

import "encoding/binary"

// The tests build their fixtures with this minimal serializer, the
// mirror of the reader: arrays and tuples with framing offsets, plus u
// and s leaves. A value is a uint32, a string, a []any (array), or a
// tuple.
type tuple []any

func encode(typ string, v any) []byte {
	switch typ[0] {
	case 'u':
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, v.(uint32))
		return b
	case 's':
		return append([]byte(v.(string)), 0)
	case 'a':
		elem := typ[1:]
		items := v.([]any)
		var body []byte
		if n, fixed := fixedSize(elem); fixed {
			for _, it := range items {
				body = append(pad(body, alignment(elem)), encode(elem, it)...)
			}
			_ = n
			return body
		}
		var ends []int
		for _, it := range items {
			body = append(pad(body, alignment(elem)), encode(elem, it)...)
			ends = append(ends, len(body))
		}
		return frame(body, ends)
	case '(':
		ms := members(typ)
		fields := v.(tuple)
		var body []byte
		var ends []int
		for i, m := range ms {
			body = append(pad(body, alignment(m)), encode(m, fields[i])...)
			if _, fixed := fixedSize(m); !fixed && i != len(ms)-1 {
				ends = append(ends, len(body))
			}
		}
		// Tuple offsets are stored in reverse order.
		for i, j := 0, len(ends)-1; i < j; i, j = i+1, j-1 {
			ends[i], ends[j] = ends[j], ends[i]
		}
		if len(ends) == 0 {
			return body
		}
		return frame(body, ends)
	}
	panic("unsupported test type " + typ)
}

func pad(b []byte, a int) []byte {
	for len(b)%a != 0 {
		b = append(b, 0)
	}
	return b
}

// frame appends the framing offsets, picking the smallest width that
// addresses the framed whole.
func frame(body []byte, ends []int) []byte {
	if len(ends) == 0 {
		return body
	}
	for o := 1; ; o *= 2 {
		if offsetSize(len(body)+o*len(ends)) != o {
			continue
		}
		out := append([]byte{}, body...)
		for _, e := range ends {
			buf := make([]byte, 8)
			binary.LittleEndian.PutUint64(buf, uint64(e))
			out = append(out, buf[:o]...)
		}
		return out
	}
}

// Package gvariant reads GVariant's serialized form (little-endian)
// for the container and basic types a GTK data blob uses: u, y, b, s,
// arrays, and tuples. It is a reader over the bytes, not a full
// implementation: enough to parse GTK's emoji tables and GResource
// entries without cgo.
package gvariant

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrMalformed is serialized data that does not fit its type.
var ErrMalformed = errors.New("gvariant: malformed data")

// Value is a serialized value with its type string.
type Value struct {
	Type string
	Data []byte
}

// New wraps bytes of a known type.
func New(typ string, data []byte) (Value, error) {
	if _, err := typeEnd(typ, 0); err != nil {
		return Value{}, err
	}
	return Value{Type: typ, Data: data}, nil
}

// typeEnd returns the index just past the complete type starting at i.
func typeEnd(t string, i int) (int, error) {
	if i >= len(t) {
		return 0, fmt.Errorf("gvariant: truncated type %q", t)
	}
	switch t[i] {
	case 'u', 'y', 'b', 's', 'i', 'q', 'n', 't', 'x', 'd', 'o', 'g', 'v':
		return i + 1, nil
	case 'a':
		return typeEnd(t, i+1)
	case '(':
		j := i + 1
		for j < len(t) && t[j] != ')' {
			end, err := typeEnd(t, j)
			if err != nil {
				return 0, err
			}
			j = end
		}
		if j >= len(t) {
			return 0, fmt.Errorf("gvariant: unterminated tuple in %q", t)
		}
		return j + 1, nil
	}
	return 0, fmt.Errorf("gvariant: unsupported type %q", t[i:])
}

// members splits a tuple type into its member types.
func members(t string) []string {
	var out []string
	for j := 1; j < len(t)-1; {
		end, _ := typeEnd(t, j)
		out = append(out, t[j:end])
		j = end
	}
	return out
}

// alignment is the type's serialized alignment.
func alignment(t string) int {
	switch t[0] {
	case 'y', 'b', 's', 'o', 'g':
		return 1
	case 'q', 'n':
		return 2
	case 'u', 'i':
		return 4
	case 't', 'x', 'd', 'v':
		return 8
	case 'a':
		return alignment(t[1:])
	case '(':
		a := 1
		for _, m := range members(t) {
			a = max(a, alignment(m))
		}
		return a
	}
	return 1
}

// fixedSize is the type's size when every value of it has the same
// size.
func fixedSize(t string) (int, bool) {
	switch t[0] {
	case 'y', 'b':
		return 1, true
	case 'q', 'n':
		return 2, true
	case 'u', 'i':
		return 4, true
	case 't', 'x', 'd':
		return 8, true
	case '(':
		size := 0
		for _, m := range members(t) {
			n, ok := fixedSize(m)
			if !ok {
				return 0, false
			}
			size = alignUp(size, alignment(m)) + n
		}
		if size == 0 {
			return 1, true
		}
		return alignUp(size, alignment(t)), true
	}
	return 0, false
}

func alignUp(n, a int) int { return (n + a - 1) / a * a }

// offsetSize is the framing offset width for a container of n bytes.
func offsetSize(n int) int {
	switch {
	case n == 0:
		return 0
	case n <= 0xff:
		return 1
	case n <= 0xffff:
		return 2
	case n <= 0xffffffff:
		return 4
	}
	return 8
}

func readOffset(b []byte, size int) int {
	switch size {
	case 1:
		return int(b[0])
	case 2:
		return int(binary.LittleEndian.Uint16(b))
	case 4:
		return int(binary.LittleEndian.Uint32(b))
	case 8:
		return int(binary.LittleEndian.Uint64(b))
	}
	return 0
}

// Uint32 reads a u.
func (v Value) Uint32() (uint32, error) {
	if v.Type != "u" || len(v.Data) != 4 {
		return 0, ErrMalformed
	}
	return binary.LittleEndian.Uint32(v.Data), nil
}

// Str reads an s: the bytes before the terminating nul.
func (v Value) Str() (string, error) {
	if v.Type != "s" || len(v.Data) == 0 || v.Data[len(v.Data)-1] != 0 {
		return "", ErrMalformed
	}
	return string(v.Data[:len(v.Data)-1]), nil
}

// Elements splits an array into its element values.
func (v Value) Elements() ([]Value, error) {
	if v.Type == "" || v.Type[0] != 'a' {
		return nil, ErrMalformed
	}
	elem := v.Type[1:]
	b := v.Data
	if n, ok := fixedSize(elem); ok {
		if len(b)%n != 0 {
			return nil, ErrMalformed
		}
		out := make([]Value, len(b)/n)
		for i := range out {
			out[i] = Value{Type: elem, Data: b[i*n : (i+1)*n]}
		}
		return out, nil
	}
	if len(b) == 0 {
		return nil, nil
	}
	o := offsetSize(len(b))
	table := readOffset(b[len(b)-o:], o)
	if table > len(b) || (len(b)-table)%o != 0 {
		return nil, ErrMalformed
	}
	count := (len(b) - table) / o
	out := make([]Value, count)
	start := 0
	for i := range count {
		end := readOffset(b[table+i*o:], o)
		start = alignUp(start, alignment(elem))
		if start > end || end > table {
			return nil, ErrMalformed
		}
		out[i] = Value{Type: elem, Data: b[start:end]}
		start = end
	}
	return out, nil
}

// Fields splits a tuple into its member values.
func (v Value) Fields() ([]Value, error) {
	if v.Type == "" || v.Type[0] != '(' {
		return nil, ErrMalformed
	}
	ms := members(v.Type)
	b := v.Data
	o := offsetSize(len(b))
	framing := len(b) // the framing offsets grow down from the end
	out := make([]Value, len(ms))
	start := 0
	for i, m := range ms {
		start = alignUp(start, alignment(m))
		var end int
		switch n, fixed := fixedSize(m); {
		case fixed:
			end = start + n
		case i == len(ms)-1:
			end = framing
		default:
			framing -= o
			if framing < 0 {
				return nil, ErrMalformed
			}
			end = readOffset(b[framing:], o)
		}
		if start > end || end > len(b) {
			return nil, ErrMalformed
		}
		out[i] = Value{Type: m, Data: b[start:end]}
		start = end
	}
	return out, nil
}

// Variant unwraps a v: the child value and its type string.
func (v Value) Variant() (Value, error) {
	if v.Type != "v" {
		return Value{}, ErrMalformed
	}
	// The child type follows the last nul.
	i := bytes.LastIndexByte(v.Data, 0)
	if i < 0 {
		return Value{}, ErrMalformed
	}
	child := string(v.Data[i+1:])
	if _, err := typeEnd(child, 0); err != nil {
		return Value{}, err
	}
	return Value{Type: child, Data: v.Data[:i]}, nil
}

// Uint32s reads an au.
func (v Value) Uint32s() ([]uint32, error) {
	if v.Type != "au" || len(v.Data)%4 != 0 {
		return nil, ErrMalformed
	}
	out := make([]uint32, len(v.Data)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(v.Data[i*4:])
	}
	return out, nil
}

// Strs reads an as.
func (v Value) Strs() ([]string, error) {
	if v.Type != "as" {
		return nil, ErrMalformed
	}
	elems, err := v.Elements()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(elems))
	for i, e := range elems {
		if out[i], err = e.Str(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

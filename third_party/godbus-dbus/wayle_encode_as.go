package dbus

import (
	"bytes"
	"reflect"
	"strings"
)

// encodeAs encodes v as the type sig names (wayle patch). decode
// returns a struct as []any, so a decoded struct inside a Variant
// encoded by its Go type became an array of variants under a struct
// signature, a malformed message the bus answers by disconnecting the
// sender. Following the Variant's signature re-encodes what decode
// produced to the same wire form; values with no struct in their
// signature take the ordinary path.
func (enc *encoder) encodeAs(v reflect.Value, sig string, depth int) {
	// An element of a decoded struct is an interface; the signature, not
	// the Go type, says what it is (a bare interface would encode as a
	// variant).
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if !strings.ContainsRune(sig, '(') {
		enc.encode(v, depth)
		return
	}
	switch {
	case sig[0] == '(' && v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Interface:
		enc.align(8)
		fields, i := sig[1:len(sig)-1], 0
		for fields != "" {
			err, rem := validSingle(fields, &depthCounter{})
			if err != nil {
				panic(err)
			}
			if i >= v.Len() {
				panic(FormatError("struct value has fewer fields than its signature " + sig))
			}
			enc.encodeAs(v.Index(i), fields[:len(fields)-len(rem)], depth+1)
			fields, i = rem, i+1
		}
		if i != v.Len() {
			panic(FormatError("struct value has more fields than its signature " + sig))
		}
	case sig[0] == 'a' && sig[1] == '{' && v.Kind() == reflect.Map:
		vsig := sig[3 : len(sig)-1]
		enc.encodeArray(depth, 8, func(bufenc *encoder) {
			for _, k := range v.MapKeys() {
				bufenc.align(8)
				bufenc.encode(k, depth+2)
				bufenc.encodeAs(v.MapIndex(k), vsig, depth+2)
			}
		})
	case sig[0] == 'a' && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array):
		elem := sig[1:]
		enc.encodeArray(depth, sigAlignment(elem), func(bufenc *encoder) {
			for i := 0; i < v.Len(); i++ {
				bufenc.encodeAs(v.Index(i), elem, depth+1)
			}
		})
	default:
		enc.encode(v, depth)
	}
}

// encodeArray writes an array whose elements fill encodes, aligned to
// elemAlign, as the encoder's own slice and map cases do.
func (enc *encoder) encodeArray(depth, elemAlign int, fill func(*encoder)) {
	n := enc.padding(0, 4) + 4
	offset := enc.pos + n + enc.padding(n, elemAlign)
	var buf bytes.Buffer
	bufenc := newEncoderAtOffset(&buf, offset, enc.order, enc.fds)
	fill(bufenc)
	if buf.Len() > 1<<26 {
		panic(FormatError("input exceeds array size limitation"))
	}
	enc.fds = bufenc.fds
	enc.encode(reflect.ValueOf(uint32(buf.Len())), depth)
	length := buf.Len()
	enc.align(elemAlign)
	if _, err := buf.WriteTo(enc.out); err != nil {
		panic(err)
	}
	enc.pos += length
}

// sigAlignment is the wire alignment of a single complete type.
func sigAlignment(sig string) int {
	switch sig[0] {
	case '(', '{':
		return 8
	}
	return alignment(typeFor(sig))
}

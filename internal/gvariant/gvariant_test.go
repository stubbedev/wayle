package gvariant

import (
	"reflect"
	"strings"
	"testing"
)

func TestHandSerializedValues(t *testing.T) {
	// as ["a", "bc"]: "a\0bc\0" then one-byte end offsets 2, 5.
	v, _ := New("as", []byte("a\x00bc\x00\x02\x05"))
	got, err := v.Strs()
	if err != nil || !reflect.DeepEqual(got, []string{"a", "bc"}) {
		t.Errorf("as = %q %v", got, err)
	}
	// (su) ("hi", 7): "hi\0", pad to 4, u32 7, then s's end offset 3.
	tup, _ := New("(su)", []byte("hi\x00\x00\x07\x00\x00\x00\x03"))
	fields, err := tup.Fields()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := fields[0].Str()
	u, _ := fields[1].Uint32()
	if s != "hi" || u != 7 {
		t.Errorf("(su) = %q %d", s, u)
	}
}

func TestTheEmojiRowShapeRoundTrips(t *testing.T) {
	rows := []any{
		tuple{[]any{uint32(0x1f37a)}, "beer mug", "chope de bière", []any{"beer"}, []any{"bière", "boire"}, uint32(3)},
		tuple{[]any{uint32(0x2764), uint32(0)}, "red heart", "", []any{}, []any{}, uint32(1)},
	}
	data := encode("a(aussasasu)", rows)
	v, err := New("a(aussasasu)", data)
	if err != nil {
		t.Fatal(err)
	}
	elems, err := v.Elements()
	if err != nil || len(elems) != 2 {
		t.Fatalf("rows = %d %v", len(elems), err)
	}
	f, err := elems[0].Fields()
	if err != nil {
		t.Fatal(err)
	}
	codes, _ := f[0].Uint32s()
	en, _ := f[1].Str()
	loc, _ := f[2].Str()
	kwEn, _ := f[3].Strs()
	kw, _ := f[4].Strs()
	group, _ := f[5].Uint32()
	if !reflect.DeepEqual(codes, []uint32{0x1f37a}) || en != "beer mug" || loc != "chope de bière" ||
		!reflect.DeepEqual(kwEn, []string{"beer"}) || !reflect.DeepEqual(kw, []string{"bière", "boire"}) || group != 3 {
		t.Errorf("row 0 = %v %q %q %q %q %d", codes, en, loc, kwEn, kw, group)
	}
	f, _ = elems[1].Fields()
	codes, _ = f[0].Uint32s()
	loc, _ = f[2].Str()
	kw, _ = f[4].Strs()
	if !reflect.DeepEqual(codes, []uint32{0x2764, 0}) || loc != "" || len(kw) != 0 {
		t.Errorf("row 1 = %v %q %q", codes, loc, kw)
	}
}

func TestALargeArrayUsesWideOffsets(t *testing.T) {
	var many []any
	for range 200 {
		many = append(many, strings.Repeat("x", 10))
	}
	v, _ := New("as", encode("as", many))
	got, err := v.Strs()
	if err != nil || len(got) != 200 || got[199] != strings.Repeat("x", 10) {
		t.Errorf("200 strings = %d %v", len(got), err)
	}
}

func TestVariantUnwraps(t *testing.T) {
	inner := encode("(su)", tuple{"x", uint32(1)})
	v, _ := New("v", append(append(inner, 0), "(su)"...))
	child, err := v.Variant()
	if err != nil || child.Type != "(su)" {
		t.Fatalf("variant = %+v %v", child, err)
	}
}

func TestMalformedDataIsAnError(t *testing.T) {
	if _, err := New("(s", nil); err == nil {
		t.Error("an unterminated tuple type must be refused")
	}
	bad, _ := New("as", []byte("a\x00\x09"))
	if _, err := bad.Strs(); err == nil {
		t.Error("an offset past the data must be refused")
	}
	noNul, _ := New("s", []byte("abc"))
	if _, err := noNul.Str(); err == nil {
		t.Error("a string without its nul must be refused")
	}
	odd, _ := New("au", []byte{1, 2, 3})
	if _, err := odd.Uint32s(); err == nil {
		t.Error("a ragged au must be refused")
	}
}

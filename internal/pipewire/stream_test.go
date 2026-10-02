package pipewire

import (
	"encoding/binary"
	"slices"
	"testing"
)

func TestClampDamage(t *testing.T) {
	full := []Rect{{0, 0, 100, 50}}
	for name, tc := range map[string]struct {
		in   []Rect
		max  int
		want []Rect
	}{
		"none is the whole frame":    {nil, 4, full},
		"clipped to the frame":       {[]Rect{{90, 40, 20, 20}}, 4, []Rect{{90, 40, 10, 10}}},
		"outside dropped":            {[]Rect{{100, 0, 5, 5}, {1, 1, 2, 2}}, 4, []Rect{{1, 1, 2, 2}}},
		"all outside is the frame":   {[]Rect{{200, 0, 5, 5}}, 4, full},
		"zero-size dropped":          {[]Rect{{1, 1, 0, 5}}, 4, full},
		"more than max is the frame": {[]Rect{{0, 0, 1, 1}, {1, 1, 1, 1}, {2, 2, 1, 1}}, 2, full},
		"no room is the frame":       {[]Rect{{0, 0, 1, 1}}, 0, full},
	} {
		if got := ClampDamage(nil, tc.in, 100, 50, tc.max); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestWriteDamageTerminates(t *testing.T) {
	slots := make([]cMetaRegion, 3)
	for i := range slots {
		slots[i] = cMetaRegion{9, 9, 9, 9}
	}
	writeDamage(slots, []Rect{{1, 2, 3, 4}, {5, 6, 7, 8}, {9, 9, 1, 1}})
	// Two rects fit before the terminator; the third does not.
	if want := []cMetaRegion{{1, 2, 3, 4}, {5, 6, 7, 8}, {}}; !slices.Equal(slots, want) {
		t.Errorf("slots = %v", slots)
	}
	writeDamage(nil, []Rect{{1, 1, 1, 1}}) // no room: nothing to write
}

func TestPodLayout(t *testing.T) {
	// An Int pod: size 4, type Int, the value, padded to 8.
	if got, want := podInt(-2).bytes(), []byte{4, 0, 0, 0, 4, 0, 0, 0, 0xfe, 0xff, 0xff, 0xff, 0, 0, 0, 0}; !slices.Equal(got, want) {
		t.Errorf("int pod = %x", got)
	}
	// An object: header, object type and id, then each property's key,
	// flags and value pod; sizes count the padded bodies.
	obj := object(objectParamMeta, paramMeta, prop{key: metaKeyType, value: podID(metaHeader)})
	u32 := func(i int) uint32 { return binary.LittleEndian.Uint32(obj[i:]) }
	if len(obj) != 8+8+8+16 || u32(0) != 32 || u32(4) != typeObject || u32(8) != objectParamMeta || u32(12) != paramMeta || u32(16) != metaKeyType || u32(24) != 4 || u32(28) != typeID || u32(32) != metaHeader {
		t.Errorf("object = %x", obj)
	}
	// A dmabuf format carries the modifier, mandatory.
	mod := uint64(0x00ffffffffffffff)
	if a, b := formatPod(1, 1, 30, VideoBGRx, nil), formatPod(1, 1, 30, VideoBGRx, &mod); len(b) != len(a)+8+16 {
		t.Errorf("modifier prop size: %d vs %d", len(b), len(a))
	}
}

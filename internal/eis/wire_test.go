package eis

import "testing"

func TestWireRoundTrip(t *testing.T) {
	m := newMessage(0xff00000000000001, 5).u64(7).str("ei_pointer").u32(1).bytes()
	// Header, the u64, the string (len 11, 11 bytes + 1 pad), the u32.
	if len(m) != 16+8+4+12+4 || order.Uint32(m[8:]) != uint32(len(m)) {
		t.Fatalf("message = %x", m)
	}
	msgs, rest, err := split(append(m, m[:10]...))
	if err != nil || len(msgs) != 1 || len(rest) != 10 {
		t.Fatalf("split = %v, rest %d, %v: a partial message stays buffered", msgs, len(rest), err)
	}
	r := &reader{b: msgs[0].args}
	if r.u64() != 7 || r.str() != "ei_pointer" || r.u32() != 1 || r.err != nil || msgs[0].opcode != 5 {
		t.Errorf("decoded wrongly: %v", r.err)
	}
	if r.u32(); r.err == nil {
		t.Error("reading past the end is not an error")
	}
	if _, _, err := split([]byte{0, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Error("a length below the header was accepted")
	}
	bad := &reader{b: []byte{2, 0, 0, 0, 'a', 'b', 0, 0}}
	if bad.str(); bad.err == nil {
		t.Error("an unterminated string was accepted")
	}
	if got := (&reader{b: []byte{0, 0, 0, 0}}).str(); got != "" {
		t.Errorf("null string = %q", got)
	}
}

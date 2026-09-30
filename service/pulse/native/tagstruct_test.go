package native

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// The golden bytes are hand-assembled from tagstruct.c's writers, so
// the encoder is pinned to the wire format, not just to its own
// decoder.
func TestWriterGoldenBytes(t *testing.T) {
	var w Writer
	w.U32(0x01020304)
	w.U8(9)
	w.Bool(true)
	w.Bool(false)
	w.String("hi")
	w.NullString()
	w.SampleSpec(SampleSpec{Format: SampleS16LE, Channels: 2, Rate: 44100})
	w.ChannelMap(ChannelMap{ChannelFrontLeft, ChannelFrontRight})
	w.CVolume(CVolume{VolumeNorm, 0x8000})
	w.Usec(1)
	w.PropList(PropList{"a": "b"})
	want := []byte{
		'L', 1, 2, 3, 4,
		'B', 9,
		'1', '0',
		't', 'h', 'i', 0,
		'N',
		'a', 3, 2, 0, 0, 0xAC, 0x44,
		'm', 2, 1, 2,
		'v', 2, 0, 1, 0, 0, 0, 0, 0x80, 0,
		'U', 0, 0, 0, 0, 0, 0, 0, 1,
		'P', 't', 'a', 0, 'L', 0, 0, 0, 2, 'x', 0, 0, 0, 2, 'b', 0, 'N',
	}
	if w.Err() != nil {
		t.Fatal(w.Err())
	}
	if !bytes.Equal(w.Bytes(), want) {
		t.Errorf("encoded\n%v\nwant\n%v", w.Bytes(), want)
	}
}

func TestReaderRoundTrip(t *testing.T) {
	var w Writer
	w.U32(7)
	w.String("name")
	w.NullString()
	w.CVolume(CVolume{1, 2, 3})
	w.PropList(PropList{"device.description": "Speakers", "x": ""})
	w.FormatInfo(FormatInfo{Encoding: EncodingPCM, Props: PropList{"k": "v"}})
	w.Volume(VolumeNorm)
	w.S64(-5)
	w.U64(1 << 40)

	r := NewReader(w.Bytes())
	if got := r.U32(); got != 7 {
		t.Errorf("U32 = %d", got)
	}
	if s, ok := r.String(); !ok || s != "name" {
		t.Errorf("String = %q, %v", s, ok)
	}
	if s, ok := r.String(); ok || s != "" {
		t.Errorf("null string = %q, %v, want absent", s, ok)
	}
	if v := r.CVolume(); len(v) != 3 || v[2] != 3 {
		t.Errorf("CVolume = %v", v)
	}
	p := r.PropList()
	if p["device.description"] != "Speakers" || p["x"] != "" || len(p) != 2 {
		t.Errorf("PropList = %v", p)
	}
	f := r.FormatInfo()
	if f.Encoding != EncodingPCM || f.Props["k"] != "v" {
		t.Errorf("FormatInfo = %+v", f)
	}
	if r.Volume() != VolumeNorm || r.S64() != -5 || r.U64() != 1<<40 {
		t.Error("volume/s64/u64 mismatch")
	}
	if r.Err() != nil || !r.EOF() {
		t.Errorf("err = %v, eof = %v", r.Err(), r.EOF())
	}
}

func TestReaderRejectsMalformed(t *testing.T) {
	cases := map[string]struct {
		data []byte
		read func(*Reader)
	}{
		"wrong tag":           {[]byte{'B', 1}, func(r *Reader) { r.U32() }},
		"truncated u32":       {[]byte{'L', 0, 0}, func(r *Reader) { r.U32() }},
		"unterminated string": {[]byte{'t', 'a', 'b'}, func(r *Reader) { r.String() }},
		"short cvolume":       {[]byte{'v', 2, 0, 0, 0, 1}, func(r *Reader) { r.CVolume() }},
		"oversized blob":      {[]byte{'x', 0, 2, 0, 0}, func(r *Reader) { r.Arbitrary() }},
		"proplist length lie": {[]byte{'P', 't', 'k', 0, 'L', 0, 0, 0, 5, 'x', 0, 0, 0, 2, 'v', 0, 'N'}, func(r *Reader) { r.PropList() }},
		"proplist no end":     {[]byte{'P', 't', 'k', 0}, func(r *Reader) { r.PropList() }},
		"empty input":         {nil, func(r *Reader) { r.Bool() }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewReader(tc.data)
			tc.read(r)
			if !errors.Is(r.Err(), ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", r.Err())
			}
			// The failure sticks: later reads stay zero.
			if r.U32() != 0 {
				t.Error("read after failure returned data")
			}
		})
	}
}

func TestPropListDropsBinaryValues(t *testing.T) {
	// A blob without a terminating NUL is binary (pa_proplist_gets
	// returns NULL for it), and so is one with an interior NUL.
	data := []byte{
		'P',
		't', 'b', 'i', 'n', 0, 'L', 0, 0, 0, 2, 'x', 0, 0, 0, 2, 0xFF, 0xFE,
		't', 'n', 'u', 'l', 0, 'L', 0, 0, 0, 3, 'x', 0, 0, 0, 3, 'a', 0, 0,
		't', 'o', 'k', 0, 'L', 0, 0, 0, 2, 'x', 0, 0, 0, 2, 'y', 0,
		'N',
	}
	p := NewReader(data).PropList()
	if len(p) != 1 || p["ok"] != "y" {
		t.Errorf("PropList = %v, want only ok=y", p)
	}
}

func TestReaderStringIsLossy(t *testing.T) {
	s, ok := NewReader([]byte{'t', 'a', 0xFF, 0}).String()
	if !ok || s != "a�" {
		t.Errorf("String = %q, %v", s, ok)
	}
}

func TestWriterRejectsNulString(t *testing.T) {
	var w Writer
	w.String("a\x00b")
	if w.Err() == nil {
		t.Fatal("a NUL in a string encoded")
	}
	var ok Writer
	ok.OptString("")
	if !bytes.Equal(ok.Bytes(), []byte{'N'}) || ok.Err() != nil {
		t.Errorf("OptString(\"\") = %v, %v", ok.Bytes(), ok.Err())
	}
}

func TestFrameRoundTripAndLimits(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Channel: 3, Offset: 1<<32 | 5, Payload: []byte("pcm")}); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 3, 0, 0, 0, 3, 0, 0, 0, 1, 0, 0, 0, 5, 0, 0, 0, 0, 'p', 'c', 'm'}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("frame = %v", buf.Bytes())
	}
	f, err := ReadFrame(&buf)
	if err != nil || f.Channel != 3 || f.Offset != 1<<32|5 || string(f.Payload) != "pcm" {
		t.Fatalf("ReadFrame = %+v, %v", f, err)
	}

	oversized := []byte{0x01, 0, 0, 1, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if _, err := ReadFrame(bytes.NewReader(oversized)); !errors.Is(err, ErrProtocol) {
		t.Errorf("oversized frame: %v, want ErrProtocol", err)
	}
	shm := []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0x80, 0, 0, 0}
	if _, err := ReadFrame(bytes.NewReader(shm)); !errors.Is(err, ErrProtocol) {
		t.Errorf("shm frame: %v, want ErrProtocol", err)
	}
	truncated := append(want[:20:20], 'p')
	if _, err := ReadFrame(bytes.NewReader(truncated)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated payload: %v, want ErrUnexpectedEOF", err)
	}
	if _, err := ReadFrame(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Errorf("clean end: %v, want EOF", err)
	}
}

func TestDecodePacketNeedsHeader(t *testing.T) {
	if _, err := DecodePacket([]byte{'L', 0, 0, 0, 2}); !errors.Is(err, ErrProtocol) {
		t.Errorf("headerless packet: %v, want ErrProtocol", err)
	}
	pkt, err := DecodePacket(EncodePacket(CmdReply, 9, []byte{'1'}))
	if err != nil || pkt.Command != CmdReply || pkt.Tag != 9 || !pkt.Body.Bool() {
		t.Errorf("DecodePacket = %+v, %v", pkt, err)
	}
}

func TestSubscribeEventDecoding(t *testing.T) {
	var w Writer
	w.U32(uint32(FacilitySinkInput) | uint32(OpRemove))
	w.U32(12)
	ev, err := decodeSubscribeEvent(NewReader(w.Bytes()))
	if err != nil || ev != (SubscribeEvent{Facility: FacilitySinkInput, Operation: OpRemove, Index: 12}) {
		t.Errorf("event = %+v, %v", ev, err)
	}
	var bad Writer
	bad.U32(0x30 | 1)
	bad.U32(1)
	if _, err := decodeSubscribeEvent(NewReader(bad.Bytes())); !errors.Is(err, ErrMalformed) {
		t.Errorf("operation 0x30: %v, want ErrMalformed", err)
	}
}

func TestServerAddresses(t *testing.T) {
	t.Setenv("PULSE_SERVER", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/7")
	addrs, err := serverAddresses("")
	if err != nil || len(addrs) != 1 || addrs[0].String() != "unix:/run/user/7/pulse/native" {
		t.Errorf("default = %v, %v", addrs, err)
	}
	t.Setenv("PULSE_SERVER", "unix:/tmp/a /tmp/b tcp:host tcp6:[::1]:99 {not-this-machine}unix:/skip")
	addrs, err = serverAddresses("")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range addrs {
		got = append(got, a.String())
	}
	want := []string{"unix:/tmp/a", "unix:/tmp/b", "tcp:host:4713", "tcp6:[::1]:99"}
	if len(got) != len(want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("address %d = %s, want %s", i, got[i], want[i])
		}
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("PULSE_SERVER", "")
	if _, err := serverAddresses(""); err == nil {
		t.Error("no PULSE_SERVER and no XDG_RUNTIME_DIR resolved a server")
	}
	if _, err := serverAddresses("{unterminated"); err == nil {
		t.Error("an unterminated {id} prefix parsed")
	}
}

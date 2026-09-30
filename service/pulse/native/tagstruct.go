package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Tag bytes (pulsecore/tagstruct.h's PA_TAG_*): every tagstruct field
// is one tag byte followed by its big-endian payload.
const (
	tagString     = 't'
	tagStringNull = 'N'
	tagU32        = 'L'
	tagU8         = 'B'
	tagU64        = 'R'
	tagS64        = 'r'
	tagSampleSpec = 'a'
	tagArbitrary  = 'x'
	tagTrue       = '1'
	tagFalse      = '0'
	tagTimeval    = 'T'
	tagUsec       = 'U'
	tagChannelMap = 'm'
	tagCVolume    = 'v'
	tagPropList   = 'P'
	tagVolume     = 'V'
	tagFormatInfo = 'f'
)

// maxTagSize bounds one arbitrary blob (tagstruct.c's MAX_TAG_SIZE).
const maxTagSize = 64 * 1024

// ErrMalformed marks a tagstruct that does not decode: a wrong tag, a
// truncated field, an out-of-range length. Decode errors wrap it.
var ErrMalformed = errors.New("pulse: malformed tagstruct")

// SampleFormat is a PA_SAMPLE_* sample format.
type SampleFormat uint8

// Sample formats (pulse/sample.h).
const (
	SampleU8        SampleFormat = 0
	SampleALaw      SampleFormat = 1
	SampleULaw      SampleFormat = 2
	SampleS16LE     SampleFormat = 3
	SampleS16BE     SampleFormat = 4
	SampleFloat32LE SampleFormat = 5
	SampleFloat32BE SampleFormat = 6
	SampleS32LE     SampleFormat = 7
	SampleS32BE     SampleFormat = 8
	SampleS24LE     SampleFormat = 9
	SampleS24BE     SampleFormat = 10
	SampleS24_32LE  SampleFormat = 11
	SampleS24_32BE  SampleFormat = 12
	SampleInvalid   SampleFormat = 0xFF
)

var sampleFormatNames = map[SampleFormat]string{
	SampleU8: "U8", SampleALaw: "ALaw", SampleULaw: "ULaw",
	SampleS16LE: "S16LE", SampleS16BE: "S16BE",
	SampleFloat32LE: "F32LE", SampleFloat32BE: "F32BE",
	SampleS32LE: "S32LE", SampleS32BE: "S32BE",
	SampleS24LE: "S24LE", SampleS24BE: "S24BE",
	SampleS24_32LE: "S24_32LE", SampleS24_32BE: "S24_32BE",
}

// String names the format like wayle-audio's SampleFormat variants;
// anything else is "Unknown" (conversion/format.rs's fallback).
func (f SampleFormat) String() string {
	if name, ok := sampleFormatNames[f]; ok {
		return name
	}
	return "Unknown"
}

// Known reports whether the format is one PulseAudio defines.
func (f SampleFormat) Known() bool {
	_, ok := sampleFormatNames[f]
	return ok
}

// BytesPerSample is the width of one sample, 0 for unknown formats.
func (f SampleFormat) BytesPerSample() int {
	switch f {
	case SampleU8, SampleALaw, SampleULaw:
		return 1
	case SampleS16LE, SampleS16BE:
		return 2
	case SampleS24LE, SampleS24BE:
		return 3
	case SampleFloat32LE, SampleFloat32BE, SampleS32LE, SampleS32BE, SampleS24_32LE, SampleS24_32BE:
		return 4
	}
	return 0
}

// SampleSpec is pa_sample_spec.
type SampleSpec struct {
	Format   SampleFormat
	Channels uint8
	Rate     uint32
}

// FrameSize is the byte width of one frame (all channels of a sample).
func (s SampleSpec) FrameSize() int { return s.Format.BytesPerSample() * int(s.Channels) }

// ChannelPosition is a PA_CHANNEL_POSITION_* value.
type ChannelPosition uint8

// Channel positions (pulse/channelmap.h). The 32 aux positions sit
// between SideRight and TopCenter.
const (
	ChannelMono               ChannelPosition = 0
	ChannelFrontLeft          ChannelPosition = 1
	ChannelFrontRight         ChannelPosition = 2
	ChannelFrontCenter        ChannelPosition = 3
	ChannelRearCenter         ChannelPosition = 4
	ChannelRearLeft           ChannelPosition = 5
	ChannelRearRight          ChannelPosition = 6
	ChannelLFE                ChannelPosition = 7
	ChannelFrontLeftOfCenter  ChannelPosition = 8
	ChannelFrontRightOfCenter ChannelPosition = 9
	ChannelSideLeft           ChannelPosition = 10
	ChannelSideRight          ChannelPosition = 11
	ChannelAux0               ChannelPosition = 12
	ChannelTopCenter          ChannelPosition = 44
	ChannelTopFrontLeft       ChannelPosition = 45
	ChannelTopFrontRight      ChannelPosition = 46
	ChannelTopFrontCenter     ChannelPosition = 47
	ChannelTopRearLeft        ChannelPosition = 48
	ChannelTopRearRight       ChannelPosition = 49
	ChannelTopRearCenter      ChannelPosition = 50
)

var channelNames = map[ChannelPosition]string{
	ChannelMono: "Mono", ChannelFrontLeft: "FrontLeft", ChannelFrontRight: "FrontRight",
	ChannelFrontCenter: "FrontCenter", ChannelRearCenter: "RearCenter",
	ChannelRearLeft: "RearLeft", ChannelRearRight: "RearRight", ChannelLFE: "LFE",
	ChannelFrontLeftOfCenter: "FrontLeftOfCenter", ChannelFrontRightOfCenter: "FrontRightOfCenter",
	ChannelSideLeft: "SideLeft", ChannelSideRight: "SideRight",
	ChannelTopCenter: "TopCenter", ChannelTopFrontLeft: "TopFrontLeft",
	ChannelTopFrontRight: "TopFrontRight", ChannelTopFrontCenter: "TopFrontCenter",
	ChannelTopRearLeft: "TopRearLeft", ChannelTopRearRight: "TopRearRight",
	ChannelTopRearCenter: "TopRearCenter",
}

// String names the position like wayle-audio's ChannelPosition; the
// aux positions and anything unassigned are "Unknown"
// (conversion/format.rs's fallback).
func (p ChannelPosition) String() string {
	if name, ok := channelNames[p]; ok {
		return name
	}
	return "Unknown"
}

// ChannelMap is pa_channel_map: one position per channel.
type ChannelMap []ChannelPosition

// Volume is one raw pa_volume_t.
type Volume uint32

// Raw volume anchors (pulse/volume.h).
const (
	VolumeMuted   Volume = 0
	VolumeNorm    Volume = 0x10000
	VolumeMax     Volume = 0x7FFFFFFF
	VolumeInvalid Volume = 0xFFFFFFFF
)

// CVolume is pa_cvolume: one raw volume per channel.
type CVolume []Volume

// PropList is a pa_proplist restricted to its string entries: binary
// values are dropped on decode, as libpulse's pa_proplist_gets (and
// wayle-audio's collect_proplist) never returns them.
type PropList map[string]string

// Encoding is a PA_ENCODING_* format encoding.
type Encoding uint8

// Format encodings (pulse/format.h).
const (
	EncodingAny              Encoding = 0
	EncodingPCM              Encoding = 1
	EncodingAC3IEC61937      Encoding = 2
	EncodingEAC3IEC61937     Encoding = 3
	EncodingMPEGIEC61937     Encoding = 4
	EncodingDTSIEC61937      Encoding = 5
	EncodingMPEG2AACIEC61937 Encoding = 6
	EncodingTrueHDIEC61937   Encoding = 7
	EncodingDTSHDIEC61937    Encoding = 8
	EncodingInvalid          Encoding = 0xFF
)

var encodingNames = []string{
	"Any", "PCM", "AC3_IEC61937", "EAC3_IEC61937", "MPEG_IEC61937",
	"DTS_IEC61937", "MPEG2_AAC_IEC61937", "TRUEHD_IEC61937", "DTSHD_IEC61937",
}

// String is the libpulse-binding Encoding debug name wayle-audio
// stores in its format strings; out-of-range values are "Invalid".
func (e Encoding) String() string {
	if int(e) < len(encodingNames) {
		return encodingNames[e]
	}
	return "Invalid"
}

// FormatInfo is pa_format_info.
type FormatInfo struct {
	Encoding Encoding
	Props    PropList
}

// Writer builds one tagstruct. The first invalid value (a string with
// a NUL byte, an oversized field) poisons the writer; Err reports it
// and the request is never sent.
type Writer struct {
	buf []byte
	err error
}

func (w *Writer) fail(format string, args ...any) {
	if w.err == nil {
		w.err = fmt.Errorf("pulse: encode: "+format, args...)
	}
}

// Err reports the first encoding error.
func (w *Writer) Err() error { return w.err }

// Bytes returns the encoded tagstruct.
func (w *Writer) Bytes() []byte { return w.buf }

func (w *Writer) raw32(v uint32) { w.buf = binary.BigEndian.AppendUint32(w.buf, v) }

// U32 appends a 'L' field.
func (w *Writer) U32(v uint32) {
	w.buf = append(w.buf, tagU32)
	w.raw32(v)
}

// U8 appends a 'B' field.
func (w *Writer) U8(v uint8) { w.buf = append(w.buf, tagU8, v) }

// U64 appends a 'R' field.
func (w *Writer) U64(v uint64) {
	w.buf = append(w.buf, tagU64)
	w.buf = binary.BigEndian.AppendUint64(w.buf, v)
}

// S64 appends a 'r' field.
func (w *Writer) S64(v int64) {
	w.buf = append(w.buf, tagS64)
	w.buf = binary.BigEndian.AppendUint64(w.buf, uint64(v))
}

// Usec appends a 'U' field.
func (w *Writer) Usec(v uint64) {
	w.buf = append(w.buf, tagUsec)
	w.buf = binary.BigEndian.AppendUint64(w.buf, v)
}

// Bool appends a '1' or '0' field.
func (w *Writer) Bool(v bool) {
	if v {
		w.buf = append(w.buf, tagTrue)
		return
	}
	w.buf = append(w.buf, tagFalse)
}

// String appends a 't' field; a string with a NUL byte cannot be
// encoded and poisons the writer.
func (w *Writer) String(s string) {
	if strings.IndexByte(s, 0) >= 0 {
		w.fail("string %q contains a NUL byte", s)
		return
	}
	w.buf = append(w.buf, tagString)
	w.buf = append(w.buf, s...)
	w.buf = append(w.buf, 0)
}

// NullString appends the 'N' null string.
func (w *Writer) NullString() { w.buf = append(w.buf, tagStringNull) }

// OptString appends s, or the null string when s is empty: the
// "name or NULL" arguments where an empty name means "use the index".
func (w *Writer) OptString(s string) {
	if s == "" {
		w.NullString()
		return
	}
	w.String(s)
}

// Arbitrary appends a 'x' blob.
func (w *Writer) Arbitrary(b []byte) {
	if len(b) > maxTagSize {
		w.fail("arbitrary field of %d bytes exceeds %d", len(b), maxTagSize)
		return
	}
	w.buf = append(w.buf, tagArbitrary)
	w.raw32(uint32(len(b)))
	w.buf = append(w.buf, b...)
}

// SampleSpec appends an 'a' field.
func (w *Writer) SampleSpec(s SampleSpec) {
	w.buf = append(w.buf, tagSampleSpec, byte(s.Format), s.Channels)
	w.raw32(s.Rate)
}

// ChannelMap appends a 'm' field.
func (w *Writer) ChannelMap(m ChannelMap) {
	if len(m) > 255 {
		w.fail("channel map of %d channels", len(m))
		return
	}
	w.buf = append(w.buf, tagChannelMap, byte(len(m)))
	for _, p := range m {
		w.buf = append(w.buf, byte(p))
	}
}

// CVolume appends a 'v' field.
func (w *Writer) CVolume(v CVolume) {
	if len(v) > 255 {
		w.fail("volume of %d channels", len(v))
		return
	}
	w.buf = append(w.buf, tagCVolume, byte(len(v)))
	for _, c := range v {
		w.raw32(uint32(c))
	}
}

// Volume appends a 'V' field.
func (w *Writer) Volume(v Volume) {
	w.buf = append(w.buf, tagVolume)
	w.raw32(uint32(v))
}

// PropList appends a 'P' field: each entry as key string, length, and
// the NUL-terminated value blob, closed by the null string. Keys are
// written sorted so the encoding is deterministic.
func (w *Writer) PropList(p PropList) {
	w.buf = append(w.buf, tagPropList)
	for _, key := range sortedKeys(p) {
		value := append([]byte(p[key]), 0)
		w.String(key)
		w.U32(uint32(len(value)))
		w.Arbitrary(value)
	}
	w.NullString()
}

// FormatInfo appends an 'f' field.
func (w *Writer) FormatInfo(f FormatInfo) {
	w.buf = append(w.buf, tagFormatInfo)
	w.U8(uint8(f.Encoding))
	w.PropList(f.Props)
}

// Reader decodes one tagstruct. The first failure sticks: later reads
// return zero values and Err reports the original cause.
type Reader struct {
	data []byte
	pos  int
	err  error
}

// NewReader decodes data.
func NewReader(data []byte) *Reader { return &Reader{data: data} }

// Err reports the first decoding error.
func (r *Reader) Err() error { return r.err }

// EOF reports whether every byte was consumed.
func (r *Reader) EOF() bool { return r.pos >= len(r.data) }

// Rest returns the unread bytes.
func (r *Reader) Rest() []byte { return r.data[r.pos:] }

func (r *Reader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("%w: at byte %d: %s", ErrMalformed, r.pos, fmt.Sprintf(format, args...))
	}
}

// take consumes n bytes, or fails on a truncated struct.
func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.data)-r.pos < n {
		r.fail("need %d bytes, %d left", n, len(r.data)-r.pos)
		return nil
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

// tag consumes one tag byte and checks it against want.
func (r *Reader) tag(want ...byte) byte {
	b := r.take(1)
	if b == nil {
		return 0
	}
	if slices.Contains(want, b[0]) {
		return b[0]
	}
	r.pos--
	r.fail("tag %q, want %q", b[0], want)
	return 0
}

func (r *Reader) raw32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *Reader) raw64() uint64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// U32 reads a 'L' field.
func (r *Reader) U32() uint32 {
	if r.tag(tagU32) == 0 {
		return 0
	}
	return r.raw32()
}

// U8 reads a 'B' field.
func (r *Reader) U8() uint8 {
	if r.tag(tagU8) == 0 {
		return 0
	}
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

// U64 reads a 'R' field.
func (r *Reader) U64() uint64 {
	if r.tag(tagU64) == 0 {
		return 0
	}
	return r.raw64()
}

// S64 reads a 'r' field.
func (r *Reader) S64() int64 {
	if r.tag(tagS64) == 0 {
		return 0
	}
	return int64(r.raw64())
}

// Usec reads a 'U' field.
func (r *Reader) Usec() uint64 {
	if r.tag(tagUsec) == 0 {
		return 0
	}
	return r.raw64()
}

// Bool reads a '1' or '0' field.
func (r *Reader) Bool() bool { return r.tag(tagTrue, tagFalse) == tagTrue }

// String reads a 't' or 'N' field; ok is false for the null string.
// Invalid UTF-8 is replaced lossily, as libpulse-binding's
// to_string_lossy does for wayle-audio.
func (r *Reader) String() (string, bool) {
	switch r.tag(tagString, tagStringNull) {
	case tagStringNull:
		return "", false
	case tagString:
	default:
		return "", false
	}
	end := -1
	for i := r.pos; i < len(r.data); i++ {
		if r.data[i] == 0 {
			end = i
			break
		}
	}
	if end < 0 {
		r.fail("unterminated string")
		return "", false
	}
	s := strings.ToValidUTF8(string(r.data[r.pos:end]), "�")
	r.pos = end + 1
	return s, true
}

// Str reads a string field where the null string reads as "".
func (r *Reader) Str() string {
	s, _ := r.String()
	return s
}

// Arbitrary reads a 'x' blob.
func (r *Reader) Arbitrary() []byte {
	if r.tag(tagArbitrary) == 0 {
		return nil
	}
	n := r.raw32()
	if n > maxTagSize {
		r.fail("arbitrary field of %d bytes exceeds %d", n, maxTagSize)
		return nil
	}
	b := r.take(int(n))
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}

// SampleSpec reads an 'a' field.
func (r *Reader) SampleSpec() SampleSpec {
	if r.tag(tagSampleSpec) == 0 {
		return SampleSpec{}
	}
	b := r.take(2)
	if b == nil {
		return SampleSpec{}
	}
	return SampleSpec{Format: SampleFormat(b[0]), Channels: b[1], Rate: r.raw32()}
}

// ChannelMap reads a 'm' field.
func (r *Reader) ChannelMap() ChannelMap {
	if r.tag(tagChannelMap) == 0 {
		return nil
	}
	n := r.take(1)
	if n == nil {
		return nil
	}
	b := r.take(int(n[0]))
	if b == nil {
		return nil
	}
	m := make(ChannelMap, len(b))
	for i, p := range b {
		m[i] = ChannelPosition(p)
	}
	return m
}

// CVolume reads a 'v' field.
func (r *Reader) CVolume() CVolume {
	if r.tag(tagCVolume) == 0 {
		return nil
	}
	n := r.take(1)
	if n == nil {
		return nil
	}
	v := make(CVolume, n[0])
	for i := range v {
		v[i] = Volume(r.raw32())
	}
	if r.err != nil {
		return nil
	}
	return v
}

// Volume reads a 'V' field.
func (r *Reader) Volume() Volume {
	if r.tag(tagVolume) == 0 {
		return 0
	}
	return Volume(r.raw32())
}

// PropList reads a 'P' field, keeping the entries whose value is a
// NUL-terminated UTF-8 string.
func (r *Reader) PropList() PropList {
	if r.tag(tagPropList) == 0 {
		return nil
	}
	p := PropList{}
	for r.err == nil {
		key, ok := r.String()
		if !ok {
			break
		}
		if key == "" {
			r.fail("empty proplist key")
			break
		}
		n := r.U32()
		value := r.Arbitrary()
		if r.err != nil {
			break
		}
		if int(n) != len(value) {
			r.fail("proplist %q length %d, blob %d", key, n, len(value))
			break
		}
		if s, ok := proplistString(value); ok {
			p[key] = s
		}
	}
	if r.err != nil {
		return nil
	}
	return p
}

// proplistString is pa_proplist_gets: the value is a string only when
// it ends in its single NUL and is valid UTF-8.
func proplistString(value []byte) (string, bool) {
	if len(value) == 0 || value[len(value)-1] != 0 {
		return "", false
	}
	s := string(value[:len(value)-1])
	if strings.IndexByte(s, 0) >= 0 || !utf8.ValidString(s) {
		return "", false
	}
	return s, true
}

// sortedKeys lists a proplist's keys in order.
func sortedKeys(p PropList) []string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// FormatInfo reads an 'f' field.
func (r *Reader) FormatInfo() FormatInfo {
	if r.tag(tagFormatInfo) == 0 {
		return FormatInfo{}
	}
	enc := r.U8()
	props := r.PropList()
	return FormatInfo{Encoding: Encoding(enc), Props: props}
}

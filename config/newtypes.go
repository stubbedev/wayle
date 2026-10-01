package config

import (
	"log"
	"reflect"
)

// Clamped numeric newtypes. Each Rust type deserializes its primitive,
// warns when the value is out of range, and clamps rather than failing
// (validated/*.rs, modules/cava/types.rs, modules/custom/types.rs); the
// schema is the primitive with its range.

type number interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

var basicTypes = map[reflect.Kind]reflect.Type{
	reflect.Uint8:   reflect.TypeFor[uint8](),
	reflect.Uint16:  reflect.TypeFor[uint16](),
	reflect.Uint32:  reflect.TypeFor[uint32](),
	reflect.Uint64:  reflect.TypeFor[uint64](),
	reflect.Float32: reflect.TypeFor[float32](),
	reflect.Float64: reflect.TypeFor[float64](),
}

// decodeClamped decodes the primitive of N and clamps it to [lo, hi],
// logging the Rust warning when it had to.
func decodeClamped[N number](v any, lo, hi N, what string) (N, error) {
	// Decode the underlying primitive: N itself is the Unmarshaler.
	prim := reflect.New(basicTypes[reflect.TypeFor[N]().Kind()]).Elem()
	if err := decodeInto(prim, v); err != nil {
		return 0, err
	}
	raw, _ := reflect.TypeAssert[N](prim.Convert(reflect.TypeFor[N]()))
	if raw < lo || raw > hi {
		clamped := min(max(raw, lo), hi)
		log.Printf("config: %s %v out of range (valid: %v-%v), clamped to %v", what, raw, lo, hi, clamped)
		raw = clamped
	}
	return raw, nil
}

// rangedSchema is a transparent newtype's schema: the primitive's
// schema, its bounds, and the type's doc.
func rangedSchema(t reflect.Type, lo, hi any) Schema {
	s := Schema{"type": "integer", "format": primitiveFormat(t.Kind())}
	if k := t.Kind(); k == reflect.Float32 || k == reflect.Float64 {
		s["type"] = "number"
	}
	if lo != nil {
		s["minimum"] = lo
	}
	if hi != nil {
		s["maximum"] = hi
	}
	if doc := typeDoc(t); doc != "" {
		s["description"] = doc
	}
	return s
}

// Percentage is a 0-100 value (validated/percentage.rs).
//
// Percentage value clamped to 0-100.
type Percentage uint8

// UnmarshalConfig implements Unmarshaler.
func (p *Percentage) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, Percentage(0), Percentage(100), "percentage")
	*p = raw
	return err
}

// MarshalConfig implements Marshaler.
func (p Percentage) MarshalConfig() any { return int64(p) }

func (Percentage) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[Percentage](), int64(0), int64(100))
}

// ScaleFactor is a 0.25-3.0 multiplier (validated/scale.rs).
//
// Scale multiplier clamped to 0.25-3.0.
type ScaleFactor float32

// UnmarshalConfig implements Unmarshaler.
func (s *ScaleFactor) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, ScaleFactor(0.25), ScaleFactor(3), "scale factor")
	*s = raw
	return err
}

// MarshalConfig implements Marshaler.
func (s ScaleFactor) MarshalConfig() any { return float32(s) }

func (ScaleFactor) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[ScaleFactor](), 0.25, 3.0)
}

// NormalizedF64 is a 0-1 float (validated/normalized.rs).
//
// Floating-point value clamped to 0.0-1.0.
type NormalizedF64 float64

// UnmarshalConfig implements Unmarshaler.
func (n *NormalizedF64) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, NormalizedF64(0), NormalizedF64(1), "normalized value")
	*n = raw
	return err
}

// MarshalConfig implements Marshaler.
func (n NormalizedF64) MarshalConfig() any { return float64(n) }

func (NormalizedF64) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[NormalizedF64](), 0.0, 1.0)
}

// BarCount is the cava bar count (modules/cava/types.rs).
//
// Frequency bar count clamped to 1-256 (mirrors `wayle_cava::BarCount`).
type BarCount uint16

// UnmarshalConfig implements Unmarshaler.
func (b *BarCount) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, BarCount(1), BarCount(256), "bar count")
	*b = raw
	return err
}

// MarshalConfig implements Marshaler.
func (b BarCount) MarshalConfig() any { return int64(b) }

func (BarCount) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[BarCount](), int64(1), int64(256))
}

// Framerate is the cava frame rate (modules/cava/types.rs).
//
// Visualization framerate clamped to 1-360 fps (mirrors `wayle_cava::Framerate`).
type Framerate uint32

// UnmarshalConfig implements Unmarshaler.
func (f *Framerate) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, Framerate(1), Framerate(360), "framerate")
	*f = raw
	return err
}

// MarshalConfig implements Marshaler.
func (f Framerate) MarshalConfig() any { return int64(f) }

func (Framerate) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[Framerate](), int64(1), int64(360))
}

// FrequencyHz is a cava cutoff frequency (modules/cava/types.rs).
//
// Frequency value in Hz, minimum 1 Hz.
//
// Cross-field constraints (high_cutoff > low_cutoff, samplerate/2 > high_cutoff)
// are validated at the service builder.
type FrequencyHz uint32

// UnmarshalConfig implements Unmarshaler.
func (f *FrequencyHz) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, FrequencyHz(1), FrequencyHz(^uint32(0)), "frequency")
	*f = raw
	return err
}

// MarshalConfig implements Marshaler.
func (f FrequencyHz) MarshalConfig() any { return int64(f) }

func (FrequencyHz) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[FrequencyHz](), int64(1), nil)
}

// RestartDelay is a custom module's restart delay in ms
// (modules/custom/types.rs).
//
// Restart delay in milliseconds, clamped to >= 1.
type RestartDelay uint64

// UnmarshalConfig implements Unmarshaler.
func (r *RestartDelay) UnmarshalConfig(v any) error {
	raw, err := decodeClamped(v, RestartDelay(1), RestartDelay(^uint64(0)), "restart delay")
	*r = raw
	return err
}

// MarshalConfig implements Marshaler.
func (r RestartDelay) MarshalConfig() any { return encodeValue(reflect.ValueOf(uint64(r))) }

func (RestartDelay) configSchema(*schemaGen) Schema {
	return rangedSchema(typeOf[RestartDelay](), int64(1), nil)
}

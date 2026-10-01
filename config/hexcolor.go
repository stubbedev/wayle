package config

import (
	"fmt"
	"strings"
)

// HexColor is a validated GTK4 CSS hex color: #rgb, #rgba, #rrggbb, or
// #rrggbbaa (wayle-config's styling HexColor). Construct it through
// ParseHexColor or TOML decoding; the zero value reads as opaque black.
type HexColor struct {
	s string
}

// ParseHexColor validates a hex color string. A missing '#', a digit
// count other than 3, 4, 6, or 8, or a non-hex character is an error,
// mirroring the Rust newtype's three failure cases.
func ParseHexColor(s string) (HexColor, error) {
	if !strings.HasPrefix(s, "#") {
		return HexColor{}, fmt.Errorf("hex color must start with '#', got: %s", s)
	}
	digits := s[1:]
	switch len(digits) {
	case 3, 4, 6, 8:
	default:
		return HexColor{}, fmt.Errorf("hex color must have 3, 4, 6, or 8 hex digits after '#', got %d digits in: %s", len(digits), s)
	}
	for _, r := range digits {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return HexColor{}, fmt.Errorf("hex color contains invalid character '%c' in: %s", r, s)
		}
	}
	return HexColor{s: s}, nil
}

// mustHex builds a schema-default color; the literal is a programming
// constant, so a bad one panics at init.
func mustHex(s string) HexColor {
	c, err := ParseHexColor(s)
	if err != nil {
		panic(err)
	}
	return c
}

// String returns the color as written.
func (c HexColor) String() string { return c.s }

// RGBA expands the color to straight-alpha 8-bit channels; the short
// forms double each digit (#abc is #aabbcc) and a missing alpha is
// opaque.
func (c HexColor) RGBA() (r, g, b, a uint8) {
	if c.s == "" {
		return 0, 0, 0, 0xFF
	}
	d := c.s[1:]
	if len(d) == 3 || len(d) == 4 {
		long := make([]byte, 0, 8)
		for i := range len(d) {
			long = append(long, d[i], d[i])
		}
		d = string(long)
	}
	byteAt := func(i int) uint8 { return hexNibble(d[i])<<4 | hexNibble(d[i+1]) }
	r, g, b, a = byteAt(0), byteAt(2), byteAt(4), 0xFF
	if len(d) == 8 {
		a = byteAt(6)
	}
	return r, g, b, a
}

// hexNibble decodes one validated hex digit.
func hexNibble(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// UnmarshalConfig implements Unmarshaler.
func (c *HexColor) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, "a string")
	}
	parsed, err := ParseHexColor(s)
	if err != nil {
		return err
	}
	*c = parsed
	return nil
}

// MarshalConfig implements Marshaler.
func (c HexColor) MarshalConfig() any { return c.s }

// configSchema is hex_color.rs's hand-written JsonSchema.
func (HexColor) configSchema(*schemaGen) Schema {
	return Schema{
		"description": "GTK4 CSS hex color (#rgb, #rgba, #rrggbb, or #rrggbbaa)",
		"type":        "string",
		"pattern":     "^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$",
	}
}

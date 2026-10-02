// Package fileuri is the percent-encoding and file:// URI handling
// wayle's portal, clipboard and VPN code share.
package fileuri

import (
	"fmt"
	"strings"
)

// Decode resolves %XX escapes to bytes. A % that does not begin a
// valid escape is kept as written rather than dropped: it is a literal
// percent sign in a filename or a cookie, and losing it silently
// corrupts the value.
func Decode(s string) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			out = append(out, unhex(s[i+1])<<4|unhex(s[i+2]))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return out
}

// DecodeString is Decode with invalid UTF-8 replaced, as Rust's
// from_utf8_lossy does.
func DecodeString(s string) string { return strings.ToValidUTF8(string(Decode(s)), "�") }

// Encode keeps RFC 3986's unreserved bytes and those in keep, and
// escapes the rest.
func Encode(s, keep string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~", c) >= 0 || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// Path decodes a file:// URI's path, dropping any authority
// (file://host/path); another scheme, or a file URI with no path, is
// not a path.
func Path(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", false
	}
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return "", false
	}
	return DecodeString(rest[slash:]), true
}

// FromPath is the file:// URI of an absolute path.
func FromPath(path string) string { return "file://" + Encode(path, "/") }

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

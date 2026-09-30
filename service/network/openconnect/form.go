package openconnect

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// application/x-www-form-urlencoded encoding (form.rs). Both the login
// body and the session cookie are this format, and openconnect
// URL-decodes the cookie it is handed — so a username containing \ or a
// password containing & has to arrive escaped or the pairs run together.
//
// net/url is not used: it encodes a space as +, which openconnect's
// decoder leaves as a literal plus sign.

// pair is one key/value of a form body, in wire order.
type pair struct{ key, value string }

// formEscape encodes one value. Unreserved characters pass through;
// everything else becomes %XX, uppercase, over the value's UTF-8 bytes.
// Space is %20, not +.
func formEscape(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := range len(value) {
		c := value[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			out.WriteByte(c)
		default:
			fmt.Fprintf(&out, "%%%02X", c)
		}
	}
	return out.String()
}

// formEncode joins key/value pairs into a form body, escaping both sides.
func formEncode(pairs ...pair) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = formEscape(p.key) + "=" + formEscape(p.value)
	}
	return strings.Join(parts, "&")
}

// percentDecode resolves %XX escapes back to bytes. A % that does not
// begin a valid escape is kept as written rather than dropped: it is a
// literal percent sign in someone's cookie or filename, and losing it
// silently corrupts the value.
func percentDecode(value string) []byte {
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '%' && i+2 < len(value) && isHex(value[i+1]) && isHex(value[i+2]) {
			out = append(out, unhex(value[i+1])<<4|unhex(value[i+2]))
			i += 2
			continue
		}
		out = append(out, value[i])
	}
	return out
}

// decodeComponent is the inverse of formEscape for one component, with
// invalid UTF-8 replaced rather than refused (decode_component).
func decodeComponent(value string) string {
	return strings.ToValidUTF8(string(percentDecode(value)), string(utf8.RuneError))
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

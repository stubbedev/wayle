package openconnect

import (
	"fmt"
	"strings"

	"github.com/stubbedev/wayle/internal/fileuri"
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

// decodeComponent is the inverse of formEscape for one component, with
// invalid UTF-8 replaced rather than refused (decode_component).
func decodeComponent(value string) string {
	return fileuri.DecodeString(value)
}

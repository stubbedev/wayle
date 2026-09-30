package openconnect

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// GlobalProtect SAML sign-in through the system browser (gp_sso.rs).
//
// A SAML gateway answers prelogin with saml-auth-method and
// saml-request instead of field labels. Completing that is a browser's
// job, not a form's. wayle does not embed one (webkitgtk is a very large
// dependency for one sign-in flow); it does what recent GlobalProtect
// clients do: the portal hands the sign-in to the real browser and takes
// the answer back through a custom URI scheme, globalprotectcallback:.
// wayle registers a handler for that scheme, the browser hands the
// payload to `wayle vpn sso-callback`, and it reaches the waiting
// sign-in over the shell's D-Bus interface (DeliverSSOCallback).
//
// Not verified, because it needs a real SAML portal: the callback
// payload's exact encoding. Palo Alto documents neither the scheme nor
// the payload, so parseCallback reads all three shapes the values are
// known to travel in — header lines, XML elements, and HTML <meta> tags
// — rather than betting on one. Nothing here runs unless the profile
// asks for it (wayle-sso).

// samlMethod is how the portal wants the SAML request delivered.
type samlMethod int

const (
	// samlRedirect: saml-request is a URL to open.
	samlRedirect samlMethod = iota + 1
	// samlPost: saml-request is an HTML document to render, which posts
	// itself to the identity provider.
	samlPost
)

// samlRequest is the sign-in a SAML gateway wants performed in a browser.
type samlRequest struct {
	method samlMethod
	// payload is the decoded request: a URL for samlRedirect, an HTML
	// document for samlPost.
	payload string
}

// gpCallback is what the browser hands back once the identity provider
// is satisfied. Empty strings are absent fields.
type gpCallback struct {
	// username is who signed in, as the portal understands it.
	username string
	// preloginCookie is the gateway's pre-login cookie.
	preloginCookie string
	// portalCookie is the portal's user-auth cookie, sent instead of the
	// above by a portal rather than a gateway.
	portalCookie string
}

// cookie is the cookie to sign in with, whichever of the two the portal
// sent; the gateway's wins.
func (c gpCallback) cookie() string {
	if c.preloginCookie != "" {
		return c.preloginCookie
	}
	return c.portalCookie
}

// callbackScheme is the URI scheme GlobalProtect sends its browser
// answer back on.
const callbackScheme = "globalprotectcallback"

// parseSAMLRequest reads the SAML request out of a prelogin response,
// or nil for an ordinary gateway (every gateway that sends field labels
// instead) and for a request this code cannot carry out.
func parseSAMLRequest(body string) *samlRequest {
	method, ok := xmlValue(body, "saml-auth-method")
	if !ok {
		return nil
	}
	request, ok := xmlNonEmpty(body, "saml-request")
	if !ok {
		return nil
	}

	var m samlMethod
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "REDIRECT":
		m = samlRedirect
	case "POST":
		m = samlPost
	default:
		// An unknown method is not something to guess at: the payload's
		// meaning is exactly what the method names.
		return nil
	}

	// Whitespace is not part of base64 but gateways wrap the value anyway.
	decoded, err := base64.StdEncoding.DecodeString(stripSpace(request))
	if err != nil || !utf8.Valid(decoded) {
		return nil
	}
	return &samlRequest{method: m, payload: string(decoded)}
}

// isOpenableURL reports whether a decoded REDIRECT payload is a URL
// worth opening. It goes to the system browser, so anything but https:
// (or http:, which some internal portals still use) is refused — a
// file: or a javascript: here would be the gateway choosing what runs on
// this machine.
func isOpenableURL(payload string) bool {
	trimmed := strings.TrimSpace(payload)
	lower := asciiLower(trimmed)
	return (strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")) &&
		strings.IndexFunc(trimmed, unicode.IsSpace) < 0
}

// parseCallback reads the payload the browser handed back: the callback
// URI whole (globalprotectcallback:<payload>) or just the payload,
// base64 or in the clear. A URI for another scheme, or a payload with no
// cookie in it, is an error.
func parseCallback(uri string) (gpCallback, error) {
	trimmed := strings.TrimSpace(uri)
	payload := trimmed
	if scheme, rest, ok := strings.Cut(trimmed, ":"); ok {
		switch {
		case asciiEqualFold(scheme, callbackScheme):
			payload = rest
		case scheme == "" || strings.IndexFunc(scheme, unicode.IsSpace) >= 0:
			// Not a scheme at all: the colon belongs to a header line.
		case !strings.Contains(trimmed, "\n") && strings.Contains(trimmed, "://"):
			return gpCallback{}, authError("the browser answered with an unexpected address")
		}
	}

	callback := readCallbackFields(decodeCallbackPayload(strings.TrimLeft(payload, "/")))
	if callback.cookie() == "" {
		return gpCallback{}, authError("the browser sign-in did not return a gateway cookie")
	}
	return callback, nil
}

// decodeCallbackPayload base64-decodes when it can and hands back the
// text percent-decoded when it cannot: both shapes have been seen, and a
// portal sending plain XML should not look like a failure.
func decodeCallbackPayload(payload string) string {
	compact := stripSpace(payload)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		decoded, err := encoding.DecodeString(compact)
		if err == nil && utf8.Valid(decoded) && strings.IndexFunc(string(decoded), isASCIILetter) >= 0 {
			return string(decoded)
		}
	}
	return decodeComponent(payload)
}

// readCallbackFields reads the three shapes the same values travel in:
// HTTP headers in the non-browser flow, <meta> tags on the portal's own
// success page, and plain XML.
func readCallbackFields(text string) gpCallback {
	return gpCallback{
		username:       callbackField(text, "saml-username"),
		preloginCookie: callbackField(text, "prelogin-cookie"),
		portalCookie:   callbackField(text, "portal-userauthcookie"),
	}
}

// callbackField is one field, in whichever shape it appears, trimmed; ""
// when absent or blank.
func callbackField(text, name string) string {
	for _, find := range []func(string, string) (string, bool){xmlValue, metaTag, headerLine} {
		if value, ok := find(text, name); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// metaTag reads <meta name="name" content="value">, in either attribute
// order.
func metaTag(text, name string) (string, bool) {
	lower := asciiLower(text)
	at := strings.Index(lower, `name="`+name+`"`)
	if at < 0 {
		return "", false
	}
	// The content attribute may sit on either side of the name attribute
	// within the same tag, so search the tag rather than the remainder.
	start := strings.LastIndexByte(lower[:at], '<')
	end := strings.IndexByte(lower[at:], '>')
	if start < 0 || end < 0 {
		return "", false
	}
	tag := text[start : at+end]
	content := strings.Index(asciiLower(tag), `content="`)
	if content < 0 {
		return "", false
	}
	value, _, closed := strings.Cut(tag[content+len(`content="`):], `"`)
	return value, closed
}

// headerLine reads `name: value` on its own line.
func headerLine(text, name string) (string, bool) {
	for line := range strings.Lines(text) {
		key, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), ":")
		if ok && asciiEqualFold(strings.TrimSpace(key), name) {
			return value, true
		}
	}
	return "", false
}

// gpWaiter is the sign-in currently waiting for a browser callback.
type gpWaiter struct {
	answer   chan string
	replaced chan struct{}
}

// One sign-in at a time: the URI scheme carries no correlation id, so a
// second concurrent sign-in would have no way to tell whose answer
// arrived. Starting one cancels the one before it, which is what a user
// retrying a stuck sign-in expects.
var (
	pendingMu sync.Mutex
	pending   *gpWaiter
)

// deliverGPCallback hands a globalprotectcallback: URI to whatever
// sign-in is waiting for it, reporting whether anything was. A callback
// with no sign-in in progress is stale (a re-opened tab, a second click)
// and is dropped rather than kept for the next one.
func deliverGPCallback(uri string) bool {
	pendingMu.Lock()
	waiter := pending
	pending = nil
	pendingMu.Unlock()
	if waiter == nil {
		return false
	}
	waiter.answer <- uri
	return true
}

// gpSSOSignIn opens the browser at the identity provider and waits for
// the answer, up to timeout.
func gpSSOSignIn(ctx context.Context, request samlRequest, timeout time.Duration) (gpCallback, error) {
	url, err := browserURL(request)
	if err != nil {
		return gpCallback{}, err
	}

	// Registered before the browser opens: the identity provider can
	// answer faster than this goroutine is scheduled again, and a
	// callback with nothing waiting is dropped.
	waiter := &gpWaiter{answer: make(chan string, 1), replaced: make(chan struct{})}
	pendingMu.Lock()
	if pending != nil {
		close(pending.replaced)
	}
	pending = waiter
	pendingMu.Unlock()
	defer func() {
		pendingMu.Lock()
		if pending == waiter {
			pending = nil
		}
		pendingMu.Unlock()
	}()

	if err := openBrowser(url); err != nil {
		return gpCallback{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case uri := <-waiter.answer:
		return parseCallback(uri)
	case <-waiter.replaced:
		return gpCallback{}, authError("the browser sign-in was replaced by another")
	case <-ctx.Done():
		return gpCallback{}, ssoWaitEnded(ctx)
	}
}

// browserURL is the address to hand the browser. A POST request is an
// HTML document rather than an address, so it is written to a file the
// browser can open.
func browserURL(request samlRequest) (string, error) {
	if request.method == samlPost {
		path, err := writeRequestDocument(request.payload)
		if err != nil {
			return "", err
		}
		return "file://" + path, nil
	}
	if !isOpenableURL(request.payload) {
		return "", authError("the gateway asked to open something that is not a web address")
	}
	return request.payload, nil
}

// writeRequestDocument writes the self-submitting form to a private file
// in the temporary directory. It carries a SAML request that names the
// user to the identity provider; it is nobody else's business on a
// shared machine.
func writeRequestDocument(html string) (string, error) {
	name := fmt.Sprintf("wayle-gp-saml-%d-%d.html", os.Getpid(), time.Now().UnixNano())
	path := filepath.Join(os.TempDir(), name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // a name this process made up, in the temp directory
	if err != nil {
		return "", authError("cannot prepare the browser sign-in: " + err.Error())
	}
	_, err = file.WriteString(html)
	err = errors.Join(err, file.Close())
	if err != nil {
		return "", authError("cannot prepare the browser sign-in: " + err.Error())
	}
	return path, nil
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

func isASCIILetter(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }

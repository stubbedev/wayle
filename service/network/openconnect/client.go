package openconnect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// userAgent is what openconnect reports itself as. Some gateways gate
	// on it.
	userAgent = "PAN GlobalProtect"
	// connectTimeout bounds reaching the gateway at all. Short, because
	// nothing about it waits on a person.
	connectTimeout = 15 * time.Second
	// preloginTimeout bounds a prelogin: a static document; a gateway
	// that has not answered in this long is not going to.
	preloginTimeout = 20 * time.Second
	// loginTimeout bounds a login post. Generous on purpose: a gateway
	// doing push MFA holds this request open until the user has approved
	// it on their phone.
	loginTimeout = 180 * time.Second
	// maxReply caps how much of a reply is read. Every document these
	// protocols exchange is a few kilobytes.
	maxReply = 8 << 20
	// maxRedirects is reqwest's default redirect limit.
	maxRedirects = 10

	formContentType = "application/x-www-form-urlencoded"
)

// clientRoots is the trust store the sign-in verifies gateways against;
// nil is the system's. Tests point it at the fake gateway's CA, so the
// production client verifies the fake for real rather than being told to
// skip the check.
var clientRoots *x509.CertPool

// client is the HTTPS client the sign-in runs on (client() in mod.rs).
// No overall timeout: the per-request ones differ by an order of
// magnitude and a client-wide one would cap them all at the shortest.
// No cookie jar, and HTTP/1.1 only, like the Rust's reqwest client;
// redirects are followed.
type client struct{ transport http.RoundTripper }

func newClient() *client {
	dialer := &net.Dialer{Timeout: connectTimeout}
	return &client{transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: connectTimeout,
		TLSClientConfig:     &tls.Config{RootCAs: clientRoots, MinVersion: tls.VersionTLS12},
	}}
}

// request is one HTTP exchange with a gateway. A POST body is always
// form-encoded as far as the Content-Type goes (openconnect sends that
// type even for AnyConnect's XML, and some gateways check it).
type request struct {
	method  string
	url     string
	body    string
	timeout time.Duration
	header  map[string]string
}

// reply is a gateway's answer, read whole.
type reply struct {
	status int
	header http.Header
	body   string
	// hops is the header of every response on the way here, redirects
	// first and this one last: a gateway sets its session cookie on the
	// response that redirects away from its login pages.
	hops []http.Header
	// pin is the certificate pin of the host that answered (after
	// redirects), "" when there was no verified TLS peer.
	pin string
	// url is the address the answer finally came from.
	url string
}

// do runs one exchange. A gateway nobody could reach, or whose reply
// broke off, is a sign-in that did not finish rather than a refusal.
func (c *client) do(ctx context.Context, r request) (reply, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var body io.Reader
	if r.method == http.MethodPost {
		body = strings.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return reply{}, incomplete("cannot reach the gateway: " + err.Error())
	}
	req.Header.Set("User-Agent", userAgent)
	if r.method == http.MethodPost {
		req.Header.Set("Content-Type", formContentType)
	}
	for key, value := range r.header {
		req.Header.Set(key, value)
	}

	var hops []http.Header
	hc := &http.Client{
		Transport: c.transport,
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if next.Response != nil {
				hops = append(hops, next.Response.Header)
			}
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	resp, err := hc.Do(req)
	if err != nil {
		return reply{}, incomplete("cannot reach the gateway: " + err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return reply{}, incomplete("cannot read the gateway's reply: " + err.Error())
	}
	pin, _ := peerPin(resp)
	return reply{
		status: resp.StatusCode,
		header: resp.Header,
		body:   string(raw),
		hops:   append(hops, resp.Header),
		pin:    pin,
		url:    resp.Request.URL.String(),
	}, nil
}

// requirePin is the pin of the host that minted a cookie. The plugin
// refuses to start without one, so a reply without it is a failed
// sign-in, however good the cookie.
func (r reply) requirePin() (string, error) {
	if r.pin == "" {
		return "", authError("cannot read the gateway's certificate, which the VPN plugin requires")
	}
	return r.pin, nil
}

func (r reply) success() bool { return r.status >= 200 && r.status < 300 }

func (r reply) redirection() bool { return r.status >= 300 && r.status < 400 }

// setCookie finds a Set-Cookie whose name accept takes, with a
// non-empty value — an empty one is a deletion, which is what a gateway
// sends before there is a session. Only the first name=value pair of a
// header is the cookie itself. The latest response that set one wins;
// within a response, the first.
func (r reply) setCookie(accept func(name, value string) bool) (name, value string, ok bool) {
	for _, hop := range slices.Backward(r.hops) {
		for _, line := range hop.Values("Set-Cookie") {
			first, _, _ := strings.Cut(line, ";")
			name, value, found := strings.Cut(first, "=")
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			if found && value != "" && accept(name, value) {
				return name, value, true
			}
		}
	}
	return "", "", false
}

// namedCookie is setCookie for one exact name, as openconnect's --cookie
// wants it: "name=value".
func (r reply) namedCookie(want string) (string, bool) {
	name, value, ok := r.setCookie(func(name, _ string) bool { return name == want })
	if !ok {
		return "", false
	}
	return name + "=" + value, true
}

// statusText renders a status the way the Rust's reqwest StatusCode
// does: "404 Not Found", "512 <unknown status code>".
func statusText(status int) string {
	text := http.StatusText(status)
	if text == "" {
		text = "<unknown status code>"
	}
	return strconv.Itoa(status) + " " + text
}

package openconnect

import (
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Sign-in for the protocols that authenticate through an HTML login
// page — Juniper (nc), Pulse Connect Secure (pulse) and F5 BIG-IP (f5)
// — the port of form_login.rs (endpoints, cookie names, the exchange)
// and web_login.rs (reading a page's form and filling it).
//
// These gateways are configured by their administrators: the realm
// picker, the second-factor page, the role-selection page and the "you
// already have a session" confirmation are all optional, and the hidden
// fields carry deployment-specific tokens. So nothing here knows the
// form. It reads every <input> on the page, keeps the values already in
// them, fills in the ones that name a username or a password, and posts
// the lot back to the form's own action — what a browser does.
//
// Not verified against any real gateway; their markup is per-deployment
// by design, which is why a profile can hand itself back to the
// plugin's own auth dialog (SignInKey).

// webDialect is what one of these protocols needs to know about itself.
type webDialect struct {
	// loginPath is the login page, relative to the gateway.
	loginPath string
	// cookie is the cookie whose value is the session.
	cookie string
}

var (
	// juniperDialect: Juniper and Pulse share the dana-na web front end,
	// and openconnect's Pulse support authenticates through the same
	// pages before it switches to its own transport.
	juniperDialect = &webDialect{loginPath: "/dana-na/auth/url_default/welcome.cgi", cookie: "DSID"}
	// f5Dialect: F5 BIG-IP APM, one policy endpoint, and MRHSession once
	// it is satisfied.
	f5Dialect = &webDialect{loginPath: "/my.policy", cookie: "MRHSession"}
)

// webDialectFor is the dialect for a protocol, or nil when this does
// not speak it.
func webDialectFor(protocol string) *webDialect {
	switch protocol {
	case "nc", "pulse":
		return juniperDialect
	case "f5":
		return f5Dialect
	}
	return nil
}

const (
	// maxPages is how many pages to work through before giving up. A
	// realm picker, a second factor and a confirmation is three; a
	// gateway still asking after this is looping.
	maxPages = 6
)

var (
	// usernameFields are input names that mean "the username",
	// lowercased (from the pages auth-juniper.c and f5.c handle).
	usernameFields = []string{"username", "user", "uname", "userid"}
	// passwordFields are input names that mean "the password".
	passwordFields = []string{"password", "passwd", "pass", "password#2"}
	// skippedTypes are input types never posted back with the rest. A
	// submit button's value is only sent for the button actually
	// clicked; reset does nothing on the wire; file has no value.
	skippedTypes = []string{"submit", "reset", "button", "image", "file"}
)

// webLogin signs in through the gateway's web login and returns the
// session.
func (a *attempt) webLogin() (signIn, error) {
	dialect := webDialectFor(a.profile.Protocol)
	if dialect == nil {
		return signIn{}, authError("no web sign-in for this openconnect protocol")
	}

	url := "https://" + a.profile.Gateway + dialect.loginPath
	// Pinned from whichever response is in hand, so it ends up being the
	// certificate the host presented while it was issuing the cookie.
	gwcert, cookie, remember := "", "", ""
	askedCredentials := false
	method, body := http.MethodGet, ""

	for range maxPages {
		r, err := a.client.do(a.ctx, request{method: method, url: url, body: body, timeout: loginTimeout})
		if err != nil {
			return signIn{}, err
		}
		if r.pin != "" {
			gwcert = r.pin
		}
		// The session cookie is set on whichever response finally accepts
		// the sign-in, usually the one that redirects away from the login
		// pages.
		if found, ok := webSessionCookie(r, dialect.cookie); ok {
			cookie = found
		}

		form, credentials, done := classifyPage(r.body)
		switch {
		case done:
			if cookie == "" {
				// No form left to fill and no session: an error page, or
				// a sign-in the gateway wants done another way.
				return signIn{}, authError("the gateway ended the sign-in without a session cookie")
			}
			if gwcert == "" {
				return signIn{}, authError("could not pin the gateway's certificate")
			}
			return signIn{
				session: session{
					cookie: formEncode(pair{dialect.cookie, cookie}),
					host:   a.profile.Gateway,
					gwcert: gwcert,
				},
				rememberPassword: remember,
			}, nil
		case credentials:
			// Asked twice means the first answer was wrong; the stored
			// password is dropped by the caller on the error.
			if askedCredentials {
				return signIn{}, authError("the gateway rejected the credentials")
			}
			username, password, typed, err := a.usernamePassword(a.profile.Username)
			if err != nil {
				return signIn{}, err
			}
			askedCredentials, remember = true, typed
			body = formEncode(form.filled(username, password)...)
		default:
			// A realm picker, a role choice, or "you already have a
			// session": answered by sending it back as it came.
			body = formEncode(form.fields...)
		}
		method, url = http.MethodPost, resolveAction(r.url, form.action)
	}
	return signIn{}, authError("the gateway kept asking for more pages")
}

// htmlForm is one HTML form, as much of it as signing in needs.
type htmlForm struct {
	// action is where to post it, exactly as written; "" when absent.
	action string
	// fields is every input worth posting back, in document order.
	fields []pair
}

func fieldNamed(names []string, name string) bool { return slices.Contains(names, asciiLower(name)) }

func (f htmlForm) wants(names []string) bool {
	return slices.ContainsFunc(f.fields, func(p pair) bool { return fieldNamed(names, p.key) })
}

// filled fills in the credentials this form asks for, leaving everything
// else — realm, tokens, hidden state — exactly as the gateway sent it.
func (f htmlForm) filled(username, password string) []pair {
	out := make([]pair, len(f.fields))
	for i, p := range f.fields {
		switch {
		case fieldNamed(usernameFields, p.key):
			p.value = username
		case fieldNamed(passwordFields, p.key):
			p.value = password
		}
		out[i] = p
	}
	return out
}

// classifyPage says what a page is: a form asking for the username or
// password (credentials), a form asking for something else (an
// interstitial, answered by posting it back), or no form at all (done:
// the gateway has stopped asking).
func classifyPage(html string) (form htmlForm, credentials, done bool) {
	form, ok := parseHTMLForm(html)
	if !ok {
		return htmlForm{}, false, true
	}
	return form, form.wants(passwordFields) || form.wants(usernameFields), false
}

// parseHTMLForm reads the first form on a page, or false when the page
// has none.
func parseHTMLForm(html string) (htmlForm, bool) {
	at, ok := findTag(html, "form")
	if !ok {
		return htmlForm{}, false
	}
	element := html[at:]
	form := htmlForm{}
	form.action, _ = nonEmpty(xmlAttribute(element, "action"))
	for _, input := range inputTags(element) {
		if field, ok := htmlField(input); ok {
			form.fields = append(form.fields, field)
		}
	}
	return form, true
}

// inputTags is every <input …> opening tag on the page. <input> is a
// void element: HTML gives it no closing tag, so the XML reader finds
// none of them; every value it carries lives in its attributes.
func inputTags(html string) []string {
	var found []string
	rest := html
	for {
		at, ok := findTag(rest, "input")
		if !ok {
			return found
		}
		after := rest[at:]
		end := strings.IndexByte(after, '>')
		if end < 0 {
			return found
		}
		found = append(found, after[:end+1])
		rest = after[end+1:]
	}
}

// findTag is where the next <name opening tag starts, matched
// case-insensitively and only when it is the whole tag name (<input,
// not <inputmode>).
func findTag(html, name string) (int, bool) {
	lower := asciiLower(html)
	needle := "<" + name
	from := 0
	for {
		at := strings.Index(lower[from:], needle)
		if at < 0 {
			return 0, false
		}
		start := from + at
		next, size := utf8.DecodeRuneInString(lower[start+len(needle):])
		if size == 0 || unicode.IsSpace(next) || next == '>' || next == '/' {
			return start, true
		}
		from = start + len(needle)
	}
}

// htmlField is one input's name and value, or false when it is not
// worth posting.
func htmlField(input string) (pair, bool) {
	name, ok := nonEmpty(xmlAttribute(input, "name"))
	if !ok {
		return pair{}, false
	}
	kind, _ := xmlAttribute(input, "type")
	kind = asciiLower(kind)
	if slices.Contains(skippedTypes, kind) {
		return pair{}, false
	}
	// An unchecked box is not submitted; a checked one is. Anything the
	// gateway pre-ticked is part of the answer it expects back.
	if kind == "checkbox" || kind == "radio" {
		if _, checked := xmlAttribute(input, "checked"); !checked && !strings.Contains(asciiLower(input), " checked") {
			return pair{}, false
		}
	}
	value, _ := xmlAttribute(input, "value")
	return pair{name, value}, true
}

// resolveAction turns a form's action into an absolute URL. Gateways
// write all three forms: absolute, root-relative, and relative to the
// page the form came from. Posting a relative action to the wrong base
// is a 404 the user would see as "wrong password".
func resolveAction(base, action string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		// No action means "post back to where this came from".
		return base
	}
	if strings.HasPrefix(action, "https://") || strings.HasPrefix(action, "http://") {
		return action
	}
	origin := originOf(base)
	if strings.HasPrefix(action, "/") {
		return origin + action
	}
	// Past https://, so the slash found is a path separator rather than
	// one of the scheme's own.
	if slash := strings.LastIndexByte(base, '/'); slash > len(origin) {
		return base[:slash+1] + action
	}
	return origin + "/" + action
}

// originOf is the scheme://host of a URL.
func originOf(url string) string {
	afterScheme := 0
	if at := strings.Index(url, "://"); at >= 0 {
		afterScheme = at + 3
	}
	if end := strings.IndexByte(url[afterScheme:], '/'); end >= 0 {
		return url[:afterScheme+end]
	}
	return url
}

// webSessionCookie is the session cookie's value, matched by name
// case-insensitively: DSID for Juniper and Pulse, MRHSession for F5. A
// gateway clears a cookie by setting it empty (or ""); that is a
// sign-out, not a session.
func webSessionCookie(r reply, want string) (string, bool) {
	_, value, ok := r.setCookie(func(name, value string) bool {
		return asciiEqualFold(name, want) && value != `""`
	})
	return value, ok
}

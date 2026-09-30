package openconnect

import (
	"net/http"
	"slices"
	"strings"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// AnyConnect (Cisco) gateway authentication, spoken natively over HTTPS
// (anyconnect.rs).
//
// The same shape as GlobalProtect and a different dialect: AnyConnect
// posts XML and is told what to ask. The gateway answers with a <form>
// of <input> elements, and the reply carries one element per input plus
// the <opaque> blob echoed back untouched. A second factor is the same
// exchange again with a different form. The wire details are
// openconnect's auth.c. Where this cannot follow the conversation it
// returns a ProtocolUnsupportedError rather than a failure — NM's cue to
// let the plugin's own auth dialog try.

const (
	// acVersion is what openconnect reports itself as. Some gateways gate
	// on the version.
	acVersion = "v9.12"
	// acDeviceID is the device identifier openconnect sends for a 64-bit
	// Linux client.
	acDeviceID = "linux-64"
	// acCookieName is the session cookie's name in the gateway's
	// Set-Cookie; as webvpn=…, it is openconnect's --cookie.
	acCookieName = "webvpn"
)

// acField is one field the gateway is asking for.
type acField struct {
	// name is the element name the answer goes back under.
	name   string
	label  string
	secret bool
	// sso marks <input type="sso">: the value comes from the browser
	// sign-in rather than from the user, so it is never prompted for.
	sso bool
}

// acForm is the form a gateway is asking to have filled in. Empty
// strings are absent parts.
type acForm struct {
	message string
	// fields to ask for, in the gateway's order.
	fields []acField
	// opaque is the <opaque> blob to echo back verbatim.
	opaque string
	// group is the tunnel group to select, when the gateway offered one.
	group string
	// ssoLogin is sso-v2-login: the URL to open in the browser.
	ssoLogin string
}

// anyconnect signs in to an AnyConnect gateway, following however many
// forms it asks for.
func (a *attempt) anyconnect() (signIn, error) {
	// The browser sign-in needs a key pair from the very first request:
	// the gateway encrypts the token to it. Generated only when the
	// profile asked for the flow.
	var keys *ssoKeys
	if a.profile.SSO {
		var err error
		if keys, err = generateSSOKeys(); err != nil {
			return signIn{}, err
		}
	}
	// Kept alongside: the header goes on every request of the
	// conversation, while the key itself is consumed the moment a token
	// arrives.
	pubkey := ""
	if keys != nil {
		pubkey = keys.publicBase64
	}
	body := acInitRequest(a.profile.Gateway, keys != nil)
	remember := ""

	for range maxChallenges + 1 {
		form, cookie, gwcert, done, err := a.acPost(body, pubkey)
		if err != nil {
			return signIn{}, err
		}
		if done {
			if cookie == "" {
				return signIn{}, authError("the gateway authenticated us but set no session cookie")
			}
			return signIn{
				session:          session{cookie: cookie, host: a.profile.Gateway, gwcert: gwcert},
				rememberPassword: remember,
			}, nil
		}

		answers, err := a.acAnswers(form, &keys)
		if err != nil {
			return signIn{}, err
		}
		// Only the first password is worth remembering: a second factor is
		// a second factor precisely because it is different every time.
		if remember == "" {
			remember = acPasswordOf(form, answers)
		}
		body = acReplyRequest(form, answers)
	}
	return signIn{}, authError("the gateway kept asking for more factors")
}

// acPost is one round trip with the gateway: the form it wants next, or
// done, with the cookie it set and the pin of the certificate it
// presented.
func (a *attempt) acPost(body, dhPubkey string) (form acForm, cookie, gwcert string, done bool, err error) {
	header := map[string]string{}
	// The key the gateway encrypts the SSO token to. Sent on every request
	// of the conversation, as openconnect does — the gateway may answer
	// any of them with the browser flow.
	if dhPubkey != "" {
		header["X-AnyConnect-STRAP-Pubkey"] = dhPubkey
		header["X-AnyConnect-STRAP-DH-Pubkey"] = dhPubkey
	}
	r, err := a.client.do(a.ctx, request{
		method:  http.MethodPost,
		url:     "https://" + a.profile.Gateway + "/",
		body:    body,
		timeout: loginTimeout,
		header:  header,
	})
	if err != nil {
		return acForm{}, "", "", false, err
	}
	if gwcert, err = r.requirePin(); err != nil {
		return acForm{}, "", "", false, err
	}
	cookie, _ = r.namedCookie(acCookieName)
	if !r.success() {
		return acForm{}, "", "", false,
			unsupported("the gateway answered " + statusText(r.status) + " to an AnyConnect authentication")
	}
	form, done, err = parseAC(r.body)
	return form, cookie, gwcert, done, err
}

// acInitRequest is the opening request: "who are you, and what do you
// want from me". The browser capability is advertised only when the
// profile asked for it: a gateway that sees it can insist on a browser
// where it would otherwise have served a form.
func acInitRequest(gateway string, sso bool) string {
	capabilities := ""
	if sso {
		capabilities = "<capabilities>" +
			"<auth-method>single-sign-on-external-browser</auth-method>" +
			"</capabilities>"
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		`<config-auth client="vpn" type="init" aggregate-auth-version="2">` +
		`<version who="vpn">` + acVersion + `</version>` +
		`<device-id>` + acDeviceID + `</device-id>` +
		`<group-access>https://` + gateway + `</group-access>` +
		capabilities +
		`</config-auth>`
}

// acReplyRequest is the answer to a form. Each input goes back as an
// element named after it, inside <auth>; the group selection goes
// outside it, and the <opaque> blob is returned untouched — the gateway
// uses it to recognise the conversation.
func acReplyRequest(form acForm, answers []pair) string {
	var fields strings.Builder
	for _, answer := range answers {
		fields.WriteString("<" + answer.key + ">" + xmlEscape(answer.value) + "</" + answer.key + ">")
	}
	group := ""
	if form.group != "" {
		group = "<group-select>" + xmlEscape(form.group) + "</group-select>"
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		`<config-auth client="vpn" type="auth-reply" aggregate-auth-version="2">` +
		`<version who="vpn">` + acVersion + `</version>` +
		`<device-id>` + acDeviceID + `</device-id>` +
		`<session-token></session-token>` +
		`<session-id></session-id>` +
		form.opaque +
		`<auth>` + fields.String() + `</auth>` +
		group +
		`</config-auth>`
}

// parseAC reads a gateway's answer to an exchange: done, a form, a
// refusal, or something this does not understand.
func parseAC(body string) (acForm, bool, error) {
	auth, ok := rawElement(body, "auth")
	if !ok {
		return acForm{}, false, unsupported("the gateway's reply is not an AnyConnect authentication")
	}
	if id, _ := xmlAttribute(auth, "id"); id == "success" {
		return acForm{}, true, nil
	}
	// An <error> is the gateway saying no, in its own words. A <message>
	// alongside a form is the gateway saying what to type next, which
	// must not read as a failure.
	if refusal, ok := xmlNonEmpty(auth, "error"); ok {
		return acForm{}, false, authError(refusal)
	}

	formXML, ok := rawElement(auth, "form")
	if !ok {
		return acForm{}, false, unsupported("the gateway asked for something that is not a form")
	}
	var fields []acField
	for _, input := range rawElements(formXML, "input") {
		if field, ok := acFieldOf(input); ok {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return acForm{}, false, unsupported("the gateway's form has no fields to fill in")
	}

	form := acForm{fields: fields, group: acGroup(formXML)}
	form.message, _ = xmlNonEmpty(auth, "message")
	form.opaque, _ = rawElement(body, "opaque")
	form.ssoLogin, _ = xmlNonEmpty(body, "sso-v2-login")
	return form, false, nil
}

// acFieldOf is one <input>, or false for the ones that are not a
// question — a hidden field carries its own value and a button is not
// typed into.
func acFieldOf(input string) (acField, bool) {
	kind, _ := xmlAttribute(input, "type")
	if kind == "hidden" || kind == "submit" || kind == "button" {
		return acField{}, false
	}
	name, ok := nonEmpty(xmlAttribute(input, "name"))
	if !ok {
		return acField{}, false
	}
	label, ok := nonEmpty(xmlAttribute(input, "label"))
	if !ok {
		label = name
	}
	return acField{
		name: name,
		// Gateways label their fields "Username:", which reads badly next
		// to an entry box that already looks like one.
		label:  strings.TrimSpace(strings.TrimRight(label, ":")),
		secret: kind == "password",
		sso:    kind == "sso",
	}, true
}

// acGroup is the tunnel group to select: whichever option the gateway
// marked selected, else the first it offered.
func acGroup(form string) string {
	selectXML, ok := rawElement(form, "select")
	if !ok {
		return ""
	}
	options := rawElements(selectXML, "option")
	if len(options) == 0 {
		return ""
	}
	chosen := options[0]
	for _, option := range options {
		if selected, ok := xmlAttribute(option, "selected"); ok && (selected == "true" || asciiEqualFold(selected, "yes")) {
			chosen = option
			break
		}
	}
	if value, ok := xmlAttribute(chosen, "value"); ok {
		return value
	}
	value, _ := xmlValue(chosen, "option")
	return value
}

// acAnswers asks the user for the fields the gateway wants, filling in
// what is already known — the stored username, and the password from a
// previous sign-in — and returns the answers in the form's order (some
// gateways read the elements positionally).
func (a *attempt) acAnswers(form acForm, keys **ssoKeys) ([]pair, error) {
	storedPassword, hasPassword := cachedPassword(a.profile.UUID)

	// An <input type="sso"> is answered by the browser, not by the user,
	// so it is resolved before anything is prompted for — otherwise the
	// prompt would sit there asking for a token nobody can type.
	ssoToken := ""
	if slices.ContainsFunc(form.fields, func(f acField) bool { return f.sso }) {
		token, err := a.acBrowserToken(form, keys)
		if err != nil {
			return nil, err
		}
		ssoToken = token
	}

	known := map[string]string{}
	var ask []secrets.Field
	for _, field := range form.fields {
		switch {
		case field.sso:
			known[field.name] = ssoToken
		case field.name == "username" && a.profile.Username != "":
			known[field.name] = a.profile.Username
		case field.name == "password" && hasPassword:
			known[field.name] = storedPassword
		default:
			ask = append(ask, secrets.Field{Key: field.name, Label: field.label, Secret: field.secret})
		}
	}

	var values map[string]string
	if len(ask) > 0 {
		var err error
		if values, err = a.prompt(form.message, ask...); err != nil {
			return nil, err
		}
	}

	var answers []pair
	seen := map[string]bool{}
	for _, field := range form.fields {
		if seen[field.name] {
			continue
		}
		seen[field.name] = true
		value, ok := known[field.name]
		if !ok {
			value = values[field.name]
		}
		answers = append(answers, pair{field.name, value})
	}
	return answers, nil
}

// acBrowserToken runs the browser sign-in and opens the token it comes
// back with. The key is taken, not borrowed: the shared secret is
// ephemeral and protects exactly one token, so a gateway that asks twice
// gets an error rather than a reused secret.
func (a *attempt) acBrowserToken(form acForm, keys **ssoKeys) (string, error) {
	if form.ssoLogin == "" {
		return "", unsupported("the gateway wants a browser sign-in but did not say where to go")
	}
	if *keys == nil {
		return "", unsupported("this gateway requires a browser sign-in; enable it on the VPN profile")
	}
	taken := *keys
	*keys = nil

	blob, err := awaitSSOToken(a.ctx, form.ssoLogin, ssoTimeout)
	if err != nil {
		return "", err
	}
	raw, ok := ssoBase64Decode(blob)
	if !ok {
		return "", authError("the browser returned a token that is not base64")
	}
	parsed, err := parseBlob(raw)
	if err != nil {
		return "", err
	}
	return decryptToken(taken, parsed)
}

// acPasswordOf is the password out of a set of answers, when the form
// asked for one; "" otherwise.
func acPasswordOf(form acForm, answers []pair) string {
	if !slices.ContainsFunc(form.fields, func(f acField) bool { return f.name == "password" }) {
		return ""
	}
	for _, answer := range answers {
		if answer.key == "password" {
			return answer.value
		}
	}
	return ""
}

// xmlEscape escapes a value going back to the gateway. A password
// containing & or < would otherwise produce a document the gateway
// cannot parse — and the failure would look like a wrong password.
func xmlEscape(value string) string {
	return xmlEscaper.Replace(value)
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")

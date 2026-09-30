package openconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// GlobalProtect gateway authentication, spoken natively over HTTPS
// (gp.rs).
//
// Authenticating to a GlobalProtect gateway is a form POST that answers
// with either a challenge (2FA) or a session cookie. The wire details —
// endpoint, body fields, and above all the positional meaning of the
// <argument> elements in the reply — are openconnect's
// auth-globalprotect.c. They are pinned by the tests, because a silent
// shift in that list would hand the gateway's cookie slot to something
// else and fail in a way no error message would explain.

// gpClientVersion is the client version every GlobalProtect gateway
// expects, and the one it echoes back for us to check.
const gpClientVersion = "4100"

// loginArgs is the positional meaning of the <argument> list in a
// successful login reply. Empty entries are real: the gateway sends
// placeholder slots, and collapsing them would shift every later
// argument onto the wrong name.
var loginArgs = []string{
	"",
	"authcookie",
	"persistent-cookie",
	"portal",
	"user",
	"authentication-source",
	"configuration",
	"domain",
	"",
	"",
	"",
	"",
	"connection-type",
	"password-expiration-days",
	"clientVer",
	"preferred-ip",
	"portal-userauthcookie",
	"portal-prelogonuserauthcookie",
	"preferred-ipv6",
	"usually-equals-4",
	"usually-equals-unknown",
}

// cookieArgs are the arguments that go into the cookie, in the order
// openconnect writes them. computer is appended after these.
var cookieArgs = []string{"authcookie", "portal", "user", "domain", "preferred-ip", "preferred-ipv6"}

const (
	// statusBadCredentials is the custom status a GlobalProtect gateway
	// answers a bad username or password with; openconnect maps it to
	// -EACCES, the one refusal it treats as "ask for the password again".
	statusBadCredentials = 512
	// reasonHeader is the header PAN-OS names its reason for a refusal in.
	reasonHeader = "x-private-pan-globalprotect"

	noGateway = "no GlobalProtect gateway at this address (a portal address needs its gateway's name)"
)

// gpChallenge is a gateway asking for a second factor.
type gpChallenge struct {
	// prompt is the gateway's own wording of what it wants.
	prompt string
	// inputStr is the opaque token identifying this challenge, echoed on
	// the next post.
	inputStr string
}

// gpPrelogin is what a gateway says about itself before anyone signs in:
// the administrator's own wording for what it wants, and — before any
// credentials are posted at it — whether it wants SAML.
type gpPrelogin struct {
	// message is the gateway's instruction to the user, "" when none.
	message string
	// saml is the browser sign-in this gateway wants instead of a
	// username and a password, nil for an ordinary gateway.
	saml          *samlRequest
	usernameLabel string
	passwordLabel string
}

// gpReply is a login reply as it came off the wire: the parts a refusal
// can hide its reason in, besides the body.
type gpReply struct {
	status int
	// reason is the reasonHeader value, "" when the gateway sent none.
	reason string
	body   string
}

// gpLoginBody builds a login request body. The base fields are
// openconnect's verbatim; the empty ones are sent because openconnect
// sends them, and a gateway that has only ever seen openconnect is not
// the place to find out which are optional.
func gpLoginBody(gateway, user, password, computer, inputStr string) string {
	return formEncode(
		pair{"jnlpReady", "jnlpReady"},
		pair{"ok", "Login"},
		pair{"direct", "yes"},
		pair{"clientVer", gpClientVersion},
		pair{"prot", "https:"},
		pair{"internal", "no"},
		pair{"ipv6-support", "yes"},
		pair{"clientos", "Linux"},
		pair{"os-version", "Linux"},
		pair{"server", gateway},
		pair{"computer", computer},
		pair{"portal-userauthcookie", ""},
		pair{"portal-prelogonuserauthcookie", ""},
		pair{"preferred-ip", ""},
		pair{"preferred-ipv6", ""},
		pair{"inputStr", inputStr},
		pair{"user", user},
		pair{"passwd", password},
	)
}

// gpScript is what the JavaScript-shaped reply some gateways write says
// (openconnect's parse_javascript): a challenge, or an error.
type gpScript struct {
	isError  bool
	message  string
	inputStr string
}

// parseScript reads `var respStatus = "…"; var respMsg = "…";
// thisForm.inputStr.value = "…";` out of a reply, wherever in it the
// script sits. An error needs no inputStr: a refusal is worth reading
// even from a gateway that leaves the line off. A challenge without one
// cannot be answered, so it is not one.
func parseScript(body string) (gpScript, bool) {
	const (
		statusMark   = `var respStatus = "`
		messageMark  = `var respMsg = "`
		inputStrMark = `thisForm.inputStr.value = "`
	)
	_, rest, ok := strings.Cut(body, statusMark)
	if !ok {
		return gpScript{}, false
	}
	status, rest, ok := strings.Cut(rest, `"`)
	if !ok {
		return gpScript{}, false
	}
	if _, rest, ok = strings.Cut(rest, messageMark); !ok {
		return gpScript{}, false
	}
	message, rest, ok := scriptString(rest)
	if !ok {
		return gpScript{}, false
	}
	if strings.HasPrefix(status, "Error") {
		return gpScript{isError: true, message: message}, true
	}
	if !strings.HasPrefix(status, "Challenge") {
		return gpScript{}, false
	}
	if _, rest, ok = strings.Cut(rest, inputStrMark); !ok {
		return gpScript{}, false
	}
	inputStr, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return gpScript{}, false
	}
	return gpScript{message: message, inputStr: inputStr}, true
}

// scriptString is the contents of a double-quoted script string,
// unescaped, and what follows its closing quote.
func scriptString(raw string) (value, rest string, ok bool) {
	var out strings.Builder
	escaped := false
	for at, c := range raw {
		switch {
		case escaped:
			escaped = false
			switch c {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			default:
				out.WriteRune(c)
			}
		case c == '\\':
			escaped = true
		case c == '"':
			return out.String(), raw[at+1:], true
		default:
			out.WriteRune(c)
		}
	}
	return "", "", false
}

// gatewayTrouble are words a gateway uses for trouble on its own side of
// a sign-in — a backend that broke, or an authentication server that did
// not answer in time.
var gatewayTrouble = []string{"internal", "timeout", "timed out", "unavailable", "try again"}

// aboutCredentials are words that put the credentials themselves in
// question.
var aboutCredentials = []string{"password", "credential"}

// gpRefusal is why the gateway refused, in the best words the reply has
// — and whether they are a verdict on the password.
//
// The body's own message first, in whichever of its shapes it came (the
// <error> of a <response status="error">, a prelogin-style <msg>, or the
// script's respMsg), because that is the administrator's wording. A bare
// 512 is openconnect's "invalid username or password" even with nothing
// written around it. Past that, PAN's reason header, and last the
// status, so a refusal nobody explained still says something a person
// can search for.
func gpRefusal(r gpReply) error {
	message, hasMessage := "", false
	if script, ok := parseScript(r.body); ok && script.isError {
		message, hasMessage = script.message, true
	} else {
		candidates := xmlValues(r.body, "error")
		if msg, ok := xmlValue(r.body, "msg"); ok {
			candidates = append(candidates, msg)
		}
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate) != "" {
				message, hasMessage = candidate, true
				break
			}
		}
	}
	message = strings.TrimSpace(message)

	var reason string
	switch reasonHeader := strings.TrimSpace(r.reason); {
	case hasMessage:
		reason = message
	case r.status == statusBadCredentials:
		reason = "wrong username or password"
	case reasonHeader != "":
		reason = "the gateway refused the sign-in (" + reasonHeader + ")"
	default:
		reason = fmt.Sprintf("the gateway refused the sign-in without saying why (HTTP %d)", r.status)
	}

	if blamesItself(message, hasMessage, r.status) {
		return incomplete(reason)
	}
	return authError(reason)
}

// blamesItself reports whether a refusal is the gateway's own trouble
// rather than a verdict on the credentials: its message says so and does
// not also name the password, or it failed with a server error and said
// nothing at all.
//
// Anything short of that is taken as a verdict, and costs the stored
// password. The lean is deliberate: a password is only asked for when
// none is stored, so a wrong one kept would fail every connect after it
// with no way to type the right one, where a right one dropped costs
// typing it again.
func blamesItself(message string, hasMessage bool, status int) bool {
	if !hasMessage {
		return status >= 500 && status != statusBadCredentials
	}
	lower := strings.ToLower(message)
	mentions := func(words []string) bool {
		for _, word := range words {
			if strings.Contains(lower, word) {
				return true
			}
		}
		return false
	}
	return mentions(gatewayTrouble) && !mentions(aboutCredentials)
}

// parseGPLogin reads a gateway's answer to a login post: a session, a
// challenge (non-nil), or a refusal.
func parseGPLogin(r gpReply, gateway, computer, gwcert string) (session, *gpChallenge, error) {
	body := r.body
	if script, ok := parseScript(body); ok {
		if script.isError {
			return session{}, nil, gpRefusal(r)
		}
		return session{}, &gpChallenge{prompt: script.message, inputStr: script.inputStr}, nil
	}

	// A 512 is a refusal whatever its body looks like; nothing in it is a
	// cookie or a challenge to be answered.
	if r.status == statusBadCredentials {
		return session{}, nil, gpRefusal(r)
	}

	if inputStr, ok := xmlValue(body, "inputstr"); ok {
		prompt, _ := xmlValue(body, "respmsg")
		return session{}, &gpChallenge{prompt: prompt, inputStr: inputStr}, nil
	}

	arguments := xmlValues(body, "argument")
	if len(arguments) == 0 {
		// SAML portals need a browser; say so plainly rather than failing
		// as "malformed".
		if _, ok := xmlValue(body, "saml-auth-method"); ok || strings.Contains(body, "saml-request") {
			return session{}, nil, authError("this gateway requires SAML sign-in, which wayle cannot do yet")
		}
		return session{}, nil, gpRefusal(r)
	}

	named := nameArguments(arguments)
	if err := checkEcho(named, "connection-type", "tunnel"); err != nil {
		return session{}, nil, err
	}
	if err := checkEcho(named, "clientVer", gpClientVersion); err != nil {
		return session{}, nil, err
	}
	if named["authcookie"] == "" {
		return session{}, nil, authError("the gateway returned no session cookie")
	}
	if named["user"] == "" {
		return session{}, nil, authError("the gateway returned no user")
	}
	return session{cookie: buildGPCookie(named, computer), host: gateway, gwcert: gwcert}, nil, nil
}

// nameArguments pairs the positional arguments with their meanings,
// dropping the placeholder slots and anything past the end of the known
// list.
func nameArguments(arguments []string) map[string]string {
	named := map[string]string{}
	for i, value := range arguments {
		if i >= len(loginArgs) {
			break
		}
		if name := loginArgs[i]; name != "" {
			named[name] = value
		}
	}
	return named
}

// checkEcho checks a field the gateway is expected to echo back
// unchanged. A mismatch means the reply is not the one this code knows
// how to read, and continuing would build a cookie out of the wrong
// slots.
func checkEcho(named map[string]string, key, expected string) error {
	value, ok := named[key]
	switch {
	case !ok:
		return authError("gateway returned no " + key)
	case value != expected:
		return authError(fmt.Sprintf("gateway returned %s=%s, expected %s", key, value, expected))
	}
	return nil
}

// buildGPCookie assembles the --cookie string openconnect's
// GlobalProtect support takes.
func buildGPCookie(named map[string]string, computer string) string {
	var pairs []pair
	for _, key := range cookieArgs {
		if value, ok := named[key]; ok {
			pairs = append(pairs, pair{key, value})
		}
	}
	return formEncode(append(pairs, pair{"computer", computer})...)
}

// parseGPPrelogin reads a gateway's prelogin response.
func parseGPPrelogin(body string) (gpPrelogin, error) {
	// Read before the status, because a SAML gateway answers Success:
	// from its point of view nothing is wrong, it just wants a browser.
	// Whether wayle can act on that is the caller's decision.
	saml := parseSAMLRequest(body)

	if status, ok := xmlValue(body, "status"); ok && !asciiEqualFold(status, "success") {
		message, ok := xmlNonEmpty(body, "msg")
		if !ok {
			message = "the gateway refused the request"
		}
		return gpPrelogin{}, authError(message)
	}

	prelogin := gpPrelogin{saml: saml, usernameLabel: "Username", passwordLabel: "Password"}
	prelogin.message, _ = xmlNonEmpty(body, "authentication-message")
	if label, ok := xmlNonEmpty(body, "username-label"); ok {
		prelogin.usernameLabel = label
	}
	if label, ok := xmlNonEmpty(body, "password-label"); ok {
		prelogin.passwordLabel = label
	}
	return prelogin, nil
}

// gpPreloginRequest asks a gateway what it wants, before anyone types
// anything.
func gpPreloginRequest(ctx context.Context, c *client, gateway string) (gpPrelogin, error) {
	r, err := c.do(ctx, request{
		method:  http.MethodGet,
		url:     "https://" + gateway + "/ssl-vpn/prelogin.esp?tmp=tmp&clientVer=" + gpClientVersion + "&clientos=Linux",
		timeout: preloginTimeout,
	})
	if err != nil {
		return gpPrelogin{}, err
	}
	if r.status == http.StatusNotFound {
		return gpPrelogin{}, authError(noGateway)
	}
	return parseGPPrelogin(r.body)
}

// gpLogin posts a login (or a challenge answer) to a gateway.
func gpLogin(ctx context.Context, c *client, gateway, user, password, computer, inputStr string) (session, *gpChallenge, error) {
	r, err := c.do(ctx, request{
		method: http.MethodPost,
		url:    "https://" + gateway + "/ssl-vpn/login.esp",
		body:   gpLoginBody(gateway, user, password, computer, inputStr),
		// A gateway doing push MFA holds this open until the user has
		// tapped approve on their phone.
		timeout: loginTimeout,
	})
	if err != nil {
		return session{}, nil, err
	}
	// Read off the response rather than a second connection: this is the
	// certificate that was in front of the gateway while it minted the
	// cookie, which is exactly what the plugin is asked to pin.
	gwcert, err := r.requirePin()
	if err != nil {
		return session{}, nil, err
	}
	// A gateway answers a bad password with 200 and an error document, so
	// the status is only interesting as a transport failure — most
	// usefully 404, which is what a portal-only host says to a gateway
	// login.
	if r.status == http.StatusNotFound {
		return session{}, nil, authError(noGateway)
	}
	return parseGPLogin(gpReply{status: r.status, reason: r.header.Get(reasonHeader), body: r.body}, gateway, computer, gwcert)
}

// verdict is what a gateway says when asked to use a cookie again.
type verdict int

const (
	// verdictAccepts: it answered with tunnel configuration.
	verdictAccepts verdict = iota + 1
	// verdictRefuses: it refused the cookie outright.
	verdictRefuses
	// verdictUnreadable: the reply says nothing usable about the cookie.
	// Keeping it and letting the plugin's own attempt be the judge beats
	// re-authenticating on the strength of a reply nobody understands.
	verdictUnreadable
)

// gpCookieVerdict asks the gateway whether it still accepts a session
// cookie, with the request the plugin itself starts every tunnel with —
// openconnect's gpst_get_config posting the cookie string to
// getconfig.esp. It errs only when the gateway cannot be reached at all,
// which the caller treats like verdictUnreadable.
func gpCookieVerdict(ctx context.Context, c *client, gateway, cookie string) (verdict, error) {
	r, err := c.do(ctx, request{
		method: http.MethodPost,
		url:    "https://" + gateway + "/ssl-vpn/getconfig.esp",
		// The fields before the cookie are openconnect's own, verbatim;
		// the cookie string is appended whole, exactly as openconnect
		// sends it.
		body: "client-type=1&protocol-version=p1&app-version=5.1.5-8&clientos=Linux&os-version=linux" +
			"&hmac-algo=sha1,md5,sha256&enc-algo=aes-128-cbc,aes-256-cbc&" + cookie,
		timeout: preloginTimeout,
	})
	if err != nil {
		return 0, err
	}
	return verdictFrom(r.status, r.body), nil
}

func verdictFrom(status int, body string) verdict {
	if cookieRefused(status, body) {
		return verdictRefuses
	}
	if _, ok := rawElement(body, "response"); ok {
		return verdictAccepts
	}
	return verdictUnreadable
}

// cookieRefused keys on what openconnect's gpst_xml_or_error does: the
// custom 512, a non-empty <error>, or <response status="error">.
func cookieRefused(status int, body string) bool {
	if status == statusBadCredentials {
		return true
	}
	for _, text := range xmlValues(body, "error") {
		if text != "" {
			return true
		}
	}
	response, ok := rawElement(body, "response")
	if !ok {
		return false
	}
	value, ok := xmlAttribute(response, "status")
	return ok && asciiEqualFold(value, "error")
}

// globalProtect is the GlobalProtect sign-in: ask the gateway what it
// wants, ask the user for what is missing, then post it.
func (a *attempt) globalProtect() (signIn, error) {
	username, password, typed, err := a.gpCredentials()
	if err != nil {
		return signIn{}, err
	}
	s, err := a.gpSignIn(username, password)
	if err != nil {
		return signIn{}, err
	}
	signed := signIn{session: s}
	if typed {
		signed.rememberPassword = password
	}
	return signed, nil
}

// gpSignIn posts the login and follows however many challenge rounds the
// gateway asks for, up to maxChallenges.
func (a *attempt) gpSignIn(username, password string) (session, error) {
	computer := hostname()
	inputStr := ""
	answer := password
	for range maxChallenges + 1 {
		s, challenge, err := gpLogin(a.ctx, a.client, a.profile.Gateway, username, answer, computer, inputStr)
		if err != nil {
			// Past the first post the password has been accepted — a
			// challenge is only ever issued to one that was — so a refusal
			// now is of the code, not of the password.
			if inputStr != "" {
				err = pastThePassword(err)
			}
			return session{}, err
		}
		if challenge == nil {
			return s, nil
		}
		if answer, err = a.gpChallenge(challenge.prompt); err != nil {
			return session{}, err
		}
		inputStr = challenge.inputStr
	}
	return session{}, authError("the gateway kept asking for more factors")
}

// gpCredentials is the username and password to sign in with, asking
// only for what is missing, and whether the password was typed.
//
// The gateway is asked what it wants before the user is: its prelogin
// response carries the field labels and the instruction an administrator
// wrote, and it is where a SAML gateway is caught — before any
// credentials have been posted at it.
func (a *attempt) gpCredentials() (username, password string, typed bool, err error) {
	storedUser := a.profile.Username
	storedPassword, hasPassword := cachedPassword(a.profile.UUID)

	// Nothing to ask, so nothing to ask the gateway about either.
	if storedUser != "" && hasPassword {
		return storedUser, storedPassword, false, nil
	}

	prelogin, err := gpPreloginRequest(a.ctx, a.client, a.profile.Gateway)
	if err != nil {
		return "", "", false, err
	}

	// A SAML gateway wants a browser, not a form. With the profile's
	// consent wayle hands the sign-in to the real browser and takes the
	// answer back through the globalprotectcallback: scheme; without it,
	// the refusal is the one this has always given.
	if prelogin.saml != nil {
		return a.gpSAML(*prelogin.saml, storedUser)
	}

	var fields []secrets.Field
	if storedUser == "" {
		fields = append(fields, secrets.Field{Key: "user", Label: prelogin.usernameLabel})
	}
	if !hasPassword {
		fields = append(fields, secrets.Field{Key: "passwd", Label: prelogin.passwordLabel, Secret: true})
	}
	values, err := a.prompt(prelogin.message, fields...)
	if err != nil {
		return "", "", false, err
	}

	username = storedUser
	if username == "" {
		username = values["user"]
	}
	if username == "" {
		return "", "", false, authError("no username given")
	}
	if hasPassword {
		return username, storedPassword, false, nil
	}
	return username, values["passwd"], true, nil
}

// gpSAML runs the browser sign-in. openconnect posts the SAML result as
// an ordinary login, with the identity provider's username and the
// pre-login cookie standing in for the password; the challenge loop
// after it is unchanged. The cookie is never cached: a pre-login cookie
// is single-use.
func (a *attempt) gpSAML(request samlRequest, storedUser string) (username, cookie string, typed bool, err error) {
	if !a.profile.SSO {
		return "", "", false, authError(`this gateway requires SAML sign-in; turn on "Browser sign-in (SAML)" ` +
			"for this profile to sign in through your browser")
	}
	callback, err := gpSSOSignIn(a.ctx, request, ssoTimeout)
	if err != nil {
		return "", "", false, err
	}
	if callback.cookie() == "" {
		return "", "", false, authError("the browser sign-in did not return a gateway cookie")
	}
	username = callback.username
	if username == "" {
		username = storedUser
	}
	if username == "" {
		return "", "", false, authError("the browser sign-in did not say who signed in")
	}
	return username, callback.cookie(), false, nil
}

// gpChallenge asks the user for a second factor, in the gateway's own
// words.
func (a *attempt) gpChallenge(prompt string) (string, error) {
	if prompt == "" {
		prompt = "Additional authentication required"
	}
	values, err := a.prompt(prompt, secrets.Field{Key: "passwd", Label: "Code", Secret: true})
	if err != nil {
		return "", err
	}
	return values["passwd"], nil
}

// pastThePassword recasts a refusal that came after the password was
// accepted as a sign-in that did not finish; anything else passes
// through.
func pastThePassword(err error) error {
	if refused, ok := errors.AsType[*secrets.AuthenticationFailedError](err); ok {
		return incomplete(refused.Reason)
	}
	return err
}

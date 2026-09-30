package openconnect

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// acMainForm is a gateway's opening form, as an ASA writes it.
const acMainForm = `<?xml version="1.0" encoding="UTF-8"?>` +
	`<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">` +
	`<opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup</tunnel-group>` +
	`<config-hash>1699999999999</config-hash></opaque>` +
	`<auth id="main"><title>Login</title>` +
	`<message>Please enter your username and password.</message>` +
	`<form><input type="text" name="username" label="Username:"/>` +
	`<input type="password" name="password" label="Password:"/>` +
	`<input type="hidden" name="tgroup"/>` +
	`<select name="group_list" label="GROUP:">` +
	`<option value="Employees">Employees</option>` +
	`<option value="Contractors" selected="true">Contractors</option>` +
	`</select></form></auth></config-auth>`

const acSuccessDoc = `<config-auth client="vpn" type="complete">` +
	`<auth id="success"><title>SSL VPN Service</title></auth>` +
	`<session-token>TOKEN</session-token></config-auth>`

func acFormOf(t *testing.T, body string) acForm {
	t.Helper()
	form, done, err := parseAC(body)
	if err != nil || done {
		t.Fatalf("expected a form: done=%v err=%v", done, err)
	}
	return form
}

func TestAGatewaysFormBecomesTheQuestionsToAsk(t *testing.T) {
	form := acFormOf(t, acMainForm)
	if form.message != "Please enter your username and password." {
		t.Errorf("message = %q", form.message)
	}
	// Hidden fields are not questions, and the label loses the colon the
	// gateway wrote for its own web form.
	want := []acField{
		{name: "username", label: "Username"},
		{name: "password", label: "Password", secret: true},
	}
	if !slices.Equal(form.fields, want) {
		t.Errorf("fields = %+v", form.fields)
	}
	// The gateway recognises its own conversation by this blob.
	if form.opaque != `<opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup</tunnel-group>`+
		`<config-hash>1699999999999</config-hash></opaque>` {
		t.Errorf("opaque = %q", form.opaque)
	}
	// The option the gateway marked selected, not merely the first.
	if form.group != "Contractors" {
		t.Errorf("group = %q", form.group)
	}
}

func TestTheFirstGroupIsChosenWhenNoneIsSelected(t *testing.T) {
	form := `<form><select name="g"><option value="A">A</option><option value="B">B</option></select></form>`
	if got := acGroup(form); got != "A" {
		t.Errorf("group = %q", got)
	}
	if got := acGroup(`<form><select name="g"><option>Text</option></select></form>`); got != "Text" {
		t.Errorf("group = %q", got)
	}
	if got := acGroup(`<form></form>`); got != "" {
		t.Errorf("group = %q", got)
	}
}

func TestASuccessIsRecognisedAndAFormIsNot(t *testing.T) {
	if _, done, err := parseAC(acSuccessDoc); err != nil || !done {
		t.Errorf("done = %v, %v", done, err)
	}
	if _, done, err := parseAC(acMainForm); err != nil || done {
		t.Errorf("done = %v, %v", done, err)
	}
}

func TestAnAnyConnectRejectionSurfacesTheGatewaysOwnMessage(t *testing.T) {
	refused := `<config-auth type="auth-request"><auth id="main">` +
		`<error id="88" param1="">Login failed.</error>` +
		`<form><input type="text" name="username"/></form></auth></config-auth>`
	_, _, err := parseAC(refused)
	// A refusal is the user's problem to fix, not another agent's.
	if !secrets.DiscreditsPassword(err) || !strings.Contains(err.Error(), "Login failed.") {
		t.Errorf("err = %v", err)
	}
}

func TestAReplyThatIsNotAnyConnectIsLeftToSomeoneElse(t *testing.T) {
	// These must reach NM as NoSecrets, so the plugin's own auth dialog
	// still gets its turn.
	for _, body := range []string{
		"<html><body>Not a gateway</body></html>",
		"",
		`<config-auth type="auth-request"><auth id="main"><message>hi</message></auth></config-auth>`,
		`<config-auth><auth id="main"><form></form></auth></config-auth>`,
	} {
		_, _, err := parseAC(body)
		if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
			t.Errorf("%q produced %v", body, err)
		}
	}
}

func TestAChallengeFormAsksOnlyForTheSecondFactor(t *testing.T) {
	form := acFormOf(t, `<config-auth type="auth-request"><auth id="challenge">`+
		`<message>Answer with the code from your token.</message>`+
		`<form><input type="password" name="secondary_password" label="Code:"/></form></auth></config-auth>`)
	if len(form.fields) != 1 || !form.fields[0].secret || form.fields[0].name != "secondary_password" ||
		form.message != "Answer with the code from your token." {
		t.Errorf("form = %+v", form)
	}
}

func TestTheReplyCarriesEveryAnswerTheOpaqueBlobAndTheGroup(t *testing.T) {
	reply := acReplyRequest(acFormOf(t, acMainForm), []pair{{"username", "alice"}, {"password", "hunter2"}})
	for _, want := range []string{
		`type="auth-reply"`,
		"<auth><username>alice</username><password>hunter2</password></auth>",
		// The gateway drops a reply that does not echo its opaque blob.
		`<opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup`,
		"<group-select>Contractors</group-select>",
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply lacks %q:\n%s", want, reply)
		}
	}
}

func TestAPasswordWithMarkupInItCannotBreakTheDocument(t *testing.T) {
	form := acForm{fields: []acField{{name: "password", label: "Password", secret: true}}}
	reply := acReplyRequest(form, []pair{{"password", `a<b&c"d`}})
	if !strings.Contains(reply, "<password>a&lt;b&amp;c&quot;d</password>") {
		t.Errorf("reply = %q", reply)
	}
	// And it reads back as what was typed, not as the escaping.
	if got, _ := xmlValue(reply, "password"); got != `a<b&c"d` {
		t.Errorf("read back = %q", got)
	}
}

// cookieReply is a reply whose one response set these cookies.
func cookieReply(lines ...string) reply {
	header := http.Header{}
	for _, line := range lines {
		header.Add("Set-Cookie", line)
	}
	return reply{header: header, hops: []http.Header{header}}
}

func TestTheSessionCookieIsTheOneOpenconnectTakes(t *testing.T) {
	if got, ok := cookieReply("webvpn=SESSIONVALUE; path=/; secure; HttpOnly").namedCookie(acCookieName); !ok || got != "webvpn=SESSIONVALUE" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
}

func TestAClearedOrAbsentCookieIsNotASession(t *testing.T) {
	// A gateway clears webvpn on the first exchange, before there is a
	// session; taking that as the cookie would hand the plugin an empty
	// one and fail at connect time instead of at sign-in time.
	cleared := cookieReply("webvpn=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/", "webvpnc=bu:/CACHE/; path=/")
	if got, ok := cleared.namedCookie(acCookieName); ok {
		t.Errorf("cookie = %q", got)
	}
	if got, ok := cookieReply().namedCookie(acCookieName); ok {
		t.Errorf("cookie = %q", got)
	}
}

func TestTheOpeningRequestNamesTheGatewayItIsAsking(t *testing.T) {
	request := acInitRequest("vpn.example.com", false)
	if !strings.Contains(request, `type="init"`) || !strings.Contains(request, "<group-access>https://vpn.example.com</group-access>") {
		t.Errorf("request = %q", request)
	}
}

func TestTheBrowserCapabilityIsNotAdvertisedUnlessTheProfileAsks(t *testing.T) {
	// A gateway seeing this capability may insist on a browser where it
	// would otherwise have served a form.
	if quiet := acInitRequest("vpn.example.com", false); strings.Contains(quiet, "single-sign-on") || strings.Contains(quiet, "<capabilities>") {
		t.Errorf("the capability leaked into an ordinary sign-in: %q", quiet)
	}
	if asked := acInitRequest("vpn.example.com", true); !strings.Contains(asked, "<auth-method>single-sign-on-external-browser</auth-method>") {
		t.Errorf("request = %q", asked)
	}
}

func TestOnlyTheFirstPasswordIsRemembered(t *testing.T) {
	answers := []pair{{"username", "alice"}, {"password", "hunter2"}}
	if got := acPasswordOf(acFormOf(t, acMainForm), answers); got != "hunter2" {
		t.Errorf("password = %q", got)
	}
	// A second factor's answer is different every time.
	challenge := acForm{fields: []acField{{name: "secondary_password", label: "Code", secret: true}}}
	if got := acPasswordOf(challenge, []pair{{"secondary_password", "123456"}}); got != "" {
		t.Errorf("password = %q", got)
	}
}

func TestAnSSOInputIsAFieldTheUserCannotAnswer(t *testing.T) {
	form := acFormOf(t, `<config-auth><opaque is-for="sg"><x>1</x></opaque>`+
		`<auth id="main"><form><input type="sso" name="sso-token" label="Single sign-on"/></form></auth>`+
		`<sso-v2-login>https://idp.example.com/saml</sso-v2-login></config-auth>`)
	if len(form.fields) != 1 || !form.fields[0].sso {
		t.Errorf("fields = %+v", form.fields)
	}
	if form.ssoLogin != "https://idp.example.com/saml" {
		t.Errorf("sso login = %q", form.ssoLogin)
	}
}

func TestAnOrdinaryFormCarriesNoSSOLoginAndNoSSOField(t *testing.T) {
	form := acFormOf(t, `<config-auth><auth id="main"><form><input type="text" name="username" label="Username:"/></form></auth></config-auth>`)
	if form.fields[0].sso || form.ssoLogin != "" {
		t.Errorf("form = %+v", form)
	}
}

func ssoForm(login string) acForm {
	return acForm{fields: []acField{{name: "sso-token", label: "Single sign-on", sso: true}}, ssoLogin: login}
}

func TestAGatewayAskingForABrowserWhenTheProfileDidNotIsUnsupported(t *testing.T) {
	// Reaches NM as "someone else should try": with no key there is
	// nothing to decrypt with.
	var keys *ssoKeys
	_, err := testAttempt(Profile{}, &fakePrompter{}).acBrowserToken(ssoForm("https://idp.example.com/saml"), &keys)
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
		t.Errorf("err = %#v", err)
	}
}

func TestABrowserSignInWithNowhereToGoIsRefusedBeforeABrowserOpens(t *testing.T) {
	keys := mustKeys(t)
	_, err := testAttempt(Profile{}, &fakePrompter{}).acBrowserToken(ssoForm(""), &keys)
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
		t.Errorf("err = %#v", err)
	}
	// And the key is still there: nothing was consumed by a flow that
	// never started.
	if keys == nil {
		t.Error("the key was consumed")
	}
}

// Against the fake AnyConnect gateway: the opening request, the form,
// the challenge, the echoed <opaque> blob it refuses a reply without,
// the session cookie and the certificate pin.

func TestAFullAnyConnectSignInAnswersTheFormTheChallengeAndComesBackWithACookie(t *testing.T) {
	g := startGateway(t, modeAnyConnect)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{
		"username": fakeUser, "password": fakePassword, "secondary_password": fakeChallengeAnswer,
	}}
	signed, err := testAttempt(profileFor("mock-anyconnect", "anyconnect", g.addr), prompter).anyconnect()
	if err != nil {
		t.Fatal(err)
	}
	// The cookie openconnect takes, in the shape it takes it.
	if signed.session != (session{cookie: "webvpn=SESSIONVALUE", host: g.addr, gwcert: fakePin}) {
		t.Errorf("session = %+v", signed.session)
	}
	// The password is worth caching; the second factor never is.
	if signed.rememberPassword != fakePassword {
		t.Errorf("remember = %q", signed.rememberPassword)
	}
	// The gateway's own wording and labels reached the prompts.
	shown := prompter.requests()
	if len(shown) != 2 || shown[0].Message != "Please enter your username and password." ||
		shown[0].Fields[0].Label != "Username" || shown[1].Fields[0].Key != "secondary_password" {
		t.Errorf("prompts = %+v", shown)
	}
	// No key was offered on an ordinary sign-in.
	for _, r := range g.requests() {
		if r.header.Get("X-AnyConnect-STRAP-DH-Pubkey") != "" {
			t.Error("an SSO key went out on an ordinary sign-in")
		}
	}
}

func TestAKnownUsernameAndCachedPasswordAreNotAskedFor(t *testing.T) {
	g := startGateway(t, modeAnyConnect)
	stateHome(t)
	RememberPassword("mock-anyconnect", fakePassword)
	p := profileFor("mock-anyconnect", "anyconnect", g.addr)
	p.Username = fakeUser
	prompter := &fakePrompter{answers: map[string]string{"secondary_password": fakeChallengeAnswer}}
	signed, err := testAttempt(p, prompter).anyconnect()
	if err != nil {
		t.Fatal(err)
	}
	if shown := prompter.requests(); len(shown) != 1 || len(shown[0].Fields) != 1 || shown[0].Fields[0].Key != "secondary_password" {
		t.Errorf("prompts = %+v", shown)
	}
	// Only a typed password is remembered.
	if signed.rememberPassword != fakePassword {
		t.Errorf("remember = %q", signed.rememberPassword)
	}
}

func TestAWrongAnyConnectPasswordIsTheGatewaysOwnRefusal(t *testing.T) {
	g := startGateway(t, modeAnyConnect)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": "wrong"}}
	_, err := testAttempt(profileFor("mock-anyconnect", "anyconnect", g.addr), prompter).anyconnect()
	// A refusal must not read as "wayle does not speak this protocol", or
	// NM would quietly hand a wrong password to the next agent.
	if !secrets.DiscreditsPassword(err) || !strings.Contains(err.Error(), "Login failed.") {
		t.Errorf("err = %v", err)
	}
}

func TestADismissedAnyConnectPromptIsASignInThatDidNotFinish(t *testing.T) {
	g := startGateway(t, modeAnyConnect)
	stateHome(t)
	_, err := testAttempt(profileFor("mock-anyconnect", "anyconnect", g.addr), &fakePrompter{dismiss: true}).anyconnect()
	if secrets.DiscreditsPassword(err) || err == nil || err.Error() != "sign-in dismissed" {
		t.Errorf("err = %v", err)
	}
}

func TestAGlobalProtectGatewayIsNotMistakenForAnAnyConnectOne(t *testing.T) {
	// The GlobalProtect fake answers this exchange with something that is
	// not a config-auth, which has to reach NM as "someone else should
	// try" rather than as a failed sign-in.
	g := startGateway(t, modeForm)
	stateHome(t)
	_, err := testAttempt(profileFor("mock-anyconnect", "anyconnect", g.addr), &fakePrompter{}).anyconnect()
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
		t.Errorf("err = %v", err)
	}
}

func TestAnSSOProfileAdvertisesItsKeyOnEveryRequest(t *testing.T) {
	g := startGateway(t, modeAnyConnect)
	stateHome(t)
	p := profileFor("mock-anyconnect", "anyconnect", g.addr)
	p.SSO = true
	prompter := &fakePrompter{answers: map[string]string{
		"username": fakeUser, "password": fakePassword, "secondary_password": fakeChallengeAnswer,
	}}
	if _, err := testAttempt(p, prompter).anyconnect(); err != nil {
		t.Fatal(err)
	}
	reqs := g.requests()
	if !strings.Contains(reqs[0].body, "single-sign-on-external-browser") {
		t.Error("the capability was not advertised")
	}
	key := reqs[0].header.Get("X-AnyConnect-STRAP-DH-Pubkey")
	if key == "" {
		t.Fatal("no key on the opening request")
	}
	for _, r := range reqs {
		if r.header.Get("X-AnyConnect-STRAP-DH-Pubkey") != key || r.header.Get("X-AnyConnect-STRAP-Pubkey") != key {
			t.Errorf("request without the conversation's key: %+v", r.header)
		}
	}
}

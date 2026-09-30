package openconnect

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/network/secrets"
)

func TestEachWebProtocolKnowsItsEndpointAndCookie(t *testing.T) {
	// Juniper and Pulse share the dana-na front end; F5 is its own.
	if webDialectFor("nc") != juniperDialect || webDialectFor("pulse") != juniperDialect || webDialectFor("f5") != f5Dialect {
		t.Error("a web protocol has the wrong dialect")
	}
	if juniperDialect.cookie != "DSID" || f5Dialect.cookie != "MRHSession" {
		t.Error("a dialect has the wrong cookie")
	}
	if !strings.HasPrefix(juniperDialect.loginPath, "/") || !strings.HasPrefix(f5Dialect.loginPath, "/") {
		t.Error("a login path is not root-relative")
	}
}

func TestTheProtocolsTheWebLoginDoesNotSpeakAreLeftAlone(t *testing.T) {
	// These have their own native sign-ins; routing them here would
	// replace a working one with a form scrape.
	for _, protocol := range []string{"gp", "anyconnect", "fortinet", "array", ""} {
		if webDialectFor(protocol) != nil {
			t.Errorf("%q was claimed", protocol)
		}
	}
}

func TestTheCookieThePluginGetsIsNamedForTheProtocol(t *testing.T) {
	if got := formEncode(pair{juniperDialect.cookie, "abc"}); got != "DSID=abc" {
		t.Errorf("cookie = %q", got)
	}
	// Values are escaped: a cookie with a separator in it would read as two.
	if got := formEncode(pair{f5Dialect.cookie, "a&b"}); got != "MRHSession=a%26b" {
		t.Errorf("cookie = %q", got)
	}
}

// juniperLogin is a Juniper login page in the shape auth-juniper.c
// describes: a realm, hidden deployment tokens, and the credentials.
const juniperLogin = `
<html><body>
<form name="frmLogin" method="post" action="/dana-na/auth/url_default/login.cgi">
  <input type="hidden" name="tz_offset" value="60">
  <input type="text" name="username" value="">
  <input type="password" name="password" value="">
  <input type="hidden" name="realm" value="Users">
  <input type="submit" name="btnSubmit" value="Sign In">
</form></body></html>`

// juniperConfirm is the "you already have a session" page: a form, but
// not one asking who you are.
const juniperConfirm = `
<html><body>
<form name="frmConfirmation" method="post" action="login.cgi">
  <input type="hidden" name="btnContinue" value="Continue the session">
  <input type="hidden" name="FormDataStr" value="deployment-token">
</form></body></html>`

func fieldMap(fields []pair) map[string]string {
	m := map[string]string{}
	for _, p := range fields {
		m[p.key] = p.value
	}
	return m
}

func TestALoginPageYieldsItsFieldsAndAction(t *testing.T) {
	form, ok := parseHTMLForm(juniperLogin)
	if !ok || form.action != "/dana-na/auth/url_default/login.cgi" {
		t.Fatalf("form = %+v, %v", form, ok)
	}
	if !form.wants(usernameFields) || !form.wants(passwordFields) {
		t.Error("the credentials were not recognised")
	}
	// Hidden deployment state has to survive: the gateway expects its own
	// tokens back.
	if fields := fieldMap(form.fields); fields["tz_offset"] != "60" || fields["realm"] != "Users" {
		t.Errorf("fields = %v", fields)
	}
}

func TestASubmitButtonIsNotPostedWithEverythingElse(t *testing.T) {
	form, _ := parseHTMLForm(juniperLogin)
	if _, ok := fieldMap(form.fields)["btnSubmit"]; ok {
		t.Error("the submit button was posted")
	}
}

func TestCredentialsReplaceOnlyTheFieldsThatAskForThem(t *testing.T) {
	form, _ := parseHTMLForm(juniperLogin)
	filled := fieldMap(form.filled("alice", "hunter2"))
	if filled["username"] != "alice" || filled["password"] != "hunter2" || filled["realm"] != "Users" || filled["tz_offset"] != "60" {
		t.Errorf("filled = %v", filled)
	}
	// Recognised case-insensitively, among the other spellings.
	form, _ = parseHTMLForm(`<form><input name="UserID"><input type="password" name="PASSWD"></form>`)
	if filled := fieldMap(form.filled("bob", "pw")); filled["UserID"] != "bob" || filled["PASSWD"] != "pw" {
		t.Errorf("filled = %v", filled)
	}
}

func TestAPageWithNoCredentialsIsAnInterstitialNotALogin(t *testing.T) {
	if _, credentials, done := classifyPage(juniperConfirm); credentials || done {
		t.Error("the confirmation page read as a login")
	}
	if _, credentials, done := classifyPage(juniperLogin); !credentials || done {
		t.Error("the login page was not a login")
	}
}

func TestAPageWithNoFormMeansTheGatewayStoppedAsking(t *testing.T) {
	for _, html := range []string{"<html><body>Welcome</body></html>", "", "<formation>no</formation>"} {
		if _, _, done := classifyPage(html); !done {
			t.Errorf("%q has a form", html)
		}
	}
}

func TestAnUncheckedBoxIsNotSubmittedAndACheckedOneIs(t *testing.T) {
	form, _ := parseHTMLForm(`<form action="/a">
		<input type="checkbox" name="remember" value="1">
		<input type="checkbox" name="agree" value="yes" checked>
		<input type="radio" name="mode" value="fast" checked="checked">
	</form>`)
	fields := fieldMap(form.fields)
	if _, ok := fields["remember"]; ok {
		t.Error("an unchecked box was submitted")
	}
	if fields["agree"] != "yes" || fields["mode"] != "fast" {
		t.Errorf("fields = %v", fields)
	}
}

func TestAnInputWithNoNameHasNothingToPost(t *testing.T) {
	if form, _ := parseHTMLForm(`<form action="/a"><input type="text" value="x"></form>`); len(form.fields) != 0 {
		t.Errorf("fields = %+v", form.fields)
	}
	// And <inputmode> is not an input.
	if got := inputTags(`<inputmode name="x"><INPUT name="y">`); len(got) != 1 || got[0] != `<INPUT name="y">` {
		t.Errorf("inputs = %q", got)
	}
}

func TestRelativeActionsResolveAgainstThePageTheyCameFrom(t *testing.T) {
	const base = "https://vpn.example.com/dana-na/auth/url_default/welcome.cgi"
	for action, want := range map[string]string{
		"login.cgi":                   "https://vpn.example.com/dana-na/auth/url_default/login.cgi",
		"/dana-na/auth/login.cgi":     "https://vpn.example.com/dana-na/auth/login.cgi",
		"https://other.example.com/x": "https://other.example.com/x",
	} {
		if got := resolveAction(base, action); got != want {
			t.Errorf("resolve(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestAMissingActionPostsBackToTheSamePage(t *testing.T) {
	const base = "https://vpn.example.com/my.policy"
	for _, action := range []string{"", "   "} {
		if got := resolveAction(base, action); got != base {
			t.Errorf("resolve(%q) = %q", action, got)
		}
	}
}

func TestAnActionOnABareHostStillResolves(t *testing.T) {
	// No path to be relative to; the scheme's own slashes must not be
	// mistaken for a path separator.
	for _, action := range []string{"login.cgi", "/login.cgi"} {
		if got := resolveAction("https://vpn.example.com", action); got != "https://vpn.example.com/login.cgi" {
			t.Errorf("resolve(%q) = %q", action, got)
		}
	}
}

func TestTheWebSessionCookieIsFoundByName(t *testing.T) {
	r := cookieReply("lastRealm=Users; path=/; secure", "DSID=abc123def; path=/; secure; HttpOnly")
	if got, ok := webSessionCookie(r, "DSID"); !ok || got != "abc123def" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
	// Cookie names are matched case-insensitively.
	if got, ok := webSessionCookie(r, "dsid"); !ok || got != "abc123def" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
}

func TestAClearedOrAbsentWebCookieIsNotASession(t *testing.T) {
	// Clearing a cookie is how a gateway signs you out.
	for _, lines := range [][]string{
		{"DSID=; path=/; expires=Thu, 01 Jan 1970"},
		{`DSID=""; path=/`},
		nil,
		{"MRHSession=x"},
		// A different cookie whose value contains the name must not match.
		{"other=DSID=x"},
	} {
		if got, ok := webSessionCookie(cookieReply(lines...), "DSID"); ok {
			t.Errorf("%q: cookie = %q", lines, got)
		}
	}
}

func TestACookieSetOnARedirectIsSeenAndTheLatestWins(t *testing.T) {
	// A gateway sets its session on the response that redirects away from
	// the login pages; the page redirected to does not repeat it.
	redirect := http.Header{"Set-Cookie": {"DSID=FROMREDIRECT; path=/"}}
	landing := http.Header{"Set-Cookie": {"other=1"}}
	r := reply{header: landing, hops: []http.Header{redirect, landing}}
	if got, ok := webSessionCookie(r, "DSID"); !ok || got != "FROMREDIRECT" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
	later := http.Header{"Set-Cookie": {"DSID=LATER"}}
	r = reply{header: later, hops: []http.Header{redirect, later}}
	if got, _ := webSessionCookie(r, "DSID"); got != "LATER" {
		t.Errorf("cookie = %q", got)
	}
}

func TestThePostBodyCarriesEveryField(t *testing.T) {
	if got := formEncode(pair{"username", "alice"}, pair{"realm", "Users"}); got != "username=alice&realm=Users" {
		t.Errorf("body = %q", got)
	}
}

func TestThereIsABoundOnHowLongAGatewayMayKeepAsking(t *testing.T) {
	if maxPages < 3 || maxPages >= 20 {
		t.Errorf("maxPages = %d", maxPages)
	}
}

// Against the fake web-login gateways (the Rust had none for these).

func TestAJuniperSignInWorksThroughTheLoginTheInterstitialAndTheRedirect(t *testing.T) {
	for _, protocol := range []string{"nc", "pulse"} {
		g := startGateway(t, modeJuniper)
		stateHome(t)
		prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": fakePassword}}
		signed, err := testAttempt(profileFor("mock-"+protocol, protocol, g.addr), prompter).webLogin()
		if err != nil {
			t.Fatalf("%s: %v", protocol, err)
		}
		if signed.session != (session{cookie: "DSID=JUNIPERSESSION", host: g.addr, gwcert: fakePin}) {
			t.Errorf("%s: session = %+v", protocol, signed.session)
		}
		if signed.rememberPassword != fakePassword {
			t.Errorf("%s: remember = %q", protocol, signed.rememberPassword)
		}
		// The interstitial was answered without asking anyone.
		if shown := prompter.requests(); len(shown) != 1 {
			t.Errorf("%s: prompts = %+v", protocol, shown)
		}
	}
}

func TestAnF5SignInPostsThePolicyFormAndIgnoresTheClearedCookie(t *testing.T) {
	g := startGateway(t, modeF5)
	stateHome(t)
	RememberPassword("mock-f5", fakePassword)
	p := profileFor("mock-f5", "f5", g.addr)
	p.Username = fakeUser
	prompter := &fakePrompter{}
	signed, err := testAttempt(p, prompter).webLogin()
	if err != nil {
		t.Fatal(err)
	}
	if signed.session != (session{cookie: "MRHSession=F5SESSION", host: g.addr, gwcert: fakePin}) {
		t.Errorf("session = %+v", signed.session)
	}
	if len(prompter.requests()) != 0 || signed.rememberPassword != "" {
		t.Errorf("prompts = %+v, remember = %q", prompter.requests(), signed.rememberPassword)
	}
}

func TestAWebLoginAskedForTheCredentialsTwiceIsARefusal(t *testing.T) {
	g := startGateway(t, modeJuniper)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": "wrong"}}
	_, err := testAttempt(profileFor("mock-nc", "nc", g.addr), prompter).webLogin()
	if !secrets.DiscreditsPassword(err) || err.Error() != "the gateway rejected the credentials" {
		t.Errorf("err = %v", err)
	}
}

func TestAWebLoginThatEndsWithoutASessionIsAFailureNotASuccess(t *testing.T) {
	// The GlobalProtect fake answers the login page with a 404 page and no
	// form: nothing to fill, and no cookie.
	g := startGateway(t, modeForm)
	stateHome(t)
	_, err := testAttempt(profileFor("mock-f5", "f5", g.addr), &fakePrompter{}).webLogin()
	if !secrets.DiscreditsPassword(err) || !strings.Contains(err.Error(), "without a session cookie") {
		t.Errorf("err = %v", err)
	}
}

package openconnect

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// testPin stands in for the pin gpLogin reads off the TLS connection.
const testPin = "pin-sha256:AAAA"

const gpConfig = `<?xml version="1.0" encoding="UTF-8" ?><response status="success">` +
	`<ip-address>192.168.241.222</ip-address><netmask>255.255.255.255</netmask><mtu>0</mtu>` +
	`<lifetime>86400</lifetime><access-routes><member>0.0.0.0/0</member></access-routes></response>`

const gpDeadCookie = `<?xml version="1.0" encoding="UTF-8" ?><response status="error">` +
	`<error>Invalid authentication cookie</error></response>`

func TestAConfigReplyMeansTheCookieStillOpensATunnel(t *testing.T) {
	if got := verdictFrom(200, gpConfig); got != verdictAccepts {
		t.Errorf("verdict = %v", got)
	}
}

func TestAnErrorDocumentIsARefusedCookie(t *testing.T) {
	if got := verdictFrom(200, gpDeadCookie); got != verdictRefuses {
		t.Errorf("verdict = %v", got)
	}
	// The status attribute alone is enough.
	if got := verdictFrom(200, `<response status="ERROR"></response>`); got != verdictRefuses {
		t.Errorf("verdict = %v", got)
	}
}

func TestTheGatewaysCustomDeadCookieStatusRefusesWithoutXML(t *testing.T) {
	if got := verdictFrom(512, "nonsense"); got != verdictRefuses {
		t.Errorf("verdict = %v", got)
	}
}

func TestAReplyThatSaysNothingAboutTheCookieIsKeptAnyway(t *testing.T) {
	// An HTML error page must not cost a cookie that might be fine.
	if got := verdictFrom(200, "<html>something else</html>"); got != verdictUnreadable {
		t.Errorf("verdict = %v", got)
	}
}

// gpSuccessXML is a real gateway's reply, trimmed to the argument list.
func gpSuccessXML(connectionType, clientVersion string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><jnlp><application-desc>` +
		`<argument/><argument>AUTHCOOKIEVALUE</argument><argument>0123456789abcdef</argument>` +
		`<argument>vpn.example.com</argument><argument>alice</argument><argument>LDAP-auth</argument>` +
		`<argument>vsys1</argument><argument>example</argument>` +
		`<argument/><argument/><argument/><argument/>` +
		`<argument>` + connectionType + `</argument><argument>-1</argument>` +
		`<argument>` + clientVersion + `</argument><argument/><argument>PORTALCOOKIE</argument>` +
		`<argument/><argument/><argument>4</argument><argument>unknown</argument>` +
		`</application-desc></jnlp>`
}

func okReply(body string) gpReply { return gpReply{status: 200, body: body} }

func TestASuccessfulLoginBecomesOpenconnectsCookieString(t *testing.T) {
	s, challenge, err := parseGPLogin(okReply(gpSuccessXML("tunnel", "4100")), "vpn.example.com", "laptop", testPin)
	if err != nil || challenge != nil {
		t.Fatalf("challenge = %+v, err = %v", challenge, err)
	}
	if s.host != "vpn.example.com" || s.gwcert != testPin {
		t.Errorf("session = %+v", s)
	}
	want := "authcookie=AUTHCOOKIEVALUE&portal=vpn.example.com&user=alice&domain=example" +
		"&preferred-ip=&preferred-ipv6=&computer=laptop"
	if s.cookie != want {
		t.Errorf("cookie = %q\nwant     %q", s.cookie, want)
	}
}

func TestTheArgumentListIsPositionalAndStaysThatWay(t *testing.T) {
	// Argument 1 is the cookie and argument 4 the user. A shift would
	// silently mint a broken cookie.
	for index, name := range map[int]string{1: "authcookie", 4: "user", 7: "domain", 12: "connection-type", 14: "clientVer"} {
		if loginArgs[index] != name {
			t.Errorf("loginArgs[%d] = %q, want %q", index, loginArgs[index], name)
		}
	}
	if len(loginArgs) != 21 {
		t.Errorf("len(loginArgs) = %d", len(loginArgs))
	}
	named := nameArguments(xmlValues(gpSuccessXML("tunnel", "4100"), "argument"))
	if named["authcookie"] != "AUTHCOOKIEVALUE" || named["user"] != "alice" {
		t.Errorf("named = %v", named)
	}
	// The placeholder slots carry no name and so contribute nothing.
	if _, ok := named[""]; ok {
		t.Error("a placeholder slot was named")
	}
}

func TestAReplyThatIsNotATunnelIsRefusedRatherThanUsed(t *testing.T) {
	_, _, err := parseGPLogin(okReply(gpSuccessXML("not-a-tunnel", "4100")), "vpn.example.com", "laptop", testPin)
	if err == nil || !strings.Contains(err.Error(), "connection-type") {
		t.Errorf("err = %v", err)
	}
}

func TestAnUnexpectedClientVersionIsRefused(t *testing.T) {
	if _, _, err := parseGPLogin(okReply(gpSuccessXML("tunnel", "5000")), "vpn.example.com", "laptop", testPin); err == nil {
		t.Error("a different protocol's argument list was read")
	}
}

func TestAChallengeIsRecognisedWithItsOwnWording(t *testing.T) {
	doc := "<challenge><respmsg>Enter your token code</respmsg><inputstr>CHALLENGE-1</inputstr></challenge>"
	_, challenge, err := parseGPLogin(okReply(doc), "vpn.example.com", "laptop", testPin)
	if err != nil || challenge == nil || *challenge != (gpChallenge{prompt: "Enter your token code", inputStr: "CHALLENGE-1"}) {
		t.Errorf("challenge = %+v, %v", challenge, err)
	}
}

func TestAChallengeIsNotMistakenForASuccess(t *testing.T) {
	_, challenge, err := parseGPLogin(okReply("<challenge><inputstr>C</inputstr></challenge>"), "vpn.example.com", "laptop", testPin)
	if err != nil || challenge == nil {
		t.Errorf("challenge = %+v, %v", challenge, err)
	}
}

// refusedReason is the reason a refused login is reported with; the
// refusal must discredit the password.
func refusedReason(t *testing.T, r gpReply) string {
	t.Helper()
	_, _, err := parseGPLogin(r, "vpn.example.com", "laptop", testPin)
	if _, ok := errors.AsType[*secrets.AuthenticationFailedError](err); !ok {
		t.Fatalf("a refusal must discredit the password: %#v", err)
	}
	return err.Error()
}

// troubleReason is the reason a login the gateway itself fell over on is
// reported with; it must not discredit the password.
func troubleReason(t *testing.T, r gpReply) string {
	t.Helper()
	_, _, err := parseGPLogin(r, "vpn.example.com", "laptop", testPin)
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok {
		t.Fatalf("the gateway's own trouble must not discredit the password: %#v", err)
	}
	return err.Error()
}

func TestARejectionSurfacesTheGatewaysOwnMessage(t *testing.T) {
	got := refusedReason(t, okReply(`<response status="error"><msg>Invalid username or password</msg></response>`))
	if !strings.Contains(got, "Invalid username or password") {
		t.Errorf("reason = %q", got)
	}
}

func TestAnErrorDocumentSurfacesItsErrorElement(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" ?><response status="error">` +
		`<error>Authentication failed: Invalid username or password</error></response>`
	if got := refusedReason(t, okReply(doc)); got != "Authentication failed: Invalid username or password" {
		t.Errorf("reason = %q", got)
	}
}

const scriptError = "var respStatus = \"Error\";\n" +
	"var respMsg = \"Authentication failed: Invalid username or password \";\n" +
	"thisForm.inputStr.value = \"\";\n"

func TestAScriptShapedRefusalSurfacesItsMessage(t *testing.T) {
	if got := refusedReason(t, okReply(scriptError)); got != "Authentication failed: Invalid username or password" {
		t.Errorf("reason = %q", got)
	}
}

func TestAScriptRefusalWrappedInHTMLIsStillRead(t *testing.T) {
	html := "<html><head></head><body>" + scriptError + "</body></html>"
	if got := refusedReason(t, okReply(html)); got != "Authentication failed: Invalid username or password" {
		t.Errorf("reason = %q", got)
	}
}

func TestAScriptShapedChallengeIsAnsweredLikeAnXMLOne(t *testing.T) {
	script := "var respStatus = \"Challenge\";\n" +
		"var respMsg = \"Enter the \\\"code\\\" from your phone\\n\\tnow\";\n" +
		"thisForm.inputStr.value = \"CHALLENGE-1\";\n"
	_, challenge, err := parseGPLogin(okReply(script), "vpn.example.com", "laptop", testPin)
	if err != nil || challenge == nil || *challenge != (gpChallenge{prompt: "Enter the \"code\" from your phone\n\tnow", inputStr: "CHALLENGE-1"}) {
		t.Errorf("challenge = %+v, %v", challenge, err)
	}
}

func TestAScriptChallengeWithNoTokenIsNotAChallenge(t *testing.T) {
	// Nothing to echo back means nothing to answer: prompting for a code
	// the gateway could never match would waste the user's second factor.
	script := "var respStatus = \"Challenge\";\nvar respMsg = \"Enter code\";\n"
	if _, challenge, _ := parseGPLogin(okReply(script), "vpn.example.com", "laptop", testPin); challenge != nil {
		t.Errorf("challenge = %+v", challenge)
	}
	// Nor is an unterminated message a script at all.
	if _, ok := parseScript(`var respStatus = "Error"; var respMsg = "no end`); ok {
		t.Error("an unterminated string was read")
	}
}

func TestABare512IsAWrongPassword(t *testing.T) {
	if got := refusedReason(t, gpReply{status: statusBadCredentials}); got != "wrong username or password" {
		t.Errorf("reason = %q", got)
	}
}

func TestA512IsARefusalWhateverItsBodyLooksLike(t *testing.T) {
	if got := refusedReason(t, gpReply{status: statusBadCredentials, body: gpSuccessXML("tunnel", "4100")}); got != "wrong username or password" {
		t.Errorf("reason = %q", got)
	}
}

func TestTheBodysWordingWinsOverTheStatusAndTheHeader(t *testing.T) {
	got := refusedReason(t, gpReply{status: statusBadCredentials, reason: "auth-failed", body: scriptError})
	if got != "Authentication failed: Invalid username or password" {
		t.Errorf("reason = %q", got)
	}
}

func TestPANsReasonHeaderExplainsARefusalWithAnEmptyBody(t *testing.T) {
	got := refusedReason(t, gpReply{status: 200, reason: "auth-failed", body: `<response status="error"></response>`})
	if got != "the gateway refused the sign-in (auth-failed)" {
		t.Errorf("reason = %q", got)
	}
}

func TestAnUnexplainedRefusalNamesItsStatusInsteadOfSayingNothing(t *testing.T) {
	got := refusedReason(t, gpReply{status: 403, body: "<html>denied</html>"})
	if !strings.Contains(got, "HTTP 403") || strings.Contains(got, "rejected the login") {
		t.Errorf("reason = %q", got)
	}
}

func TestBlankMessagesAreSkippedForOnesThatSaySomething(t *testing.T) {
	got := refusedReason(t, okReply(`<response status="error"><error>  </error><msg>Account locked</msg></response>`))
	if got != "Account locked" {
		t.Errorf("reason = %q", got)
	}
}

const scriptInternalError = "var respStatus = \"Error\";\n" +
	"var respMsg = \"Authentication failed: Internal error\";\n" +
	"thisForm.inputStr.value = \"\";\n"

func TestAnInternalErrorIsShownWithoutCostingThePassword(t *testing.T) {
	// What the row showed just before the next connect asked for the
	// password again: the gateway's own failure, held against a password
	// it had not got as far as judging.
	if got := troubleReason(t, okReply(scriptInternalError)); got != "Authentication failed: Internal error" {
		t.Errorf("reason = %q", got)
	}
	if got := troubleReason(t, okReply(`<response status="error"><error>Internal error</error></response>`)); got != "Internal error" {
		t.Errorf("reason = %q", got)
	}
	// On PAN's refusal status too: the wording decides.
	if got := troubleReason(t, gpReply{status: statusBadCredentials, reason: "auth-failed", body: scriptInternalError}); got != "Authentication failed: Internal error" {
		t.Errorf("reason = %q", got)
	}
}

func TestAnAuthenticationServerThatDidNotAnswerKeepsThePassword(t *testing.T) {
	script := "var respStatus = \"Error\";\nvar respMsg = \"Authentication server timed out\";\nthisForm.inputStr.value = \"\";\n"
	if got := troubleReason(t, okReply(script)); got != "Authentication server timed out" {
		t.Errorf("reason = %q", got)
	}
}

func TestAServerErrorThatSaysNothingKeepsThePassword(t *testing.T) {
	for _, status := range []int{500, 502, 503} {
		if got := troubleReason(t, gpReply{status: status, body: "<html>Service Unavailable</html>"}); !strings.Contains(got, "HTTP "+strconv.Itoa(status)) {
			t.Errorf("reason = %q", got)
		}
	}
}

func TestARefusalThatIsNotPlainlyTheGatewaysTroubleCostsThePassword(t *testing.T) {
	// The lean the other way. A wrong password kept fails every connect
	// after it, with no prompt to type the right one.
	if got := refusedReason(t, okReply(`<response status="error"><error>Authentication failed</error></response>`)); got != "Authentication failed" {
		t.Errorf("reason = %q", got)
	}
	// Naming the password outweighs naming the gateway's trouble.
	if got := refusedReason(t, okReply(`<response status="error"><error>Internal error: password expired</error></response>`)); got != "Internal error: password expired" {
		t.Errorf("reason = %q", got)
	}
	// A silent 512 is PAN's wrong-password status, not a server error.
	if got := refusedReason(t, gpReply{status: statusBadCredentials}); got != "wrong username or password" {
		t.Errorf("reason = %q", got)
	}
}

// preloginForm is the real response from a form-authenticating gateway.
const preloginForm = `<?xml version="1.0" encoding="UTF-8" ?>` +
	`<prelogin-response><status>Success</status><ccusername></ccusername>` +
	`<autosubmit>false</autosubmit><msg></msg><newmsg></newmsg><license>yes</license>` +
	`<authentication-message>Enter login credentials</authentication-message>` +
	`<username-label>Username</username-label><password-label>Password</password-label>` +
	`<panos-version>2</panos-version><saml-default-browser>yes</saml-default-browser>` +
	`<auth-api>no</auth-api><region>DK</region></prelogin-response>`

func TestAFormGatewayHandsBackItsOwnLabelsAndMessage(t *testing.T) {
	prelogin, err := parseGPPrelogin(preloginForm)
	if err != nil || prelogin.message != "Enter login credentials" ||
		prelogin.usernameLabel != "Username" || prelogin.passwordLabel != "Password" {
		t.Errorf("prelogin = %+v, %v", prelogin, err)
	}
}

func TestAdvertisingASAMLBrowserIsNotTheSameAsRequiringSAML(t *testing.T) {
	// A form gateway still reports saml-default-browser; treating that as
	// "requires SAML" would send a working gateway to the browser.
	prelogin, err := parseGPPrelogin(preloginForm)
	if err != nil || prelogin.saml != nil {
		t.Errorf("prelogin = %+v, %v", prelogin, err)
	}
}

func TestASAMLGatewayIsRecognisedBeforeAnyCredentialsArePostedAtIt(t *testing.T) {
	prelogin, err := parseGPPrelogin("<prelogin-response><status>Success</status>" +
		"<saml-auth-method>REDIRECT</saml-auth-method><saml-request>aHR0cHM6Ly9pZHA=</saml-request></prelogin-response>")
	if err != nil || prelogin.saml == nil || prelogin.saml.payload != "https://idp" || prelogin.saml.method != samlRedirect {
		t.Errorf("prelogin = %+v, %v", prelogin, err)
	}
}

func TestAPreloginErrorSurfacesTheGatewaysMessage(t *testing.T) {
	_, err := parseGPPrelogin("<prelogin-response><status>Error</status><msg>Portal not licensed</msg></prelogin-response>")
	if err == nil || !strings.Contains(err.Error(), "Portal not licensed") {
		t.Errorf("err = %v", err)
	}
	_, err = parseGPPrelogin("<prelogin-response><status>Error</status><msg></msg></prelogin-response>")
	if err == nil || err.Error() != "the gateway refused the request" {
		t.Errorf("err = %v", err)
	}
}

func TestAGatewayThatSendsNoLabelsStillProducesAUsablePrompt(t *testing.T) {
	prelogin, err := parseGPPrelogin("<prelogin-response><status>Success</status></prelogin-response>")
	if err != nil || prelogin.usernameLabel != "Username" || prelogin.passwordLabel != "Password" || prelogin.message != "" {
		t.Errorf("prelogin = %+v, %v", prelogin, err)
	}
}

func TestASAMLPortalSaysSoInsteadOfReadingAsMalformed(t *testing.T) {
	doc := "<prelogin-response><saml-auth-method>REDIRECT</saml-auth-method>" +
		"<saml-request>aHR0cHM6Ly9pZHA=</saml-request></prelogin-response>"
	if _, _, err := parseGPLogin(okReply(doc), "vpn.example.com", "laptop", testPin); err == nil || !strings.Contains(err.Error(), "SAML") {
		t.Errorf("err = %v", err)
	}
}

func TestALoginWithoutACookieIsAFailureNotAnEmptySuccess(t *testing.T) {
	doc := strings.Replace(gpSuccessXML("tunnel", "4100"), "AUTHCOOKIEVALUE", "", 1)
	if _, _, err := parseGPLogin(okReply(doc), "vpn.example.com", "laptop", testPin); err == nil {
		t.Error("an empty cookie read as a sign-in")
	}
}

func TestTheFirstPostCarriesAnEmptyChallengeField(t *testing.T) {
	body := gpLoginBody("vpn.example.com", "alice", "pw", "laptop", "")
	for _, want := range []string{"&inputStr=&", "clientVer=4100", "&user=alice&passwd=pw"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q lacks %q", body, want)
		}
	}
}

func TestAChallengeAnswerCarriesTheTokenItAnswers(t *testing.T) {
	body := gpLoginBody("vpn.example.com", "alice", "123456", "laptop", "CHALLENGE-1")
	if !strings.Contains(body, "inputStr=CHALLENGE-1") || !strings.Contains(body, "passwd=123456") {
		t.Errorf("body = %q", body)
	}
}

func TestCredentialsNeedingEscapingSurviveTheRoundTrip(t *testing.T) {
	body := gpLoginBody("vpn.example.com", `EXAMPLE\alice`, "p@ss&word", "laptop", "")
	if !strings.Contains(body, "user=EXAMPLE%5Calice") || !strings.Contains(body, "passwd=p%40ss%26word") {
		t.Errorf("body = %q", body)
	}
}

// Against the fake GlobalProtect gateway: the only tests that speak the
// protocol over a real TLS connection, which is the point — the gwcert
// secret is read off that connection.

// gpFakeSignIn signs in the way globalProtect does: a password post,
// then the answer to the challenge it comes back with.
func gpFakeSignIn(t *testing.T, gateway string) session {
	t.Helper()
	c := newClient()
	_, challenge, err := gpLogin(context.Background(), c, gateway, fakeUser, fakePassword, "laptop", "")
	if err != nil || challenge == nil {
		t.Fatalf("the fake always challenges once: %+v, %v", challenge, err)
	}
	s, challenge, err := gpLogin(context.Background(), c, gateway, fakeUser, fakeChallengeAnswer, "laptop", challenge.inputStr)
	if err != nil || challenge != nil {
		t.Fatalf("the second post completes the sign-in: %+v, %v", challenge, err)
	}
	return s
}

func TestPreloginCarriesTheGatewaysOwnWording(t *testing.T) {
	g := startGateway(t, modeForm)
	prelogin, err := gpPreloginRequest(context.Background(), newClient(), g.addr)
	if err != nil || prelogin.usernameLabel != "Company ID" || prelogin.passwordLabel != "Passphrase" ||
		prelogin.message != "Sign in to the mock gateway" {
		t.Errorf("prelogin = %+v, %v", prelogin, err)
	}
	if reqs := g.requests(); len(reqs) != 1 || reqs[0].header.Get("User-Agent") != userAgent ||
		reqs[0].path != "/ssl-vpn/prelogin.esp?tmp=tmp&clientVer=4100&clientos=Linux" {
		t.Errorf("requests = %+v", reqs)
	}
}

func TestASignInProducesACookieAndTheCertificatePin(t *testing.T) {
	g := startGateway(t, modeForm)
	s := gpFakeSignIn(t, g.addr)
	if !strings.HasPrefix(s.cookie, "authcookie=AUTHCOOKIEVALUE&") || !strings.Contains(s.cookie, "&user=alice&") ||
		!strings.HasSuffix(s.cookie, "&computer=laptop") {
		t.Errorf("cookie = %q", s.cookie)
	}
	// Without this the plugin never launches openconnect, however good the
	// cookie is — and it is the pin of the certificate this connection
	// actually presented.
	if s.host != g.addr || s.gwcert != fakePin {
		t.Errorf("session = %+v", s)
	}
	if reqs := g.requests(); reqs[0].header.Get("Content-Type") != formContentType {
		t.Errorf("content type = %q", reqs[0].header.Get("Content-Type"))
	}
}

func TestTheGatewayAcceptsTheCookieItMinted(t *testing.T) {
	g := startGateway(t, modeForm)
	s := gpFakeSignIn(t, g.addr)
	if v, err := gpCookieVerdict(context.Background(), newClient(), s.host, s.cookie); err != nil || v != verdictAccepts {
		t.Errorf("verdict = %v, %v", v, err)
	}
}

func TestTheGatewayRefusesACookieItDidNotMint(t *testing.T) {
	g := startGateway(t, modeForm)
	v, err := gpCookieVerdict(context.Background(), newClient(), g.addr, "authcookie=EXPIRED&portal=127.0.0.1&user=alice&domain=example")
	if err != nil || v != verdictRefuses {
		t.Errorf("verdict = %v, %v", v, err)
	}
}

func TestAnUnreachableGatewayGivesNoVerdict(t *testing.T) {
	if _, err := gpCookieVerdict(context.Background(), newClient(), "127.0.0.1:1", "authcookie=x"); err == nil {
		t.Error("a gateway nobody reached gave a verdict")
	}
}

func TestAWrongPasswordIsARefusalInTheGatewaysWords(t *testing.T) {
	g := startGateway(t, modeForm)
	_, _, err := gpLogin(context.Background(), newClient(), g.addr, fakeUser, "wrong", "laptop", "")
	if !secrets.DiscreditsPassword(err) || !strings.Contains(err.Error(), "Invalid username or password") {
		t.Errorf("err = %v", err)
	}
}

func TestASAMLPortalIsReportedRatherThanSignedInto(t *testing.T) {
	// prelogin only reports what the gateway wants; whether to refuse it
	// is the caller's decision, because a profile may opt into the
	// browser sign-in.
	g := startGateway(t, modeSAML)
	prelogin, err := gpPreloginRequest(context.Background(), newClient(), g.addr)
	if err != nil || prelogin.saml == nil || prelogin.saml.method != samlRedirect || prelogin.saml.payload != "https://idp.example.com/" {
		t.Errorf("prelogin = %+v, %v", prelogin, err)
	}
}

func TestAPortalOnlyHostIsNoGateway(t *testing.T) {
	// The SAML fake 404s a login post, which is what a portal-only host
	// says to a gateway login.
	g := startGateway(t, modeSAML)
	_, _, err := gpLogin(context.Background(), newClient(), g.addr, fakeUser, fakePassword, "laptop", "")
	if err == nil || !strings.Contains(err.Error(), "no GlobalProtect gateway") {
		t.Errorf("err = %v", err)
	}
}

func TestAnUntrustedCertificateIsNotSignedInto(t *testing.T) {
	// The fake's CA trusted for the server, then forgotten for the client:
	// a gateway nobody verified must not mint a cookie.
	g := startGateway(t, modeForm)
	clientRoots = nil
	_, _, err := gpLogin(context.Background(), newClient(), g.addr, fakeUser, fakePassword, "laptop", "")
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok || !strings.Contains(err.Error(), "cannot reach the gateway") {
		t.Errorf("err = %v", err)
	}
}

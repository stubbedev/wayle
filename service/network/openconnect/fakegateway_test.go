package openconnect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// The fake gateways: the Go port of crates/wayle-network/tests/
// mock-gateway/gateway.py, one httptest TLS server per MODE, serving the
// committed mock certificate (testdata/gateway.crt, signed by
// testdata/ca.crt) so the gwcert pin is a constant. The client trusts
// the CA through clientRoots and verifies the fake for real. Nothing
// here talks to a real VPN.
//
// The response shapes come from openconnect's auth-globalprotect.c,
// auth.c, fortinet.c, array.c, auth-juniper.c and f5.c, which is also
// where the sign-in got them; the fakes cannot falsify those
// assumptions, only pin that the sign-in speaks them.

// fakePin is the pin of testdata/gateway.crt:
// openssl x509 -pubkey | openssl pkey -pubin -outform der | sha256 | base64.
const fakePin = "pin-sha256:eQO9gC6TVZtfFqt1YHSe7HUSxgHyRmhNo3UXeSAxvZI="

const (
	fakeUser            = "alice"
	fakePassword        = "hunter2"
	fakeChallengeToken  = "CHALLENGE-1"
	fakeChallengeAnswer = "123456"
	fakeCookie          = "AUTHCOOKIEVALUE"
	fakeTroubledUser    = "troubled"
	// The identity the SAML fake's browser sign-in reports, and the
	// pre-login cookie it hands back.
	fakeSAMLUser   = "alice@example.com"
	fakeSAMLCookie = "PRELOGIN-COOKIE"

	fortiReqID  = "17"
	fortiMagic  = "deadbeef"
	fortiCookie = "SVPNSESSIONVALUE"

	arrayCookie = "ARRAYSESSION"
)

const fakePreloginForm = `<?xml version="1.0" encoding="UTF-8" ?>
<prelogin-response><status>Success</status><ccusername></ccusername>
<autosubmit>false</autosubmit><msg></msg><newmsg></newmsg><license>yes</license>
<authentication-message>Sign in to the mock gateway</authentication-message>
<username-label>Company ID</username-label><password-label>Passphrase</password-label>
<panos-version>2</panos-version><saml-default-browser>yes</saml-default-browser>
<auth-api>no</auth-api><region>DK</region></prelogin-response>`

// fakePreloginSAML redirects to https://idp.example.com/.
const fakePreloginSAML = `<?xml version="1.0" encoding="UTF-8" ?>
<prelogin-response><status>Success</status>
<saml-auth-method>REDIRECT</saml-auth-method>
<saml-request>aHR0cHM6Ly9pZHAuZXhhbXBsZS5jb20v</saml-request>
</prelogin-response>`

const fakeChallenge = `<?xml version="1.0" encoding="UTF-8" ?>
<challenge><respmsg>Approve the push on your phone</respmsg>
<inputstr>` + fakeChallengeToken + `</inputstr></challenge>`

// How PAN-OS refuses a login: the custom 512, its reason header, and the
// three lines of script openconnect's parse_javascript reads.
const fakeRejected = `var respStatus = "Error";
var respMsg = "Authentication failed: Invalid username or password ";
thisForm.inputStr.value = "";
`

const fakeInternalError = `var respStatus = "Error";
var respMsg = "Authentication failed: Internal error";
thisForm.inputStr.value = "";
`

const fakeConfigSuccess = `<?xml version="1.0" encoding="UTF-8" ?>
<response status="success"><ip-address>192.168.241.222</ip-address>
<netmask>255.255.255.255</netmask><mtu>0</mtu><lifetime>86400</lifetime>
<access-routes><member>0.0.0.0/0</member></access-routes></response>`

const fakeCookieRejected = `<?xml version="1.0" encoding="UTF-8" ?>
<response status="error"><error>Invalid authentication cookie</error></response>`

const fakeOpaque = `<opaque is-for="sg"><tunnel-group>DefaultWEBVPNGroup</tunnel-group>` +
	`<config-hash>1699999999999</config-hash></opaque>`

const acMain = `<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">
` + fakeOpaque + `
<auth id="main"><title>Login</title>
<message>Please enter your username and password.</message>
<form><input type="text" name="username" label="Username:"/>
<input type="password" name="password" label="Password:"/>
<select name="group_list" label="GROUP:">
<option value="Employees" selected="true">Employees</option>
</select></form></auth></config-auth>`

const acChallenge = `<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">
` + fakeOpaque + `
<auth id="challenge"><message>Answer with the code from your token.</message>
<form><input type="password" name="secondary_password" label="Code:"/></form>
</auth></config-auth>`

const acSuccess = `<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="complete" aggregate-auth-version="2">
<auth id="success"><title>SSL VPN Service</title></auth>
<session-token>SESSIONTOKEN</session-token></config-auth>`

const acRejected = `<?xml version="1.0" encoding="UTF-8"?>
<config-auth client="vpn" type="auth-request" aggregate-auth-version="2">
<auth id="main"><error id="88" param1="">Login failed.</error>
<form><input type="text" name="username" label="Username:"/>
<input type="password" name="password" label="Password:"/></form>
</auth></config-auth>`

// fakeGPSuccess is the positional argument list a real gateway answers a
// good login with.
func fakeGPSuccess() string {
	arguments := []string{
		"", fakeCookie, "0123456789abcdef", "127.0.0.1", fakeUser, "LDAP-auth", "vsys1", "example",
		"", "", "", "", "tunnel", "-1", "4100", "", "PORTALCOOKIE", "", "", "4", "unknown",
	}
	var body strings.Builder
	for _, value := range arguments {
		if value == "" {
			body.WriteString("<argument/>")
		} else {
			body.WriteString("<argument>" + value + "</argument>")
		}
	}
	return `<?xml version="1.0" encoding="UTF-8"?><jnlp><application-desc>` + body.String() + `</application-desc></jnlp>`
}

// fakeMode picks which gateway a fake is.
type fakeMode int

const (
	// modeForm is GlobalProtect: username/password, one challenge round,
	// cookie; the user "troubled" gets PAN's refusal around an internal
	// error.
	modeForm fakeMode = iota + 1
	// modeSAML is a GlobalProtect portal answering prelogin with a SAML
	// redirect and nothing else (a posted login 404s).
	modeSAML
	// modeAnyConnect is Cisco: an XML form, a challenge, then webvpn.
	modeAnyConnect
	// modeFortinet is FortiGate: /remote/logincheck, a tokeninfo second
	// factor, then SVPNCOOKIE.
	modeFortinet
	// modeArray is Array Networks: one form POST, then ANsession….
	modeArray
	// modeJuniper is a dana-na login: a login page, a "you already have a
	// session" interstitial, then a redirect that sets DSID.
	modeJuniper
	// modeF5 is F5 BIG-IP APM: /my.policy with a login form, then
	// MRHSession on a page with no form.
	modeF5
)

// fakeGateway is one running fake, and the requests it has seen.
type fakeGateway struct {
	addr string
	mode fakeMode

	mu   sync.Mutex
	seen []seenRequest
}

// seenRequest is what the fake recorded of one request.
type seenRequest struct {
	method string
	path   string
	header http.Header
	body   string
}

func (g *fakeGateway) requests() []seenRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]seenRequest(nil), g.seen...)
}

// trustFakeCA makes the sign-in's client trust testdata/ca.crt for the
// rest of the test.
func trustFakeCA(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("testdata/ca.crt")
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		t.Fatal("testdata/ca.crt holds no certificate")
	}
	previous := clientRoots
	clientRoots = pool
	t.Cleanup(func() { clientRoots = previous })
}

// startGateway runs a fake in mode on a loopback port for the rest of
// the test, trusting its CA.
func startGateway(t *testing.T, mode fakeMode) *fakeGateway {
	t.Helper()
	trustFakeCA(t)
	cert, err := tls.LoadX509KeyPair("testdata/gateway.crt", "testdata/gateway.key")
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeGateway{mode: mode}
	server := httptest.NewUnstartedServer(http.HandlerFunc(g.serve))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	g.addr = server.Listener.Addr().String()
	return g
}

func (g *fakeGateway) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	g.mu.Lock()
	g.seen = append(g.seen, seenRequest{method: r.Method, path: r.URL.RequestURI(), header: r.Header.Clone(), body: body})
	g.mu.Unlock()

	switch g.mode {
	case modeForm, modeSAML:
		g.globalProtect(w, r, body)
	case modeAnyConnect:
		fakeAnyConnect(w, r, body)
	case modeFortinet:
		fakeFortinet(w, r, body)
	case modeArray:
		fakeArray(w, r, body)
	case modeJuniper:
		fakeJuniper(w, r, body)
	case modeF5:
		fakeF5(w, r, body)
	}
}

func fakeReply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func fakeRefuse(w http.ResponseWriter, body string) {
	w.Header().Set("x-private-pan-globalprotect", "auth-failed")
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(statusBadCredentials)
	_, _ = io.WriteString(w, body)
}

func formOf(body string) url.Values {
	values, _ := url.ParseQuery(body)
	return values
}

func (g *fakeGateway) globalProtect(w http.ResponseWriter, r *http.Request, body string) {
	if r.Method == http.MethodGet {
		if !strings.HasPrefix(r.URL.Path, "/ssl-vpn/prelogin.esp") {
			fakeReply(w, http.StatusNotFound, "<html>not a gateway</html>")
			return
		}
		if g.mode == modeSAML {
			fakeReply(w, http.StatusOK, fakePreloginSAML)
		} else {
			fakeReply(w, http.StatusOK, fakePreloginForm)
		}
		return
	}
	form := formOf(body)
	switch {
	case strings.HasPrefix(r.URL.Path, "/ssl-vpn/getconfig.esp") && g.mode == modeForm:
		// The cookie probe: the minted cookie buys tunnel configuration;
		// anything else is the refusal a real gateway writes around a
		// dead one.
		if form.Get("authcookie") == fakeCookie {
			fakeReply(w, http.StatusOK, fakeConfigSuccess)
		} else {
			fakeReply(w, http.StatusOK, fakeCookieRejected)
		}
	case g.mode == modeSAML && strings.HasPrefix(r.URL.Path, "/ssl-vpn/login.esp") &&
		form.Get("user") == fakeSAMLUser && form.Get("passwd") == fakeSAMLCookie:
		// The browser sign-in's result, posted as an ordinary login.
		fakeReply(w, http.StatusOK, fakeGPSuccess())
	case !strings.HasPrefix(r.URL.Path, "/ssl-vpn/login.esp") || g.mode == modeSAML:
		fakeReply(w, http.StatusNotFound, "<html>not a gateway</html>")
	case form.Get("user") == fakeTroubledUser:
		fakeRefuse(w, fakeInternalError)
	case form.Get("user") != fakeUser:
		fakeRefuse(w, fakeRejected)
	case form.Get("inputStr") == "":
		// The first post carries the password and no challenge token; the
		// answer to the challenge comes back in the same passwd field.
		if form.Get("passwd") == fakePassword {
			fakeReply(w, http.StatusOK, fakeChallenge)
		} else {
			fakeRefuse(w, fakeRejected)
		}
	case form.Get("inputStr") == fakeChallengeToken && form.Get("passwd") == fakeChallengeAnswer:
		fakeReply(w, http.StatusOK, fakeGPSuccess())
	default:
		fakeRefuse(w, fakeRejected)
	}
}

func fakeAnyConnect(w http.ResponseWriter, _ *http.Request, body string) {
	switch {
	case !strings.Contains(body, "<config-auth"):
		fakeReply(w, http.StatusBadRequest, "<html>not a gateway</html>")
	case strings.Contains(body, `type="init"`):
		// The cookie a gateway clears before there is a session: wayle
		// must not mistake it for one.
		w.Header().Add("Set-Cookie", "webvpn=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/")
		fakeReply(w, http.StatusOK, acMain)
	case !strings.Contains(body, "<tunnel-group>DefaultWEBVPNGroup</tunnel-group>"):
		// Every reply has to echo the opaque blob back.
		fakeReply(w, http.StatusOK, acRejected)
	case strings.Contains(body, "<secondary_password>"+fakeChallengeAnswer+"</secondary_password>"):
		w.Header().Add("Set-Cookie", "webvpn=SESSIONVALUE; path=/; secure; HttpOnly")
		fakeReply(w, http.StatusOK, acSuccess)
	case strings.Contains(body, "<username>"+fakeUser+"</username>") &&
		strings.Contains(body, "<password>"+fakePassword+"</password>"):
		fakeReply(w, http.StatusOK, acChallenge)
	default:
		fakeReply(w, http.StatusOK, acRejected)
	}
}

func fakeFortinet(w http.ResponseWriter, r *http.Request, body string) {
	if !strings.HasPrefix(r.URL.Path, "/remote/logincheck") {
		fakeReply(w, http.StatusNotFound, "<html>not a gateway</html>")
		return
	}
	form := formOf(body)
	switch {
	case form.Get("username") != fakeUser:
		fakeReply(w, http.StatusOK, "ret=0,err=Permission denied")
	case form.Has("code"):
		// A challenge round echoes the values from the previous reply; the
		// real thing recognises the conversation by them.
		switch {
		case form.Get("reqid") != fortiReqID || form.Get("magic") != fortiMagic:
			fakeReply(w, http.StatusOK, "ret=0,err=Session not recognised")
		case form.Get("code") == fakeChallengeAnswer:
			w.Header().Add("Set-Cookie", "SVPNCOOKIE="+fortiCookie+"; path=/; secure; HttpOnly")
			fakeReply(w, http.StatusOK, "")
		default:
			fakeReply(w, http.StatusOK, "ret=0,err=Wrong code")
		}
	case form.Get("credential") == fakePassword:
		fakeReply(w, http.StatusOK, "ret=2,tokeninfo=,grp=Employees,reqid="+fortiReqID+",polid=3,"+
			"portal=web,peer=1,magic="+fortiMagic+",chal_msg=Enter your token code")
	default:
		fakeReply(w, http.StatusOK, "ret=0,err=Permission denied")
	}
}

func fakeArray(w http.ResponseWriter, r *http.Request, body string) {
	if !strings.HasPrefix(r.URL.Path, "/prx/000/http/localhost/login") {
		fakeReply(w, http.StatusNotFound, "<html>not a gateway</html>")
		return
	}
	// Array's own field names: answering to username/password would let a
	// client using the wrong names pass.
	form := formOf(body)
	if form.Get("uname") == fakeUser && form.Get("pwd") == fakePassword {
		w.Header().Add("Set-Cookie", "ANsession1234="+arrayCookie+"; path=/; secure; HttpOnly")
		fakeReply(w, http.StatusOK, "")
		return
	}
	fakeReply(w, http.StatusOK, "<html>Login failed</html>")
}

const juniperLoginPage = `<html><body>
<form name="frmLogin" method="post" action="login.cgi">
  <input type="hidden" name="tz_offset" value="60">
  <input type="text" name="username" value="">
  <input type="password" name="password" value="">
  <input type="hidden" name="realm" value="Users">
  <input type="submit" name="btnSubmit" value="Sign In">
</form></body></html>`

const juniperConfirmPage = `<html><body>
<form name="frmConfirmation" method="post" action="/dana-na/auth/url_default/confirm.cgi">
  <input type="hidden" name="btnContinue" value="Continue the session">
  <input type="hidden" name="FormDataStr" value="deployment-token">
</form></body></html>`

func fakeJuniper(w http.ResponseWriter, r *http.Request, body string) {
	form := formOf(body)
	switch r.URL.Path {
	case "/dana-na/auth/url_default/welcome.cgi":
		fakeReply(w, http.StatusOK, juniperLoginPage)
	case "/dana-na/auth/url_default/login.cgi":
		// The relative action resolved against welcome.cgi's directory,
		// with the hidden deployment state echoed and the button left out.
		if form.Get("username") != fakeUser || form.Get("password") != fakePassword ||
			form.Get("realm") != "Users" || form.Get("tz_offset") != "60" || form.Has("btnSubmit") {
			fakeReply(w, http.StatusOK, juniperLoginPage)
			return
		}
		fakeReply(w, http.StatusOK, juniperConfirmPage)
	case "/dana-na/auth/url_default/confirm.cgi":
		if form.Get("FormDataStr") != "deployment-token" {
			fakeReply(w, http.StatusOK, juniperConfirmPage)
			return
		}
		// The session is set on the redirect away from the login pages.
		w.Header().Add("Set-Cookie", "DSID=JUNIPERSESSION; path=/; secure; HttpOnly")
		http.Redirect(w, r, "/dana/home/index.cgi", http.StatusFound)
	case "/dana/home/index.cgi":
		fakeReply(w, http.StatusOK, "<html><body>Welcome</body></html>")
	default:
		fakeReply(w, http.StatusNotFound, "<html>not a gateway</html>")
	}
}

const f5LoginPage = `<html><body><form id="auth_form" method="post" action="/my.policy">
<input type="text" name="username" value="">
<input type="password" name="password" value="">
<input type="hidden" name="vhost" value="standard">
</form></body></html>`

func fakeF5(w http.ResponseWriter, r *http.Request, body string) {
	if r.URL.Path != "/my.policy" {
		fakeReply(w, http.StatusNotFound, "<html>not a gateway</html>")
		return
	}
	form := formOf(body)
	if r.Method == http.MethodPost && form.Get("username") == fakeUser && form.Get("password") == fakePassword &&
		form.Get("vhost") == "standard" {
		w.Header().Add("Set-Cookie", "MRHSession=F5SESSION; path=/; secure")
		fakeReply(w, http.StatusOK, "<html><body>Access granted</body></html>")
		return
	}
	// The cleared cookie a gateway sends before there is a session.
	w.Header().Add("Set-Cookie", `MRHSession=""; path=/`)
	fakeReply(w, http.StatusOK, f5LoginPage)
}

// fakePrompter answers prompts the way the user would, from answers by
// field key ("" for a key it has no answer for), and records every
// request it was shown. With dismiss set it dismisses every prompt.
type fakePrompter struct {
	answers map[string]string
	dismiss bool

	mu    sync.Mutex
	shown []secrets.Request
}

func (f *fakePrompter) Prompt(_ context.Context, req secrets.Request) (map[string]string, bool) {
	f.mu.Lock()
	f.shown = append(f.shown, req)
	f.mu.Unlock()
	if f.dismiss {
		return nil, false
	}
	values := map[string]string{}
	for _, field := range req.Fields {
		values[field.Key] = f.answers[field.Key]
	}
	return values, true
}

func (f *fakePrompter) requests() []secrets.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]secrets.Request(nil), f.shown...)
}

// stateHome points the credential cache at a fresh directory for the
// rest of the test.
func stateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

// testAttempt is an attempt against gateway for profile p.
func testAttempt(p Profile, prompter secrets.Prompter) *attempt {
	return &attempt{ctx: context.Background(), profile: p, client: newClient(), prompter: prompter}
}

func profileFor(uuid, protocol, gateway string) Profile {
	return Profile{UUID: uuid, Name: "Work", Gateway: gateway, Protocol: protocol}
}

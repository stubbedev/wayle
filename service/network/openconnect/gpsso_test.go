package openconnect

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func encode64(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

func samlPrelogin(method, request string) string {
	return "<prelogin-response><status>Success</status>" +
		"<saml-auth-method>" + method + "</saml-auth-method>" +
		"<saml-request>" + request + "</saml-request></prelogin-response>"
}

func TestARedirectPortalYieldsTheURLToOpen(t *testing.T) {
	request := parseSAMLRequest(samlPrelogin("REDIRECT", encode64("https://idp.example.com/saml?x=1")))
	if request == nil || request.method != samlRedirect || request.payload != "https://idp.example.com/saml?x=1" {
		t.Fatalf("request = %+v", request)
	}
	if !isOpenableURL(request.payload) {
		t.Error("the IdP address is not openable")
	}
}

func TestAPostPortalYieldsTheHTMLThatSubmitsItself(t *testing.T) {
	html := `<html><body onload="document.f.submit()"><form name=f></form></body></html>`
	request := parseSAMLRequest(samlPrelogin("POST", encode64(html)))
	if request == nil || request.method != samlPost || request.payload != html {
		t.Fatalf("request = %+v", request)
	}
	// It is a document, not an address: opening it as a URL would be wrong.
	if isOpenableURL(request.payload) {
		t.Error("a document read as an address")
	}
}

func TestAWrappedBase64ValueStillDecodes(t *testing.T) {
	encoded := encode64("https://idp.example.com/a")
	request := parseSAMLRequest(samlPrelogin("redirect", encoded[:8]+"\n  "+encoded[8:]))
	// The method is matched case-insensitively, as sent.
	if request == nil || request.payload != "https://idp.example.com/a" || request.method != samlRedirect {
		t.Errorf("request = %+v", request)
	}
}

func TestAnOrdinaryGatewayHasNoSAMLRequest(t *testing.T) {
	body := "<prelogin-response><status>Success</status><username-label>Username</username-label></prelogin-response>"
	if request := parseSAMLRequest(body); request != nil {
		t.Errorf("request = %+v", request)
	}
}

func TestARequestThisCodeCannotCarryOutIsNotGuessedAt(t *testing.T) {
	for _, body := range []string{
		samlPrelogin("CAS", encode64("https://a")),
		samlPrelogin("REDIRECT", ""),
		samlPrelogin("REDIRECT", "!!!not base64!!!"),
	} {
		if request := parseSAMLRequest(body); request != nil {
			t.Errorf("%q read as %+v", body, request)
		}
	}
}

func TestOnlyHTTPAddressesAreHandedToTheBrowser(t *testing.T) {
	for _, url := range []string{"https://idp.example.com/saml", "http://portal.internal/saml"} {
		if !isOpenableURL(url) {
			t.Errorf("%q refused", url)
		}
	}
	// The gateway does not get to choose what runs on this machine.
	for _, url := range []string{"file:///etc/passwd", "javascript:alert(1)", "xdg-open;rm -rf /", "https://a.example.com /etc/passwd", ""} {
		if isOpenableURL(url) {
			t.Errorf("%q accepted", url)
		}
	}
}

func TestTheCallbackReadsXMLFields(t *testing.T) {
	payload := encode64("<saml-auth-status>1</saml-auth-status>" +
		"<saml-username>alice@example.com</saml-username><prelogin-cookie>COOKIE-VALUE</prelogin-cookie>")
	callback, err := parseCallback("globalprotectcallback:" + payload)
	if err != nil || callback.username != "alice@example.com" || callback.cookie() != "COOKIE-VALUE" {
		t.Errorf("callback = %+v, %v", callback, err)
	}
}

func TestTheCallbackReadsMetaTags(t *testing.T) {
	payload := encode64(`<html><head><meta name="saml-username" content="bob@example.com">` +
		`<meta content="META-COOKIE" name="prelogin-cookie"></head></html>`)
	callback, err := parseCallback(payload)
	// Attribute order is not fixed, so both orders have to read.
	if err != nil || callback.username != "bob@example.com" || callback.cookie() != "META-COOKIE" {
		t.Errorf("callback = %+v, %v", callback, err)
	}
}

func TestTheCallbackReadsHeaderLines(t *testing.T) {
	payload := encode64("saml-auth-status: 1\r\nsaml-username: carol@example.com\r\nportal-userauthcookie: PORTAL-COOKIE\r\n")
	callback, err := parseCallback(payload)
	// A portal sends its own cookie where a gateway sends prelogin-cookie.
	if err != nil || callback.username != "carol@example.com" || callback.cookie() != "PORTAL-COOKIE" {
		t.Errorf("callback = %+v, %v", callback, err)
	}
}

func TestTheGatewayCookieWinsWhenBothAreSent(t *testing.T) {
	if got := (gpCallback{preloginCookie: "GATEWAY", portalCookie: "PORTAL"}).cookie(); got != "GATEWAY" {
		t.Errorf("cookie = %q", got)
	}
}

func TestAPlainUnencodedPayloadIsReadToo(t *testing.T) {
	callback, err := parseCallback("globalprotectcallback:<prelogin-cookie>PLAIN</prelogin-cookie>")
	if err != nil || callback.cookie() != "PLAIN" {
		t.Errorf("callback = %+v, %v", callback, err)
	}
	// Percent-encoded, the way a browser hands over a URI's tail.
	callback, err = parseCallback("globalprotectcallback:%3Cprelogin-cookie%3EPCT%3C%2Fprelogin-cookie%3E")
	if err != nil || callback.cookie() != "PCT" {
		t.Errorf("callback = %+v, %v", callback, err)
	}
}

func TestACallbackWithoutACookieIsAnErrorNotAHalfSignIn(t *testing.T) {
	for _, uri := range []string{
		encode64("<saml-username>dave@example.com</saml-username>"),
		encode64("<prelogin-cookie></prelogin-cookie>"),
		"",
		"globalprotectcallback:",
	} {
		if callback, err := parseCallback(uri); err == nil {
			t.Errorf("%q read as %+v", uri, callback)
		}
	}
}

func TestAnotherSchemesURIIsRefused(t *testing.T) {
	if _, err := parseCallback("https://example.com/?prelogin-cookie=NOPE"); err == nil {
		t.Error("an https callback was taken")
	}
}

func TestARedirectRequestIsOpenedAsItStands(t *testing.T) {
	url, err := browserURL(samlRequest{method: samlRedirect, payload: "https://idp.example.com/saml"})
	if err != nil || url != "https://idp.example.com/saml" {
		t.Errorf("url = %q, %v", url, err)
	}
}

func TestARedirectToSomethingThatIsNotAWebAddressIsRefused(t *testing.T) {
	for _, payload := range []string{"file:///etc/passwd", "javascript:alert(1)", "not a url"} {
		if url, err := browserURL(samlRequest{method: samlRedirect, payload: payload}); err == nil {
			t.Errorf("%q handed to the browser as %q", payload, url)
		}
	}
}

func TestAPostRequestBecomesAPrivateFileTheBrowserCanOpen(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	html := `<html><body onload="f.submit()"></body></html>`
	url, err := browserURL(samlRequest{method: samlPost, payload: html})
	if err != nil {
		t.Fatal(err)
	}
	path, ok := strings.CutPrefix(url, "file://")
	if !ok {
		t.Fatalf("url = %q", url)
	}
	if raw, _ := os.ReadFile(path); string(raw) != html {
		t.Errorf("document = %q", raw)
	}
	// It names the user to their identity provider.
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v", info, err)
	}
}

func TestACallbackWithNothingWaitingIsDropped(t *testing.T) {
	// A re-opened tab or a second click, long after the sign-in finished.
	if DeliverSSOCallback("globalprotectcallback:stale") {
		t.Error("a stale callback was taken")
	}
}

// deliverWhenOpened swaps the browser for one that, once opened, hands
// uri back through DeliverSSOCallback the way `wayle vpn sso-callback`
// does through the shell.
func deliverWhenOpened(t *testing.T, uri string) *string {
	t.Helper()
	opened := new(string)
	previous := openBrowser
	openBrowser = func(url string) error {
		*opened = url
		go func() {
			for !DeliverSSOCallback(uri) {
				time.Sleep(time.Millisecond)
			}
		}()
		return nil
	}
	t.Cleanup(func() { openBrowser = previous })
	return opened
}

func TestADeliveredCallbackCompletesTheWaitingSignIn(t *testing.T) {
	opened := deliverWhenOpened(t, "globalprotectcallback:"+encode64(
		"<saml-username>alice</saml-username><prelogin-cookie>PRE</prelogin-cookie>"))
	callback, err := gpSSOSignIn(context.Background(), samlRequest{method: samlRedirect, payload: "https://idp.example.com/"}, 5*time.Second)
	if err != nil || callback.username != "alice" || callback.cookie() != "PRE" {
		t.Errorf("callback = %+v, %v", callback, err)
	}
	if *opened != "https://idp.example.com/" {
		t.Errorf("opened %q", *opened)
	}
	// Nothing is waiting any more.
	if DeliverSSOCallback("globalprotectcallback:late") {
		t.Error("a second callback was taken")
	}
}

func TestASignInNobodyAnswersTimesOut(t *testing.T) {
	previous := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = previous })
	_, err := gpSSOSignIn(context.Background(), samlRequest{method: samlRedirect, payload: "https://idp.example.com/"}, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "not completed in time") {
		t.Errorf("err = %v", err)
	}
	if DeliverSSOCallback("globalprotectcallback:late") {
		t.Error("a timed-out sign-in still takes callbacks")
	}
}

func TestANewSignInReplacesTheOneBeforeIt(t *testing.T) {
	previous := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = previous })

	request := samlRequest{method: samlRedirect, payload: "https://idp.example.com/"}
	first := make(chan error, 1)
	go func() {
		_, err := gpSSOSignIn(context.Background(), request, 5*time.Second)
		first <- err
	}()
	for {
		pendingMu.Lock()
		waiting := pending != nil
		pendingMu.Unlock()
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}

	second := make(chan gpCallback, 1)
	go func() {
		callback, _ := gpSSOSignIn(context.Background(), request, 5*time.Second)
		second <- callback
	}()
	if err := <-first; err == nil || !strings.Contains(err.Error(), "replaced by another") {
		t.Errorf("first = %v", err)
	}
	// The newer sign-in still takes the answer.
	for !DeliverSSOCallback("globalprotectcallback:<prelogin-cookie>NEW</prelogin-cookie>") {
		time.Sleep(time.Millisecond)
	}
	if got := <-second; got.cookie() != "NEW" {
		t.Errorf("second = %+v", got)
	}
}

func TestAWithdrawnSignInStopsWaiting(t *testing.T) {
	previous := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = previous })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gpSSOSignIn(ctx, samlRequest{method: samlRedirect, payload: "https://idp.example.com/"}, time.Minute); err == nil {
		t.Error("a withdrawn sign-in completed")
	}
}

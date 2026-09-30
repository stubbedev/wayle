package openconnect

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network/openconnect/openconnecttest"
	"github.com/stubbedev/wayle/service/network/secrets"
)

// connection is an NM connection dictionary as GetSecrets hands it.
func connection(service string, data map[string]string) map[string]map[string]dbus.Variant {
	return map[string]map[string]dbus.Variant{"vpn": {
		"service-type": dbus.MakeVariant(service),
		"data":         dbus.MakeVariant(data),
	}}
}

func mustProfile(t *testing.T, data map[string]string) Profile {
	t.Helper()
	p, ok := ProfileFrom(connection(ServiceType, data), "uuid-1", "Work")
	if !ok {
		t.Fatal("an openconnect profile was not recognised")
	}
	return p
}

func TestAnOpenconnectProfileIsReadOutOfNMsDictionary(t *testing.T) {
	p := mustProfile(t, map[string]string{"gateway": "vpn.example.com", "protocol": "gp", UsernameKey: "alice"})
	if p != (Profile{UUID: "uuid-1", Name: "Work", Gateway: "vpn.example.com", Protocol: "gp", Username: "alice"}) {
		t.Errorf("profile = %+v", p)
	}
	if !IsSupported(p) {
		t.Error("a GlobalProtect profile is not supported")
	}
}

func TestAnotherPluginsProfileIsNotOurs(t *testing.T) {
	if p, ok := ProfileFrom(connection("org.freedesktop.NetworkManager.openvpn", map[string]string{"gateway": "x"}), "uuid-1", "Work"); ok {
		t.Errorf("profile = %+v", p)
	}
	if _, ok := ProfileFrom(nil, "uuid-1", "Work"); ok {
		t.Error("an empty dictionary is a profile")
	}
	// Data in a shape NM does not send is not a profile either.
	bad := map[string]map[string]dbus.Variant{"vpn": {"service-type": dbus.MakeVariant(ServiceType), "data": dbus.MakeVariant("x")}}
	if _, ok := ProfileFrom(bad, "uuid-1", "Work"); ok {
		t.Error("a non-dict data read as a profile")
	}
}

func TestEveryProtocolWayleSignsIntoIsClaimed(t *testing.T) {
	// A protocol wayle implements must be claimed, or the sign-in it has
	// would never run — the web-login ones included.
	for _, protocol := range []string{"gp", "anyconnect", "fortinet", "array", "nc", "pulse", "f5"} {
		if p := mustProfile(t, map[string]string{"gateway": "vpn.example.com", "protocol": protocol}); !IsSupported(p) {
			t.Errorf("%s should sign in natively", protocol)
		}
	}
	for _, protocol := range []string{"openvpn", ""} {
		if SignsInNatively(protocol) {
			t.Errorf("%q claimed", protocol)
		}
	}
}

func TestTheProtocolPickerOffersEveryProtocolNativeFirst(t *testing.T) {
	var values []string
	for _, p := range Protocols {
		values = append(values, p.Value)
		if !SignsInNatively(p.Value) {
			t.Errorf("%s is offered but not signed into", p.Value)
		}
	}
	if !slices.Equal(values, []string{"gp", "anyconnect", "fortinet", "array", "nc", "pulse", "f5"}) {
		t.Errorf("order = %q", values)
	}
	if Protocols[0].Label != "Palo Alto GlobalProtect" || Protocols[6].Label != "F5 BIG-IP" {
		t.Errorf("labels = %+v", Protocols)
	}
}

func TestAProfileCanHandItselfBackToThePluginsAuthDialog(t *testing.T) {
	// The escape hatch for a gateway wayle reads wrong.
	opted := mustProfile(t, map[string]string{"gateway": "vpn.example.com", "protocol": "f5", SignInKey: "plugin"})
	if !opted.PluginSignIn || IsSupported(opted) {
		t.Errorf("profile = %+v", opted)
	}
	// Any other value, and the absent key, mean the default: wayle.
	for _, value := range []string{"wayle", "", "yes"} {
		normal := mustProfile(t, map[string]string{"gateway": "vpn.example.com", "protocol": "f5", SignInKey: value})
		if normal.PluginSignIn || !IsSupported(normal) {
			t.Errorf("%q opted out: %+v", value, normal)
		}
	}
}

func TestAProfileWithNoProtocolDefaultsTheWayOpenconnectDoes(t *testing.T) {
	p := mustProfile(t, map[string]string{"gateway": "vpn.example.com"})
	if p.Protocol != "anyconnect" || !IsSupported(p) {
		t.Errorf("profile = %+v", p)
	}
}

func TestAProfileWithNoGatewayCannotBeSignedInto(t *testing.T) {
	if p := mustProfile(t, map[string]string{"protocol": "gp"}); IsSupported(p) {
		t.Errorf("profile = %+v", p)
	}
}

func TestAnEmptyUsernameIsNoUsername(t *testing.T) {
	if p := mustProfile(t, map[string]string{"gateway": "g", UsernameKey: ""}); p.Username != "" {
		t.Errorf("username = %q", p.Username)
	}
}

func TestTheBrowserSignInIsOffUnlessTheProfileTurnsItOn(t *testing.T) {
	// Every profile made before this existed has no such key.
	if p := mustProfile(t, map[string]string{"gateway": "vpn.example.com"}); p.SSO {
		t.Error("an absent key enabled the browser sign-in")
	}
	for _, value := range []string{"no", "false", "", "off"} {
		if p := mustProfile(t, map[string]string{"gateway": "vpn.example.com", SSOKey: value}); p.SSO {
			t.Errorf("%q enabled the browser sign-in", value)
		}
	}
	for _, value := range []string{"yes", "true"} {
		if p := mustProfile(t, map[string]string{"gateway": "vpn.example.com", SSOKey: value}); !p.SSO {
			t.Errorf("%q did not enable the browser sign-in", value)
		}
	}
}

func testSession() session {
	return session{cookie: "authcookie=abc", host: "vpn.example.com", gwcert: "pin-sha256:AAAA"}
}

func TestTheSecretsAreExactlyTheThreeKeysThePluginAsksFor(t *testing.T) {
	// Pinned as a set: one missing key makes NM retry silently rather than
	// error, which is a loop with no message in it.
	got := secretsOf(testSession())
	if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, []string{"cookie", "gateway", "gwcert"}) {
		t.Errorf("keys = %q", keys)
	}
	if got["cookie"] != "authcookie=abc" || got["gateway"] != "vpn.example.com" || got["gwcert"] != "pin-sha256:AAAA" {
		t.Errorf("secrets = %v", got)
	}
}

func TestACookieAskedForAgainMomentsLaterIsTreatedAsRefused(t *testing.T) {
	// NM re-asking straight away means the plugin would not take what it
	// was given. Handing the same cookie back is the loop this prevents.
	now := time.Now()
	if !isSpent(&handout{cookie: "authcookie=abc", at: now}, "authcookie=abc", now) {
		t.Error("a cookie handed out just now is not spent")
	}
}

func TestAReconnectMuchLaterStillReusesItsCachedCookie(t *testing.T) {
	now := time.Now()
	if isSpent(&handout{cookie: "authcookie=abc", at: now.Add(-retryWindow - time.Second)}, "authcookie=abc", now) {
		t.Error("a cookie from long ago is spent")
	}
	if isSpent(nil, "authcookie=abc", now) {
		t.Error("a profile nothing was handed out for is spent")
	}
	if isSpent(&handout{cookie: "authcookie=old", at: now}, "authcookie=new", now) {
		t.Error("a different cookie is spent")
	}
}

func TestOnlyARefusalDiscreditsTheStoredPassword(t *testing.T) {
	if !secrets.DiscreditsPassword(authError("Invalid username or password")) {
		t.Error("a refusal does not discredit the password")
	}
	// Dismissed, unreachable, or refused past the password: none of these
	// say the password is wrong.
	for _, err := range []error{dismissed(), incomplete("cannot reach the gateway: timed out"), unsupported("unrecognised reply")} {
		if secrets.DiscreditsPassword(err) {
			t.Errorf("%v discredits the password", err)
		}
	}
}

func TestARefusalAfterThePasswordIsRecastAndNothingElseIs(t *testing.T) {
	err := pastThePassword(authError("wrong code"))
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok || err.Error() != "wrong code" {
		t.Errorf("err = %#v", err)
	}
	err = pastThePassword(unsupported("odd"))
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
		t.Errorf("err = %#v", err)
	}
}

func gpProfile(uuid, gateway string) Profile { return profileFor(uuid, "gp", gateway) }

func TestARejectedSessionCostsTheCookieButNotThePassword(t *testing.T) {
	// REQUEST_NEW means the plugin refused the cookie: a statement about
	// the session, not about the password that minted it.
	stateHome(t)
	storeSession("request-new", testSession())
	storePassword("request-new", "hunter2")
	if _, ok := reusableSession(context.Background(), gpProfile("request-new", "127.0.0.1:1"), true, newClient()); ok {
		t.Error("a rejected session was handed out again")
	}
	if _, ok := cachedSession("request-new"); ok {
		t.Error("the rejected cookie survived")
	}
	if got, _ := cachedPassword("request-new"); got != "hunter2" {
		t.Errorf("the password went with the cookie: %q", got)
	}
}

func TestAnUnreachableGatewayDoesNotCostTheStoredPassword(t *testing.T) {
	// The first attempt after a resume often runs before the network has
	// settled; failing to reach the gateway says nothing about the
	// password.
	stateHome(t)
	storePassword("unreachable-password", "hunter2")
	p := gpProfile("unreachable-password", "127.0.0.1:1")
	p.Username = "alice"
	_, err := Authenticate(context.Background(), p, false, false, &fakePrompter{})
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok {
		t.Errorf("err = %#v", err)
	}
	if got, _ := cachedPassword("unreachable-password"); got != "hunter2" {
		t.Errorf("the stored password was dropped for an unreachable gateway: %q", got)
	}
}

func TestAnUnreachableGatewayDoesNotCostTheCachedCookie(t *testing.T) {
	// The probe catches a cookie the gateway has expired. A gateway
	// nobody could reach has said nothing at all.
	stateHome(t)
	unreachable := session{cookie: "authcookie=abc&portal=vpn.example.com&user=alice&domain=example", host: "127.0.0.1:1", gwcert: "pin-sha256:AAAA"}
	storeSession("probe-unreachable", unreachable)
	if got, ok := reusableSession(context.Background(), gpProfile("probe-unreachable", "127.0.0.1:1"), false, newClient()); !ok || got != unreachable {
		t.Errorf("session = %+v, %v", got, ok)
	}
	if got, ok := cachedSession("probe-unreachable"); !ok || got != unreachable {
		t.Errorf("the cached cookie did not survive: %+v", got)
	}
}

func TestAnUnattendedRestoreHandsBackTheCachedSession(t *testing.T) {
	stateHome(t)
	cached := session{cookie: "authcookie=abc", host: "127.0.0.1:1", gwcert: "pin-sha256:AAAA"}
	storeSession("unattended-cached", cached)
	prompter := &fakePrompter{}
	got, err := Authenticate(context.Background(), gpProfile("unattended-cached", "127.0.0.1:1"), false, true, prompter)
	if err != nil || got["cookie"] != cached.cookie {
		t.Errorf("secrets = %v, %v", got, err)
	}
	if len(prompter.requests()) != 0 {
		t.Error("a prompt went up for nobody")
	}
}

func TestAnUnattendedRestoreWithNoSessionSignsNobodyIn(t *testing.T) {
	// A night's sleep outlasts the session. Signing in again from here
	// would post the password, and a push MFA gateway would buzz a phone
	// nobody is looking at.
	stateHome(t)
	storePassword("unattended-lapsed", "hunter2")
	gateway := openconnecttest.NewSilentGateway(t)
	p := gpProfile("unattended-lapsed", gateway.Addr)
	p.Username = "alice"
	prompter := &fakePrompter{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Authenticate(ctx, p, false, true, prompter)
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok || ctx.Err() != nil {
		t.Errorf("err = %#v (ctx %v)", err, ctx.Err())
	}
	if gateway.Accepted() != 0 {
		t.Error("credentials were posted with nobody there to answer")
	}
	if len(prompter.requests()) != 0 {
		t.Error("a prompt went up for nobody")
	}
	if got, _ := cachedPassword("unattended-lapsed"); got != "hunter2" {
		t.Errorf("the password went with a restore that never tried it: %q", got)
	}
}

func TestAConnectSomeoneAskedForSignsInWhenTheSessionIsGone(t *testing.T) {
	stateHome(t)
	storePassword("attended-lapsed", "hunter2")
	gateway := openconnecttest.NewSilentGateway(t)
	p := gpProfile("attended-lapsed", gateway.Addr)
	p.Username = "alice"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Authenticate(ctx, p, false, false, &fakePrompter{})
		done <- err
	}()
	if !gateway.Reached() {
		t.Error("a connect someone asked for never went to the gateway")
	}
	// Withdrawing the request ends the sign-in, and does not cost the
	// password.
	cancel()
	if err := <-done; secrets.DiscreditsPassword(err) || err == nil {
		t.Errorf("err = %v", err)
	}
	if got, _ := cachedPassword("attended-lapsed"); got != "hunter2" {
		t.Errorf("a withdrawn sign-in cost the password: %q", got)
	}
}

// Against the fake GlobalProtect gateways, end to end through
// Authenticate.

// challengeAnswering answers only challenges, with code.
type challengeAnswering struct {
	code  string
	shown []secrets.Request
}

func (c *challengeAnswering) Prompt(_ context.Context, req secrets.Request) (map[string]string, bool) {
	c.shown = append(c.shown, req)
	return map[string]string{"passwd": c.code}, true
}

func alice(uuid, gateway string) Profile {
	p := gpProfile(uuid, gateway)
	p.Username = fakeUser
	return p
}

func TestAGatewayRefusedCookieIsDroppedInsteadOfHandedOut(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	dead := session{cookie: "authcookie=EXPIRED&portal=127.0.0.1&user=alice&domain=example", host: g.addr, gwcert: fakePin}
	storeSession("mock-refused", dead)
	if _, ok := reusableSession(context.Background(), gpProfile("mock-refused", g.addr), false, newClient()); ok {
		t.Error("a cookie the gateway refused was handed to NM")
	}
	if _, ok := cachedSession("mock-refused"); ok {
		t.Error("the refused cookie survived")
	}
}

func TestAGatewayAcceptedCookieIsStillReused(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	live := gpFakeSignIn(t, g.addr)
	storeSession("mock-accepted", live)
	if got, ok := reusableSession(context.Background(), gpProfile("mock-accepted", g.addr), false, newClient()); !ok || got != live {
		t.Errorf("session = %+v, %v", got, ok)
	}
}

func TestAFullGlobalProtectSignInCachesTheSessionAndTheTypedPassword(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	codes := &sequencePrompter{rounds: []map[string]string{
		{"user": fakeUser, "passwd": fakePassword},
		{"passwd": fakeChallengeAnswer},
	}}
	got, err := Authenticate(context.Background(), gpProfile("mock-full", g.addr), false, false, codes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got["cookie"], "authcookie=AUTHCOOKIEVALUE&") || got["gateway"] != g.addr || got["gwcert"] != fakePin {
		t.Errorf("secrets = %v", got)
	}
	// The prelogin's own wording reached the first prompt, and the
	// challenge's the second.
	if len(codes.shown) != 2 || codes.shown[0].Message != "Sign in to the mock gateway" ||
		codes.shown[0].Fields[0].Label != "Company ID" || codes.shown[0].Fields[1].Label != "Passphrase" ||
		codes.shown[1].Message != "Approve the push on your phone" {
		t.Errorf("prompts = %+v", codes.shown)
	}
	if s, ok := cachedSession("mock-full"); !ok || s.cookie != got["cookie"] {
		t.Errorf("cached session = %+v", s)
	}
	if pw, _ := cachedPassword("mock-full"); pw != fakePassword {
		t.Errorf("cached password = %q", pw)
	}

	// Asked again at once, the same cookie is spent: NM would only ask
	// again because the plugin refused it. The sign-in runs afresh.
	again := &sequencePrompter{rounds: []map[string]string{{"passwd": fakeChallengeAnswer}}}
	p := alice("mock-full", g.addr)
	if _, err := Authenticate(context.Background(), p, false, false, again); err != nil {
		t.Fatal(err)
	}
	if len(again.shown) != 1 {
		t.Errorf("a spent cookie was handed back: prompts = %+v", again.shown)
	}
}

// sequencePrompter answers prompts with one map per round, in order.
type sequencePrompter struct {
	rounds []map[string]string
	shown  []secrets.Request
}

func (s *sequencePrompter) Prompt(_ context.Context, req secrets.Request) (map[string]string, bool) {
	s.shown = append(s.shown, req)
	if len(s.rounds) == 0 {
		return nil, false
	}
	round := s.rounds[0]
	s.rounds = s.rounds[1:]
	return round, true
}

func TestARefusedPasswordIsForgotten(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	storePassword("mock-bad-password", "wrong")
	_, err := Authenticate(context.Background(), alice("mock-bad-password", g.addr), false, false, &fakePrompter{})
	if !secrets.DiscreditsPassword(err) {
		t.Errorf("err = %#v", err)
	}
	if _, ok := cachedPassword("mock-bad-password"); ok {
		t.Error("a password the gateway refused is kept, and would fail every connect")
	}
}

func TestAGatewaysInternalErrorKeepsThePassword(t *testing.T) {
	// Answered with PAN's refusal status, reason header and all: only the
	// words say it was the gateway, and the words decide.
	g := startGateway(t, modeForm)
	stateHome(t)
	storePassword("mock-internal-error", "hunter2")
	p := gpProfile("mock-internal-error", g.addr)
	p.Username = fakeTroubledUser
	_, err := Authenticate(context.Background(), p, false, false, &fakePrompter{})
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok || !strings.Contains(err.Error(), "Internal error") {
		t.Errorf("err = %#v", err)
	}
	if got, _ := cachedPassword("mock-internal-error"); got != "hunter2" {
		t.Errorf("the gateway's own failure cost the password: %q", got)
	}
}

func TestARefusedCodeKeepsThePasswordThatWasAccepted(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	storePassword("mock-bad-code", "hunter2")
	prompter := &challengeAnswering{code: "000000"}
	_, err := Authenticate(context.Background(), alice("mock-bad-code", g.addr), false, false, prompter)
	if _, ok := errors.AsType[*secrets.SignInIncompleteError](err); !ok {
		t.Errorf("err = %#v", err)
	}
	if got, _ := cachedPassword("mock-bad-code"); got != "hunter2" {
		t.Errorf("a wrong 2FA code cost the password the gateway had just accepted: %q", got)
	}
	if len(prompter.shown) != 1 || prompter.shown[0].Fields[0].Label != "Code" {
		t.Errorf("prompts = %+v", prompter.shown)
	}
}

func TestADismissedChallengeIsASignInThatDidNotFinish(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	storePassword("mock-dismissed", fakePassword)
	_, err := Authenticate(context.Background(), alice("mock-dismissed", g.addr), false, false, &fakePrompter{dismiss: true})
	if err == nil || err.Error() != "sign-in dismissed" {
		t.Errorf("err = %v", err)
	}
	if got, _ := cachedPassword("mock-dismissed"); got != fakePassword {
		t.Errorf("a dismissal cost the password: %q", got)
	}
}

func TestAPromptWithNoUsernameIsARefusal(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	_, err := Authenticate(context.Background(), gpProfile("mock-no-user", g.addr), false, false,
		&fakePrompter{answers: map[string]string{"passwd": fakePassword}})
	if !secrets.DiscreditsPassword(err) || err.Error() != "no username given" {
		t.Errorf("err = %v", err)
	}
}

func TestASAMLPortalIsRefusedBeforeAnyCredentialsArePosted(t *testing.T) {
	g := startGateway(t, modeSAML)
	stateHome(t)
	prompter := &fakePrompter{}
	_, err := Authenticate(context.Background(), gpProfile("mock-saml", g.addr), false, false, prompter)
	if err == nil || !strings.Contains(err.Error(), "SAML sign-in") || strings.Contains(err.Error(), "no GlobalProtect gateway") {
		t.Errorf("err = %v", err)
	}
	for _, r := range g.requests() {
		if r.method != "GET" {
			t.Errorf("something was posted at the SAML portal: %+v", r)
		}
	}
	if len(prompter.requests()) != 0 {
		t.Error("the user was asked for credentials a SAML portal cannot use")
	}
}

func TestASAMLPortalSignsInThroughTheBrowserWhenTheProfileAsks(t *testing.T) {
	g := startGateway(t, modeSAML)
	stateHome(t)
	opened := deliverWhenOpened(t, "globalprotectcallback:"+encode64(
		"<saml-username>"+fakeSAMLUser+"</saml-username><prelogin-cookie>"+fakeSAMLCookie+"</prelogin-cookie>"))
	p := gpProfile("mock-saml-sso", g.addr)
	p.SSO = true
	got, err := Authenticate(context.Background(), p, false, false, &fakePrompter{})
	if err != nil {
		t.Fatal(err)
	}
	if *opened != "https://idp.example.com/" {
		t.Errorf("opened %q", *opened)
	}
	if !strings.HasPrefix(got["cookie"], "authcookie=AUTHCOOKIEVALUE&") || got["gwcert"] != fakePin {
		t.Errorf("secrets = %v", got)
	}
	// A pre-login cookie is single-use: it is never cached as a password.
	if _, ok := cachedPassword("mock-saml-sso"); ok {
		t.Error("the pre-login cookie was cached as a password")
	}
}

func TestAnAnyConnectProfileRoutesToItsOwnSignIn(t *testing.T) {
	g := startGateway(t, modeAnyConnect)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{
		"username": fakeUser, "password": fakePassword, "secondary_password": fakeChallengeAnswer,
	}}
	got, err := Authenticate(context.Background(), profileFor("route-ac", "anyconnect", g.addr), false, false, prompter)
	if err != nil || got["cookie"] != "webvpn=SESSIONVALUE" {
		t.Errorf("secrets = %v, %v", got, err)
	}
	if pw, _ := cachedPassword("route-ac"); pw != fakePassword {
		t.Errorf("cached password = %q", pw)
	}
	seen := len(g.requests())

	// A reconnect well after the handout gets the cached session back,
	// without a GlobalProtect probe or any request to the gateway.
	handedMu.Lock()
	handedOut["route-ac"] = handout{cookie: got["cookie"], at: time.Now().Add(-2 * retryWindow)}
	handedMu.Unlock()
	again, err := Authenticate(context.Background(), profileFor("route-ac", "anyconnect", g.addr), false, false, &fakePrompter{dismiss: true})
	if err != nil || again["cookie"] != got["cookie"] || len(g.requests()) != seen {
		t.Errorf("reconnect = %v, %v (%d requests)", again, err, len(g.requests())-seen)
	}

	// Asked again at once, the cookie is spent, and a fresh sign-in runs.
	if _, err := Authenticate(context.Background(), profileFor("route-ac", "anyconnect", g.addr), false, false, &fakePrompter{dismiss: true}); err == nil {
		t.Error("a cookie handed out moments ago was handed out again")
	}
	if _, ok := cachedSession("route-ac"); ok {
		t.Error("the spent cookie survived")
	}
}

func TestAnUnsupportedGatewayIsLeftToTheNextAgentWithThePasswordKept(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	storePassword("route-unsupported", fakePassword)
	p := profileFor("route-unsupported", "anyconnect", g.addr)
	p.Username = fakeUser
	_, err := Authenticate(context.Background(), p, false, false, &fakePrompter{})
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
		t.Errorf("err = %#v", err)
	}
	if got, _ := cachedPassword("route-unsupported"); got != fakePassword {
		t.Errorf("an unsupported gateway cost the password: %q", got)
	}
}

func TestTheHostnameIsReportedToTheGateway(t *testing.T) {
	if hostname() == "" {
		t.Error("no hostname")
	}
}

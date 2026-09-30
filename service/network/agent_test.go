package network

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network/openconnect"
	"github.com/stubbedev/wayle/service/network/secrets"
)

// fakeSignIn stands in for package openconnect: it claims openconnect
// profiles and signs in however the test says.
type fakeSignIn struct {
	unsupported  bool
	authenticate func(ctx context.Context, p openconnect.Profile, requestNew, unattended bool, prompter secrets.Prompter) (map[string]string, error)
	forgotten    []string
	callbacks    []string
	waiting      bool
}

func (s *fakeSignIn) ProfileFrom(conn ConnectionDict, uuid, name string) (openconnect.Profile, bool) {
	if variantString(conn["vpn"]["service-type"]) != openconnect.ServiceType {
		return openconnect.Profile{}, false
	}
	data := stringDict(conn["vpn"]["data"])
	return openconnect.Profile{UUID: uuid, Name: name, Gateway: data["gateway"], Protocol: data["protocol"]}, true
}

func (s *fakeSignIn) IsSupported(openconnect.Profile) bool { return !s.unsupported }

func (s *fakeSignIn) Authenticate(ctx context.Context, p openconnect.Profile, requestNew, unattended bool, prompter secrets.Prompter) (map[string]string, error) {
	return s.authenticate(ctx, p, requestNew, unattended, prompter)
}

func (s *fakeSignIn) Forget(uuid string) { s.forgotten = append(s.forgotten, uuid) }

func (s *fakeSignIn) DeliverSSOCallback(uri string) bool {
	s.callbacks = append(s.callbacks, uri)
	return s.waiting
}

// silentGateway is a sign-in that reaches the gateway and then waits
// on it until its context ends.
func silentGateway(reached chan<- struct{}) func(context.Context, openconnect.Profile, bool, bool, secrets.Prompter) (map[string]string, error) {
	return func(ctx context.Context, _ openconnect.Profile, _, _ bool, _ secrets.Prompter) (map[string]string, error) {
		reached <- struct{}{}
		<-ctx.Done()
		return nil, &secrets.SignInIncompleteError{Reason: "gateway went quiet"}
	}
}

func gpConnection(uuid string) ConnectionDict {
	return vpnConnection(uuid, "Work", openconnect.ServiceType, map[string]string{"gateway": "vpn.example.com", "protocol": "gp"})
}

func formVPNConnection(uuid string) ConnectionDict {
	return vpnConnection(uuid, "Office", "org.freedesktop.NetworkManager.openvpn", nil)
}

// agentFixture is an agent served on a client peer of a fake NM.
type agentFixture struct {
	nm     *fakeNM
	agent  *Agent
	name   string
	signIn *fakeSignIn
}

func serveFixture(t *testing.T, budget time.Duration) *agentFixture {
	t.Helper()
	f := startFakeNM(t)
	conn := f.client()
	agent := NewAgent()
	signIn := &fakeSignIn{}
	must(t, serveAgent(t.Context(), conn, agent, signIn, budget))
	f.registered(t)
	return &agentFixture{nm: f, agent: agent, name: conn.Names()[0], signIn: signIn}
}

type answer struct {
	reply ConnectionDict
	err   error
}

// ask starts a request with interaction allowed, as NM makes one.
func (fx *agentFixture) ask(conn ConnectionDict, at int, setting string, hints ...string) <-chan answer {
	out := make(chan answer, 1)
	go func() {
		reply, err := fx.nm.getSecrets(context.Background(), fx.name, conn, settingPath(at), setting, hints, flagAllowInteraction)
		out <- answer{reply, err}
	}()
	return out
}

func (fx *agentFixture) shown(t *testing.T) secrets.Request {
	t.Helper()
	var req secrets.Request
	waitFor(t, "a prompt", func() bool {
		var ok bool
		req, ok = fx.agent.Request()
		return ok
	})
	return req
}

func await(t *testing.T, ch <-chan answer) answer {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(3 * time.Second):
		t.Fatal("the request never answered")
	}
	return answer{}
}

func errName(err error) string {
	if dbusErr, ok := errors.AsType[dbus.Error](err); ok {
		return dbusErr.Name
	}
	return ""
}

func errText(err error) string {
	if dbusErr, ok := errors.AsType[dbus.Error](err); ok && len(dbusErr.Body) > 0 {
		s, _ := dbusErr.Body[0].(string)
		return s
	}
	return ""
}

func TestAgentRegistersWithItsIdentifierAndVPNHints(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	got := fx.nm.registered(t)
	if got.id != "com.wayle.network" || got.caps != 0x1 {
		t.Errorf("registered as %q caps %#x, want com.wayle.network 0x1", got.id, got.caps)
	}
	if got.sender != fx.name {
		t.Errorf("registered from %q, want the agent's own peer %q", got.sender, fx.name)
	}
}

func TestAgentReregistersWhenNetworkManagerComesBack(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	fx.nm.stop()
	fx.nm.own()
	// A registration on the new NM, which started with none.
	got := fx.nm.registered(t)
	if got.id != AgentID {
		t.Errorf("re-registered as %q", got.id)
	}
}

func TestAgentWithoutInteractionHasNoSecrets(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	_, err := fx.nm.getSecrets(context.Background(), fx.name, wifiConnection("home", "Home", []byte("home")), settingPath(1), "802-11-wireless-security", nil, 0)
	if errName(err) != errNoSecrets {
		t.Fatalf("err = %v, want NoSecrets", err)
	}
	if _, ok := fx.agent.Request(); ok {
		t.Error("a prompt went up for a request that forbade interaction")
	}
}

func TestAgentAsksForAWifiKeyAndAnswersFlat(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	pending := fx.ask(wifiConnection("home", "Home", []byte("home")), 2, "802-11-wireless-security")
	req := fx.shown(t)
	if req.UUID != "home" || req.Name != "Home" || req.Setting != "802-11-wireless-security" {
		t.Errorf("request = %+v", req)
	}
	if len(req.Fields) != 1 || req.Fields[0].Key != "psk" || !req.Fields[0].Secret {
		t.Fatalf("fields = %+v, want one masked psk", req.Fields)
	}
	fx.agent.Submit(map[string]string{"psk": "correct horse"})
	a := await(t, pending)
	if a.err != nil {
		t.Fatal(a.err)
	}
	if got := variantString(a.reply["802-11-wireless-security"]["psk"]); got != "correct horse" {
		t.Errorf("psk = %q", got)
	}
	if _, ok := fx.agent.Request(); ok {
		t.Error("the prompt stayed up after it was answered")
	}
}

func TestAgentAsksForTheHintedEnterpriseKeys(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	conn := ConnectionDict{"connection": connectionSection("corp", "Corp", "802-11-wireless")}
	pending := fx.ask(conn, 3, "802-1x", "identity", "password")
	req := fx.shown(t)
	if len(req.Fields) != 2 || req.Fields[1].Key != "password" || !req.Fields[1].Secret {
		t.Fatalf("fields = %+v", req.Fields)
	}
	fx.agent.Submit(map[string]string{"identity": "alice", "password": "hunter2"})
	a := await(t, pending)
	if a.err != nil {
		t.Fatal(a.err)
	}
	if got := variantString(a.reply["802-1x"]["password"]); got != "hunter2" {
		t.Errorf("802-1x password = %q", got)
	}
}

func TestAgentNestsAVPNFormsSecretsUnderTheSecretsKey(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	pending := fx.ask(formVPNConnection("office"), 4, "vpn", "password")
	fx.shown(t)
	fx.agent.Submit(map[string]string{"password": "hunter2"})
	a := await(t, pending)
	if a.err != nil {
		t.Fatal(a.err)
	}
	nested := stringDict(a.reply["vpn"]["secrets"])
	if nested["password"] != "hunter2" {
		t.Errorf("vpn secrets = %v", a.reply["vpn"])
	}
	if _, flat := a.reply["vpn"]["password"]; flat {
		t.Error("a flat vpn password reads to NM as no secrets at all")
	}
}

func TestAgentDismissedPromptIsUserCanceled(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	pending := fx.ask(wifiConnection("home", "Home", []byte("home")), 2, "802-11-wireless-security")
	fx.shown(t)
	fx.agent.Cancel()
	a := await(t, pending)
	if errName(a.err) != errUserCanceled || errText(a.err) != "dismissed" {
		t.Fatalf("err = %v, want UserCanceled(dismissed)", a.err)
	}
}

func TestAgentHasNothingToAskForAnOpenconnectCookieOrAnUnknownSetting(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	// No sign-in wired: an openconnect request is not a prompt.
	conn := fx.nm.client()
	agent := NewAgent()
	must(t, serveAgent(t.Context(), conn, agent, nil, RequestBudget))
	name := conn.Names()[0]
	_, err := fx.nm.getSecrets(context.Background(), name, gpConnection("work"), settingPath(1), "vpn", []string{"cookie", "gateway", "gwcert"}, flagAllowInteraction)
	if errName(err) != errNoSecrets {
		t.Errorf("openconnect cookie: err = %v, want NoSecrets", err)
	}
	_, err = fx.nm.getSecrets(context.Background(), name, ConnectionDict{}, settingPath(2), "bluetooth", nil, flagAllowInteraction)
	if errName(err) != errNoSecrets || errText(err) != "nothing to ask for bluetooth" {
		t.Errorf("bluetooth: err = %v", err)
	}
	if _, ok := agent.Request(); ok {
		t.Error("a prompt went up with nothing to ask")
	}
}

func TestAgentSaveAndDeleteSecretsAreAcceptedNoOps(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	obj := fx.nm.conn.Object(fx.name, AgentPath)
	for _, method := range []string{"SaveSecrets", "DeleteSecrets"} {
		if err := obj.Call(agentIface+"."+method, 0, gpConnection("work"), settingPath(1)).Err; err != nil {
			t.Errorf("%s: %v", method, err)
		}
	}
	if _, ok := fx.agent.Failure(); ok {
		t.Error("saving secrets published a failure")
	}
	// A method NM never calls is not part of the interface.
	if err := obj.Call(agentIface+".Submit", 0, map[string]string{}).Err; err == nil {
		t.Error("Submit is exported on the bus; only the SecretAgent methods may be")
	}
}

func TestAgentSignsIntoOpenconnectAndNestsTheCookie(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	var sawNew bool
	fx.signIn.authenticate = func(_ context.Context, p openconnect.Profile, requestNew, unattended bool, _ secrets.Prompter) (map[string]string, error) {
		sawNew = requestNew
		if p.UUID != "work" || p.Protocol != "gp" || unattended {
			t.Errorf("sign-in for %+v unattended=%v", p, unattended)
		}
		return map[string]string{"cookie": "c00kie", "gateway": "vpn.example.com"}, nil
	}
	fx.agent.setFailure(&AuthFailure{UUID: "work", Reason: "stale"})
	reply, err := fx.nm.getSecrets(context.Background(), fx.name, gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction|flagRequestNew)
	if err != nil {
		t.Fatal(err)
	}
	if got := stringDict(reply["vpn"]["secrets"])["cookie"]; got != "c00kie" {
		t.Errorf("cookie = %q", got)
	}
	if !sawNew {
		t.Error("REQUEST_NEW did not reach the sign-in")
	}
	if _, ok := fx.agent.Failure(); ok {
		t.Error("a successful sign-in left the last failure on the row")
	}
}

func TestAgentPublishesTheGatewaysWordingOnARefusal(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	fx.signIn.authenticate = func(context.Context, openconnect.Profile, bool, bool, secrets.Prompter) (map[string]string, error) {
		return nil, &secrets.AuthenticationFailedError{Reason: "Invalid username or password"}
	}
	_, err := fx.nm.getSecrets(context.Background(), fx.name, gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction)
	if errName(err) != errUserCanceled || errText(err) != "Invalid username or password" {
		t.Fatalf("err = %v", err)
	}
	failure, ok := fx.agent.Failure()
	if !ok || failure.UUID != "work" || failure.Reason != "Invalid username or password" {
		t.Errorf("failure = %+v, %v", failure, ok)
	}
}

func TestAgentLeavesAGatewayItCannotFollowToAnotherAgent(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	fx.signIn.authenticate = func(context.Context, openconnect.Profile, bool, bool, secrets.Prompter) (map[string]string, error) {
		return nil, &secrets.ProtocolUnsupportedError{Reason: "unknown login form"}
	}
	_, err := fx.nm.getSecrets(context.Background(), fx.name, gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction)
	if errName(err) != errNoSecrets {
		t.Fatalf("err = %v, want NoSecrets", err)
	}
	if _, ok := fx.agent.Failure(); ok {
		t.Error("a hand-off to another agent was reported as a failed sign-in")
	}

	fx.signIn.unsupported = true
	_, err = fx.nm.getSecrets(context.Background(), fx.name, gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction)
	if errName(err) != errNoSecrets || !strings.Contains(errText(err), "no native sign-in for openconnect protocol gp") {
		t.Fatalf("unsupported protocol: err = %v", err)
	}
}

func TestAWithdrawnSignInStopsWhereItIsAndReportsNothing(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	reached := make(chan struct{}, 1)
	fx.signIn.authenticate = silentGateway(reached)
	pending := fx.ask(gpConnection("withdrawn"), 1, "vpn")
	<-reached
	fx.nm.cancelSecrets(fx.name, settingPath(1), "vpn")
	a := await(t, pending)
	if errName(a.err) != errAgentCanceled {
		t.Fatalf("err = %v, want AgentCanceled", a.err)
	}
	if _, ok := fx.agent.Failure(); ok {
		t.Error("a withdrawn sign-in reported a failure nobody was waiting for")
	}
}

func TestWithdrawingOneRequestLeavesTheOthersAlone(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	reached := make(chan struct{}, 1)
	fx.signIn.authenticate = silentGateway(reached)
	vpn := fx.ask(gpConnection("work"), 1, "vpn")
	<-reached
	wifi := fx.ask(wifiConnection("home-wifi", "Home", []byte("home")), 2, "802-11-wireless-security")
	fx.shown(t)

	fx.nm.cancelSecrets(fx.name, settingPath(1), "vpn")
	if a := await(t, vpn); errName(a.err) != errAgentCanceled {
		t.Fatalf("vpn err = %v", a.err)
	}
	// Withdrawing the VPN must not dismiss the form on screen.
	if req, ok := fx.agent.Request(); !ok || req.UUID != "home-wifi" {
		t.Fatalf("the wifi prompt went down with the VPN's request: %+v %v", req, ok)
	}
	fx.agent.Submit(map[string]string{"psk": "correct horse"})
	if a := await(t, wifi); a.err != nil {
		t.Fatalf("wifi err = %v", a.err)
	}
}

func TestAWithdrawnPromptComesDown(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	pending := fx.ask(wifiConnection("home", "Home", []byte("home")), 2, "802-11-wireless-security")
	fx.shown(t)
	fx.nm.cancelSecrets(fx.name, settingPath(2), "802-11-wireless-security")
	if a := await(t, pending); errName(a.err) != errAgentCanceled {
		t.Fatalf("err = %v", a.err)
	}
	waitFor(t, "the prompt to come down", func() bool { _, ok := fx.agent.Request(); return !ok })
}

func TestANewRequestForTheSameSettingSupersedesTheOld(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	first := fx.ask(wifiConnection("home", "Home", []byte("home")), 2, "802-11-wireless-security")
	fx.shown(t)
	second := fx.ask(wifiConnection("home", "Home", []byte("home")), 2, "802-11-wireless-security")
	if a := await(t, first); errName(a.err) != errAgentCanceled {
		t.Fatalf("superseded err = %v", a.err)
	}
	fx.shown(t)
	fx.agent.Submit(map[string]string{"psk": "x"})
	if a := await(t, second); a.err != nil {
		t.Fatalf("second err = %v", a.err)
	}
}

func TestASignInThatOutlastsNetworkManagersPatienceIsEndedAndSaysWhy(t *testing.T) {
	fx := serveFixture(t, 300*time.Millisecond)
	fx.signIn.authenticate = func(context.Context, openconnect.Profile, bool, bool, secrets.Prompter) (map[string]string, error) {
		// Ignores its context: the budget must not depend on the
		// sign-in noticing.
		time.Sleep(5 * time.Second)
		return map[string]string{"cookie": "late"}, nil
	}
	start := time.Now()
	_, err := fx.nm.getSecrets(context.Background(), fx.name, gpConnection("out-of-time"), settingPath(1), "vpn", nil, flagAllowInteraction)
	if time.Since(start) > 2*time.Second {
		t.Fatal("a sign-in ran on past the time NM waits for it")
	}
	if errName(err) != errUserCanceled {
		t.Fatalf("err = %v", err)
	}
	failure, ok := fx.agent.Failure()
	if !ok || failure.UUID != "out-of-time" || !strings.Contains(failure.Reason, "did not finish") {
		t.Errorf("failure = %+v, %v", failure, ok)
	}
}

func TestAPromptNobodyAnswersComesDownWhenTimeRunsOut(t *testing.T) {
	fx := serveFixture(t, 200*time.Millisecond)
	_, err := fx.nm.getSecrets(context.Background(), fx.name, wifiConnection("home", "Home", []byte("home")), settingPath(2), "802-11-wireless-security", nil, flagAllowInteraction)
	if errName(err) != errUserCanceled || errText(err) != "no answer in time" {
		t.Fatalf("err = %v", err)
	}
	if _, ok := fx.agent.Request(); ok {
		t.Error("the form stayed up after NM stopped listening")
	}
	// Nor does the next prompt queue behind it, and an answer in time
	// is still an answer.
	pending := fx.ask(wifiConnection("office", "Office", []byte("office")), 3, "802-11-wireless-security")
	if req := fx.shown(t); req.UUID != "office" {
		t.Errorf("next prompt = %+v", req)
	}
	fx.agent.Submit(map[string]string{"psk": "x"})
	if a := await(t, pending); a.err != nil {
		t.Fatal(a.err)
	}
}

func TestPromptsAreServedOneAtATime(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	first := fx.ask(wifiConnection("a", "A", []byte("a")), 1, "802-11-wireless-security")
	if req := fx.shown(t); req.UUID != "a" {
		t.Fatalf("first = %+v", req)
	}
	second := fx.ask(wifiConnection("b", "B", []byte("b")), 2, "802-11-wireless-security")
	time.Sleep(50 * time.Millisecond)
	if req, _ := fx.agent.Request(); req.UUID != "a" {
		t.Fatalf("the second prompt took over the form: %+v", req)
	}
	fx.agent.Submit(map[string]string{"psk": "1"})
	await(t, first)
	waitFor(t, "the second prompt", func() bool { req, ok := fx.agent.Request(); return ok && req.UUID == "b" })
	fx.agent.Submit(map[string]string{"psk": "2"})
	if a := await(t, second); variantString(a.reply["802-11-wireless-security"]["psk"]) != "2" {
		t.Errorf("second reply = %v %v", a.reply, a.err)
	}
}

func TestNobodyIsShownAFormForAnActivationNobodyIsWatching(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	fx.agent.ExpectUnattended("office")
	_, err := fx.nm.getSecrets(context.Background(), fx.name, formVPNConnection("office"), settingPath(4), "vpn", []string{"password"}, flagAllowInteraction)
	if errName(err) != errUserCanceled || !strings.Contains(errText(err), "nobody was there to answer") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := fx.agent.Request(); ok {
		t.Error("a form went up for an unattended activation")
	}
	// Asked for by someone, the same profile gets its form.
	pending := fx.ask(formVPNConnection("office"), 4, "vpn", "password")
	if req := fx.shown(t); req.UUID != "office" {
		t.Errorf("request = %+v", req)
	}
	fx.agent.Cancel()
	await(t, pending)
}

func TestAnUnattendedSignInIsToldSo(t *testing.T) {
	fx := serveFixture(t, RequestBudget)
	var got bool
	fx.signIn.authenticate = func(_ context.Context, _ openconnect.Profile, _, unattended bool, _ secrets.Prompter) (map[string]string, error) {
		got = unattended
		return map[string]string{"cookie": "c"}, nil
	}
	fx.agent.ExpectUnattended("work")
	if _, err := fx.nm.getSecrets(context.Background(), fx.name, gpConnection("work"), settingPath(1), "vpn", nil, flagAllowInteraction); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("the restore's sign-in was not told nobody is watching")
	}
}

func TestARestoresWordThatNobodyIsWatchingHoldsForOneRequest(t *testing.T) {
	a := NewAgent()
	now := time.Now()
	a.expectUnattendedAt("work", now)
	if !a.takeUnattended("work", now) {
		t.Fatal("the mark did not hold")
	}
	if a.takeUnattended("work", now) {
		t.Error("the mark was not used up")
	}
	if a.takeUnattended("home", now) {
		t.Error("an unmarked profile was unattended")
	}
}

func TestAClickOrAMarkGoneStaleIsAttended(t *testing.T) {
	a := NewAgent()
	a.ExpectUnattended("work")
	a.ExpectAttended("work")
	if a.takeUnattended("work", time.Now()) {
		t.Error("a click stayed unattended")
	}
	now := time.Now()
	a.expectUnattendedAt("work", now)
	if a.takeUnattended("work", now.Add(unattendedWindow+time.Second)) {
		t.Error("a mark outlived the activation it was made for")
	}
}

func TestAnAnswerWithNoPromptIsDropped(t *testing.T) {
	a := NewAgent()
	a.Submit(map[string]string{"psk": "x"})
	a.Cancel()
	if _, ok := a.Request(); ok {
		t.Error("an answer with nothing pending raised a prompt")
	}
}

func TestReplyMapShapes(t *testing.T) {
	vpn := replyMap("vpn", map[string]string{"password": "hunter2"})
	if _, ok := vpn["vpn"]["secrets"]; !ok {
		t.Error("vpn secrets must be nested")
	}
	if _, ok := vpn["vpn"]["password"]; ok {
		t.Error("a flat vpn password reads to NM as no secrets")
	}
	wifi := replyMap("802-11-wireless-security", map[string]string{"psk": "hunter2"})
	if got := variantString(wifi["802-11-wireless-security"]["psk"]); got != "hunter2" {
		t.Errorf("psk = %q", got)
	}
	if _, ok := wifi["802-11-wireless-security"]["secrets"]; ok {
		t.Error("a non-VPN setting grew a secrets sub-dict")
	}
}

func TestFieldsForRequests(t *testing.T) {
	fields := fieldsFor("vpn", []string{"user", "password"})
	if len(fields) != 2 || fields[0].Key != "user" || fields[0].Secret || !fields[1].Secret {
		t.Errorf("hinted fields = %+v", fields)
	}
	if f := fieldsFor("802-11-wireless-security", nil); len(f) != 1 || f[0].Key != "psk" || f[0].Label != "Password" {
		t.Errorf("hintless wifi = %+v", f)
	}
	if f := fieldsFor("vpn", []string{"cookie", "gateway", "gwcert"}); f != nil {
		t.Errorf("a session cookie is not a prompt: %+v", f)
	}
	if f := fieldsFor("vpn", []string{"password", "gwcert"}); len(f) != 1 || f[0].Key != "password" {
		t.Errorf("mixed hints = %+v", f)
	}
	if f := fieldsFor("bluetooth", nil); f != nil {
		t.Errorf("unknown setting = %+v", f)
	}
	if f := fieldsFor("vpn", []string{"totp-token"}); !f[0].Secret {
		t.Error("an unknown key must be masked")
	}
	if f := fieldsFor("wireguard", nil); f[0].Key != "private-key" || f[0].Label != "Private key" {
		t.Errorf("wireguard = %+v", f)
	}
}

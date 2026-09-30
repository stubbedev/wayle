package openconnect

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/network/secrets"
)

func TestTheFortinetSessionCookieIsTheOneOpenconnectAsksFor(t *testing.T) {
	r := cookieReply("SVPNCOOKIE=abc123; path=/; secure; HttpOnly", "other=x")
	if got, ok := r.namedCookie(fortiCookieName); !ok || got != "SVPNCOOKIE=abc123" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
}

func TestAnEmptiedFortinetCookieIsADeletionAndNotASession(t *testing.T) {
	for _, lines := range [][]string{{"SVPNCOOKIE=; path=/"}, {"SVPNCOOKIE=  ; Max-Age=0"}, {"JSESSIONID=abc"}, nil} {
		if got, ok := cookieReply(lines...).namedCookie(fortiCookieName); ok {
			t.Errorf("%q: cookie = %q", lines, got)
		}
	}
}

func TestACookieMeansAuthenticatedWhateverTheBodySays(t *testing.T) {
	if challenge, err := parseForti("ret=1,redir=/remote/fortisslvpn", "SVPNCOOKIE=abc", http.StatusOK); err != nil || challenge != nil {
		t.Errorf("challenge = %+v, %v", challenge, err)
	}
	// Even on a status that would otherwise be unsupported.
	if challenge, err := parseForti("", "SVPNCOOKIE=abc", http.StatusUnauthorized); err != nil || challenge != nil {
		t.Errorf("challenge = %+v, %v", challenge, err)
	}
}

func TestATokeninfoBodyIsAChallengeAndCarriesWhatMustGoBack(t *testing.T) {
	body := "ret=2,tokeninfo=,grp=Employees,reqid=17,polid=3,portal=web," +
		"peer=1,magic=deadbeef,chal_msg=Enter your token code"
	challenge, err := parseForti(body, "", http.StatusOK)
	if err != nil || challenge == nil {
		t.Fatalf("challenge = %+v, %v", challenge, err)
	}
	if challenge.message != "Enter your token code" || challenge.ftmPush {
		t.Errorf("challenge = %+v", challenge)
	}
	var keys []string
	for _, p := range challenge.parroted {
		keys = append(keys, p.key)
	}
	// Every value the next request has to echo, magic last so the push
	// case can truncate at it, and only the gateway's own bookkeeping.
	if !slices.Equal(keys, []string{"reqid", "polid", "grp", "portal", "peer", "magic"}) {
		t.Errorf("parroted = %q", keys)
	}
}

func TestAnFTMPushTokeninfoIsThePushFlow(t *testing.T) {
	challenge, err := parseForti("ret=2,tokeninfo=ftm_push,reqid=1,magic=m", "", http.StatusOK)
	if err != nil || challenge == nil || !challenge.ftmPush {
		t.Errorf("challenge = %+v, %v", challenge, err)
	}
}

func TestAFortinetRefusalSurfacesTheGatewaysOwnWording(t *testing.T) {
	_, err := parseForti("ret=0,err=Permission denied", "", http.StatusOK)
	if !secrets.DiscreditsPassword(err) || !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("err = %v", err)
	}
	_, err = parseForti("ret=0", "", http.StatusOK)
	if !secrets.DiscreditsPassword(err) || err.Error() != "the gateway refused the credentials" {
		t.Errorf("err = %v", err)
	}
}

func TestAnHTMLFormGatewayIsReportedAsUnsupportedNotAsABadPassword(t *testing.T) {
	// A refusal would blame the user's password and drop the cached one.
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound} {
		_, err := parseForti("<html>…</html>", "", status)
		if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}

func TestTheFirstRoundPostsThePlainLoginForm(t *testing.T) {
	body := fortiRequestBody(fortiAnswers{username: "alice", credential: "p@ss&word"}, nil)
	for _, want := range []string{"username=alice", "credential=p%40ss%26word", "&ajax=1&just_logged_in=1"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q lacks %q", body, want)
		}
	}
}

func TestAChallengeRoundSendsACodeAndEchoesTheGatewaysValues(t *testing.T) {
	challenge := &fortiChallenge{parroted: []pair{{"reqid", "17"}, {"magic", "deadbeef"}}}
	body := fortiRequestBody(fortiAnswers{username: "alice", credential: "123456"}, challenge)
	for _, want := range []string{"code=123456", "reqid=17", "magic=deadbeef"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q lacks %q", body, want)
		}
	}
	if strings.Contains(body, "credential=") || strings.Contains(body, "ftmpush") {
		t.Errorf("body = %q", body)
	}
}

func TestAPushWithNoCodeDropsMagicAndAsksForThePush(t *testing.T) {
	// openconnect's own quirk: this exact combination tells the gateway to
	// send a notification rather than verify a typed code.
	challenge := &fortiChallenge{parroted: []pair{{"reqid", "17"}, {"magic", "deadbeef"}}, ftmPush: true}
	body := fortiRequestBody(fortiAnswers{username: "alice"}, challenge)
	if !strings.HasSuffix(body, "&ftmpush=1") || strings.Contains(body, "magic=") || !strings.Contains(body, "reqid=17") {
		t.Errorf("body = %q", body)
	}
	// With a code typed, it is an ordinary challenge again.
	typed := fortiRequestBody(fortiAnswers{username: "alice", credential: "123456"}, challenge)
	if !strings.Contains(typed, "magic=deadbeef") || strings.Contains(typed, "ftmpush") {
		t.Errorf("typed = %q", typed)
	}
}

func TestAResponseBodySplitsOnCommasNotAmpersands(t *testing.T) {
	fields := parseFortiFields("ret=1,redir=/remote/x,tokeninfo=ftm_push")
	if len(fields) != 3 || fields[0] != (pair{"ret", "1"}) || fields[2] != (pair{"tokeninfo", "ftm_push"}) {
		t.Errorf("fields = %+v", fields)
	}
	// Junk without an = is skipped rather than producing empty keys.
	if got := parseFortiFields("garbage"); len(got) != 0 {
		t.Errorf("fields = %+v", got)
	}
	if got := parseFortiFields(""); len(got) != 0 {
		t.Errorf("fields = %+v", got)
	}
}

// Against the fake FortiGate.

func TestAFullFortinetSignInAnswersTheFormTheChallengeAndComesBackWithACookie(t *testing.T) {
	g := startGateway(t, modeFortinet)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": fakePassword, "code": fakeChallengeAnswer}}
	signed, err := testAttempt(profileFor("mock-fortinet", "fortinet", g.addr), prompter).fortinet()
	if err != nil {
		t.Fatal(err)
	}
	if signed.session != (session{cookie: "SVPNCOOKIE=" + fortiCookie, host: g.addr, gwcert: fakePin}) {
		t.Errorf("session = %+v", signed.session)
	}
	// The password is worth caching; the token code never is.
	if signed.rememberPassword != fakePassword {
		t.Errorf("remember = %q", signed.rememberPassword)
	}
	// The challenge prompt is in the gateway's words, and the username
	// typed on the first round went out again on the second: the fake
	// refuses a challenge reply that does not echo reqid and magic, or
	// that has lost the username.
	shown := prompter.requests()
	if len(shown) != 2 || shown[1].Message != "Enter your token code" || shown[1].Fields[0].Key != "code" {
		t.Errorf("prompts = %+v", shown)
	}
}

func TestAWrongFortinetCodeIsARefusal(t *testing.T) {
	g := startGateway(t, modeFortinet)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": fakePassword, "code": "000000"}}
	_, err := testAttempt(profileFor("mock-fortinet", "fortinet", g.addr), prompter).fortinet()
	if err == nil || !strings.Contains(err.Error(), "Wrong code") {
		t.Errorf("err = %v", err)
	}
}

func TestAWrongFortinetPasswordIsTheGatewaysOwnRefusal(t *testing.T) {
	g := startGateway(t, modeFortinet)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": "wrong"}}
	_, err := testAttempt(profileFor("mock-fortinet", "fortinet", g.addr), prompter).fortinet()
	if !secrets.DiscreditsPassword(err) || !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("err = %v", err)
	}
}

func TestAGlobalProtectGatewayIsNotMistakenForAFortiGate(t *testing.T) {
	// The GlobalProtect fake has no /remote/logincheck, so this has to
	// reach NM as "someone else should try".
	g := startGateway(t, modeForm)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": fakePassword}}
	_, err := testAttempt(profileFor("mock-fortinet", "fortinet", g.addr), prompter).fortinet()
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok {
		t.Errorf("err = %v", err)
	}
}

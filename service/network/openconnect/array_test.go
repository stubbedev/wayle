package openconnect

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/service/network/secrets"
)

func TestTheArraySessionCookieIsMatchedByPrefixAndKeepsItsOwnName(t *testing.T) {
	// The suffix varies, so an exact match would find nothing — and the
	// name openconnect sends back is the full one, suffix included.
	if got, ok := arraySessionCookie(cookieReply("ANsession1234=abc; path=/; secure")); !ok || got != "ANsession1234=abc" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
	if got, ok := arraySessionCookie(cookieReply("ANsession=xyz")); !ok || got != "ANsession=xyz" {
		t.Errorf("cookie = %q, %v", got, ok)
	}
}

func TestACookieThatIsNotAnArraySessionIsNotTaken(t *testing.T) {
	// A name that merely contains the prefix is not one that starts with it.
	for _, lines := range [][]string{{"ANsession1234=; Max-Age=0"}, {"JSESSIONID=abc"}, {"notANsession=abc"}, nil} {
		if got, ok := arraySessionCookie(cookieReply(lines...)); ok {
			t.Errorf("%q: cookie = %q", lines, got)
		}
	}
}

func TestTheLoginBodyUsesArraysOwnFieldNames(t *testing.T) {
	// uname/pwd, not username/password: the other protocols' names would
	// simply be ignored by an Array gateway.
	body := arrayRequestBody("alice", "p@ss&word")
	if body != "method=&uname=alice&pwd=p%40ss%26word" {
		t.Errorf("body = %q", body)
	}
}

// Against the fake Array gateway.

func TestAnArraySignInComesBackWithTheSessionCookieAndThePin(t *testing.T) {
	g := startGateway(t, modeArray)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": fakePassword}}
	signed, err := testAttempt(profileFor("mock-array", "array", g.addr), prompter).array()
	if err != nil {
		t.Fatal(err)
	}
	if signed.session != (session{cookie: "ANsession1234=" + arrayCookie, host: g.addr, gwcert: fakePin}) {
		t.Errorf("session = %+v", signed.session)
	}
	if signed.rememberPassword != fakePassword {
		t.Errorf("remember = %q", signed.rememberPassword)
	}
}

func TestAnArrayProfileWithAStoredPasswordAsksForNothing(t *testing.T) {
	g := startGateway(t, modeArray)
	stateHome(t)
	RememberPassword("mock-array", fakePassword)
	p := profileFor("mock-array", "array", g.addr)
	p.Username = fakeUser
	prompter := &fakePrompter{}
	signed, err := testAttempt(p, prompter).array()
	if err != nil {
		t.Fatal(err)
	}
	if len(prompter.requests()) != 0 {
		t.Errorf("prompts = %+v", prompter.requests())
	}
	// A cached password is not "typed", so there is nothing new to store.
	if signed.rememberPassword != "" {
		t.Errorf("remember = %q", signed.rememberPassword)
	}
}

func TestAWrongArrayPasswordIsARefusalAndNotAnUnsupportedProtocol(t *testing.T) {
	g := startGateway(t, modeArray)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": "wrong"}}
	_, err := testAttempt(profileFor("mock-array", "array", g.addr), prompter).array()
	if !secrets.DiscreditsPassword(err) {
		t.Errorf("err = %v", err)
	}
}

func TestAGlobalProtectGatewayIsNotMistakenForAnArrayOne(t *testing.T) {
	g := startGateway(t, modeForm)
	stateHome(t)
	prompter := &fakePrompter{answers: map[string]string{"username": fakeUser, "password": fakePassword}}
	_, err := testAttempt(profileFor("mock-array", "array", g.addr), prompter).array()
	if _, ok := errors.AsType[*secrets.ProtocolUnsupportedError](err); !ok || !strings.Contains(err.Error(), "404 Not Found") {
		t.Errorf("err = %v", err)
	}
}

func TestStatusTextReadsLikeReqwests(t *testing.T) {
	if got := statusText(http.StatusNotFound); got != "404 Not Found" {
		t.Errorf("status = %q", got)
	}
	if got := statusText(statusBadCredentials); got != "512 <unknown status code>" {
		t.Errorf("status = %q", got)
	}
}

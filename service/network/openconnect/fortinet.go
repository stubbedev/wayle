package openconnect

import (
	"net/http"
	"strings"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// Fortinet SSL VPN sign-in (fortinet.rs). The wire shapes come from
// openconnect's fortinet.c:
//
//  1. POST /remote/logincheck with username, credential, realm and
//     ajax=1&just_logged_in=1, form-urlencoded;
//  2. success is a Set-Cookie: SVPNCOOKIE=…, and that cookie is
//     openconnect's --cookie;
//  3. a 200 with a body of ret=…,tokeninfo=… is a second factor. The
//     reply sends code instead of credential and parrots back
//     reqid,polid,grp,portal,peer,magic from that body untouched;
//     chal_msg= is the prompt to show.
//
// One deliberate quirk carried over: for tokeninfo=ftm_push with an
// empty code, magic is dropped and ftmpush=1 added, which asks the
// gateway to send a mobile push instead of expecting a typed code.
//
// Not verified against a real FortiGate; the fake gateway in the tests
// encodes these assumptions and cannot falsify them.

// fortiCookieName is the cookie a successful sign-in sets, and
// openconnect's --cookie for this protocol.
const fortiCookieName = "SVPNCOOKIE"

// fortiParroted are the values a challenge response carries that the
// next request has to send back. magic is last on purpose: the ftmpush
// case truncates the body at it.
var fortiParroted = []string{"reqid", "polid", "grp", "portal", "peer", "magic"}

// fortiChallenge is a second-factor round.
type fortiChallenge struct {
	// message is what to show the user, "" when the gateway said nothing.
	message string
	// parroted are the key=value pairs to send back untouched, in
	// fortiParroted order.
	parroted []pair
	// ftmPush is the mobile-push flow rather than a typed code.
	ftmPush bool
}

// fortiAnswers is what the user (or the cache, or the profile) supplied
// for one round.
type fortiAnswers struct {
	username string
	// credential is the password on the first round, the second-factor
	// code after that.
	credential string
}

// fortinet signs in and returns the session for the plugin.
func (a *attempt) fortinet() (signIn, error) {
	remember := ""
	var challenge *fortiChallenge
	// Carried across rounds: the challenge posts username too, and the
	// profile may not hold one — it is whatever was typed on the first
	// round. Without this the second post arrives with an empty username
	// and the gateway refuses it as a bad credential.
	username := a.profile.Username

	for range maxChallenges + 1 {
		answers, typed, err := a.fortiAsk(username, challenge)
		if err != nil {
			return signIn{}, err
		}
		username = answers.username
		if challenge == nil && remember == "" {
			remember = typed
		}

		r, err := a.client.do(a.ctx, request{
			method:  http.MethodPost,
			url:     "https://" + a.profile.Gateway + "/remote/logincheck",
			body:    fortiRequestBody(answers, challenge),
			timeout: loginTimeout,
		})
		if err != nil {
			return signIn{}, err
		}
		gwcert, err := r.requirePin()
		if err != nil {
			return signIn{}, err
		}
		cookie, _ := r.namedCookie(fortiCookieName)
		next, err := parseForti(r.body, cookie, r.status)
		if err != nil {
			return signIn{}, err
		}
		if next == nil {
			return signIn{
				session:          session{cookie: cookie, host: a.profile.Gateway, gwcert: gwcert},
				rememberPassword: remember,
			}, nil
		}
		challenge = next
	}
	return signIn{}, authError("the gateway kept asking for more factors")
}

// fortiAsk collects the username and whatever secret this round needs,
// with the typed password when there is one to remember.
func (a *attempt) fortiAsk(username string, challenge *fortiChallenge) (fortiAnswers, string, error) {
	// A challenge round always asks: the code is different every time,
	// and a remembered one is guaranteed stale. The push flow expects an
	// empty code, so a blank answer is not a dismissal.
	if challenge != nil {
		values, err := a.prompt(challenge.message, secrets.Field{Key: "code", Label: "Code", Secret: true})
		if err != nil {
			return fortiAnswers{}, "", err
		}
		return fortiAnswers{username: username, credential: values["code"]}, "", nil
	}
	user, password, remember, err := a.usernamePassword(username)
	if err != nil {
		return fortiAnswers{}, "", err
	}
	return fortiAnswers{username: user, credential: password}, remember, nil
}

// fortiRequestBody is the POST /remote/logincheck body for this round.
func fortiRequestBody(answers fortiAnswers, challenge *fortiChallenge) string {
	if challenge == nil {
		return formEncode(
			pair{"username", answers.username},
			pair{"credential", answers.credential},
			pair{"realm", ""},
		) + "&ajax=1&just_logged_in=1"
	}

	// A challenge: the code replaces the credential, and the gateway's
	// own values go back with it. magic is dropped for a push with no
	// code, which is the signal that asks the gateway to push.
	pairs := []pair{{"username", answers.username}, {"code", answers.credential}, {"realm", ""}}
	push := challenge.ftmPush && answers.credential == ""
	for _, p := range challenge.parroted {
		if push && p.key == "magic" {
			continue
		}
		pairs = append(pairs, p)
	}
	body := formEncode(pairs...)
	if push {
		body += "&ftmpush=1"
	}
	return body
}

// parseForti reads the gateway's answer: done (nil, nil), a challenge,
// or an error. A 401 with an HTML body is openconnect's HTML-form 2FA,
// which wayle does not do; handing that back as a refusal would blame
// the user's password for it, so it is unsupported instead.
func parseForti(body, cookie string, status int) (*fortiChallenge, error) {
	// The cookie settles it whatever else the body says.
	if cookie != "" {
		return nil, nil
	}
	if status == http.StatusUnauthorized {
		return nil, unsupported("this FortiGate wants an HTML sign-in form, which wayle cannot do")
	}
	if status < 200 || status >= 300 {
		return nil, unsupported("the gateway answered " + statusText(status) + " to a Fortinet authentication")
	}

	fields := parseFortiFields(body)
	find := func(key string) (string, bool) {
		for _, f := range fields {
			if f.key == key {
				return f.value, true
			}
		}
		return "", false
	}
	if _, ok := find("tokeninfo"); ok {
		challenge := &fortiChallenge{}
		challenge.message, _ = find("chal_msg")
		for _, wanted := range fortiParroted {
			if value, ok := find(wanted); ok {
				challenge.parroted = append(challenge.parroted, pair{wanted, value})
			}
		}
		for _, f := range fields {
			if f.key == "tokeninfo" && f.value == "ftm_push" {
				challenge.ftmPush = true
			}
		}
		return challenge, nil
	}

	// ret=0 (or any non-1 ret) with no cookie is a refusal, in the
	// gateway's own wording where it gave any.
	for _, f := range fields {
		if f.key == "err" || f.key == "chal_msg" {
			return nil, authError(f.value)
		}
	}
	return nil, authError("the gateway refused the credentials")
}

// parseFortiFields splits a Fortinet response body into its key=value
// pairs. The body is one comma-separated line — ret=1,redir=…,tokeninfo=…
// — so the split is on commas, not the & of a form.
func parseFortiFields(body string) []pair {
	var fields []pair
	for part := range strings.SplitSeq(strings.TrimSpace(body), ",") {
		if key, value, ok := strings.Cut(part, "="); ok {
			fields = append(fields, pair{strings.TrimSpace(key), strings.TrimSpace(value)})
		}
	}
	return fields
}

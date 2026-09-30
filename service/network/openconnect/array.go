package openconnect

import (
	"net/http"
	"strings"
)

// Array Networks sign-in (array.rs), the shortest of the family; the
// shapes come from openconnect's array.c:
//
//  1. POST prx/000/http/localhost/login, form-urlencoded, with method
//     (the auth group), uname and pwd — Array's own field names;
//  2. success is a cookie whose name starts with ANsession, and
//     openconnect's --cookie is that cookie's own name and value. The
//     prefix match is deliberate: the name carries a varying suffix.
//
// There is no challenge round. A gateway that wants a second factor is
// not something this dialect can describe, so it reads as a refusal.
// Not verified against a real Array gateway; the likeliest thing to be
// wrong is method, which wayle sends empty.

// arrayCookiePrefix is the prefix of the cookie a successful sign-in
// sets.
const arrayCookiePrefix = "ANsession"

// array signs in and returns the session for the plugin.
func (a *attempt) array() (signIn, error) {
	username, password, remember, err := a.usernamePassword(a.profile.Username)
	if err != nil {
		return signIn{}, err
	}

	r, err := a.client.do(a.ctx, request{
		method:  http.MethodPost,
		url:     "https://" + a.profile.Gateway + "/prx/000/http/localhost/login",
		body:    arrayRequestBody(username, password),
		timeout: loginTimeout,
	})
	if err != nil {
		return signIn{}, err
	}
	gwcert, err := r.requirePin()
	if err != nil {
		return signIn{}, err
	}
	cookie, found := arraySessionCookie(r)
	if !r.success() && !r.redirection() {
		return signIn{}, unsupported("the gateway answered " + statusText(r.status) + " to an Array authentication")
	}
	if !found {
		return signIn{}, authError("the gateway refused the credentials")
	}
	return signIn{
		session:          session{cookie: cookie, host: a.profile.Gateway, gwcert: gwcert},
		rememberPassword: remember,
	}, nil
}

// arrayRequestBody is the login body: Array's own field names, and an
// empty method when the profile names no auth group.
func arrayRequestBody(username, password string) string {
	return formEncode(pair{"method", ""}, pair{"uname", username}, pair{"pwd", password})
}

// arraySessionCookie is the ANsession…=… cookie the gateway set, with
// its own (suffixed) name.
func arraySessionCookie(r reply) (string, bool) {
	name, value, ok := r.setCookie(func(name, _ string) bool {
		return strings.HasPrefix(name, arrayCookiePrefix)
	})
	if !ok {
		return "", false
	}
	return name + "=" + value, true
}

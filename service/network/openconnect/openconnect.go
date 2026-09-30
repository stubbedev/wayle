// Package openconnect produces the NetworkManager openconnect plugin's
// secrets without an openconnect binary, the Go counterpart of
// crates/wayle-network/src/vpn/openconnect.
//
// NetworkManager's openconnect plugin does not ask for a password. It
// asks for a cookie, a gateway and a gwcert — the output of an
// authentication, not its input. Conventionally an agent gets those by
// spawning the plugin's own nm-openconnect-auth-dialog, which drives
// libopenconnect through the whole sign-in.
//
// wayle authenticates itself instead. For GlobalProtect that is a form
// POST and, when the gateway asks, a second one carrying the 2FA
// answer; for AnyConnect it is an XML exchange in which the gateway
// describes the form it wants filled in; Fortinet, Array and the
// web-login protocols (nc, pulse, f5) are plain HTML form posts. Either
// way it is plain HTTPS with no tunnel involved, which is exactly what
// `openconnect --authenticate` does before handing the cookie to
// something else. The resulting cookie is cached, so a reconnect costs
// no second factor.
//
// The tunnel itself is still NetworkManager's plugin. This package
// replaces the sign-in, not the thing that moves packets.
package openconnect

import (
	"context"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/network/secrets"
)

// ServiceType is the NM service name of the openconnect plugin.
const ServiceType = "org.freedesktop.NetworkManager.openconnect"

// The profile's vpn.data keys wayle adds of its own; the plugin has no
// notion of any of them.
const (
	// UsernameKey is where wayle keeps the sign-in username. The
	// plugin's auth dialog asks every time; wayle stores it in the
	// profile, where it is a setting the user can see and edit rather
	// than hidden state.
	UsernameKey = "wayle-username"
	// SSOKey records that this profile wants the browser sign-in
	// ("yes" or "true"). openconnect's own switch for this is a
	// command-line flag, not a profile setting.
	SSOKey = "wayle-sso"
	// SignInKey lets a profile ask for the plugin's own auth dialog
	// instead of wayle's sign-in: "plugin" opts out; anything else is the
	// default, which is wayle. The escape hatch for a gateway wayle reads
	// wrong — above all one of the web-login protocols, whose pages are
	// per-deployment markup.
	SignInKey = "wayle-signin"
)

const (
	// maxChallenges is how many challenge rounds to follow before giving
	// up. A gateway that keeps asking is misconfigured or hostile; an
	// unbounded loop would keep the prompt on screen forever.
	maxChallenges = 5
	// ssoTimeout is how long to wait for the browser sign-in to come
	// back: the user has to find the browser, sign in to their identity
	// provider and very likely approve a second factor.
	ssoTimeout = 300 * time.Second
	// retryWindow is how long a cookie stays "just handed over" for. NM
	// re-asks within milliseconds when the plugin rejects a secret set,
	// so anything on this scale is a retry rather than a reconnect. See
	// isSpent.
	retryWindow = 120 * time.Second
)

// session is a completed authentication: what the plugin needs to bring
// the tunnel up without signing in again.
type session struct {
	// cookie is the --cookie string openconnect takes, in the protocol's
	// shape: authcookie=…&portal=… for GlobalProtect, webvpn=… for
	// AnyConnect.
	cookie string
	// host is the host the cookie was issued for, handed back as the
	// gateway secret so the plugin connects to the one that
	// authenticated.
	host string
	// gwcert is the pin-sha256: fingerprint of the certificate that host
	// presented while issuing the cookie.
	gwcert string
}

// signIn is a finished sign-in, and whatever of it is worth remembering.
type signIn struct {
	session session
	// rememberPassword is a password the user typed that turned out to
	// work, cached so the next connect only asks for the second factor;
	// "" when there is none to remember.
	rememberPassword string
}

// Profile is the openconnect settings wayle needs out of an NM profile.
type Profile struct {
	UUID     string
	Name     string
	Gateway  string
	Protocol string
	// Username is the stored sign-in username, "" when there is none.
	Username string
	// SSO is whether this profile asked for the browser (SAML) sign-in.
	// Opt-in because advertising the capability is a one-way door per
	// gateway.
	SSO bool
	// PluginSignIn is whether this profile asked to be signed in by the
	// plugin's own auth dialog rather than by wayle.
	PluginSignIn bool
}

// ProfileFrom reads an openconnect profile out of NM's connection
// dictionary (as GetSecrets hands it), or false when this is some other
// kind of VPN.
func ProfileFrom(conn map[string]map[string]dbus.Variant, uuid, name string) (Profile, bool) {
	vpn, ok := conn["vpn"]
	if !ok {
		return Profile{}, false
	}
	serviceType, ok := vpn["service-type"].Value().(string)
	if !ok || serviceType != ServiceType {
		return Profile{}, false
	}
	data, ok := vpn["data"].Value().(map[string]string)
	if !ok {
		return Profile{}, false
	}

	protocol, ok := data["protocol"]
	if !ok {
		// NM omits the key entirely on a profile created before the
		// protocol picker existed, and openconnect's own default is
		// AnyConnect.
		protocol = "anyconnect"
	}
	sso := data[SSOKey]
	return Profile{
		UUID:         uuid,
		Name:         name,
		Gateway:      data["gateway"],
		Protocol:     protocol,
		Username:     data[UsernameKey],
		SSO:          sso == "yes" || sso == "true",
		PluginSignIn: data[SignInKey] == "plugin",
	}, true
}

// Protocol is one protocol the openconnect plugin speaks.
type Protocol struct {
	// Value is what the plugin's --protocol takes.
	Value string
	// Label is its display name.
	Label string
}

// Protocols is every protocol the openconnect plugin speaks, in the
// order the picker offers them (PROTOCOLS).
var Protocols = []Protocol{
	{"gp", "Palo Alto GlobalProtect"},
	{"anyconnect", "Cisco AnyConnect"},
	{"fortinet", "Fortinet"},
	{"array", "Array Networks"},
	{"nc", "Juniper Network Connect"},
	{"pulse", "Pulse Connect Secure"},
	{"f5", "F5 BIG-IP"},
}

// nativeProtocols are the protocols wayle signs into itself.
var nativeProtocols = []string{"gp", "anyconnect", "fortinet", "array", "nc", "pulse", "f5"}

// SignsInNatively reports whether wayle signs into this protocol itself,
// rather than leaving it to the plugin's own auth dialog. The form asks
// this at configuration time, so the answer arrives while the protocol
// is being picked rather than as a failure on the first connect.
func SignsInNatively(protocol string) bool { return slices.Contains(nativeProtocols, protocol) }

// IsSupported reports whether wayle can produce this profile's secrets
// natively.
func IsSupported(p Profile) bool {
	return SignsInNatively(p.Protocol) && p.Gateway != "" && !p.PluginSignIn
}

// DeliverSSOCallback hands a browser callback (a globalprotectcallback:
// URI) to a waiting GlobalProtect SAML sign-in, reporting whether one
// was waiting.
func DeliverSSOCallback(uri string) bool { return deliverGPCallback(uri) }

// handout is the cookie last handed to NM for a profile, and when.
type handout struct {
	cookie string
	at     time.Time
}

// handedOut records what NM was last given per profile. NM does not set
// REQUEST_NEW on every retry — on a rejected secret set it often does
// not set it at all — so without this the same stale cookie is handed
// back on every attempt and the activation loops instead of failing.
var (
	handedMu  sync.Mutex
	handedOut = map[string]handout{}
)

// Authenticate signs into the profile's gateway and returns the secrets
// the openconnect plugin asked for.
//
// requestNew is NM saying the secrets it had were rejected: the cached
// cookie is dropped and the gateway signed into afresh. The stored
// password is kept — NM's secrets are the cookie's derivatives, so their
// rejection says nothing about the password; only the gateway refusing
// the password itself drops it.
//
// unattended is an activation nobody is watching — a tunnel brought
// back after a suspend. It gets the cached session if that is still
// alive, and nothing else: no prompt, and no password posted, since a
// gateway doing push MFA answers that with a push to a phone nobody is
// looking at.
//
// Errors are the secrets package's: a refusal
// (*secrets.AuthenticationFailedError), a sign-in that did not finish
// (*secrets.SignInIncompleteError), or a gateway this cannot follow
// (*secrets.ProtocolUnsupportedError).
func Authenticate(ctx context.Context, p Profile, requestNew, unattended bool, prompter secrets.Prompter) (map[string]string, error) {
	c := newClient()
	if s, ok := reusableSession(ctx, p, requestNew, c); ok {
		return handOut(p.UUID, s), nil
	}
	if unattended {
		log.Printf("vpn: %s: no live VPN session to restore; waiting for a connect", p.Name)
		return nil, incomplete("the VPN session has ended; connect again to sign in")
	}

	a := &attempt{ctx: ctx, profile: p, client: c, prompter: prompter}
	var signed signIn
	var err error
	switch {
	case p.Protocol == "anyconnect":
		signed, err = a.anyconnect()
	case p.Protocol == "fortinet":
		signed, err = a.fortinet()
	case p.Protocol == "array":
		signed, err = a.array()
	case webDialectFor(p.Protocol) != nil:
		// Juniper, Pulse and F5 all sign in through the gateway's own web
		// login.
		signed, err = a.webLogin()
	default:
		signed, err = a.globalProtect()
	}
	if err != nil {
		// A refused sign-in drops the stored password: the likeliest
		// thing that went stale, and keeping it would make every later
		// attempt fail the same way. A sign-in that merely did not finish
		// keeps it.
		if secrets.DiscreditsPassword(err) {
			forgetPassword(p.UUID)
		}
		return nil, err
	}

	storeSession(p.UUID, signed.session)
	if signed.rememberPassword != "" {
		storePassword(p.UUID, signed.rememberPassword)
	}
	log.Printf("vpn: %s: VPN sign-in complete", p.Name)
	return handOut(p.UUID, signed.session), nil
}

// Forget drops everything cached for a profile: the session, the
// password, and the record of what was last handed over. Called when the
// profile is deleted: a session cookie and a password outliving their
// profile is a leak, and a profile recreated under the same name gets a
// new UUID, so nothing would ever collect them.
func Forget(uuid string) {
	forgetSession(uuid)
	forgetPassword(uuid)
	handedMu.Lock()
	delete(handedOut, uuid)
	handedMu.Unlock()
}

// handOut records which cookie NM is being given, so the next request
// for the same profile can tell a rejected cookie from a fresh
// reconnect.
func handOut(uuid string, s session) map[string]string {
	handedMu.Lock()
	handedOut[uuid] = handout{cookie: s.cookie, at: time.Now()}
	handedMu.Unlock()
	return secretsOf(s)
}

// secretsOf is the secrets the openconnect plugin consumes — all three.
// gwcert is not optional: the plugin's own need_secrets counts the key
// as missing until it is present, so a reply without it makes NM report
// "final secrets request failed to provide sufficient secrets" and never
// launch openconnect at all.
func secretsOf(s session) map[string]string {
	return map[string]string{"cookie": s.cookie, "gateway": s.host, "gwcert": s.gwcert}
}

// reusableSession is the cached session to reuse, or false when there
// is none — nothing is cached, or the cached one has stopped working.
func reusableSession(ctx context.Context, p Profile, requestNew bool, c *client) (session, bool) {
	if requestNew {
		forgetSession(p.UUID)
		return session{}, false
	}
	s, ok := cachedSession(p.UUID)
	if !ok {
		return session{}, false
	}
	if nmRefused(p, s) {
		forgetSession(p.UUID)
		return session{}, false
	}
	// GlobalProtect answers the plugin's own getconfig request with
	// either tunnel configuration or a refusal, so the gateway is asked
	// about the cookie before it is handed over. Without this, a cookie
	// that expired overnight fails the first activation silently, NM
	// tears it down without re-asking for secrets, and the user has to
	// click a second time to reach the sign-in they always needed.
	if p.Protocol == "gp" && gatewayRefused(ctx, p, s, c) {
		forgetSession(p.UUID)
		return session{}, false
	}
	log.Printf("vpn: %s: reusing cached VPN session; no sign-in needed", p.Name)
	return s, true
}

// nmRefused reports whether NM itself has just told us the cached
// cookie does not work.
func nmRefused(p Profile, s session) bool {
	handedMu.Lock()
	handed, ok := handedOut[p.UUID]
	handedMu.Unlock()
	var last *handout
	if ok {
		last = &handed
	}
	spent := isSpent(last, s.cookie, time.Now())
	if spent {
		log.Printf("vpn: %s: the cached VPN cookie was just refused; signing in again", p.Name)
	}
	return spent
}

// gatewayRefused asks the gateway whether it still opens a tunnel with
// the cached cookie. A gateway that answers anything but a refusal
// cannot have said the cookie is dead, and one nobody could reach says
// nothing at all: both leave the decision to the plugin's own attempt.
func gatewayRefused(ctx context.Context, p Profile, s session, c *client) bool {
	v, err := gpCookieVerdict(ctx, c, s.host, s.cookie)
	if err != nil {
		return false
	}
	if v == verdictRefuses {
		log.Printf("vpn: %s: the gateway has expired the cached VPN cookie; signing in again", p.Name)
		return true
	}
	return false
}

// isSpent reports whether NM is asking again for a cookie it was given
// moments ago — which only happens when whatever it was given did not
// work. The window is what separates a retry from a legitimate
// reconnect: a tunnel coming back after a suspend asks minutes or hours
// later, and must get the cached cookie rather than a fresh second
// factor.
func isSpent(handed *handout, cookie string, now time.Time) bool {
	return handed != nil && handed.cookie == cookie && now.Sub(handed.at) < retryWindow
}

// attempt is one sign-in in progress: who it is for, what it talks to,
// and who it asks.
type attempt struct {
	// ctx is the lifetime of the NM request this sign-in answers.
	ctx      context.Context
	profile  Profile
	client   *client
	prompter secrets.Prompter
}

// prompt asks the user for fields; a dismissed (or withdrawn) prompt is
// a sign-in that did not finish.
func (a *attempt) prompt(message string, fields ...secrets.Field) (map[string]string, error) {
	values, ok := a.prompter.Prompt(a.ctx, secrets.Request{
		UUID:    a.profile.UUID,
		Name:    a.profile.Name,
		Setting: "vpn",
		Message: message,
		Fields:  fields,
	})
	if !ok {
		return nil, dismissed()
	}
	return values, nil
}

// usernamePassword collects the username and password for a
// form-posting protocol (Fortinet's first round, Array, the web logins):
// nothing is asked when the username is known and a password is cached.
// remember is the password when the user typed a non-empty one.
func (a *attempt) usernamePassword(username string) (user, password, remember string, err error) {
	if stored, ok := cachedPassword(a.profile.UUID); ok && username != "" {
		return username, stored, "", nil
	}
	var fields []secrets.Field
	if username == "" {
		fields = append(fields, secrets.Field{Key: "username", Label: "Username"})
	}
	fields = append(fields, secrets.Field{Key: "password", Label: "Password", Secret: true})
	values, err := a.prompt("", fields...)
	if err != nil {
		return "", "", "", err
	}
	if typed, ok := values["username"]; ok {
		username = typed
	}
	password = values["password"]
	return username, password, password, nil
}

// hostname is the computer field a gateway records the session against.
func hostname() string {
	raw, err := os.ReadFile("/proc/sys/kernel/hostname")
	if name := strings.TrimSpace(string(raw)); err == nil && name != "" {
		return name
	}
	log.Printf("vpn: cannot read the hostname; reporting a placeholder to the VPN gateway")
	return "localhost"
}

func authError(reason string) error { return &secrets.AuthenticationFailedError{Reason: reason} }

func incomplete(reason string) error { return &secrets.SignInIncompleteError{Reason: reason} }

func unsupported(reason string) error { return &secrets.ProtocolUnsupportedError{Reason: reason} }

func dismissed() error { return incomplete("sign-in dismissed") }

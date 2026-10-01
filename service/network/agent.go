package network

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/feed"
	"github.com/stubbedev/wayle/service/network/openconnect"
	"github.com/stubbedev/wayle/service/network/secrets"
)

// This file is wayle as NetworkManager's secret agent (agent/mod.rs).
//
// NM does not store every credential: a VPN password can be marked
// not-saved, a 2FA challenge is new each time, and an OpenConnect
// session cookie is not something a user could type. For those NM asks
// a registered secret agent, and with none registered the activation
// fails with "no secrets". Registering here closes that hole: NM asks
// wayle, wayle asks the user in the network dropdown, and the answer
// goes back over the same call.
//
// Coexistence is deliberate. Anything wayle cannot answer comes back as
// NoSecrets, NM's cue to try the next agent rather than give up, so
// running beside nm-applet degrades to "whoever knows the answer wins".

const (
	// AgentPath is where NM expects the agent object: its agent manager
	// builds its proxy against this exact path.
	AgentPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/SecretAgent")
	// AgentID is the identifier wayle registers under; NM requires it
	// be formatted like a bus name.
	AgentID    = "com.wayle.network"
	agentIface = "org.freedesktop.NetworkManager.SecretAgent"

	// capabilityVPNHints is NM_SECRET_AGENT_CAPABILITY_VPN_HINTS:
	// without it NM never passes the per-key hints a VPN plugin asks
	// with, and every VPN prompt would be a guess.
	capabilityVPNHints uint32 = 0x1
	// flagAllowInteraction is
	// NM_SECRET_AGENT_GET_SECRETS_FLAG_ALLOW_INTERACTION.
	flagAllowInteraction uint32 = 0x1
	// flagRequestNew is NM_SECRET_AGENT_GET_SECRETS_FLAG_REQUEST_NEW:
	// the stored secret was rejected, so asking again is the point.
	flagRequestNew uint32 = 0x2

	// RequestBudget is how long one request is worked on before it is
	// given up. NM waits 120 seconds for an agent's answer (a D-Bus
	// timeout fixed in nm_secret_agent_get_secrets) and then fails the
	// activation without telling the agent: no CancelGetSecrets follows
	// a timeout. Work that ran past it finished for nobody, and a
	// failure it wrote landed on the attempt that came since. This
	// stops short of NM's limit so the answer still reaches someone.
	RequestBudget = 110 * time.Second

	// unattendedWindow is how long a restore's word that nobody is
	// watching holds. NM asks for an activation's secrets within
	// moments of starting it; an older mark belongs to an activation
	// that never got that far and must not turn a later one (from
	// nmcli, say) into one that refuses to sign in.
	unattendedWindow = 30 * time.Second
)

// The errors NM understands from an agent. The names matter: NoSecrets
// is what makes NM move on to the next agent instead of failing.
const (
	errNoSecrets     = agentIface + ".Error.NoSecrets"
	errUserCanceled  = agentIface + ".Error.UserCanceled"
	errAgentCanceled = agentIface + ".Error.AgentCanceled"
)

func agentError(name, message string) *dbus.Error {
	return dbus.NewError(name, []any{message})
}

// AuthFailure is a sign-in that did not produce secrets, and why: the
// gateway's own wording where it gave any.
type AuthFailure struct {
	// UUID of the profile that failed to authenticate.
	UUID string
	// Reason is shown on the VPN row verbatim.
	Reason string
}

// SecretPrompt is the agent's face toward the UI: the credential
// prompt NM is blocked on, and the two ways to answer it.
type SecretPrompt interface {
	// Request is the prompt waiting on the user, if any.
	Request() (secrets.Request, bool)
	// Submit answers the pending prompt; with none waiting it is
	// dropped.
	Submit(values map[string]string)
	// Cancel dismisses the pending prompt, failing the activation.
	Cancel()
	// Changes ticks when a prompt goes up or comes down.
	Changes(ctx context.Context) <-chan struct{}
}

// Agent is the pending-prompt state shared by the D-Bus object and the
// UI (SecretAgentState). It implements secrets.Prompter for the VPN
// sign-ins that run inside a request, and SecretPrompt for the UI.
type Agent struct {
	mu sync.Mutex
	// request is the prompt on screen; responder is its answer
	// channel, nil when no prompt is up.
	request   *secrets.Request
	responder chan map[string]string
	// failure is the last sign-in failure. NM only ever reports
	// "credentials not provided" for these, which tells the user
	// nothing; the gateway's wording is published here instead.
	failure *AuthFailure
	// inFlight are the requests being worked on, keyed the way
	// CancelGetSecrets names them, so a withdrawn request stops its own
	// work and nobody else's.
	inFlight map[requestKey]inFlight
	nextID   uint64
	// unattended are profiles whose next activation has nobody
	// watching it, and when that was said.
	unattended map[string]time.Time

	// turn serializes prompts: two VPNs activating at once would
	// otherwise race to own the single visible form, and the loser's
	// request would hang until NM timed it out.
	turn chan struct{}

	changes  feed.Tick
	failures feed.Tick
}

type requestKey struct {
	path    dbus.ObjectPath
	setting string
}

type inFlight struct {
	id     uint64
	cancel context.CancelFunc
}

// NewAgent returns an agent state with nothing pending.
func NewAgent() *Agent {
	return &Agent{
		inFlight:   make(map[requestKey]inFlight),
		unattended: make(map[string]time.Time),
		turn:       make(chan struct{}, 1),
	}
}

var (
	_ secrets.Prompter = (*Agent)(nil)
	_ SecretPrompt     = (*Agent)(nil)
)

// Request implements SecretPrompt.
func (a *Agent) Request() (secrets.Request, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.request == nil {
		return secrets.Request{}, false
	}
	return *a.request, true
}

// Changes implements SecretPrompt.
func (a *Agent) Changes(ctx context.Context) <-chan struct{} { return a.changes.SubscribeContext(ctx) }

// Failure is the last sign-in failure, if one is standing.
func (a *Agent) Failure() (AuthFailure, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failure == nil {
		return AuthFailure{}, false
	}
	return *a.failure, true
}

// Failures ticks when a failure is published or cleared.
func (a *Agent) Failures(ctx context.Context) <-chan struct{} {
	return a.failures.SubscribeContext(ctx)
}

func (a *Agent) setFailure(f *AuthFailure) {
	a.mu.Lock()
	a.failure = f
	a.mu.Unlock()
	feed.Notify(&a.failures)
}

// Submit implements SecretPrompt.
func (a *Agent) Submit(values map[string]string) { a.answer(maps.Clone(values)) }

// Cancel implements SecretPrompt.
func (a *Agent) Cancel() { a.answer(nil) }

// answer delivers a reply (nil for a cancel) and takes the prompt down.
func (a *Agent) answer(values map[string]string) {
	a.mu.Lock()
	responder := a.responder
	a.responder = nil
	a.request = nil
	a.mu.Unlock()
	feed.Notify(&a.changes)
	if responder != nil {
		responder <- values
	}
}

// Prompt implements secrets.Prompter: it publishes req and waits for
// the user, holding the prompt turn for the whole exchange so only one
// form is ever live. The wait also ends when ctx does (the request was
// withdrawn or ran out of time), and the form comes down with it: left
// up, it would take an answer nobody is waiting for and hold the turn
// every later prompt queues on.
func (a *Agent) Prompt(ctx context.Context, req secrets.Request) (map[string]string, bool) {
	select {
	case a.turn <- struct{}{}:
	case <-ctx.Done():
		return nil, false
	}
	defer func() { <-a.turn }()

	responder := make(chan map[string]string, 1)
	shown := req
	a.mu.Lock()
	a.responder = responder
	a.request = &shown
	a.mu.Unlock()
	feed.Notify(&a.changes)
	defer func() {
		a.mu.Lock()
		taken := a.responder != responder
		if !taken {
			a.responder = nil
			a.request = nil
		}
		a.mu.Unlock()
		if !taken {
			feed.Notify(&a.changes)
		}
	}()

	select {
	case values := <-responder:
		return values, values != nil
	case <-ctx.Done():
		return nil, false
	}
}

// ExpectUnattended says the next activation of this profile is made
// with nobody watching: a tunnel brought back after a suspend, or after
// NM restarted. Its sign-in may reuse a session that is still alive and
// nothing else; asking for a password, or pushing a second factor to a
// phone nobody is looking at, is what made the first connect of the day
// hang.
func (a *Agent) ExpectUnattended(uuid string) { a.expectUnattendedAt(uuid, time.Now()) }

func (a *Agent) expectUnattendedAt(uuid string, at time.Time) {
	a.mu.Lock()
	a.unattended[uuid] = at
	a.mu.Unlock()
}

// ExpectAttended takes ExpectUnattended back: someone is here and
// asked.
func (a *Agent) ExpectAttended(uuid string) {
	a.mu.Lock()
	delete(a.unattended, uuid)
	a.mu.Unlock()
}

// takeUnattended reports whether this profile's request is the
// unattended one. The mark is used up either way: it speaks for one
// activation, not for every request after it.
func (a *Agent) takeUnattended(uuid string, now time.Time) bool {
	a.mu.Lock()
	at, ok := a.unattended[uuid]
	delete(a.unattended, uuid)
	a.mu.Unlock()
	return ok && now.Sub(at) <= unattendedWindow
}

// begin registers a request as in flight until done is called, and
// returns the context its withdrawal cancels. A request for a
// connection and setting already in flight supersedes the older one:
// NM has stopped listening to that one, or it would not be asking
// again.
func (a *Agent) begin(path dbus.ObjectPath, setting string) (ctx context.Context, done func()) {
	key := requestKey{path: path, setting: setting}
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.nextID++
	id := a.nextID
	older, superseded := a.inFlight[key]
	a.inFlight[key] = inFlight{id: id, cancel: cancel}
	a.mu.Unlock()
	if superseded {
		older.cancel()
	}
	return ctx, func() {
		cancel()
		// Only its own entry: a newer request under the same key has
		// taken the slot and is not finished just because this one is.
		a.mu.Lock()
		if current, ok := a.inFlight[key]; ok && current.id == id {
			delete(a.inFlight, key)
		}
		a.mu.Unlock()
	}
}

// withdraw stops the request NM has withdrawn, wherever it has got to.
func (a *Agent) withdraw(path dbus.ObjectPath, setting string) {
	key := requestKey{path: path, setting: setting}
	a.mu.Lock()
	entry, ok := a.inFlight[key]
	delete(a.inFlight, key)
	a.mu.Unlock()
	if ok {
		entry.cancel()
	}
}

// agentObject is the D-Bus object NM calls. Its exported methods are
// the org.freedesktop.NetworkManager.SecretAgent interface and nothing
// else: godbus exports every exported method.
type agentObject struct {
	agent  *Agent
	signIn SignIn
	// budget is RequestBudget, short of NM's own timeout; tests
	// shorten it.
	budget time.Duration
}

// GetSecrets answers NM's request for credentials it does not have.
func (o *agentObject) GetSecrets(connection ConnectionDict, connectionPath dbus.ObjectPath, settingName string, hints []string, flags uint32) (ConnectionDict, *dbus.Error) {
	// Without interaction there is nothing an interactive agent can
	// add. Saying so at once lets NM get on with retrying rather than
	// wait on a prompt that may not appear.
	if flags&flagAllowInteraction == 0 {
		return nil, agentError(errNoSecrets, "interaction not allowed")
	}
	// Withdrawn, the work is dropped wherever it has got to: a sign-in
	// stops waiting on the gateway, a prompt comes down, and nothing is
	// reported, because nobody is asking any more.
	ctx, done := o.agent.begin(connectionPath, settingName)
	defer done()
	reply, err := o.secretsFor(ctx, connection, settingName, hints, flags)
	if ctx.Err() != nil {
		return nil, agentError(errAgentCanceled, "withdrawn by NetworkManager")
	}
	return reply, err
}

// CancelGetSecrets is NM giving up on a request: that request stops,
// and only that one.
func (o *agentObject) CancelGetSecrets(connectionPath dbus.ObjectPath, settingName string) *dbus.Error {
	o.agent.withdraw(connectionPath, settingName)
	return nil
}

// SaveSecrets asks agents to persist secrets they own. wayle owns
// none: everything it collects goes back for NM's own store to keep.
func (o *agentObject) SaveSecrets(ConnectionDict, dbus.ObjectPath) *dbus.Error { return nil }

// DeleteSecrets is the mirror of SaveSecrets, equally a no-op.
func (o *agentObject) DeleteSecrets(ConnectionDict, dbus.ObjectPath) *dbus.Error { return nil }

// secretsFor answers one request: a sign-in for an openconnect VPN, a
// prompt for everything else.
func (o *agentObject) secretsFor(ctx context.Context, connection ConnectionDict, setting string, hints []string, flags uint32) (ConnectionDict, *dbus.Error) {
	r := dictReader(connection)
	uuid, name := r.str("connection", "uuid"), r.str("connection", "id")
	unattended := o.agent.takeUnattended(uuid, time.Now())

	if setting == "vpn" && o.signIn != nil {
		if vpn, ok := o.signIn.ProfileFrom(connection, uuid, name); ok {
			return o.vpnSecrets(ctx, vpn, setting, flags, unattended)
		}
	}

	// A form nobody is there to fill in would sit on screen until the
	// budget ran out, and fail the activation just the same.
	if unattended {
		return nil, agentError(errUserCanceled, "nobody was there to answer; connect again to sign in")
	}
	fields := fieldsFor(setting, hints)
	if fields == nil {
		return nil, agentError(errNoSecrets, "nothing to ask for "+setting)
	}
	req := secrets.Request{UUID: uuid, Name: name, Setting: setting, Fields: fields}
	if flags&flagRequestNew != 0 {
		log.Printf("network: %s: previous credentials rejected, asking again", name)
	}
	budget, cancel := context.WithTimeout(ctx, o.budget)
	defer cancel()
	values, ok := o.agent.Prompt(budget, req)
	switch {
	case ok:
		return replyMap(setting, values), nil
	case errors.Is(budget.Err(), context.DeadlineExceeded):
		return nil, agentError(errUserCanceled, "no answer in time")
	default:
		return nil, agentError(errUserCanceled, "dismissed")
	}
}

// vpnSecrets signs in to an openconnect VPN and answers with the
// plugin's secrets. The three outcomes are three different things to
// tell NM: NoSecrets passes the request on, UserCanceled fails the
// activation with a reason the row shows, and a success clears the
// row's failure.
func (o *agentObject) vpnSecrets(ctx context.Context, vpn openconnect.Profile, setting string, flags uint32, unattended bool) (ConnectionDict, *dbus.Error) {
	if !o.signIn.IsSupported(vpn) {
		return nil, agentError(errNoSecrets, "no native sign-in for openconnect protocol "+vpn.Protocol)
	}
	budget, cancel := context.WithTimeout(ctx, o.budget)
	defer cancel()
	type outcome struct {
		values map[string]string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		values, err := o.signIn.Authenticate(budget, vpn, flags&flagRequestNew != 0, unattended, o.agent)
		done <- outcome{values: values, err: err}
	}()
	var res outcome
	select {
	case res = <-done:
	case <-budget.Done():
		res.err = &secrets.SignInIncompleteError{Reason: "the sign-in did not finish in the time NetworkManager waits for one"}
	}
	if ctx.Err() != nil {
		// Withdrawn: whatever the sign-in came to, nobody is asking.
		return nil, nil
	}
	if res.err == nil {
		o.agent.setFailure(nil)
		return replyMap(setting, res.values), nil
	}
	// A gateway wayle could not follow is not a failed sign-in:
	// NoSecrets hands the request to the plugin's own auth dialog, so
	// claiming a protocol natively never leaves a VPN worse off.
	if _, unsupported := errors.AsType[*secrets.ProtocolUnsupportedError](res.err); unsupported {
		log.Printf("network: %s: VPN sign-in not understood; leaving it to another agent: %v", vpn.Name, res.err)
		return nil, agentError(errNoSecrets, res.err.Error())
	}
	log.Printf("network: %s: VPN sign-in failed: %v", vpn.Name, res.err)
	o.agent.setFailure(&AuthFailure{UUID: vpn.UUID, Reason: res.err.Error()})
	return nil, agentError(errUserCanceled, res.err.Error())
}

// replyMap shapes the answer the way the setting expects. A VPN's
// secrets nest one level deeper, in the vpn setting's own "secrets"
// dict; flat, they read to NM as "the agent returned nothing".
func replyMap(setting string, values map[string]string) ConnectionDict {
	section := make(map[string]dbus.Variant)
	if setting == "vpn" {
		nested := maps.Clone(values)
		if nested == nil {
			nested = map[string]string{}
		}
		section["secrets"] = dbus.MakeVariant(nested)
	} else {
		for key, value := range values {
			section[key] = dbus.MakeVariant(value)
		}
	}
	return ConnectionDict{setting: section}
}

// ServeAgent exports the agent object on conn and registers it with
// NetworkManager, re-registering whenever NM comes back (the
// registration lives in NM's process, so a restart silently drops it,
// and a silently unregistered agent looks exactly like a VPN that asks
// for nothing and fails). A failure to register is logged, not fatal:
// wayle still works without answering secrets, and NM may not be up
// yet. The watch ends with ctx.
func ServeAgent(ctx context.Context, conn *dbus.Conn, agent *Agent, signIn SignIn) error {
	return serveAgent(ctx, conn, agent, signIn, RequestBudget)
}

func serveAgent(ctx context.Context, conn *dbus.Conn, agent *Agent, signIn SignIn, budget time.Duration) error {
	obj := &agentObject{agent: agent, signIn: signIn, budget: budget}
	if err := conn.Export(obj, AgentPath, agentIface); err != nil {
		return fmt.Errorf("network: export secret agent: %w", err)
	}
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	if err := conn.AddMatchSignalContext(ctx,
		dbus.WithMatchInterface(busName), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, nmName),
	); err != nil {
		conn.RemoveSignal(signals)
		return fmt.Errorf("network: watch NetworkManager: %w", err)
	}
	register(ctx, conn)
	go func() {
		defer conn.RemoveSignal(signals)
		for {
			select {
			case <-ctx.Done():
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				// Only a new owner is interesting; NM going away takes
				// the registration with it and there is nothing to redo.
				if _, owner, isNM := nameOwnerChange(sig, nmName); isNM && owner != "" {
					register(ctx, conn)
				}
			}
		}
	}()
	return nil
}

// register announces the agent to NM's agent manager.
func register(ctx context.Context, conn *dbus.Conn) {
	err := conn.Object(nmName, agentManagerPath).CallWithContext(ctx,
		agentManagerIface+".RegisterWithCapabilities", 0, AgentID, capabilityVPNHints).Err
	if err != nil {
		log.Printf("network: cannot register as secret agent; VPN prompts will not appear: %v", err)
		return
	}
	log.Printf("network: registered as NetworkManager secret agent")
}

// nameOwnerChange reads a NameOwnerChanged signal about name: the old
// and new owners ("" for none), and whether the signal is that one.
func nameOwnerChange(sig *dbus.Signal, name string) (oldOwner, newOwner string, ok bool) {
	if sig.Name != busName+".NameOwnerChanged" || len(sig.Body) != 3 {
		return "", "", false
	}
	if n, _ := sig.Body[0].(string); n != name {
		return "", "", false
	}
	oldOwner, _ = sig.Body[1].(string)
	newOwner, _ = sig.Body[2].(string)
	return oldOwner, newOwner, true
}

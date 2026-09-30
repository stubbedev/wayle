package network

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// This file is VPN state and control with NetworkManager as the single
// source of truth (vpn/mod.rs). Every VPN is an NM profile, a plugin
// VPN or a native WireGuard one; nothing is declared in wayle's config,
// so a VPN added from the widget shows up with no restart.
//
// Neither watcher polls: the list rides Settings (NM's NewConnection /
// ConnectionRemoved), and state rides NM's active-connection list plus
// each active VPN's StateChanged, because activating -> activated never
// changes the list it is in. Tunnels that were up when the machine
// slept are brought back on wake (vpn_resume.go). Credentials are the
// secret agent's business (agent.go).

// VPNState is one VPN's state as the bar and the dropdown show it.
type VPNState int

// VPN states.
const (
	// VPNDisconnected is down with nothing in flight.
	VPNDisconnected VPNState = iota
	// VPNConnecting is an attempt in flight: NM negotiating, or the
	// plugin waiting on credentials.
	VPNConnecting
	// VPNConnected is the tunnel up.
	VPNConnected
	// VPNFailed is the last attempt failed; the row shows why.
	VPNFailed
)

// IsConnected reports whether the state counts as "a VPN is up".
func (s VPNState) IsConnected() bool { return s == VPNConnected }

// IsConnecting reports whether an attempt is in flight.
func (s VPNState) IsConnecting() bool { return s == VPNConnecting }

// isUp is connected or on its way.
func (s VPNState) isUp() bool { return s.IsConnected() || s.IsConnecting() }

// String names the state.
func (s VPNState) String() string {
	switch s {
	case VPNConnecting:
		return "connecting"
	case VPNConnected:
		return "connected"
	case VPNFailed:
		return "failed"
	}
	return "disconnected"
}

// VPN is one profile's row: identity, live state, and why the last
// attempt failed.
type VPN struct {
	// UUID is NM's profile UUID, stable across renames.
	UUID string
	// Name is the profile's id.
	Name string
	// WireGuard is a native WireGuard profile rather than a plugin VPN.
	WireGuard bool
	// State is the live state.
	State VPNState
	// Detail is why the last attempt failed, "" when it did not.
	Detail string
}

// vpnEntry is a row's live state. Entries survive rebuilds by UUID, so
// an add or remove elsewhere in the list leaves a connected tunnel's
// row, and anything waiting on it, untouched.
type vpnEntry struct {
	uuid      string
	path      dbus.ObjectPath
	name      string
	wireguard bool
	state     VPNState
	detail    string
	// wentDownAt is when the tunnel last stopped being up, zero if it
	// has come up since: what finds the tunnels an NM restart took.
	wentDownAt time.Time
	// turnedOff is a tunnel someone turned off (from any NM client),
	// which is never brought back on its own.
	turnedOff bool
	// toggle serializes toggles so a double-click can't start and stop
	// at once.
	toggle sync.Mutex
}

func (e *vpnEntry) row() VPN {
	return VPN{UUID: e.uuid, Name: e.name, WireGuard: e.wireguard, State: e.state, Detail: e.detail}
}

// VPNService is every VPN NetworkManager knows about plus the aggregate
// the bar icon reads (VpnService).
type VPNService struct {
	nm       nm
	settings *Settings
	agent    *Agent
	signIn   SignIn
	// owner is whose the profiles made here are, "" when the passwd
	// database has no name for this user (profiles are then
	// system-wide).
	owner      string
	pluginDirs []string
	timing     resumeTiming

	mu      sync.Mutex
	entries []*vpnEntry
	// watched maps the active-connection objects of the current sweep
	// to their rows: StateChanged on any other object is stale.
	watched map[dbus.ObjectPath]*vpnEntry
	// managerState is NM's last reported State.
	managerState State
	changes      feed
	stateChanges feed
}

// newVPNService builds the rows from NM's current profiles and starts
// watching until ctx ends.
func newVPNService(ctx context.Context, n nm, settings *Settings, agent *Agent, signIn SignIn, timing resumeTiming) (*VPNService, error) {
	s := &VPNService{
		nm:         n,
		settings:   settings,
		agent:      agent,
		signIn:     signIn,
		owner:      ownerName(),
		pluginDirs: pluginDirectories,
		timing:     timing,
		watched:    make(map[dbus.ObjectPath]*vpnEntry),
	}
	signals := make(chan *dbus.Signal, 64)
	n.conn.Signal(signals)
	for _, match := range [][]dbus.MatchOption{
		{dbus.WithMatchObjectPath(nmPath), dbus.WithMatchInterface(properties), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchArg(0, nmIface)},
		{dbus.WithMatchInterface(activeIface), dbus.WithMatchMember("StateChanged")},
		{dbus.WithMatchInterface(busName), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, nmName)},
		{dbus.WithMatchObjectPath(login1Path), dbus.WithMatchInterface(login1Iface), dbus.WithMatchMember("PrepareForSleep")},
	} {
		if err := n.conn.AddMatchSignalContext(ctx, match...); err != nil {
			n.conn.RemoveSignal(signals)
			return nil, fmt.Errorf("network: watch VPNs: %w", err)
		}
	}
	s.managerState = n.managerState(ctx)
	profiles := settings.Changes(ctx)
	failures := agent.Failures(ctx)
	s.rebuild()
	s.resync(ctx)
	go s.run(ctx, signals, profiles, failures)
	return s, nil
}

// Entries snapshots the rows in NM's profile order.
func (s *VPNService) Entries() []VPN {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]VPN, 0, len(s.entries))
	for _, e := range s.entries {
		rows = append(rows, e.row())
	}
	return rows
}

// Get is the row with this UUID.
func (s *VPNService) Get(uuid string) (VPN, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(uuid); e != nil {
		return e.row(), true
	}
	return VPN{}, false
}

// IsEmpty reports whether NM knows of no VPN at all (vpn-show = auto).
func (s *VPNService) IsEmpty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries) == 0
}

// Aggregate is the state the module icon shows: connected beats
// connecting beats failed beats disconnected, so one connected VPN
// lights the indicator while another negotiates.
func (s *VPNService) Aggregate() VPNState {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := make([]VPNState, 0, len(s.entries))
	for _, e := range s.entries {
		states = append(states, e.state)
	}
	return foldStates(states)
}

// Changes ticks whenever a row or the list changes.
func (s *VPNService) Changes(ctx context.Context) <-chan struct{} { return s.changes.Subscribe(ctx) }

func foldStates(states []VPNState) VPNState {
	result := VPNDisconnected
	for _, state := range states {
		switch {
		case state == VPNConnected:
			return VPNConnected
		case state == VPNConnecting:
			result = VPNConnecting
		case state == VPNFailed && result == VPNDisconnected:
			result = VPNFailed
		}
	}
	return result
}

func (s *VPNService) entryLocked(uuid string) *vpnEntry {
	for _, e := range s.entries {
		if e.uuid == uuid {
			return e
		}
	}
	return nil
}

func (s *VPNService) entry(uuid string) (*vpnEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entryLocked(uuid); e != nil {
		return e, nil
	}
	return nil, errNoProfile(uuid)
}

// errNoProfile is the Rust service's wording for a UUID that names no
// profile (Error::ServiceInitializationFailed).
func errNoProfile(uuid string) error {
	return fmt.Errorf("cannot initialize network service: no VPN profile with uuid %s", uuid)
}

// update mutates a row under the lock and announces it.
func (s *VPNService) update(e *vpnEntry, fn func(e *vpnEntry)) {
	s.mu.Lock()
	fn(e)
	s.mu.Unlock()
	s.changes.notify()
}

// Connect brings the tunnel up if it is not already. The connecting
// state is read back off the active connection NM creates, not
// guessed. A refusal (a missing plugin, a polkit denial, a profile NM
// cannot make sense of) is also published on the row, where the user
// is looking when it fails.
func (s *VPNService) Connect(ctx context.Context, uuid string) error {
	return s.activate(ctx, uuid, false)
}

// restore brings the tunnel back with nobody watching: only a session
// still alive is reused, and a sign-in that would need someone is left
// for the next connect.
func (s *VPNService) restore(ctx context.Context, uuid string) error {
	return s.activate(ctx, uuid, true)
}

func (s *VPNService) activate(ctx context.Context, uuid string, unattended bool) error {
	e, err := s.entry(uuid)
	if err != nil {
		return err
	}
	e.toggle.Lock()
	defer e.toggle.Unlock()
	// Whatever an earlier restore said about this profile's next
	// sign-in, this activation decides it.
	s.agent.ExpectAttended(uuid)
	s.mu.Lock()
	up, path := e.state.IsConnected(), e.path
	s.mu.Unlock()
	if up {
		return nil
	}
	s.update(e, func(e *vpnEntry) { e.turnedOff, e.detail = false, "" })
	// Said before NM is asked: it can want the secrets before the call
	// that starts the activation has returned.
	if unattended {
		s.agent.ExpectUnattended(uuid)
	}
	active, err := s.nm.activate(ctx, path, rootPath, rootPath)
	if err != nil {
		// Refused outright, so no sign-in is coming to use the mark.
		s.agent.ExpectAttended(uuid)
		err = fmt.Errorf("dbus operation failed: %w", err)
		s.update(e, func(e *vpnEntry) { e.state, e.detail = VPNFailed, err.Error() })
		return err
	}
	state := vpnStateOf(s.nm.activeState(ctx, active))
	s.update(e, func(e *vpnEntry) { e.state = state })
	return nil
}

// Disconnect tears the tunnel down; already down is not an error. An
// openconnect session ends with it: the plugin stops openconnect with
// the signal that logs off, and the next connect signs in afresh.
func (s *VPNService) Disconnect(ctx context.Context, uuid string) error {
	e, err := s.entry(uuid)
	if err != nil {
		return err
	}
	e.toggle.Lock()
	defer e.toggle.Unlock()
	s.update(e, func(e *vpnEntry) { e.turnedOff = true })
	// A restore cut short here never reaches its sign-in to use the
	// mark.
	s.agent.ExpectAttended(uuid)
	active, err := s.nm.activeByUUID(ctx)
	if err != nil {
		return fmt.Errorf("dbus operation failed: %w", err)
	}
	path, ok := active[uuid]
	if !ok {
		return nil
	}
	if err := s.nm.deactivate(ctx, path); err != nil {
		return fmt.Errorf("dbus operation failed: %w", err)
	}
	return nil
}

// Toggle connects when down, and disconnects when up or connecting
// (clicking a connecting row cancels, which is a disconnect).
func (s *VPNService) Toggle(ctx context.Context, uuid string) error {
	row, ok := s.Get(uuid)
	if !ok {
		return errNoProfile(uuid)
	}
	if row.State == VPNDisconnected || row.State == VPNFailed {
		return s.Connect(ctx, uuid)
	}
	return s.Disconnect(ctx, uuid)
}

// Add creates a profile from a kind and form values. It appears in the
// rows on its own once NM announces it. It is the running user's own
// when NM can run it that way (canBePrivate) and system-wide otherwise.
func (s *VPNService) Add(ctx context.Context, kind, name string, values map[string]string) error {
	dict := BuildProfile(kind, name, newUUID(), values)
	s.own(kind, dict)
	_, err := s.settings.Add(ctx, dict)
	return err
}

// Update rewrites a profile in place, keeping its UUID so anything
// referring to it (a cached session included) still matches. Saved
// from here it becomes the user's own when a new profile would be.
func (s *VPNService) Update(ctx context.Context, uuid, kind, name string, values map[string]string) error {
	p, ok := s.settings.byUUID(uuid)
	if !ok {
		return errNoProfile(uuid)
	}
	dict := BuildProfile(kind, name, uuid, values)
	s.own(kind, dict)
	return s.settings.Update(ctx, p.Path, dict)
}

func (s *VPNService) own(kind string, dict ConnectionDict) {
	if s.owner != "" && canBePrivate(kind, s.pluginDirs) {
		ownedBy(dict, s.owner)
	}
}

// Remove deletes a profile and whatever wayle cached for it: leaving a
// cached cookie or password behind would keep a live credential on disk
// for a profile that no longer exists. Nothing is forgotten when NM
// refuses: the profile is still there.
func (s *VPNService) Remove(ctx context.Context, uuid string) error {
	p, ok := s.settings.byUUID(uuid)
	if !ok {
		return errNoProfile(uuid)
	}
	if err := s.settings.Delete(ctx, p.Path); err != nil {
		return err
	}
	if s.signIn != nil {
		s.signIn.Forget(uuid)
	}
	return nil
}

// SettingsOf reads a profile's saved settings, for prefilling the edit
// form.
func (s *VPNService) SettingsOf(ctx context.Context, uuid string) (ConnectionDict, error) {
	p, ok := s.settings.byUUID(uuid)
	if !ok {
		return nil, errNoProfile(uuid)
	}
	return s.settings.Get(ctx, p.Path)
}

// DeliverSSOCallback hands a globalprotectcallback: URI to a waiting
// GlobalProtect browser sign-in, reporting whether one was waiting; a
// callback with nothing waiting is stale and dropped.
func (s *VPNService) DeliverSSOCallback(uri string) bool {
	return s.signIn != nil && s.signIn.DeliverSSOCallback(uri)
}

// rebuild derives the rows from the saved profiles, reusing the entry
// of any profile already listed.
func (s *VPNService) rebuild() {
	profiles := s.settings.Profiles()
	s.mu.Lock()
	previous := s.entries
	next := make([]*vpnEntry, 0, len(profiles))
	for _, p := range profiles {
		if p.Type != "vpn" && p.Type != WireGuard {
			continue
		}
		i := slices.IndexFunc(previous, func(e *vpnEntry) bool { return e.uuid == p.UUID })
		e := &vpnEntry{uuid: p.UUID}
		if i >= 0 {
			e = previous[i]
		}
		e.path, e.name, e.wireguard = p.Path, p.ID, p.Type == WireGuard
		next = append(next, e)
	}
	s.entries = next
	s.mu.Unlock()
	s.changes.notify()
}

// resync reads every row's state straight off NM and records which
// active objects' transitions to follow.
func (s *VPNService) resync(ctx context.Context) {
	active, err := s.nm.activeByUUID(ctx)
	if err != nil {
		return
	}
	states := make(map[string]ActiveState, len(active))
	for uuid, path := range active {
		states[uuid] = s.nm.activeState(ctx, path)
	}
	now := time.Now()
	s.mu.Lock()
	s.watched = make(map[dbus.ObjectPath]*vpnEntry)
	for _, e := range s.entries {
		if path, ok := active[e.uuid]; ok {
			e.state = vpnStateOf(states[e.uuid])
			s.watched[path] = e
			continue
		}
		// A failed attempt keeps its state and its reason: NM has torn
		// the object down by the time anyone reads the row, and
		// "disconnected" with no explanation is exactly what the user
		// needed the reason for.
		if e.state != VPNFailed {
			// This can land before the tunnel's own last state change,
			// so it stamps the way down too.
			e.wentDownAt = nextWentDownAt(e.state, VPNDisconnected, e.wentDownAt, now)
			e.state, e.detail = VPNDisconnected, ""
		}
	}
	s.mu.Unlock()
	s.changes.notify()
}

// stateChanged follows one active connection's StateChanged.
func (s *VPNService) stateChanged(path dbus.ObjectPath, state, reason uint32) {
	now := time.Now()
	s.mu.Lock()
	e := s.watched[path]
	if e == nil {
		s.mu.Unlock()
		return
	}
	next, detail := resolveState(ActiveState(state), ActiveReason(reason))
	e.wentDownAt = nextWentDownAt(e.state, next, e.wentDownAt, now)
	e.turnedOff = nextTurnedOff(e.turnedOff, next, ActiveReason(reason))
	e.state = next
	e.detail = mergeDetail(e.detail, detail)
	s.mu.Unlock()
	s.changes.notify()
}

// applyFailure copies a sign-in failure onto its row. It lands before
// NM reports the activation failed (the agent publishes the moment it
// gives up, which is what makes NM give up), and resolveState's generic
// reason then defers to it.
func (s *VPNService) applyFailure() {
	failure, ok := s.agent.Failure()
	if !ok {
		return
	}
	s.mu.Lock()
	e := s.entryLocked(failure.UUID)
	if e != nil {
		e.state, e.detail = VPNFailed, failure.Reason
	}
	s.mu.Unlock()
	if e != nil {
		s.changes.notify()
	}
}

// run is the service's one event loop: profile churn, the active list,
// per-connection transitions, sign-in failures, and the two resume
// triggers.
func (s *VPNService) run(ctx context.Context, signals chan *dbus.Signal, profiles, failures <-chan struct{}) {
	defer s.nm.conn.RemoveSignal(signals)
	sleep := &sleepResume{vpn: s}
	restart := &restartResume{vpn: s}
	for {
		select {
		case <-ctx.Done():
			sleep.cancel()
			restart.cancel()
			return
		case <-profiles:
			s.rebuild()
			s.resync(ctx)
		case <-failures:
			s.applyFailure()
		case sig, ok := <-signals:
			if !ok {
				return
			}
			s.signal(ctx, sig, sleep, restart)
		}
	}
}

func (s *VPNService) signal(ctx context.Context, sig *dbus.Signal, sleep *sleepResume, restart *restartResume) {
	switch sig.Name {
	case properties + ".PropertiesChanged":
		if sig.Path != nmPath || len(sig.Body) < 2 {
			return
		}
		changed, _ := sig.Body[1].(map[string]dbus.Variant)
		if v, ok := changed["State"]; ok {
			if state, ok := v.Value().(uint32); ok {
				s.mu.Lock()
				s.managerState = State(state)
				s.mu.Unlock()
				s.stateChanges.notify()
			}
		}
		if _, ok := changed["ActiveConnections"]; ok {
			s.resync(ctx)
		}
	case activeIface + ".StateChanged":
		if len(sig.Body) == 2 {
			state, _ := sig.Body[0].(uint32)
			reason, _ := sig.Body[1].(uint32)
			s.stateChanged(sig.Path, state, reason)
		}
	case login1Iface + ".PrepareForSleep":
		if len(sig.Body) == 1 {
			if start, ok := sig.Body[0].(bool); ok {
				sleep.prepareForSleep(ctx, start)
			}
		}
	case busName + ".NameOwnerChanged":
		if oldOwner, newOwner, ok := nameOwnerChange(sig, nmName); ok {
			restart.ownerChanged(ctx, oldOwner != "", newOwner != "")
		}
	}
}

// vpnStateOf maps an active-connection state to a VPN state.
func vpnStateOf(state ActiveState) VPNState {
	switch state {
	case ActiveActivated:
		return VPNConnected
	case ActiveActivating:
		return VPNConnecting
	}
	return VPNDisconnected
}

// reasonText is a short reason for a state change, "" when it carries
// nothing worth showing. Only failures get text: a VPN the user
// disconnected needs no caption saying so.
func reasonText(reason ActiveReason) string {
	switch reason {
	case ReasonNoSecrets:
		return "credentials not provided"
	case ReasonLoginFailed:
		return "authentication failed"
	case ReasonConnectTimeout:
		return "connection timed out"
	case ReasonServiceStartTimeout:
		return "VPN service did not start in time"
	case ReasonServiceStartFailed:
		return "VPN service failed to start"
	case ReasonServiceStopped:
		return "VPN service stopped"
	case ReasonIPConfigInvalid:
		return "invalid IP configuration"
	case ReasonDependencyFailed:
		return "the connection it depends on failed"
	case ReasonDeviceRealizeFailed:
		return "cannot create the tunnel device"
	case ReasonDeviceRemoved, ReasonDeviceDisconnected:
		return "the underlying network went away"
	}
	return ""
}

// resolveState turns one StateChanged into the row's state and caption.
// A reason only becomes a caption when the connection went down: NM
// repeats the last reason on every transition, so reading it on the
// way up would caption a healthy tunnel with the previous failure.
func resolveState(state ActiveState, reason ActiveReason) (VPNState, string) {
	mapped := vpnStateOf(state)
	if mapped == VPNDisconnected {
		if text := reasonText(reason); text != "" {
			return VPNFailed, text
		}
	}
	return mapped, ""
}

// nextWentDownAt is when the tunnel last stopped being up after a
// transition: stamped on the way out of up, whatever comes next (NM
// passes through deactivating before it settles on how the tunnel
// ended), cleared on coming up again.
func nextWentDownAt(before, after VPNState, previous, now time.Time) time.Time {
	switch {
	case after.isUp():
		return time.Time{}
	case before.isUp():
		return now
	}
	return previous
}

// nextTurnedOff is whether the tunnel counts as turned off after a
// change to after carrying reason. NM's user-disconnected reason covers
// a deactivation from any client; coming up again clears it, and any
// other way down leaves it be, since NM repeats the last reason.
func nextTurnedOff(previous bool, after VPNState, reason ActiveReason) bool {
	if after.isUp() {
		return false
	}
	return previous || reason == ReasonUserDisconnected
}

// mergeDetail decides the row's caption: NM's own reason is generic
// ("credentials not provided" for every refused sign-in), so a caption
// already there (from the gateway) wins; a change with no reason clears
// it, which is what drops the last failure on a good reconnect.
func mergeDetail(existing, incoming string) string {
	if incoming == "" {
		return ""
	}
	if existing != "" {
		return existing
	}
	return incoming
}

// ownerName is this process's login name as NM will read it: the
// passwd name of the effective user, who the bus tells NM every call
// comes from. $USER is only probably right, and NM refuses a profile
// whose permissions leave its caller out.
func ownerName() string {
	u, err := user.LookupId(strconv.Itoa(os.Geteuid()))
	if err != nil {
		if _, unknown := errors.AsType[user.UnknownUserIdError](err); unknown {
			log.Printf("network: this user has no passwd entry; new VPN profiles will be system-wide")
		} else {
			log.Printf("network: cannot look up this user; new VPN profiles will be system-wide: %v", err)
		}
		return ""
	}
	return u.Username
}

// newUUID is a random RFC 4122 version-4 UUID in the form NM stores.
// NM would generate one itself, but then it is only learned by reading
// the profile back, a race with every other client on the bus.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

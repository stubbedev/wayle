package network

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeNM is a NetworkManager on a private bus: the settings tree,
// active connections with their StateChanged signal, the manager's
// properties, the agent manager, and one wifi device. It is the only
// NM the tests talk to; the real system bus is never touched.
type fakeNM struct {
	t    *testing.T
	bus  *dbustest.Bus
	conn *dbus.Conn

	mu            sync.Mutex
	registrations []registration
	order         []dbus.ObjectPath
	profiles      map[dbus.ObjectPath]ConnectionDict
	active        map[dbus.ObjectPath]*fakeActive
	nextProfile   int
	nextActive    int
	activateErr   *dbus.Error
	activations   []dbus.ObjectPath
	deactivations []dbus.ObjectPath
	added         []ConnectionDict
	addAndActive  []ConnectionDict

	manager *prop.Properties
	wifi    *fakeWifi
	wired   bool
}

type registration struct {
	sender string
	id     string
	caps   uint32
}

type fakeActive struct {
	uuid  string
	props *prop.Properties
}

// startFakeNM brings up a bus with a fake NM owning its name.
func startFakeNM(t *testing.T) *fakeNM {
	t.Helper()
	bus := dbustest.Start(t)
	f := &fakeNM{
		t:        t,
		bus:      bus,
		profiles: make(map[dbus.ObjectPath]ConnectionDict),
		active:   make(map[dbus.ObjectPath]*fakeActive),
	}
	f.own()
	return f
}

// own connects a fresh peer that exports NM and takes its name: the
// start of the fake, and of a restarted one.
func (f *fakeNM) own() {
	f.t.Helper()
	f.conn = f.bus.Conn(f.t)
	must(f.t, f.conn.Export(fakeManager{f}, nmPath, nmIface))
	must(f.t, f.conn.Export(fakeSettings{f}, settingsPath, settingsIface))
	must(f.t, f.conn.Export(fakeAgentManager{f}, agentManagerPath, agentManagerIface))
	f.mu.Lock()
	paths := slices.Clone(f.order)
	f.mu.Unlock()
	for _, path := range paths {
		must(f.t, f.conn.Export(fakeConnection{f: f, path: path}, path, connectionIface))
	}
	props, err := prop.Export(f.conn, nmPath, prop.Map{nmIface: {
		"ActiveConnections": {Value: []dbus.ObjectPath{}, Emit: prop.EmitTrue},
		"State":             {Value: uint32(StateConnectedGlobal), Emit: prop.EmitTrue},
		"WirelessEnabled":   {Value: true, Writable: true, Emit: prop.EmitTrue},
	}})
	must(f.t, err)
	f.manager = props
	f.exportWifi()
	reply, err := f.conn.RequestName(nmName, dbus.NameFlagDoNotQueue)
	must(f.t, err)
	if reply != dbus.RequestNameReplyPrimaryOwner {
		f.t.Fatalf("fake NM: name reply %v", reply)
	}
}

// stop takes NM off the bus, as a restart's first half does.
func (f *fakeNM) stop() {
	f.t.Helper()
	f.mu.Lock()
	f.active = make(map[dbus.ObjectPath]*fakeActive)
	f.registrations = nil
	f.mu.Unlock()
	_ = f.conn.Close()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// client is a peer the service under test runs on.
func (f *fakeNM) client() *dbus.Conn { return f.bus.Conn(f.t) }

// addProfile saves a connection directly, as another NM client would.
func (f *fakeNM) addProfile(dict ConnectionDict) dbus.ObjectPath {
	f.mu.Lock()
	f.nextProfile++
	path := dbus.ObjectPath(fmt.Sprintf("%s/%d", settingsPath, f.nextProfile))
	f.profiles[path] = dict
	f.order = append(f.order, path)
	f.mu.Unlock()
	must(f.t, f.conn.Export(fakeConnection{f: f, path: path}, path, connectionIface))
	must(f.t, f.conn.Emit(settingsPath, settingsIface+".NewConnection", path))
	return path
}

// removeProfile deletes a connection and announces it.
func (f *fakeNM) removeProfile(path dbus.ObjectPath) {
	f.mu.Lock()
	delete(f.profiles, path)
	f.order = slices.DeleteFunc(f.order, func(p dbus.ObjectPath) bool { return p == path })
	f.mu.Unlock()
	_ = f.conn.Export(nil, path, connectionIface)
	must(f.t, f.conn.Emit(settingsPath, settingsIface+".ConnectionRemoved", path))
}

func (f *fakeNM) profile(path dbus.ObjectPath) (ConnectionDict, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.profiles[path]
	return d, ok
}

func (f *fakeNM) uuidOf(path dbus.ObjectPath) string {
	d, _ := f.profile(path)
	return dictReader(d).str("connection", "uuid")
}

// startActive creates an active connection for a profile uuid in the
// given state and lists it.
func (f *fakeNM) startActive(uuid string, state ActiveState) dbus.ObjectPath {
	f.mu.Lock()
	f.nextActive++
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/NetworkManager/ActiveConnection/%d", f.nextActive))
	f.mu.Unlock()
	props, err := prop.Export(f.conn, path, prop.Map{activeIface: {
		"Uuid":  {Value: uuid, Emit: prop.EmitTrue},
		"State": {Value: uint32(state), Emit: prop.EmitTrue},
	}})
	must(f.t, err)
	f.mu.Lock()
	f.active[path] = &fakeActive{uuid: uuid, props: props}
	f.mu.Unlock()
	f.publishActive()
	return path
}

// setActiveState moves an active connection and signals it the way NM
// does, StateChanged(state, reason).
func (f *fakeNM) setActiveState(path dbus.ObjectPath, state ActiveState, reason ActiveReason) {
	f.mu.Lock()
	a := f.active[path]
	f.mu.Unlock()
	if a == nil {
		f.t.Fatalf("no active connection %s", path)
	}
	a.props.SetMust(activeIface, "State", uint32(state))
	must(f.t, f.conn.Emit(path, activeIface+".StateChanged", uint32(state), uint32(reason)))
}

// dropActive tears an active connection down and unlists it.
func (f *fakeNM) dropActive(path dbus.ObjectPath) {
	f.mu.Lock()
	delete(f.active, path)
	f.mu.Unlock()
	f.publishActive()
}

func (f *fakeNM) publishActive() {
	f.mu.Lock()
	paths := make([]dbus.ObjectPath, 0, len(f.active))
	for path := range f.active {
		paths = append(paths, path)
	}
	f.mu.Unlock()
	slices.Sort(paths)
	f.manager.SetMust(nmIface, "ActiveConnections", paths)
}

func (f *fakeNM) setState(state State) { f.manager.SetMust(nmIface, "State", uint32(state)) }

// registered waits for an agent registration.
func (f *fakeNM) registered(t *testing.T) registration {
	t.Helper()
	var got registration
	waitFor(t, "an agent registration", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.registrations) == 0 {
			return false
		}
		got = f.registrations[len(f.registrations)-1]
		return true
	})
	return got
}

// getSecrets calls the registered agent the way NM does.
func (f *fakeNM) getSecrets(ctx context.Context, agent string, conn ConnectionDict, path dbus.ObjectPath, setting string, hints []string, flags uint32) (ConnectionDict, error) {
	var reply ConnectionDict
	err := f.conn.Object(agent, AgentPath).CallWithContext(ctx, agentIface+".GetSecrets", 0, conn, path, setting, hints, flags).Store(&reply)
	return reply, err
}

func (f *fakeNM) cancelSecrets(agent string, path dbus.ObjectPath, setting string) {
	must(f.t, f.conn.Object(agent, AgentPath).Call(agentIface+".CancelGetSecrets", 0, path, setting).Err)
}

// waitFor polls cond for up to three seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type fakeManager struct{ f *fakeNM }

func (m fakeManager) ActivateConnection(profile, _, _ dbus.ObjectPath) (dbus.ObjectPath, *dbus.Error) {
	f := m.f
	f.mu.Lock()
	f.activations = append(f.activations, profile)
	refused := f.activateErr
	f.mu.Unlock()
	if refused != nil {
		return "", refused
	}
	uuid := f.uuidOf(profile)
	if uuid == "" {
		return "", dbus.NewError("org.freedesktop.NetworkManager.UnknownConnection", []any{"no such connection"})
	}
	return f.startActive(uuid, ActiveActivating), nil
}

func (m fakeManager) DeactivateConnection(active dbus.ObjectPath) *dbus.Error {
	f := m.f
	f.mu.Lock()
	f.deactivations = append(f.deactivations, active)
	_, ok := f.active[active]
	f.mu.Unlock()
	if !ok {
		return dbus.NewError("org.freedesktop.NetworkManager.ConnectionNotActive", []any{"not active"})
	}
	f.setActiveState(active, ActiveDeactivated, ReasonUserDisconnected)
	f.dropActive(active)
	return nil
}

func (m fakeManager) GetDevices() ([]dbus.ObjectPath, *dbus.Error) {
	var devices []dbus.ObjectPath
	if m.f.wifi != nil {
		devices = append(devices, fakeWifiPath)
	}
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	if m.f.wired {
		devices = append(devices, fakeWiredPath)
	}
	return devices, nil
}

func (m fakeManager) AddAndActivateConnection(dict ConnectionDict, device, specific dbus.ObjectPath) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f := m.f
	f.mu.Lock()
	f.addAndActive = append(f.addAndActive, dict)
	f.mu.Unlock()
	if _, ok := dict["connection"]["uuid"]; !ok {
		dict["connection"]["uuid"] = dbus.MakeVariant(fmt.Sprintf("generated-%d", time.Now().UnixNano()))
	}
	path := f.addProfile(dict)
	active, err := m.ActivateConnection(path, device, specific)
	return path, active, err
}

type fakeSettings struct{ f *fakeNM }

func (s fakeSettings) ListConnections() ([]dbus.ObjectPath, *dbus.Error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	return slices.Clone(s.f.order), nil
}

func (s fakeSettings) AddConnection(dict ConnectionDict) (dbus.ObjectPath, *dbus.Error) {
	s.f.mu.Lock()
	s.f.added = append(s.f.added, dict)
	s.f.mu.Unlock()
	return s.f.addProfile(dict), nil
}

type fakeConnection struct {
	f    *fakeNM
	path dbus.ObjectPath
}

func (c fakeConnection) GetSettings() (ConnectionDict, *dbus.Error) {
	d, ok := c.f.profile(c.path)
	if !ok {
		return nil, dbus.NewError("org.freedesktop.NetworkManager.Settings.InvalidConnection", []any{"gone"})
	}
	return d, nil
}

func (c fakeConnection) Update(dict ConnectionDict) *dbus.Error {
	c.f.mu.Lock()
	c.f.profiles[c.path] = dict
	c.f.mu.Unlock()
	if err := c.f.conn.Emit(c.path, connectionIface+".Updated"); err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (c fakeConnection) Delete() *dbus.Error {
	c.f.removeProfile(c.path)
	return nil
}

type fakeAgentManager struct{ f *fakeNM }

func (a fakeAgentManager) RegisterWithCapabilities(sender dbus.Sender, id string, caps uint32) *dbus.Error {
	a.f.mu.Lock()
	a.f.registrations = append(a.f.registrations, registration{sender: string(sender), id: id, caps: caps})
	a.f.mu.Unlock()
	return nil
}

// Connection dictionaries as NM sends them.

func connectionSection(uuid, name, kind string) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"id":   dbus.MakeVariant(name),
		"uuid": dbus.MakeVariant(uuid),
		"type": dbus.MakeVariant(kind),
	}
}

func vpnConnection(uuid, name, service string, data map[string]string) ConnectionDict {
	vpn := map[string]dbus.Variant{"service-type": dbus.MakeVariant(service)}
	if data != nil {
		vpn["data"] = dbus.MakeVariant(data)
	}
	return ConnectionDict{"connection": connectionSection(uuid, name, "vpn"), "vpn": vpn}
}

func wifiConnection(uuid, name string, ssid []byte) ConnectionDict {
	return ConnectionDict{
		"connection":      connectionSection(uuid, name, "802-11-wireless"),
		"802-11-wireless": {"ssid": dbus.MakeVariant(ssid)},
	}
}

func settingPath(i int) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf("%s/%d", settingsPath, i))
}

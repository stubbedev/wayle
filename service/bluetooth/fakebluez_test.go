package bluetooth

import (
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

// fakeBlueZ is org.bluez on a private bus: the ObjectManager at /, the
// AgentManager1 at /org/bluez, and Adapter1/Device1/Battery1 objects
// with Properties, enough of bluetoothd's behavior for the service and
// its agent to be tested end to end.
type fakeBlueZ struct {
	t    *testing.T
	bus  *dbustest.Bus
	conn *dbus.Conn

	mu      sync.Mutex
	objects map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	calls   []string
	// agentOwner/agentAt/capability are the registered agent;
	// defaultAgent marks RequestDefaultAgent.
	agentOwner   string
	agentAt      dbus.ObjectPath
	capability   string
	defaultAgent bool
	// rejectRegister makes RegisterAgent fail.
	rejectRegister bool
	// fail maps "Iface.Method path" to the error that call returns.
	fail map[string]*dbus.Error
	// pairMode picks which agent method Pair drives per device.
	pairMode map[dbus.ObjectPath]string
	// agentReplies records each agent call's outcome: the returned
	// value, or the D-Bus error name.
	agentReplies []any
	filter       map[string]dbus.Variant
}

const (
	hci0 = dbus.ObjectPath("/org/bluez/hci0")
	hci1 = dbus.ObjectPath("/org/bluez/hci1")
	dev1 = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_01")
	dev2 = dbus.ObjectPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_02")
)

// writable is bluetoothd's read-write property set.
var writable = map[string][]string{
	adapterIface: {"Alias", "Powered", "Discoverable", "DiscoverableTimeout", "Pairable", "PairableTimeout", "Connectable"},
	deviceIface:  {"Alias", "Trusted", "Blocked", "WakeAllowed", "PreferredBearer"},
}

func startFakeBlueZ(t *testing.T) *fakeBlueZ {
	t.Helper()
	bus := dbustest.Start(t)
	conn := bus.Conn(t)
	if reply, err := conn.RequestName(bluezName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("own org.bluez: %v %v", reply, err)
	}
	f := &fakeBlueZ{
		t:        t,
		bus:      bus,
		conn:     conn,
		objects:  make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant),
		fail:     make(map[string]*dbus.Error),
		pairMode: make(map[dbus.ObjectPath]string),
	}
	if err := conn.Export(fakeObjectManager{f}, objectManagerAt, objectManager); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(fakeAgentManager{f}, bluezRoot, agentManager); err != nil {
		t.Fatal(err)
	}
	return f
}

// service starts the Service under test on its own connection.
func (f *fakeBlueZ) service() *Service {
	f.t.Helper()
	s, err := New(f.bus.Conn(f.t))
	if err != nil {
		f.t.Fatalf("New: %v", err)
	}
	f.t.Cleanup(func() { _ = s.Close() })
	return s
}

func adapterProps(address string, powered bool) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Address":             dbus.MakeVariant(address),
		"AddressType":         dbus.MakeVariant("public"),
		"Name":                dbus.MakeVariant("host"),
		"Alias":               dbus.MakeVariant("host"),
		"Class":               dbus.MakeVariant(uint32(0x7c010c)),
		"Connectable":         dbus.MakeVariant(true),
		"Powered":             dbus.MakeVariant(powered),
		"PowerState":          dbus.MakeVariant(map[bool]string{true: "on", false: "off"}[powered]),
		"Discoverable":        dbus.MakeVariant(false),
		"DiscoverableTimeout": dbus.MakeVariant(uint32(180)),
		"Discovering":         dbus.MakeVariant(false),
		"Pairable":            dbus.MakeVariant(true),
		"PairableTimeout":     dbus.MakeVariant(uint32(0)),
		"UUIDs":               dbus.MakeVariant([]string{"0000110a-0000-1000-8000-00805f9b34fb"}),
		"Modalias":            dbus.MakeVariant("usb:v1D6Bp0246d0548"),
		"Roles":               dbus.MakeVariant([]string{"central", "peripheral"}),
		"Manufacturer":        dbus.MakeVariant(uint16(2)),
		"Version":             dbus.MakeVariant(uint8(12)),
	}
}

func deviceProps(address, alias string, adapter dbus.ObjectPath) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Address":          dbus.MakeVariant(address),
		"AddressType":      dbus.MakeVariant("public"),
		"Name":             dbus.MakeVariant(alias),
		"Alias":            dbus.MakeVariant(alias),
		"Icon":             dbus.MakeVariant("audio-headphones"),
		"Class":            dbus.MakeVariant(uint32(0x240418)),
		"Paired":           dbus.MakeVariant(false),
		"Bonded":           dbus.MakeVariant(false),
		"Connected":        dbus.MakeVariant(false),
		"Trusted":          dbus.MakeVariant(false),
		"Blocked":          dbus.MakeVariant(false),
		"WakeAllowed":      dbus.MakeVariant(false),
		"LegacyPairing":    dbus.MakeVariant(false),
		"CablePairing":     dbus.MakeVariant(false),
		"Adapter":          dbus.MakeVariant(adapter),
		"RSSI":             dbus.MakeVariant(int16(-60)),
		"ServicesResolved": dbus.MakeVariant(false),
		"ManufacturerData": dbus.MakeVariant(map[uint16]dbus.Variant{76: dbus.MakeVariant([]byte{1, 2})}),
		"UUIDs":            dbus.MakeVariant([]string{"0000110b-0000-1000-8000-00805f9b34fb"}),
	}
}

// add creates (or extends) an object and exports its interfaces; with
// announce it also emits InterfacesAdded, as bluetoothd does for
// objects that appear at runtime.
func (f *fakeBlueZ) add(path dbus.ObjectPath, iface string, props map[string]dbus.Variant, announce bool) {
	f.t.Helper()
	f.mu.Lock()
	if f.objects[path] == nil {
		f.objects[path] = make(map[string]map[string]dbus.Variant)
	}
	f.objects[path][iface] = props
	f.mu.Unlock()
	if err := f.conn.Export(fakeProperties{f, path}, path, propertiesIface); err != nil {
		f.t.Fatal(err)
	}
	switch iface {
	case adapterIface:
		if err := f.conn.Export(fakeAdapter{f, path}, path, adapterIface); err != nil {
			f.t.Fatal(err)
		}
	case deviceIface:
		if err := f.conn.Export(fakeDevice{f, path}, path, deviceIface); err != nil {
			f.t.Fatal(err)
		}
	}
	if announce {
		f.emit(objectManagerAt, objectManager+".InterfacesAdded", path, map[string]map[string]dbus.Variant{iface: props})
	}
}

// remove drops interfaces from an object, emitting InterfacesRemoved.
func (f *fakeBlueZ) remove(path dbus.ObjectPath, ifaces ...string) {
	f.mu.Lock()
	for _, iface := range ifaces {
		delete(f.objects[path], iface)
	}
	if len(f.objects[path]) == 0 {
		delete(f.objects, path)
	}
	f.mu.Unlock()
	for _, iface := range ifaces {
		_ = f.conn.Export(nil, path, iface)
	}
	f.emit(objectManagerAt, objectManager+".InterfacesRemoved", path, ifaces)
}

// update changes properties and emits PropertiesChanged.
func (f *fakeBlueZ) update(path dbus.ObjectPath, iface string, changed map[string]dbus.Variant, invalidated ...string) {
	f.mu.Lock()
	if props := f.objects[path][iface]; props != nil {
		maps.Copy(props, changed)
		for _, k := range invalidated {
			delete(props, k)
		}
	}
	f.mu.Unlock()
	if invalidated == nil {
		invalidated = []string{}
	}
	f.emit(path, propertiesChange, iface, changed, invalidated)
}

func (f *fakeBlueZ) emit(path dbus.ObjectPath, name string, args ...any) {
	if err := f.conn.Emit(path, name, args...); err != nil {
		f.t.Errorf("emit %s: %v", name, err)
	}
}

func (f *fakeBlueZ) prop(path dbus.ObjectPath, iface, name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.objects[path][iface][name]
	if !ok {
		return nil
	}
	return v.Value()
}

func (f *fakeBlueZ) record(call string) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return f.fail[call]
}

func (f *fakeBlueZ) called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.calls, call)
}

func (f *fakeBlueZ) replies() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.agentReplies)
}

// callAgent drives the registered agent as bluetoothd would, recording
// the reply; it returns the D-Bus error name, empty on success.
func (f *fakeBlueZ) callAgent(method string, out any, args ...any) string {
	f.mu.Lock()
	owner, at := f.agentOwner, f.agentAt
	f.mu.Unlock()
	call := f.conn.Object(owner, at).Call(agentIface+"."+method, 0, args...)
	var reply any
	name := ""
	if call.Err != nil {
		name = errorName(call.Err)
		reply = name
	} else if out != nil {
		if err := call.Store(out); err != nil {
			f.t.Errorf("agent %s reply: %v", method, err)
		}
		reply = out
	}
	f.mu.Lock()
	f.agentReplies = append(f.agentReplies, reply)
	f.mu.Unlock()
	return name
}

type fakeObjectManager struct{ f *fakeBlueZ }

func (m fakeObjectManager) GetManagedObjects() (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, *dbus.Error) {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	out := make(map[dbus.ObjectPath]map[string]map[string]dbus.Variant, len(m.f.objects))
	for path, ifaces := range m.f.objects {
		out[path] = make(map[string]map[string]dbus.Variant, len(ifaces))
		for iface, props := range ifaces {
			// A copy: godbus encodes the reply after the lock is gone.
			out[path][iface] = maps.Clone(props)
		}
	}
	return out, nil
}

type fakeAgentManager struct{ f *fakeBlueZ }

func (m fakeAgentManager) RegisterAgent(sender dbus.Sender, path dbus.ObjectPath, capability string) *dbus.Error {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	if m.f.rejectRegister {
		return dbus.NewError("org.bluez.Error.AlreadyExists", []any{"Already Exists"})
	}
	m.f.agentOwner, m.f.agentAt, m.f.capability = string(sender), path, capability
	return nil
}

func (m fakeAgentManager) RequestDefaultAgent(sender dbus.Sender, path dbus.ObjectPath) *dbus.Error {
	m.f.mu.Lock()
	defer m.f.mu.Unlock()
	if string(sender) != m.f.agentOwner || path != m.f.agentAt {
		return dbus.NewError("org.bluez.Error.DoesNotExist", []any{"Does Not Exist"})
	}
	m.f.defaultAgent = true
	return nil
}

func (m fakeAgentManager) UnregisterAgent(dbus.ObjectPath) *dbus.Error { return nil }

type fakeProperties struct {
	f    *fakeBlueZ
	path dbus.ObjectPath
}

func (p fakeProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	v, ok := p.f.objects[p.path][iface][name]
	if !ok {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"No such property " + name})
	}
	return v, nil
}

func (p fakeProperties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	return maps.Clone(p.f.objects[p.path][iface]), nil
}

func (p fakeProperties) Set(iface, name string, value dbus.Variant) *dbus.Error {
	if !slices.Contains(writable[iface], name) {
		return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{name + " is read-only"})
	}
	if err := p.f.record(iface + ".Set." + name + " " + string(p.path)); err != nil {
		return err
	}
	p.f.update(p.path, iface, map[string]dbus.Variant{name: value})
	return nil
}

type fakeAdapter struct {
	f    *fakeBlueZ
	path dbus.ObjectPath
}

func (a fakeAdapter) StartDiscovery() *dbus.Error {
	if err := a.f.record("StartDiscovery " + string(a.path)); err != nil {
		return err
	}
	a.f.update(a.path, adapterIface, map[string]dbus.Variant{"Discovering": dbus.MakeVariant(true)})
	return nil
}

func (a fakeAdapter) StopDiscovery() *dbus.Error {
	if err := a.f.record("StopDiscovery " + string(a.path)); err != nil {
		return err
	}
	if on, _ := a.f.prop(a.path, adapterIface, "Discovering").(bool); !on {
		return dbus.NewError("org.bluez.Error.Failed", []any{"No discovery started"})
	}
	a.f.update(a.path, adapterIface, map[string]dbus.Variant{"Discovering": dbus.MakeVariant(false)})
	return nil
}

func (a fakeAdapter) SetDiscoveryFilter(filter map[string]dbus.Variant) *dbus.Error {
	if err := a.f.record("SetDiscoveryFilter " + string(a.path)); err != nil {
		return err
	}
	a.f.mu.Lock()
	a.f.filter = filter
	a.f.mu.Unlock()
	return nil
}

func (a fakeAdapter) GetDiscoveryFilters() ([]string, *dbus.Error) {
	return []string{"UUIDs", "RSSI", "Pathloss", "Transport", "DuplicateData", "Discoverable", "Pattern"}, nil
}

func (a fakeAdapter) RemoveDevice(device dbus.ObjectPath) *dbus.Error {
	if err := a.f.record("RemoveDevice " + string(device)); err != nil {
		return err
	}
	a.f.mu.Lock()
	_, ok := a.f.objects[device][deviceIface]
	a.f.mu.Unlock()
	if !ok {
		return dbus.NewError("org.bluez.Error.DoesNotExist", []any{"Does Not Exist"})
	}
	a.f.remove(device, deviceIface)
	return nil
}

func (a fakeAdapter) ConnectDevice(props map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	if err := a.f.record("ConnectDevice " + string(a.path)); err != nil {
		return "", err
	}
	return dev1, nil
}

type fakeDevice struct {
	f    *fakeBlueZ
	path dbus.ObjectPath
}

func (d fakeDevice) Connect() *dbus.Error {
	if err := d.f.record("Connect " + string(d.path)); err != nil {
		return err
	}
	d.f.update(d.path, deviceIface, map[string]dbus.Variant{"Connected": dbus.MakeVariant(true)})
	return nil
}

func (d fakeDevice) Disconnect() *dbus.Error {
	if err := d.f.record("Disconnect " + string(d.path)); err != nil {
		return err
	}
	d.f.update(d.path, deviceIface, map[string]dbus.Variant{"Connected": dbus.MakeVariant(false)})
	d.f.emit(d.path, deviceIface+".Disconnected", "org.bluez.Reason.Local", "Connection terminated by local host")
	return nil
}

func (d fakeDevice) ConnectProfile(uuid string) *dbus.Error {
	return d.f.record("ConnectProfile " + uuid + " " + string(d.path))
}

func (d fakeDevice) DisconnectProfile(uuid string) *dbus.Error {
	return d.f.record("DisconnectProfile " + uuid + " " + string(d.path))
}

func (d fakeDevice) CancelPairing() *dbus.Error {
	return d.f.record("CancelPairing " + string(d.path))
}

func (d fakeDevice) GetServiceRecords() ([][]byte, *dbus.Error) {
	return [][]byte{{0x35, 0x01}}, d.f.record("GetServiceRecords " + string(d.path))
}

// Pair drives the agent the way bluetoothd's bonding does for the
// device's pair mode; a refusing agent fails the pairing.
func (d fakeDevice) Pair() *dbus.Error {
	if err := d.f.record("Pair " + string(d.path)); err != nil {
		return err
	}
	d.f.mu.Lock()
	mode := d.f.pairMode[d.path]
	d.f.mu.Unlock()
	var refused string
	switch mode {
	case "confirm":
		refused = d.f.callAgent("RequestConfirmation", nil, d.path, uint32(123456))
	case "authorize":
		refused = d.f.callAgent("RequestAuthorization", nil, d.path)
	case "pin":
		var pin string
		refused = d.f.callAgent("RequestPinCode", &pin, d.path)
	case "passkey":
		var passkey uint32
		refused = d.f.callAgent("RequestPasskey", &passkey, d.path)
	case "display-passkey":
		refused = d.f.callAgent("DisplayPasskey", nil, d.path, uint32(4321), uint16(0))
		// The remote finishes typing; bluetoothd withdraws the prompt.
		time.Sleep(20 * time.Millisecond)
		d.f.callAgent("Cancel", nil)
	}
	switch refused {
	case "":
	case errCanceled:
		return dbus.NewError("org.bluez.Error.AuthenticationCanceled", []any{"Authentication Canceled"})
	default:
		return dbus.NewError("org.bluez.Error.AuthenticationRejected", []any{"Authentication Rejected"})
	}
	d.f.update(d.path, deviceIface, map[string]dbus.Variant{
		"Paired": dbus.MakeVariant(true),
		"Bonded": dbus.MakeVariant(true),
	})
	return nil
}

// waitFor polls cond until it holds or two seconds pass.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// errorName is a D-Bus error reply's name, empty for nil or a non-D-Bus
// error.
func errorName(err error) string {
	if dbusErr, ok := errors.AsType[dbus.Error](err); ok {
		return dbusErr.Name
	}
	return ""
}

// failOn makes the named call return err.
func (f *fakeBlueZ) failOn(call string, err *dbus.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[call] = err
}

// setPairMode picks the agent method Pair drives for device.
func (f *fakeBlueZ) setPairMode(device dbus.ObjectPath, mode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pairMode[device] = mode
}

// lastFilter is the last SetDiscoveryFilter dictionary.
func (f *fakeBlueZ) lastFilter() map[string]dbus.Variant {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.filter
}

// registration is the registered agent's path and capability, and
// whether it asked to be the default.
func (f *fakeBlueZ) registration() (dbus.ObjectPath, string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.agentAt, f.capability, f.defaultAgent
}

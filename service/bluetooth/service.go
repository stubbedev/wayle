// Package bluetooth is the BlueZ service, the Go counterpart of
// crates/wayle-bluetooth: the adapter and device model kept live from
// the ObjectManager and PropertiesChanged signals, the adapter and
// device controls, and the org.bluez.Agent1 pairing agent whose
// requests the UI answers.
package bluetooth

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/feed"
)

// Source is the UI's seam onto the service: the live state, change
// ticks, and the operations the bar and its dropdown drive.
type Source interface {
	// State is the current model.
	State() State
	// Subscribe ticks (coalesced) on every change until stop.
	Subscribe() (ticks <-chan struct{}, stop func())

	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
	StartTimedDiscovery(ctx context.Context, d time.Duration) error

	Connect(ctx context.Context, device dbus.ObjectPath) error
	Disconnect(ctx context.Context, device dbus.ObjectPath) error
	Forget(ctx context.Context, device dbus.ObjectPath) error
	SetTrusted(ctx context.Context, device dbus.ObjectPath, trusted bool) error
	TrustPaired(ctx context.Context, device dbus.ObjectPath) error

	ProvidePin(pin string) error
	ProvidePasskey(passkey uint32) error
	ProvideConfirmation(confirmed bool) error
	ProvideAuthorization(allowed bool) error
	ProvideServiceAuthorization(allowed bool) error
	CancelPendingRequest()
}

// State is one read of the model (the BluetoothService fields).
type State struct {
	// Adapters in discovery order.
	Adapters []Adapter
	// Primary is the adapter the service-level operations use; nil
	// when there is none.
	Primary *Adapter
	// Devices in discovery order, across adapters.
	Devices []Device
	// Available reports a primary adapter.
	Available bool
	// Enabled is the primary adapter's Powered.
	Enabled bool
	// Connected holds the connected devices' addresses.
	Connected []string
	// Pairing is the agent request on screen, nil when none.
	Pairing PairingRequest
}

// Device looks a device up by path.
func (s State) Device(path dbus.ObjectPath) (Device, bool) {
	for _, d := range s.Devices {
		if d.Path == path {
			return d, true
		}
	}
	return Device{}, false
}

// Snapshot is the bar module's summary of State.
type Snapshot struct {
	// Available reports whether a primary adapter exists.
	Available bool
	// Enabled is the primary adapter's Powered.
	Enabled bool
	// Discovering is the primary adapter's Discovering.
	Discovering bool
	// Connected holds the aliases of connected devices.
	Connected []string
}

// Snapshot summarizes the state for the bar label and icon.
func (s State) Snapshot() Snapshot {
	snap := Snapshot{Available: s.Available, Enabled: s.Enabled}
	if s.Primary != nil {
		snap.Discovering = s.Primary.Discovering
	}
	for _, d := range s.Devices {
		if !d.Connected {
			continue
		}
		alias := d.Alias
		if alias == "" {
			alias = "unknown"
		}
		snap.Connected = append(snap.Connected, alias)
	}
	return snap
}

// Service is the live BlueZ model plus the pairing agent.
type Service struct {
	conn    *dbus.Conn
	signals chan *dbus.Signal
	done    chan struct{}
	wg      sync.WaitGroup

	mu       sync.Mutex
	adapters []*Adapter
	devices  []*Device
	primary  dbus.ObjectPath
	pairing  *pending
	ticks    *feed.Tick
	discSubs map[chan DisconnectedEvent]dbus.ObjectPath
}

// NewSystem connects to the system bus and starts the service.
func NewSystem() (*Service, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("bluetooth: cannot initialize bluetooth service: %w", err)
	}
	s, err := New(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

// New starts the service on conn, which it owns from here on: it
// exports and registers the agent, reads the object tree, and follows
// BlueZ's signals (BluetoothService::new).
func New(conn *dbus.Conn) (*Service, error) {
	s := &Service{
		conn:     conn,
		signals:  make(chan *dbus.Signal, 256),
		done:     make(chan struct{}),
		ticks:    feed.NewTick(),
		discSubs: make(map[chan DisconnectedEvent]dbus.ObjectPath),
	}
	if err := s.registerAgent(); err != nil {
		return nil, err
	}
	// Match before reading the tree so nothing between the two is lost;
	// replays of what the read already saw are idempotent.
	for _, opts := range matchRules() {
		if err := conn.AddMatchSignal(opts...); err != nil {
			return nil, fmt.Errorf("bluetooth: match signals: %w", err)
		}
	}
	conn.Signal(s.signals)
	if err := s.discover(); err != nil {
		conn.RemoveSignal(s.signals)
		return nil, err
	}
	s.wg.Add(1)
	go s.run()
	return s, nil
}

func matchRules() [][]dbus.MatchOption {
	return [][]dbus.MatchOption{
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface(objectManager), dbus.WithMatchObjectPath(objectManagerAt)},
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface(propertiesIface), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchPathNamespace(bluezRoot)},
		{dbus.WithMatchSender(bluezName), dbus.WithMatchInterface(deviceIface), dbus.WithMatchMember("Disconnected"), dbus.WithMatchPathNamespace(bluezRoot)},
	}
}

// Close stops following BlueZ, cancels a pending agent request, and
// drops the connection (which unregisters the agent).
func (s *Service) Close() error {
	select {
	case <-s.done:
		return nil
	default:
	}
	close(s.done)
	s.conn.RemoveSignal(s.signals)
	s.wg.Wait()
	s.withdraw(func(pending) agentAnswer { return canceled() })
	s.ticks.Close()
	s.mu.Lock()
	for ch := range s.discSubs {
		close(ch)
	}
	s.discSubs = map[chan DisconnectedEvent]dbus.ObjectPath{}
	s.mu.Unlock()
	return s.conn.Close()
}

type managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// discover reads the object tree once (discovery.rs), in path order so
// the first adapter is stable.
func (s *Service) discover() error {
	var objects managedObjects
	obj := s.conn.Object(bluezName, objectManagerAt)
	if err := obj.Call(objectManager+".GetManagedObjects", 0).Store(&objects); err != nil {
		return fmt.Errorf("bluetooth: cannot discover bluetooth objects: %w", err)
	}
	paths := make([]dbus.ObjectPath, 0, len(objects))
	for path := range objects {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	s.mu.Lock()
	for _, path := range paths {
		s.addInterfacesLocked(path, objects[path])
	}
	s.primary = bestAdapter(s.adapters)
	s.mu.Unlock()
	return nil
}

// run folds BlueZ's signals into the model until Close.
func (s *Service) run() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case sig, ok := <-s.signals:
			if !ok {
				return
			}
			s.handle(sig)
		}
	}
}

func (s *Service) handle(sig *dbus.Signal) {
	switch sig.Name {
	case objectManager + ".InterfacesAdded":
		var path dbus.ObjectPath
		var ifaces map[string]map[string]dbus.Variant
		if dbus.Store(sig.Body, &path, &ifaces) != nil {
			return
		}
		s.mu.Lock()
		adapterAdded := s.addInterfacesLocked(path, ifaces)
		if adapterAdded {
			s.primary = selectPrimary(s.primary, s.adapters)
		}
		s.mu.Unlock()
	case objectManager + ".InterfacesRemoved":
		var path dbus.ObjectPath
		var ifaces []string
		if dbus.Store(sig.Body, &path, &ifaces) != nil {
			return
		}
		s.mu.Lock()
		s.removeInterfacesLocked(path, ifaces)
		s.mu.Unlock()
	case propertiesChange:
		var iface string
		var changed map[string]dbus.Variant
		var invalidated []string
		if dbus.Store(sig.Body, &iface, &changed, &invalidated) != nil {
			return
		}
		s.mu.Lock()
		s.applyPropertiesLocked(sig.Path, iface, changed, invalidated)
		s.mu.Unlock()
	case deviceIface + ".Disconnected":
		var reason, message string
		if dbus.Store(sig.Body, &reason, &message) != nil {
			return
		}
		s.fanDisconnected(sig.Path, DisconnectedEvent{Reason: ParseDisconnectReason(reason), Message: message})
		return
	default:
		return
	}
	s.changed()
}

// addInterfacesLocked adds the adapter/device at path, or the battery
// of a known device; it reports whether an adapter was added. Known
// objects are left alone (handle_device_added's existence check).
func (s *Service) addInterfacesLocked(path dbus.ObjectPath, ifaces map[string]map[string]dbus.Variant) bool {
	added := false
	if props, ok := ifaces[adapterIface]; ok && s.adapterLocked(path) == nil {
		a := &Adapter{Path: path}
		applyAdapter(a, props, nil)
		s.adapters = append(s.adapters, a)
		added = true
	}
	if props, ok := ifaces[deviceIface]; ok && s.deviceLocked(path) == nil {
		d := &Device{Path: path}
		applyDevice(d, props, nil)
		s.devices = append(s.devices, d)
	}
	if props, ok := ifaces[batteryIface]; ok {
		if d := s.deviceLocked(path); d != nil {
			applyBattery(d, props, nil)
		}
	}
	return added
}

// removeInterfacesLocked drops the device or adapter at path, or clears
// a device's battery when only Battery1 went.
func (s *Service) removeInterfacesLocked(path dbus.ObjectPath, ifaces []string) {
	switch {
	case slices.Contains(ifaces, deviceIface):
		s.devices = slices.DeleteFunc(s.devices, func(d *Device) bool { return d.Path == path })
	case slices.Contains(ifaces, batteryIface):
		if d := s.deviceLocked(path); d != nil {
			d.BatteryPercentage = nil
		}
	}
	if slices.Contains(ifaces, adapterIface) {
		s.adapters = slices.DeleteFunc(s.adapters, func(a *Adapter) bool { return a.Path == path })
		s.primary = selectPrimary(s.primary, s.adapters)
	}
}

func (s *Service) applyPropertiesLocked(path dbus.ObjectPath, iface string, changed map[string]dbus.Variant, invalidated []string) {
	switch iface {
	case adapterIface:
		if a := s.adapterLocked(path); a != nil {
			applyAdapter(a, changed, invalidated)
		}
	case deviceIface:
		if d := s.deviceLocked(path); d != nil {
			applyDevice(d, changed, invalidated)
		}
	case batteryIface:
		if d := s.deviceLocked(path); d != nil {
			applyBattery(d, changed, invalidated)
		}
	}
}

func (s *Service) adapterLocked(path dbus.ObjectPath) *Adapter {
	for _, a := range s.adapters {
		if a.Path == path {
			return a
		}
	}
	return nil
}

func (s *Service) deviceLocked(path dbus.ObjectPath) *Device {
	for _, d := range s.devices {
		if d.Path == path {
			return d
		}
	}
	return nil
}

// bestAdapter is find_best_adapter: the first powered adapter, else
// the first; "" when there are none.
func bestAdapter(adapters []*Adapter) dbus.ObjectPath {
	for _, a := range adapters {
		if a.Powered {
			return a.Path
		}
	}
	if len(adapters) > 0 {
		return adapters[0].Path
	}
	return ""
}

// selectPrimary is select_primary_adapter, run when the adapter list
// changes: keep a current adapter that is still present and powered,
// else prefer a powered one, else keep the present current one.
func selectPrimary(current dbus.ObjectPath, adapters []*Adapter) dbus.ObjectPath {
	if len(adapters) == 0 {
		return ""
	}
	i := slices.IndexFunc(adapters, func(a *Adapter) bool { return a.Path == current })
	if current == "" || i < 0 {
		return bestAdapter(adapters)
	}
	if adapters[i].Powered {
		return current
	}
	for _, a := range adapters {
		if a.Powered {
			return a.Path
		}
	}
	return current
}

// State copies the model out.
func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := State{
		Adapters: make([]Adapter, len(s.adapters)),
		Devices:  make([]Device, len(s.devices)),
	}
	for i, a := range s.adapters {
		st.Adapters[i] = *a
		if a.Path == s.primary {
			primary := *a
			st.Primary = &primary
		}
	}
	for i, d := range s.devices {
		st.Devices[i] = *d
		if d.Connected {
			st.Connected = append(st.Connected, d.Address)
		}
	}
	st.Available = st.Primary != nil
	st.Enabled = st.Primary != nil && st.Primary.Powered
	if s.pairing != nil {
		st.Pairing = s.pairing.request
	}
	return st
}

// Subscribe ticks on every model or pairing change; the channel closes
// on stop or Close.
func (s *Service) Subscribe() (<-chan struct{}, func()) { return s.ticks.Subscribe() }

// changed ticks every subscriber without blocking.
func (s *Service) changed() { feed.Notify(s.ticks) }

// primaryRef resolves the primary adapter for a service-level
// operation.
func (s *Service) primaryRef(operation string) (AdapterRef, error) {
	s.mu.Lock()
	primary := s.primary
	s.mu.Unlock()
	if primary == "" {
		return AdapterRef{}, &NoPrimaryAdapterError{Operation: operation}
	}
	return s.Adapter(primary), nil
}

// Enable powers the primary adapter on.
func (s *Service) Enable(ctx context.Context) error {
	a, err := s.primaryRef("enable bluetooth")
	if err != nil {
		return err
	}
	return a.SetPowered(ctx, true)
}

// Disable powers the primary adapter off.
func (s *Service) Disable(ctx context.Context) error {
	a, err := s.primaryRef("disable bluetooth")
	if err != nil {
		return err
	}
	return a.SetPowered(ctx, false)
}

// StartDiscovery scans on the primary adapter until StopDiscovery.
func (s *Service) StartDiscovery(ctx context.Context) error {
	a, err := s.primaryRef("start discovery")
	if err != nil {
		return err
	}
	return a.StartDiscovery(ctx)
}

// StartTimedDiscovery scans on the primary adapter and stops that
// adapter's session after d.
func (s *Service) StartTimedDiscovery(ctx context.Context, d time.Duration) error {
	a, err := s.primaryRef("start timed discovery")
	if err != nil {
		return err
	}
	if err := a.StartDiscovery(ctx); err != nil {
		return err
	}
	time.AfterFunc(d, func() {
		if err := a.StopDiscovery(context.Background()); err != nil {
			log.Printf("bluetooth: cannot stop timed discovery: %v", err)
		}
	})
	return nil
}

// StopDiscovery ends the primary adapter's discovery session.
func (s *Service) StopDiscovery(ctx context.Context) error {
	a, err := s.primaryRef("stop discovery")
	if err != nil {
		return err
	}
	return a.StopDiscovery(ctx)
}

// Connect connects the device.
func (s *Service) Connect(ctx context.Context, device dbus.ObjectPath) error {
	return s.Device(device).Connect(ctx)
}

// Disconnect disconnects the device.
func (s *Service) Disconnect(ctx context.Context, device dbus.ObjectPath) error {
	return s.Device(device).Disconnect(ctx)
}

// Forget removes the device from its adapter.
func (s *Service) Forget(ctx context.Context, device dbus.ObjectPath) error {
	return s.Device(device).Forget(ctx)
}

// SetTrusted sets the device's Trusted.
func (s *Service) SetTrusted(ctx context.Context, device dbus.ObjectPath, trusted bool) error {
	return s.Device(device).SetTrusted(ctx, trusted)
}

// TrustPaired marks a paired, untrusted device trusted, so BlueZ stops
// asking the agent to authorize its services on every reconnect; an
// unpaired device is left alone (a stray trust would linger). It reads
// Paired and Trusted live, as the UI's trust_paired_device does, since
// the model may not have folded the pairing's signals in yet.
func (s *Service) TrustPaired(ctx context.Context, device dbus.ObjectPath) error {
	var props map[string]dbus.Variant
	call := s.conn.Object(bluezName, device).CallWithContext(ctx, propertiesIface+".GetAll", 0, deviceIface)
	if call.Err != nil {
		return &OperationError{Operation: "read device", Err: call.Err}
	}
	if err := call.Store(&props); err != nil {
		return &OperationError{Operation: "read device", Err: err}
	}
	paired, _ := variantAs[bool](props["Paired"])
	trusted, _ := variantAs[bool](props["Trusted"])
	if !paired || trusted {
		return nil
	}
	return s.Device(device).SetTrusted(ctx, true)
}

// Disconnected streams the device's Disconnected signals until stop
// (Device::disconnected_signal).
func (s *Service) Disconnected(device dbus.ObjectPath) (<-chan DisconnectedEvent, func()) {
	ch := make(chan DisconnectedEvent, 4)
	s.mu.Lock()
	s.discSubs[ch] = device
	s.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			if _, ok := s.discSubs[ch]; ok {
				delete(s.discSubs, ch)
				close(ch)
			}
			s.mu.Unlock()
		})
	}
}

func (s *Service) fanDisconnected(path dbus.ObjectPath, ev DisconnectedEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch, device := range s.discSubs {
		if device != path {
			continue
		}
		select {
		case ch <- ev:
		default:
		}
	}
}

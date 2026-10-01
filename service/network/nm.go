package network

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// The NetworkManager D-Bus surface the service uses beyond network.go's
// status read: settings, active connections, the agent manager, and
// the wireless device (crates/wayle-network/src/proxy).
const (
	settingsPath      = "/org/freedesktop/NetworkManager/Settings"
	settingsIface     = "org.freedesktop.NetworkManager.Settings"
	connectionIface   = "org.freedesktop.NetworkManager.Settings.Connection"
	activeIface       = "org.freedesktop.NetworkManager.Connection.Active"
	agentManagerPath  = "/org/freedesktop/NetworkManager/AgentManager"
	agentManagerIface = "org.freedesktop.NetworkManager.AgentManager"
	wirelessIface     = "org.freedesktop.NetworkManager.Device.Wireless"
	busName           = "org.freedesktop.DBus"
	busPath           = "/org/freedesktop/DBus"
	rootPath          = dbus.ObjectPath("/")
)

// ConnectionDict is a connection profile in NM's a{sa{sv}} shape
// (profile.rs's ConnectionDict).
type ConnectionDict = map[string]map[string]dbus.Variant

// ActiveState is NMActiveConnectionState.
type ActiveState uint32

// Active connection states.
const (
	ActiveUnknown      ActiveState = 0
	ActiveActivating   ActiveState = 1
	ActiveActivated    ActiveState = 2
	ActiveDeactivating ActiveState = 3
	ActiveDeactivated  ActiveState = 4
)

// ActiveReason is NMActiveConnectionStateReason.
type ActiveReason uint32

// Active connection state reasons.
const (
	ReasonUnknown             ActiveReason = 0
	ReasonNone                ActiveReason = 1
	ReasonUserDisconnected    ActiveReason = 2
	ReasonDeviceDisconnected  ActiveReason = 3
	ReasonServiceStopped      ActiveReason = 4
	ReasonIPConfigInvalid     ActiveReason = 5
	ReasonConnectTimeout      ActiveReason = 6
	ReasonServiceStartTimeout ActiveReason = 7
	ReasonServiceStartFailed  ActiveReason = 8
	ReasonNoSecrets           ActiveReason = 9
	ReasonLoginFailed         ActiveReason = 10
	ReasonConnectionRemoved   ActiveReason = 11
	ReasonDependencyFailed    ActiveReason = 12
	ReasonDeviceRealizeFailed ActiveReason = 13
	ReasonDeviceRemoved       ActiveReason = 14
)

// State is NMState, the manager's overall state.
type State uint32

// Manager states.
const (
	StateUnknown         State = 0
	StateAsleep          State = 10
	StateDisconnected    State = 20
	StateDisconnecting   State = 30
	StateConnecting      State = 40
	StateConnectedLocal  State = 50
	StateConnectedSite   State = 60
	StateConnectedGlobal State = 70
)

// nm is one bus connection's view of NetworkManager: the calls the
// agent, VPN, settings, and wifi halves share.
type nm struct {
	conn *dbus.Conn
}

func (n nm) manager() dbus.BusObject { return n.conn.Object(nmName, nmPath) }

func (n nm) object(path dbus.ObjectPath) dbus.BusObject { return n.conn.Object(nmName, path) }

// prop reads one property of an NM object.
func (n nm) prop(ctx context.Context, path dbus.ObjectPath, iface, name string, out any) error {
	return n.object(path).CallWithContext(ctx, properties+".Get", 0, iface, name).Store(out)
}

// activeConnections lists NM's active-connection objects.
func (n nm) activeConnections(ctx context.Context) ([]dbus.ObjectPath, error) {
	var paths []dbus.ObjectPath
	if err := n.prop(ctx, nmPath, nmIface, "ActiveConnections", &paths); err != nil {
		return nil, fmt.Errorf("read active connections: %w", err)
	}
	return paths, nil
}

// activeByUUID maps every active connection's profile UUID to its
// object (nm.rs's active_by_uuid). An object that vanished mid-sweep
// is skipped.
func (n nm) activeByUUID(ctx context.Context) (map[string]dbus.ObjectPath, error) {
	paths, err := n.activeConnections(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[string]dbus.ObjectPath, len(paths))
	for _, path := range paths {
		var uuid string
		if err := n.prop(ctx, path, activeIface, "Uuid", &uuid); err != nil {
			continue
		}
		active[uuid] = path
	}
	return active, nil
}

// activeState reads one active connection's state; an unreadable
// object counts as unknown.
func (n nm) activeState(ctx context.Context, path dbus.ObjectPath) ActiveState {
	var state uint32
	if err := n.prop(ctx, path, activeIface, "State", &state); err != nil {
		return ActiveUnknown
	}
	return ActiveState(state)
}

// managerState reads NMState.
func (n nm) managerState(ctx context.Context) State {
	var state uint32
	if err := n.prop(ctx, nmPath, nmIface, "State", &state); err != nil {
		return StateUnknown
	}
	return State(state)
}

// activate starts a saved profile on device/specific object ("/" lets
// NM pick, which is what `nmcli connection up` does for a VPN).
func (n nm) activate(ctx context.Context, profile, device, specific dbus.ObjectPath) (dbus.ObjectPath, error) {
	var active dbus.ObjectPath
	if err := n.manager().CallWithContext(ctx, nmIface+".ActivateConnection", 0, profile, device, specific).Store(&active); err != nil {
		return "", err
	}
	return active, nil
}

// deactivate tears one active connection down.
func (n nm) deactivate(ctx context.Context, active dbus.ObjectPath) error {
	return n.manager().CallWithContext(ctx, nmIface+".DeactivateConnection", 0, active).Err
}

// getSettings reads a saved profile.
func (n nm) getSettings(ctx context.Context, path dbus.ObjectPath) (ConnectionDict, error) {
	var dict ConnectionDict
	if err := n.object(path).CallWithContext(ctx, connectionIface+".GetSettings", 0).Store(&dict); err != nil {
		return nil, err
	}
	return dict, nil
}

// variantString reads a string variant, "" for anything else.
func variantString(v dbus.Variant) string {
	s, _ := v.Value().(string)
	return s
}

// stringDict reads an a{ss} variant (vpn.data, vpn.secrets).
func stringDict(v dbus.Variant) map[string]string {
	m, _ := v.Value().(map[string]string)
	return m
}

// dictReader reads the handful of fields the service pulls out of a
// connection dictionary.
type dictReader ConnectionDict

// str reads one string key of a section, "" when absent.
func (d dictReader) str(section, key string) string {
	return variantString(d[section][key])
}

package bluetooth

import (
	"strings"

	"github.com/godbus/dbus/v5"
)

// BlueZ names (types/mod.rs).
const (
	bluezName        = "org.bluez"
	bluezRoot        = "/org/bluez"
	objectManagerAt  = "/"
	adapterIface     = "org.bluez.Adapter1"
	deviceIface      = "org.bluez.Device1"
	batteryIface     = "org.bluez.Battery1"
	agentIface       = "org.bluez.Agent1"
	agentManager     = "org.bluez.AgentManager1"
	objectManager    = "org.freedesktop.DBus.ObjectManager"
	propertiesIface  = "org.freedesktop.DBus.Properties"
	propertiesChange = propertiesIface + ".PropertiesChanged"
)

// AddressType is the Bluetooth address type (types/adapter.rs). BlueZ
// reports it; unknown values read as public, as in the Rust From<&str>.
type AddressType uint8

// The address types.
const (
	AddressPublic AddressType = iota
	AddressRandom
)

// ParseAddressType maps BlueZ's AddressType string, case-insensitively.
func ParseAddressType(s string) AddressType {
	if strings.EqualFold(s, "random") {
		return AddressRandom
	}
	return AddressPublic
}

// String is BlueZ's spelling.
func (a AddressType) String() string {
	if a == AddressRandom {
		return "random"
	}
	return "public"
}

// PowerState is the adapter's power transition state (BlueZ
// experimental PowerState).
type PowerState uint8

// The power states.
const (
	PowerOff PowerState = iota
	PowerOn
	PowerOffToOn
	PowerOnToOff
	PowerOffBlocked
)

var powerStateNames = map[PowerState]string{
	PowerOn:         "on",
	PowerOff:        "off",
	PowerOffToOn:    "off-enabling",
	PowerOnToOff:    "on-disabling",
	PowerOffBlocked: "off-blocked",
}

// ParsePowerState maps BlueZ's PowerState string; unknown reads as off.
func ParsePowerState(s string) PowerState {
	for state, name := range powerStateNames {
		if name == s {
			return state
		}
	}
	return PowerOff
}

// String is BlueZ's spelling.
func (p PowerState) String() string { return powerStateNames[p] }

// AdapterRole is one of the adapter's supported roles.
type AdapterRole uint8

// The adapter roles.
const (
	RoleCentral AdapterRole = iota
	RolePeripheral
	RoleCentralPeripheral
)

// ParseAdapterRole maps BlueZ's role string; unknown reads as central.
func ParseAdapterRole(s string) AdapterRole {
	switch s {
	case "peripheral":
		return RolePeripheral
	case "central-peripheral":
		return RoleCentralPeripheral
	default:
		return RoleCentral
	}
}

// String is BlueZ's spelling.
func (r AdapterRole) String() string {
	switch r {
	case RolePeripheral:
		return "peripheral"
	case RoleCentralPeripheral:
		return "central-peripheral"
	default:
		return "central"
	}
}

// DiscoveryTransport is the discovery filter's transport.
type DiscoveryTransport uint8

// The discovery transports.
const (
	TransportAuto DiscoveryTransport = iota
	TransportBrEdr
	TransportLE
)

// ParseDiscoveryTransport maps the transport name case-insensitively;
// unknown reads as auto.
func ParseDiscoveryTransport(s string) DiscoveryTransport {
	switch strings.ToLower(s) {
	case "bredr":
		return TransportBrEdr
	case "le":
		return TransportLE
	default:
		return TransportAuto
	}
}

// String is BlueZ's spelling.
func (d DiscoveryTransport) String() string {
	switch d {
	case TransportBrEdr:
		return "bredr"
	case TransportLE:
		return "le"
	default:
		return "auto"
	}
}

// DiscoveryFilterOptions is SetDiscoveryFilter's argument; nil fields
// are left out of the filter (DiscoveryFilterOptions in
// types/adapter.rs). The zero value clears the filter.
type DiscoveryFilterOptions struct {
	UUIDs         []string
	RSSI          *int16
	Pathloss      *uint16
	Transport     *DiscoveryTransport
	DuplicateData *bool
	Discoverable  *bool
	Pattern       *string
	AutoConnect   *bool
}

// Filter builds the a{sv} dictionary BlueZ takes (to_filter).
func (o DiscoveryFilterOptions) Filter() map[string]dbus.Variant {
	filter := make(map[string]dbus.Variant)
	if o.UUIDs != nil {
		filter["UUIDs"] = dbus.MakeVariant(o.UUIDs)
	}
	if o.RSSI != nil {
		filter["RSSI"] = dbus.MakeVariant(*o.RSSI)
	}
	if o.Pathloss != nil {
		filter["Pathloss"] = dbus.MakeVariant(*o.Pathloss)
	}
	if o.Transport != nil {
		filter["Transport"] = dbus.MakeVariant(o.Transport.String())
	}
	if o.DuplicateData != nil {
		filter["DuplicateData"] = dbus.MakeVariant(*o.DuplicateData)
	}
	if o.Discoverable != nil {
		filter["Discoverable"] = dbus.MakeVariant(*o.Discoverable)
	}
	if o.Pattern != nil {
		filter["Pattern"] = dbus.MakeVariant(*o.Pattern)
	}
	if o.AutoConnect != nil {
		filter["AutoConnect"] = dbus.MakeVariant(*o.AutoConnect)
	}
	return filter
}

// PreferredBearer is the device's preferred bearer (types/device.rs).
type PreferredBearer uint8

// The preferred bearers.
const (
	BearerLastUsed PreferredBearer = iota
	BearerBrEdr
	BearerLE
	BearerLastSeen
)

// ParsePreferredBearer maps BlueZ's bearer string; unknown reads as
// last-used.
func ParsePreferredBearer(s string) PreferredBearer {
	switch s {
	case "bredr":
		return BearerBrEdr
	case "le":
		return BearerLE
	case "last-seen":
		return BearerLastSeen
	default:
		return BearerLastUsed
	}
}

// String is BlueZ's spelling.
func (b PreferredBearer) String() string {
	switch b {
	case BearerBrEdr:
		return "bredr"
	case BearerLE:
		return "le"
	case BearerLastSeen:
		return "last-seen"
	default:
		return "last-used"
	}
}

// DisconnectReason is Device1.Disconnected's reason.
type DisconnectReason uint8

// The disconnect reasons.
const (
	DisconnectUnknown DisconnectReason = iota
	DisconnectTimeout
	DisconnectLocal
	DisconnectRemote
	DisconnectAuthentication
	DisconnectSuspend
)

// ParseDisconnectReason maps the org.bluez.Reason.* name.
func ParseDisconnectReason(s string) DisconnectReason {
	switch s {
	case "org.bluez.Reason.Timeout":
		return DisconnectTimeout
	case "org.bluez.Reason.Local":
		return DisconnectLocal
	case "org.bluez.Reason.Remote":
		return DisconnectRemote
	case "org.bluez.Reason.Authentication":
		return DisconnectAuthentication
	case "org.bluez.Reason.Suspend":
		return DisconnectSuspend
	default:
		return DisconnectUnknown
	}
}

// String is the Rust Display text.
func (r DisconnectReason) String() string {
	switch r {
	case DisconnectTimeout:
		return "Connection timeout"
	case DisconnectLocal:
		return "Connection terminated by local host"
	case DisconnectRemote:
		return "Connection terminated by remote host"
	case DisconnectAuthentication:
		return "Authentication failure"
	case DisconnectSuspend:
		return "Suspend"
	default:
		return "Unknown"
	}
}

// DisconnectedEvent is one Device1.Disconnected signal.
type DisconnectedEvent struct {
	Reason DisconnectReason
	// Message is BlueZ's human-readable text.
	Message string
}

// AgentCapability is the IO capability the agent registers with
// (types/agent.rs).
type AgentCapability uint8

// The agent capabilities. KeyboardDisplay is the Rust default.
const (
	CapabilityKeyboardDisplay AgentCapability = iota
	CapabilityDisplayYesNo
	CapabilityDisplayOnly
	CapabilityKeyboardOnly
	CapabilityNoInputNoOutput
)

// ParseAgentCapability maps the capability name; empty or unknown reads
// as KeyboardDisplay, as in the Rust From<&str>.
func ParseAgentCapability(s string) AgentCapability {
	switch s {
	case "DisplayYesNo":
		return CapabilityDisplayYesNo
	case "DisplayOnly":
		return CapabilityDisplayOnly
	case "KeyboardOnly":
		return CapabilityKeyboardOnly
	case "NoInputNoOutput":
		return CapabilityNoInputNoOutput
	default:
		return CapabilityKeyboardDisplay
	}
}

// String is BlueZ's spelling.
func (c AgentCapability) String() string {
	switch c {
	case CapabilityDisplayYesNo:
		return "DisplayYesNo"
	case CapabilityDisplayOnly:
		return "DisplayOnly"
	case CapabilityKeyboardOnly:
		return "KeyboardOnly"
	case CapabilityNoInputNoOutput:
		return "NoInputNoOutput"
	default:
		return "KeyboardDisplay"
	}
}

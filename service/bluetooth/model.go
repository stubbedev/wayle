package bluetooth

import (
	"github.com/godbus/dbus/v5"
)

// Adapter is one org.bluez.Adapter1 (core/adapter). Values handed out
// by State are snapshots: treat the slices as read-only, the service
// replaces rather than mutates them.
type Adapter struct {
	Path                 dbus.ObjectPath
	Address              string
	AddressType          AddressType
	Name                 string
	Alias                string
	Class                uint32
	Connectable          bool
	Powered              bool
	PowerState           PowerState
	Discoverable         bool
	DiscoverableTimeout  uint32
	Discovering          bool
	Pairable             bool
	PairableTimeout      uint32
	UUIDs                []string
	Modalias             *string
	Roles                []AdapterRole
	ExperimentalFeatures []string
	Manufacturer         uint16
	Version              uint8
}

// DeviceSet is one Device1.Sets membership.
type DeviceSet struct {
	Path dbus.ObjectPath
	// Rank is the device's rank in the set, nil when BlueZ omits it.
	Rank *uint8
}

// Device is one org.bluez.Device1 plus its Battery1 percentage
// (core/device). Optional BlueZ properties are nil when absent, as the
// Rust Option fields; a nil map or slice is an absent property.
type Device struct {
	Path              dbus.ObjectPath
	Address           string
	AddressType       AddressType
	Name              *string
	Icon              *string
	BatteryPercentage *uint8
	Class             *uint32
	Appearance        *uint16
	UUIDs             []string
	Paired            bool
	Bonded            bool
	Connected         bool
	Trusted           bool
	Blocked           bool
	WakeAllowed       bool
	Alias             string
	Adapter           dbus.ObjectPath
	LegacyPairing     bool
	CablePairing      bool
	Modalias          *string
	RSSI              *int16
	TxPower           *int16
	ManufacturerData  map[uint16][]byte
	ServiceData       map[string][]byte
	ServicesResolved  bool
	AdvertisingFlags  []byte
	AdvertisingData   map[uint8][]byte
	Sets              []DeviceSet
	PreferredBearer   *PreferredBearer
}

// DisplayName is the alias, falling back to the name (the UI's
// resolve_device_display); empty when BlueZ has neither.
func (d Device) DisplayName() string {
	if d.Alias != "" {
		return d.Alias
	}
	if d.Name != nil {
		return *d.Name
	}
	return ""
}

// variantAs unwraps v into T, reporting whether it had that type.
func variantAs[T any](v dbus.Variant) (T, bool) {
	out, ok := v.Value().(T)
	return out, ok
}

// setFrom stores props[name] into dst when present with type T.
func setFrom[T any](props map[string]dbus.Variant, name string, dst *T) {
	if v, ok := props[name]; ok {
		if val, ok := variantAs[T](v); ok {
			*dst = val
		}
	}
}

// optFrom stores a fresh pointer to props[name] into dst when present
// with type T.
func optFrom[T any](props map[string]dbus.Variant, name string, dst **T) {
	if v, ok := props[name]; ok {
		if val, ok := variantAs[T](v); ok {
			*dst = &val
		}
	}
}

// applyAdapter folds one property dictionary (InterfacesAdded or
// PropertiesChanged) into a; invalidated names clear their optional
// fields.
func applyAdapter(a *Adapter, props map[string]dbus.Variant, invalidated []string) {
	setFrom(props, "Address", &a.Address)
	if s, ok := props["AddressType"]; ok {
		if v, ok := variantAs[string](s); ok {
			a.AddressType = ParseAddressType(v)
		}
	}
	setFrom(props, "Name", &a.Name)
	setFrom(props, "Alias", &a.Alias)
	setFrom(props, "Class", &a.Class)
	setFrom(props, "Connectable", &a.Connectable)
	setFrom(props, "Powered", &a.Powered)
	if s, ok := props["PowerState"]; ok {
		if v, ok := variantAs[string](s); ok {
			a.PowerState = ParsePowerState(v)
		}
	}
	setFrom(props, "Discoverable", &a.Discoverable)
	setFrom(props, "DiscoverableTimeout", &a.DiscoverableTimeout)
	setFrom(props, "Discovering", &a.Discovering)
	setFrom(props, "Pairable", &a.Pairable)
	setFrom(props, "PairableTimeout", &a.PairableTimeout)
	setFrom(props, "UUIDs", &a.UUIDs)
	if v, ok := props["Modalias"]; ok {
		// monitoring.rs: an empty modalias is no modalias.
		if s, ok := variantAs[string](v); ok && s != "" {
			a.Modalias = &s
		} else {
			a.Modalias = nil
		}
	}
	if v, ok := props["Roles"]; ok {
		if names, ok := variantAs[[]string](v); ok {
			roles := make([]AdapterRole, len(names))
			for i, name := range names {
				roles[i] = ParseAdapterRole(name)
			}
			a.Roles = roles
		}
	}
	setFrom(props, "ExperimentalFeatures", &a.ExperimentalFeatures)
	setFrom(props, "Manufacturer", &a.Manufacturer)
	setFrom(props, "Version", &a.Version)
	for _, name := range invalidated {
		if name == "Modalias" {
			a.Modalias = nil
		}
	}
}

// applyDevice folds one Device1 property dictionary into d;
// invalidated names clear their optional fields.
func applyDevice(d *Device, props map[string]dbus.Variant, invalidated []string) {
	setFrom(props, "Address", &d.Address)
	if s, ok := props["AddressType"]; ok {
		if v, ok := variantAs[string](s); ok {
			d.AddressType = ParseAddressType(v)
		}
	}
	optFrom(props, "Name", &d.Name)
	optFrom(props, "Icon", &d.Icon)
	optFrom(props, "Class", &d.Class)
	optFrom(props, "Appearance", &d.Appearance)
	setFrom(props, "UUIDs", &d.UUIDs)
	setFrom(props, "Paired", &d.Paired)
	setFrom(props, "Bonded", &d.Bonded)
	setFrom(props, "Connected", &d.Connected)
	setFrom(props, "Trusted", &d.Trusted)
	setFrom(props, "Blocked", &d.Blocked)
	setFrom(props, "WakeAllowed", &d.WakeAllowed)
	setFrom(props, "Alias", &d.Alias)
	setFrom(props, "Adapter", &d.Adapter)
	setFrom(props, "LegacyPairing", &d.LegacyPairing)
	setFrom(props, "CablePairing", &d.CablePairing)
	optFrom(props, "Modalias", &d.Modalias)
	optFrom(props, "RSSI", &d.RSSI)
	optFrom(props, "TxPower", &d.TxPower)
	if v, ok := props["ManufacturerData"]; ok {
		if m, ok := variantAs[map[uint16]dbus.Variant](v); ok {
			d.ManufacturerData = bytesMap(m)
		}
	}
	if v, ok := props["ServiceData"]; ok {
		if m, ok := variantAs[map[string]dbus.Variant](v); ok {
			d.ServiceData = bytesMap(m)
		}
	}
	setFrom(props, "ServicesResolved", &d.ServicesResolved)
	setFrom(props, "AdvertisingFlags", &d.AdvertisingFlags)
	if v, ok := props["AdvertisingData"]; ok {
		if m, ok := variantAs[map[uint8]dbus.Variant](v); ok {
			d.AdvertisingData = bytesMap(m)
		}
	}
	if v, ok := props["Sets"]; ok {
		if m, ok := variantAs[map[dbus.ObjectPath]map[string]dbus.Variant](v); ok {
			d.Sets = deviceSets(m)
		}
	}
	if v, ok := props["PreferredBearer"]; ok {
		if s, ok := variantAs[string](v); ok {
			bearer := ParsePreferredBearer(s)
			d.PreferredBearer = &bearer
		}
	}
	for _, name := range invalidated {
		clearDeviceProperty(d, name)
	}
}

// clearDeviceProperty drops an invalidated optional property (RSSI and
// TxPower go when a device leaves range).
func clearDeviceProperty(d *Device, name string) {
	switch name {
	case "Name":
		d.Name = nil
	case "Icon":
		d.Icon = nil
	case "Class":
		d.Class = nil
	case "Appearance":
		d.Appearance = nil
	case "UUIDs":
		d.UUIDs = nil
	case "Modalias":
		d.Modalias = nil
	case "RSSI":
		d.RSSI = nil
	case "TxPower":
		d.TxPower = nil
	case "ManufacturerData":
		d.ManufacturerData = nil
	case "ServiceData":
		d.ServiceData = nil
	case "PreferredBearer":
		d.PreferredBearer = nil
	}
}

// applyBattery folds a Battery1 dictionary into d.
func applyBattery(d *Device, props map[string]dbus.Variant, invalidated []string) {
	optFrom(props, "Percentage", &d.BatteryPercentage)
	for _, name := range invalidated {
		if name == "Percentage" {
			d.BatteryPercentage = nil
		}
	}
}

// bytesMap unwraps a dictionary of byte-array variants, skipping values
// of any other type.
func bytesMap[K comparable](m map[K]dbus.Variant) map[K][]byte {
	out := make(map[K][]byte, len(m))
	for k, v := range m {
		if b, ok := variantAs[[]byte](v); ok {
			out[k] = b
		}
	}
	return out
}

// deviceSets is DeviceSet::from_dbus over the a{oa{sv}} property.
func deviceSets(m map[dbus.ObjectPath]map[string]dbus.Variant) []DeviceSet {
	sets := make([]DeviceSet, 0, len(m))
	for path, props := range m {
		set := DeviceSet{Path: path}
		optFrom(props, "Rank", &set.Rank)
		sets = append(sets, set)
	}
	return sets
}

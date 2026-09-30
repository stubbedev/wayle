package network

import (
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const fakeWifiPath = dbus.ObjectPath("/org/freedesktop/NetworkManager/Devices/1")

// fakeWifi is the fake NM's wireless device and its access points.
type fakeWifi struct {
	f     *fakeNM
	props *prop.Properties

	mu     sync.Mutex
	aps    []dbus.ObjectPath
	nextAP int
	scans  int
}

type fakeAP struct {
	ssid     string
	strength uint8
	flags    uint32
	wpa      uint32
	rsn      uint32
	bssid    string
}

// exportWifi puts the fake's wireless device on the bus.
func (f *fakeNM) exportWifi() {
	w := f.wifi
	if w == nil {
		w = &fakeWifi{f: f}
		f.wifi = w
	}
	props, err := prop.Export(f.conn, fakeWifiPath, prop.Map{
		deviceIface: {
			"DeviceType":       {Value: uint32(deviceWifi)},
			"State":            {Value: uint32(DeviceDisconnected), Emit: prop.EmitTrue},
			"ActiveConnection": {Value: rootPath, Emit: prop.EmitTrue},
		},
		wirelessIface: {
			"LastScan":          {Value: int64(0), Emit: prop.EmitTrue},
			"ActiveAccessPoint": {Value: rootPath, Emit: prop.EmitTrue},
		},
	})
	must(f.t, err)
	w.props = props
	must(f.t, f.conn.Export(fakeWireless{w}, fakeWifiPath, wirelessIface))
}

// addAP makes an access point visible.
func (w *fakeWifi) addAP(ap fakeAP) dbus.ObjectPath {
	w.mu.Lock()
	w.nextAP++
	path := dbus.ObjectPath(fmt.Sprintf("/org/freedesktop/NetworkManager/AccessPoint/%d", w.nextAP))
	w.aps = append(w.aps, path)
	w.mu.Unlock()
	_, err := prop.Export(w.f.conn, path, prop.Map{apIface: {
		"Ssid":      {Value: []byte(ap.ssid)},
		"Strength":  {Value: ap.strength},
		"Flags":     {Value: ap.flags},
		"WpaFlags":  {Value: ap.wpa},
		"RsnFlags":  {Value: ap.rsn},
		"Frequency": {Value: uint32(2412)},
		"HwAddress": {Value: ap.bssid},
	}})
	must(w.f.t, err)
	return path
}

// deviceState moves the device and signals it, StateChanged(new, old,
// reason).
func (w *fakeWifi) deviceState(state DeviceState, reason DeviceReason) {
	old, _ := w.props.GetMust(deviceIface, "State").(uint32)
	w.props.SetMust(deviceIface, "State", uint32(state))
	must(w.f.t, w.f.conn.Emit(fakeWifiPath, deviceIface+".StateChanged", uint32(state), old, uint32(reason)))
}

type fakeWireless struct{ w *fakeWifi }

func (x fakeWireless) GetAllAccessPoints() ([]dbus.ObjectPath, *dbus.Error) {
	x.w.mu.Lock()
	defer x.w.mu.Unlock()
	return append([]dbus.ObjectPath(nil), x.w.aps...), nil
}

func (x fakeWireless) RequestScan(map[string]dbus.Variant) *dbus.Error {
	x.w.mu.Lock()
	x.w.scans++
	n := x.w.scans
	x.w.mu.Unlock()
	x.w.props.SetMust(wirelessIface, "LastScan", int64(n))
	return nil
}

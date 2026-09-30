package network

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// This file is the wifi half of the service: the device's access
// points, scanning, and connecting (wifi/mod.rs, wifi/controls.rs,
// core/device/wifi), plus the dropdown's view of the list
// (dropdowns/network/helpers.rs).

// DeviceState is NMDeviceState.
type DeviceState uint32

// Device states.
const (
	DeviceUnknown      DeviceState = 0
	DeviceUnmanaged    DeviceState = 10
	DeviceUnavailable  DeviceState = 20
	DeviceDisconnected DeviceState = 30
	DevicePrepare      DeviceState = 40
	DeviceConfig       DeviceState = 50
	DeviceNeedAuth     DeviceState = 60
	DeviceIPConfig     DeviceState = 70
	DeviceIPCheck      DeviceState = 80
	DeviceSecondaries  DeviceState = 90
	DeviceActivated    DeviceState = 100
	DeviceDeactivating DeviceState = 110
	DeviceFailed       DeviceState = 120
)

// DeviceReason is NMDeviceStateReason (the values the connect watch
// reads).
type DeviceReason uint32

// Device state reasons.
const (
	DeviceReasonNone                   DeviceReason = 0
	DeviceReasonUnknown                DeviceReason = 1
	DeviceReasonIPConfigUnavailable    DeviceReason = 5
	DeviceReasonIPConfigExpired        DeviceReason = 6
	DeviceReasonNoSecrets              DeviceReason = 7
	DeviceReasonSupplicantDisconnect   DeviceReason = 8
	DeviceReasonSupplicantConfigFailed DeviceReason = 9
	DeviceReasonSupplicantFailed       DeviceReason = 10
	DeviceReasonSupplicantTimeout      DeviceReason = 11
	DeviceReasonUserRequested          DeviceReason = 39
	DeviceReasonSSIDNotFound           DeviceReason = 53
	DeviceReasonNewActivation          DeviceReason = 60
)

// SecurityType is an access point's security classification.
type SecurityType int

// Security types.
const (
	SecurityNone SecurityType = iota
	SecurityWEP
	SecurityWPA
	SecurityWPA2
	SecurityWPA3
	SecurityEnterprise
)

// String is the English name (SecurityType::as_str).
func (s SecurityType) String() string {
	switch s {
	case SecurityWEP:
		return "WEP"
	case SecurityWPA:
		return "WPA"
	case SecurityWPA2:
		return "WPA2"
	case SecurityWPA3:
		return "WPA3"
	case SecurityEnterprise:
		return "Enterprise"
	}
	return "Open"
}

// RequiresPassword reports whether connecting asks for a password
// first; enterprise networks need more than one and are not offered.
func (s SecurityType) RequiresPassword() bool {
	return s != SecurityNone && s != SecurityEnterprise
}

// keyMgmt is the 802-11-wireless-security key-mgmt for a security type.
func (s SecurityType) keyMgmt() string {
	switch s {
	case SecurityWPA, SecurityWPA2:
		return "wpa-psk"
	case SecurityWPA3:
		return "sae"
	case SecurityEnterprise:
		return "wpa-eap"
	}
	return "none"
}

// secretKey is where the password goes: wep-key0 for WEP, psk otherwise.
func (s SecurityType) secretKey() string {
	if s == SecurityWEP {
		return "wep-key0"
	}
	return "psk"
}

// NM80211ApFlags / NM80211ApSecurityFlags bits the classification reads.
const (
	apFlagPrivacy       = 0x1
	secPairWEP40        = 0x1
	secPairWEP104       = 0x2
	secGroupWEP40       = 0x10
	secGroupWEP104      = 0x20
	secKeyMgmtPSK       = 0x100
	secKeyMgmt8021X     = 0x200
	secKeyMgmtSAE       = 0x400
	secKeyMgmtOWE       = 0x800
	secKeyMgmtOWETM     = 0x1000
	secKeyMgmtEAPSuiteB = 0x2000
)

// securityFromFlags derives the strongest security an AP supports
// (SecurityType::from_flags).
func securityFromFlags(flags, wpa, rsn uint32) SecurityType {
	const (
		enterprise = secKeyMgmt8021X | secKeyMgmtEAPSuiteB
		wpa3       = secKeyMgmtSAE | secKeyMgmtOWE | secKeyMgmtOWETM
		wep        = secPairWEP40 | secPairWEP104 | secGroupWEP40 | secGroupWEP104
	)
	switch {
	case rsn&enterprise != 0 || wpa&enterprise != 0:
		return SecurityEnterprise
	case rsn&wpa3 != 0:
		return SecurityWPA3
	case rsn&secKeyMgmtPSK != 0:
		return SecurityWPA2
	case wpa&secKeyMgmtPSK != 0:
		return SecurityWPA
	case wpa&wep != 0 || rsn&wep != 0:
		return SecurityWEP
	case flags&apFlagPrivacy != 0 && wpa == 0 && rsn == 0:
		return SecurityWEP
	}
	return SecurityNone
}

// manufacturerDefaultSSIDs are router default names locked to the BSSID
// on a new profile, so a neighbour's router of the same make is not
// joined by name (nm-applet's approach).
var manufacturerDefaultSSIDs = []string{
	"linksys", "linksys-a", "linksys-g", "default", "belkin54g",
	"NETGEAR", "o2DSL", "WLAN", "ALICE-WLAN",
}

// AccessPoint is one visible network.
type AccessPoint struct {
	// Path is the AccessPoint object, which a connect names.
	Path dbus.ObjectPath
	// SSID is the raw SSID; empty for a hidden network.
	SSID []byte
	// Strength is 0-100.
	Strength uint8
	// Security is the classification from the AP's flags.
	Security SecurityType
	// Frequency is in MHz.
	Frequency uint32
	// BSSID is the hardware address.
	BSSID string
}

// Name is the SSID as text, invalid UTF-8 replaced (to_string_lossy).
func (ap AccessPoint) Name() string { return strings.ToValidUTF8(string(ap.SSID), "�") }

// Wifi is the wireless device and its controls.
type Wifi struct {
	nm       nm
	settings *Settings
	// Device is the wireless device's object path.
	Device dbus.ObjectPath
}

// findWifiDevice is the first wireless device NM manages
// (discovery.rs's wifi_device_path).
func findWifiDevice(ctx context.Context, n nm) (dbus.ObjectPath, bool) {
	var devices []dbus.ObjectPath
	if err := n.manager().CallWithContext(ctx, nmIface+".GetDevices", 0).Store(&devices); err != nil {
		return "", false
	}
	for _, path := range devices {
		var deviceType uint32
		if err := n.prop(ctx, path, deviceIface, "DeviceType", &deviceType); err == nil && deviceType == deviceWifi {
			return path, true
		}
	}
	return "", false
}

// Enabled is NM's WirelessEnabled.
func (w *Wifi) Enabled(ctx context.Context) (bool, error) {
	var on bool
	err := w.nm.prop(ctx, nmPath, nmIface, "WirelessEnabled", &on)
	return on, err
}

// SetEnabled turns the radio on or off system-wide; off terminates
// every wifi connection.
func (w *Wifi) SetEnabled(ctx context.Context, on bool) error {
	err := w.nm.manager().CallWithContext(ctx, properties+".Set", 0, nmIface, "WirelessEnabled", dbus.MakeVariant(on)).Err
	if err != nil {
		return fmt.Errorf("cannot set wireless enabled: %w", err)
	}
	return nil
}

// Scan asks NM for a fresh scan. A refusal is usually NM saying one is
// already running, which still ends with a LastScan bump.
func (w *Wifi) Scan(ctx context.Context) error {
	err := w.nm.object(w.Device).CallWithContext(ctx, wirelessIface+".RequestScan", 0, map[string]dbus.Variant{}).Err
	if err != nil {
		return fmt.Errorf("cannot request wifi scan: %w", err)
	}
	return nil
}

// LastScan is the device's LastScan stamp; NM bumps it when a scan
// finishes, the only real "done" signal.
func (w *Wifi) LastScan(ctx context.Context) int64 {
	var at int64
	_ = w.nm.prop(ctx, w.Device, wirelessIface, "LastScan", &at)
	return at
}

// ScanAndWait scans and returns once LastScan moves past its value
// before the request, or after timeout.
func (w *Wifi) ScanAndWait(ctx context.Context, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	before := w.LastScan(ctx)
	_ = w.Scan(ctx)
	for w.LastScan(ctx) == before {
		if !sleepCtx(ctx, 200*time.Millisecond) {
			return
		}
	}
}

// AccessPoints reads every visible access point.
func (w *Wifi) AccessPoints(ctx context.Context) ([]AccessPoint, error) {
	var paths []dbus.ObjectPath
	if err := w.nm.object(w.Device).CallWithContext(ctx, wirelessIface+".GetAllAccessPoints", 0).Store(&paths); err != nil {
		return nil, fmt.Errorf("cannot get all access points: %w", err)
	}
	aps := make([]AccessPoint, 0, len(paths))
	for _, path := range paths {
		if ap, err := w.accessPoint(ctx, path); err == nil {
			aps = append(aps, ap)
		}
	}
	return aps, nil
}

func (w *Wifi) accessPoint(ctx context.Context, path dbus.ObjectPath) (AccessPoint, error) {
	var props map[string]dbus.Variant
	if err := w.nm.object(path).CallWithContext(ctx, properties+".GetAll", 0, apIface).Store(&props); err != nil {
		return AccessPoint{}, err
	}
	u32 := func(key string) uint32 { v, _ := props[key].Value().(uint32); return v }
	ssid, _ := props["Ssid"].Value().([]byte)
	strength, _ := props["Strength"].Value().(byte)
	bssid, _ := props["HwAddress"].Value().(string)
	return AccessPoint{
		Path:      path,
		SSID:      ssid,
		Strength:  strength,
		Security:  securityFromFlags(u32("Flags"), u32("WpaFlags"), u32("RsnFlags")),
		Frequency: u32("Frequency"),
		BSSID:     bssid,
	}, nil
}

// ActiveSSID is the SSID of the access point the device is on, "" when
// it is on none.
func (w *Wifi) ActiveSSID(ctx context.Context) string {
	var ap dbus.ObjectPath
	if err := w.nm.prop(ctx, w.Device, wirelessIface, "ActiveAccessPoint", &ap); err != nil || ap == rootPath || ap == "" {
		return ""
	}
	point, err := w.accessPoint(ctx, ap)
	if err != nil {
		return ""
	}
	return point.Name()
}

// KnownSSIDs are the SSIDs a saved profile exists for.
func (w *Wifi) KnownSSIDs() map[string]bool {
	known := map[string]bool{}
	for _, p := range w.settings.Profiles() {
		if p.SSID != nil {
			known[strings.ToValidUTF8(string(p.SSID), "�")] = true
		}
	}
	return known
}

// Forget deletes every saved profile of a network.
func (w *Wifi) Forget(ctx context.Context, ssid string) {
	w.settings.DeleteForSSID(ctx, []byte(ssid))
}

// Connect joins an access point. A saved profile for its SSID is reused
// (its password updated when one is given); otherwise a new profile is
// made, locked to the BSSID for a manufacturer-default SSID. It returns
// the active connection.
func (w *Wifi) Connect(ctx context.Context, apPath dbus.ObjectPath, password string) (dbus.ObjectPath, error) {
	ap, err := w.accessPoint(ctx, apPath)
	if err != nil {
		return "", fmt.Errorf("cannot get ssid: %w", err)
	}
	if saved := w.settings.ForSSID(ap.SSID); len(saved) > 0 {
		profile := saved[0]
		if password != "" {
			if err := w.updatePassword(ctx, profile.Path, password, ap.Security); err != nil {
				return "", err
			}
		}
		active, err := w.nm.activate(ctx, profile.Path, w.Device, apPath)
		if err != nil {
			return "", fmt.Errorf("cannot activate existing connection: %w", err)
		}
		return active, nil
	}
	dict := newWifiProfile(ap, password)
	var settingsPath, active dbus.ObjectPath
	if err := w.nm.manager().CallWithContext(ctx, nmIface+".AddAndActivateConnection", 0, dict, w.Device, apPath).Store(&settingsPath, &active); err != nil {
		return "", fmt.Errorf("cannot add and activate connection: %w", err)
	}
	return active, nil
}

// newWifiProfile is a new profile for an access point
// (build_connection_settings).
func newWifiProfile(ap AccessPoint, password string) ConnectionDict {
	wireless := map[string]dbus.Variant{"ssid": dbus.MakeVariant(ap.SSID)}
	if slices.Contains(manufacturerDefaultSSIDs, ap.Name()) {
		if mac, ok := parseMAC(ap.BSSID); ok {
			wireless["bssid"] = dbus.MakeVariant(mac)
		}
	}
	dict := ConnectionDict{
		"connection": {
			"type": dbus.MakeVariant("802-11-wireless"),
			"id":   dbus.MakeVariant(ap.Name()),
		},
		"802-11-wireless": wireless,
	}
	if password != "" {
		dict["802-11-wireless-security"] = map[string]dbus.Variant{
			"key-mgmt":              dbus.MakeVariant(ap.Security.keyMgmt()),
			ap.Security.secretKey(): dbus.MakeVariant(password),
		}
	}
	return dict
}

func parseMAC(s string) ([]byte, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return nil, false
	}
	mac := make([]byte, 0, 6)
	for _, part := range parts {
		b, err := strconv.ParseUint(part, 16, 8)
		if err != nil {
			return nil, false
		}
		mac = append(mac, byte(b))
	}
	return mac, true
}

func (w *Wifi) updatePassword(ctx context.Context, profile dbus.ObjectPath, password string, security SecurityType) error {
	dict, err := w.settings.Get(ctx, profile)
	if err != nil {
		return err
	}
	section := dict["802-11-wireless-security"]
	if section == nil {
		section = map[string]dbus.Variant{}
		dict["802-11-wireless-security"] = section
	}
	section["key-mgmt"] = dbus.MakeVariant(security.keyMgmt())
	section[security.secretKey()] = dbus.MakeVariant(password)
	return w.settings.Update(ctx, profile, dict)
}

// Disconnect deactivates the device's connection; none is a no-op.
func (w *Wifi) Disconnect(ctx context.Context) error {
	var active dbus.ObjectPath
	if err := w.nm.prop(ctx, w.Device, deviceIface, "ActiveConnection", &active); err != nil {
		return fmt.Errorf("cannot get active connection: %w", err)
	}
	if active == rootPath || active == "" {
		return nil
	}
	if err := w.nm.deactivate(ctx, active); err != nil {
		return fmt.Errorf("cannot deactivate connection: %w", err)
	}
	return nil
}

// ConnectFailure is why a wifi connect did not come up, in the
// dropdown's vocabulary.
type ConnectFailure int

// Connect failures.
const (
	// FailureGeneric is "Connection failed".
	FailureGeneric ConnectFailure = iota
	// FailureAuth is a refused password.
	FailureAuth
	// FailureTimeout is the attempt timing out.
	FailureTimeout
	// FailureNotFound is the network gone.
	FailureNotFound
	// FailureIPConfig is no IP address.
	FailureIPConfig
	// FailureSuperseded is the attempt replaced by another (a user
	// request or a new activation): not an error to show.
	FailureSuperseded
)

// ConnectError is a wifi connect that ended without the device coming
// up.
type ConnectError struct {
	Failure ConnectFailure
	Reason  DeviceReason
}

func (e *ConnectError) Error() string {
	return "wifi connection failed (reason " + strconv.FormatUint(uint64(e.Reason), 10) + ")"
}

// ConnectTimeout is how long a connect is watched (CONNECTION_TIMEOUT).
const ConnectTimeout = 30 * time.Second

// ConnectAndWait connects and follows the device until it is up, fails,
// or ConnectTimeout passes, reporting each intermediate state to
// progress (the connection watcher). Subscribed before connecting, so
// a fast activation is not missed.
func (w *Wifi) ConnectAndWait(ctx context.Context, apPath dbus.ObjectPath, password string, progress func(DeviceState)) error {
	ctx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	defer cancel()
	signals := make(chan *dbus.Signal, 32)
	w.nm.conn.Signal(signals)
	defer w.nm.conn.RemoveSignal(signals)
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(w.Device), dbus.WithMatchInterface(deviceIface), dbus.WithMatchMember("StateChanged")}
	if err := w.nm.conn.AddMatchSignalContext(ctx, match...); err != nil {
		return fmt.Errorf("cannot watch the device: %w", err)
	}
	defer func() { _ = w.nm.conn.RemoveMatchSignal(match...) }()
	if _, err := w.Connect(ctx, apPath, password); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return &ConnectError{Failure: FailureTimeout}
			}
			return ctx.Err()
		case sig := <-signals:
			if sig.Path != w.Device || sig.Name != deviceIface+".StateChanged" || len(sig.Body) != 3 {
				continue
			}
			state, _ := sig.Body[0].(uint32)
			reason, _ := sig.Body[2].(uint32)
			done, err := deviceStep(DeviceState(state), DeviceReason(reason), progress)
			if done {
				return err
			}
		}
	}
}

// deviceStep handles one device transition during a connect: whether
// the watch is over, and how it ended.
func deviceStep(state DeviceState, reason DeviceReason, progress func(DeviceState)) (bool, error) {
	switch state {
	case DeviceActivated:
		return true, nil
	case DeviceFailed:
		return true, &ConnectError{Failure: classifyDeviceFailure(reason), Reason: reason}
	case DeviceDisconnected:
		if isTransientDisconnect(reason) {
			return false, nil
		}
		return true, &ConnectError{Failure: classifyDeviceFailure(reason), Reason: reason}
	}
	if progress != nil {
		progress(state)
	}
	return false, nil
}

func isTransientDisconnect(reason DeviceReason) bool {
	return reason == DeviceReasonNone || reason == DeviceReasonUnknown || reason == DeviceReasonNewActivation
}

func classifyDeviceFailure(reason DeviceReason) ConnectFailure {
	switch reason {
	case DeviceReasonNoSecrets, DeviceReasonSupplicantDisconnect, DeviceReasonSupplicantConfigFailed, DeviceReasonSupplicantFailed:
		return FailureAuth
	case DeviceReasonSupplicantTimeout:
		return FailureTimeout
	case DeviceReasonSSIDNotFound:
		return FailureNotFound
	case DeviceReasonIPConfigUnavailable, DeviceReasonIPConfigExpired:
		return FailureIPConfig
	case DeviceReasonUserRequested, DeviceReasonNewActivation:
		return FailureSuperseded
	}
	return FailureGeneric
}

// ListedNetwork is one row of the dropdown's network list.
type ListedNetwork struct {
	AccessPoint
	// Known is a network with a saved profile: it connects without
	// asking for the password.
	Known bool
	// Stale was seen by an earlier scan but NM has pruned it; its path
	// is dead, and connecting means scanning for it first.
	Stale bool
}

// SortedUniqueNetworks deduplicates by SSID (strongest wins), drops
// hidden and enterprise networks and the connected SSID, and sorts by
// strength descending (sorted_unique_access_points).
func SortedUniqueNetworks(aps []AccessPoint, connected string, known map[string]bool) []ListedNetwork {
	best := map[string]ListedNetwork{}
	var order []string
	for _, ap := range aps {
		name := ap.Name()
		if len(ap.SSID) == 0 || ap.Security == SecurityEnterprise || name == connected {
			continue
		}
		existing, seen := best[name]
		if !seen {
			order = append(order, name)
		}
		if !seen || ap.Strength > existing.Strength {
			best[name] = ListedNetwork{AccessPoint: ap, Known: known[name]}
		}
	}
	out := make([]ListedNetwork, 0, len(order))
	for _, name := range order {
		out = append(out, best[name])
	}
	slices.SortStableFunc(out, func(a, b ListedNetwork) int { return cmp.Compare(b.Strength, a.Strength) })
	return out
}

// MergeWithCache folds the live list over the one already shown, so a
// network NM pruned stays listed (stale) instead of vanishing. Live
// entries win and come first; the rest follow in their previous order;
// the connected SSID is dropped from both; Known is recomputed.
func MergeWithCache(live, cached []ListedNetwork, connected string, known map[string]bool) []ListedNetwork {
	out := slices.Clone(live)
	for _, ap := range cached {
		name := ap.Name()
		if name == connected || slices.ContainsFunc(live, func(l ListedNetwork) bool { return l.Name() == name }) {
			continue
		}
		ap.Known, ap.Stale = known[name], true
		out = append(out, ap)
	}
	return out
}

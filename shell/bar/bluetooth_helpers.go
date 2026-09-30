package bar

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/bluetooth"
)

// The bluetooth dropdown's pure helpers, ported from
// crates/wayle-bar-network/src/dropdowns/bluetooth/helpers.rs. Labels
// travel as their Fluent keys (the Rust t!/td! arguments) and resolve
// through btText, so the i18n port swaps one table.

// btStrings is the en-US text of dropdowns/_bluetooth.ftl the dropdown
// uses; placeholders are the Fluent { $name } arguments.
var btStrings = map[string]string{ //nolint:gosec // Fluent UI text; the "pin" keys are labels, not credentials
	"dropdown-bluetooth-title":             "Bluetooth",
	"dropdown-bluetooth-my-devices":        "My Devices",
	"dropdown-bluetooth-available-devices": "Available Devices",
	"dropdown-bluetooth-connected":         "Connected",
	"dropdown-bluetooth-connect":           "Connect",
	"dropdown-bluetooth-disconnect":        "Disconnect",
	"dropdown-bluetooth-forget":            "Forget",
	"dropdown-bluetooth-pair":              "Pair",
	"dropdown-bluetooth-cancel":            "Cancel",
	"dropdown-bluetooth-confirm":           "Confirm",
	"dropdown-bluetooth-reject":            "Reject",
	"dropdown-bluetooth-allow":             "Allow",
	"dropdown-bluetooth-deny":              "Deny",

	"dropdown-bluetooth-battery":              "{ $percent }%",
	"dropdown-bluetooth-paired":               "Paired",
	"dropdown-bluetooth-new-device":           "New device",
	"dropdown-bluetooth-status-connecting":    "Connecting...",
	"dropdown-bluetooth-status-disconnecting": "Disconnecting...",
	"dropdown-bluetooth-status-forgetting":    "Removing...",

	"dropdown-bluetooth-type-computer":              "Computer",
	"dropdown-bluetooth-type-desktop":               "Desktop",
	"dropdown-bluetooth-type-server":                "Server",
	"dropdown-bluetooth-type-laptop":                "Laptop",
	"dropdown-bluetooth-type-handheld":              "Handheld PC",
	"dropdown-bluetooth-type-palm":                  "Palm PC",
	"dropdown-bluetooth-type-wearable-computer":     "Wearable computer",
	"dropdown-bluetooth-type-computer-tablet":       "Tablet",
	"dropdown-bluetooth-type-phone":                 "Phone",
	"dropdown-bluetooth-type-cellular":              "Cellular",
	"dropdown-bluetooth-type-cordless":              "Cordless",
	"dropdown-bluetooth-type-smartphone":            "Smart phone",
	"dropdown-bluetooth-type-modem":                 "Modem",
	"dropdown-bluetooth-type-network":               "Access point",
	"dropdown-bluetooth-type-headset":               "Headset",
	"dropdown-bluetooth-type-handsfree":             "Hands-free",
	"dropdown-bluetooth-type-microphone":            "Microphone",
	"dropdown-bluetooth-type-loudspeaker":           "Loudspeaker",
	"dropdown-bluetooth-type-headphones":            "Headphones",
	"dropdown-bluetooth-type-portable-audio":        "Portable audio",
	"dropdown-bluetooth-type-car-audio":             "Car audio",
	"dropdown-bluetooth-type-set-top-box":           "Set-top box",
	"dropdown-bluetooth-type-hifi":                  "Hi-Fi audio",
	"dropdown-bluetooth-type-vcr":                   "VCR",
	"dropdown-bluetooth-type-video-camera":          "Video camera",
	"dropdown-bluetooth-type-camcorder":             "Camcorder",
	"dropdown-bluetooth-type-video-monitor":         "Video monitor",
	"dropdown-bluetooth-type-video-display":         "Video display and loudspeaker",
	"dropdown-bluetooth-type-video-conferencing":    "Video conferencing",
	"dropdown-bluetooth-type-gaming":                "Gaming/Toy",
	"dropdown-bluetooth-type-audio-video":           "Audio/Video",
	"dropdown-bluetooth-type-keyboard":              "Keyboard",
	"dropdown-bluetooth-type-mouse":                 "Pointing device",
	"dropdown-bluetooth-type-combo-keyboard":        "Keyboard/Pointing device",
	"dropdown-bluetooth-type-joystick":              "Joystick",
	"dropdown-bluetooth-type-gamepad":               "Gamepad",
	"dropdown-bluetooth-type-remote":                "Remote control",
	"dropdown-bluetooth-type-sensing":               "Sensing device",
	"dropdown-bluetooth-type-tablet":                "Digitizer tablet",
	"dropdown-bluetooth-type-card-reader":           "Card reader",
	"dropdown-bluetooth-type-peripheral":            "Peripheral",
	"dropdown-bluetooth-type-imaging":               "Imaging",
	"dropdown-bluetooth-type-display":               "Display",
	"dropdown-bluetooth-type-camera":                "Camera",
	"dropdown-bluetooth-type-scanner":               "Scanner",
	"dropdown-bluetooth-type-printer":               "Printer",
	"dropdown-bluetooth-type-wearable":              "Wearable",
	"dropdown-bluetooth-type-wrist-watch":           "Wrist watch",
	"dropdown-bluetooth-type-pager":                 "Pager",
	"dropdown-bluetooth-type-jacket":                "Jacket",
	"dropdown-bluetooth-type-helmet":                "Helmet",
	"dropdown-bluetooth-type-glasses":               "Glasses",
	"dropdown-bluetooth-type-toy":                   "Toy",
	"dropdown-bluetooth-type-robot":                 "Robot",
	"dropdown-bluetooth-type-vehicle":               "Vehicle",
	"dropdown-bluetooth-type-doll":                  "Doll",
	"dropdown-bluetooth-type-controller":            "Controller",
	"dropdown-bluetooth-type-game":                  "Game",
	"dropdown-bluetooth-type-health":                "Health",
	"dropdown-bluetooth-type-unknown":               "Bluetooth device",
	"dropdown-bluetooth-service-serial-port":        "Serial Port",
	"dropdown-bluetooth-service-lan-access":         "LAN Access",
	"dropdown-bluetooth-service-dialup-networking":  "Dialup Networking",
	"dropdown-bluetooth-service-object-push":        "Object Push",
	"dropdown-bluetooth-service-file-transfer":      "File Transfer",
	"dropdown-bluetooth-service-headset":            "Headset Audio",
	"dropdown-bluetooth-service-audio-source":       "Audio Source",
	"dropdown-bluetooth-service-audio-sink":         "Audio Sink",
	"dropdown-bluetooth-service-remote-control":     "Remote Control",
	"dropdown-bluetooth-service-audio-distribution": "Audio Streaming",
	"dropdown-bluetooth-service-handsfree":          "Hands-Free",
	"dropdown-bluetooth-service-network-access":     "Network Access",
	"dropdown-bluetooth-service-input-device":       "Input Device",
	"dropdown-bluetooth-service-sim-access":         "SIM Access",
	"dropdown-bluetooth-service-phonebook":          "Phonebook Access",
	"dropdown-bluetooth-service-messaging":          "Messaging",
	"dropdown-bluetooth-service-unknown":            "Bluetooth Service",
	"dropdown-bluetooth-service-proprietary":        "Bluetooth Service",

	"dropdown-bluetooth-no-devices-title":       "No Devices Found",
	"dropdown-bluetooth-no-devices-description": "Make sure your device is in pairing mode",
	"dropdown-bluetooth-off-title":              "Bluetooth is Off",
	"dropdown-bluetooth-off-description":        "Turn on Bluetooth to connect devices",
	"dropdown-bluetooth-notify-title":           "Bluetooth pairing request",
	"dropdown-bluetooth-notify-body":            "{ $device } wants to connect — open the Bluetooth menu to respond",
	"dropdown-bluetooth-notify-passkey":         "{ $device } wants to pair with passkey { $passkey } — open the Bluetooth menu to respond",
	"dropdown-bluetooth-no-adapter-title":       "No Bluetooth Adapter",
	"dropdown-bluetooth-no-adapter-description": "No Bluetooth adapter was detected",
	"dropdown-bluetooth-no-new":                 "No new devices found",

	"dropdown-bluetooth-pairing-enter-pin":        "Enter this PIN on the device",
	"dropdown-bluetooth-pairing-type-on-device":   "Type the PIN on the device, then press Enter",
	"dropdown-bluetooth-pairing-enter-shown-pin":  "Enter the PIN displayed on the device",
	"dropdown-bluetooth-pairing-confirm-code":     "Confirm that this code matches the one on the device:",
	"dropdown-bluetooth-pairing-entering":         "{ $entered } of { $total } digits entered",
	"dropdown-bluetooth-pairing-allow-pairing":    "Allow this device to pair?",
	"dropdown-bluetooth-pairing-service-allow":    "Allow this device to access the requested service?",
	"dropdown-bluetooth-pairing-enter-legacy-pin": "Enter the PIN for this device",
	"dropdown-bluetooth-pairing-pin-placeholder":  "PIN",
	"dropdown-bluetooth-pairing-common-pins":      "Common PINs: 0000, 1234, 1111",
}

// btText resolves a key with its Fluent arguments as name/value pairs.
// An unknown key is a programming error and renders as the key itself,
// as Fluent does.
func btText(key string, args ...any) string {
	text, ok := btStrings[key]
	if !ok {
		return key
	}
	for i := 0; i+1 < len(args); i += 2 {
		text = strings.ReplaceAll(text, "{ $"+fmt.Sprint(args[i])+" }", fmt.Sprint(args[i+1]))
	}
	return text
}

// The Bluetooth class-of-device major classes.
const (
	majorComputer   = 0x01
	majorPhone      = 0x02
	majorNetwork    = 0x03
	majorAV         = 0x04
	majorPeripheral = 0x05
	majorImaging    = 0x06
	majorWearable   = 0x07
	majorToy        = 0x08
	majorHealth     = 0x09
)

func majorClass(raw uint32) uint32 { return (raw >> 8) & 0x1F }
func minorClass(raw uint32) uint32 { return (raw >> 2) & 0x3F }

// deviceIcon picks the row icon from BlueZ's icon hint, else the class
// of device (device_icon).
func deviceIcon(hint *string, class *uint32) string {
	if hint != nil {
		switch *hint {
		case "audio-headphones", "audio-headset":
			return "ld-headphones-symbolic"
		case "audio-speakers":
			return "ld-speaker-symbolic"
		case "input-keyboard":
			return "ld-keyboard-symbolic"
		case "input-mouse", "input-tablet":
			return "ld-mouse-symbolic"
		case "input-gaming":
			return "ld-gamepad-2-symbolic"
		case "video-display", "computer":
			return "ld-monitor-symbolic"
		case "phone":
			return "ld-smartphone-symbolic"
		}
	}
	if class == nil {
		return "ld-bluetooth-symbolic"
	}
	raw := *class
	minor := minorClass(raw)
	switch majorClass(raw) {
	case majorComputer:
		switch minor {
		case 0x03:
			return "ld-laptop-symbolic"
		case 0x07:
			return "ld-tablet-symbolic"
		}
		return "ld-monitor-symbolic"
	case majorPhone:
		return "ld-smartphone-symbolic"
	case majorAV:
		return avIcon(minor)
	case majorPeripheral:
		return peripheralIcon(minor)
	case majorImaging:
		switch {
		case minor&0x08 != 0:
			return "ld-printer-symbolic"
		case minor&0x02 != 0:
			return "ld-camera-symbolic"
		case minor&0x01 != 0:
			return "ld-monitor-symbolic"
		}
		return "ld-printer-symbolic"
	case majorWearable:
		return "ld-watch-symbolic"
	}
	return "ld-bluetooth-symbolic"
}

func avIcon(minor uint32) string {
	switch minor {
	case 0x04:
		return "ld-mic-symbolic"
	case 0x05, 0x07, 0x08, 0x0A:
		return "ld-speaker-symbolic"
	case 0x09, 0x0B:
		return "tb-device-tv-symbolic"
	case 0x0C, 0x0D:
		return "ld-camera-symbolic"
	case 0x0E, 0x0F, 0x10:
		return "ld-monitor-symbolic"
	case 0x12:
		return "ld-gamepad-2-symbolic"
	}
	return "ld-headphones-symbolic"
}

func peripheralIcon(minor uint32) string {
	switch minor >> 4 {
	case 0x01, 0x03:
		return "ld-keyboard-symbolic"
	case 0x02:
		return "ld-mouse-symbolic"
	}
	switch minor & 0x0F {
	case 0x01, 0x02:
		return "ld-gamepad-2-symbolic"
	case 0x05:
		return "ld-mouse-symbolic"
	}
	return "ld-bluetooth-symbolic"
}

// batteryLevelIcon is battery_level_icon.
func batteryLevelIcon(percent uint8) string {
	switch {
	case percent <= 5:
		return "tb-battery-vertical-symbolic"
	case percent <= 25:
		return "tb-battery-vertical-1-symbolic"
	case percent <= 50:
		return "tb-battery-vertical-2-symbolic"
	case percent <= 75:
		return "tb-battery-vertical-3-symbolic"
	}
	return "tb-battery-vertical-4-symbolic"
}

// deviceTypeKey is device_type_key: the Fluent key naming the device's
// kind.
func deviceTypeKey(hint *string, class *uint32) string {
	if hint != nil {
		switch *hint {
		case "audio-headphones":
			return "dropdown-bluetooth-type-headphones"
		case "audio-headset":
			return "dropdown-bluetooth-type-headset"
		case "audio-speakers":
			return "dropdown-bluetooth-type-loudspeaker"
		case "input-keyboard":
			return "dropdown-bluetooth-type-keyboard"
		case "input-mouse", "input-tablet":
			return "dropdown-bluetooth-type-mouse"
		case "input-gaming":
			return "dropdown-bluetooth-type-gamepad"
		case "video-display":
			return "dropdown-bluetooth-type-video-display"
		case "computer":
			return "dropdown-bluetooth-type-computer"
		case "phone":
			return "dropdown-bluetooth-type-phone"
		}
	}
	if class == nil {
		return "dropdown-bluetooth-type-unknown"
	}
	raw := *class
	minor := minorClass(raw)
	switch majorClass(raw) {
	case majorComputer:
		return pick(minor, "computer", map[uint32]string{
			0x01: "desktop", 0x02: "server", 0x03: "laptop", 0x04: "handheld",
			0x05: "palm", 0x06: "wearable-computer", 0x07: "computer-tablet",
		})
	case majorPhone:
		return pick(minor, "phone", map[uint32]string{
			0x01: "cellular", 0x02: "cordless", 0x03: "smartphone", 0x04: "modem",
		})
	case majorNetwork:
		return "dropdown-bluetooth-type-network"
	case majorAV:
		return pick(minor, "audio-video", map[uint32]string{
			0x01: "headset", 0x02: "handsfree", 0x04: "microphone", 0x05: "loudspeaker",
			0x06: "headphones", 0x07: "portable-audio", 0x08: "car-audio", 0x09: "set-top-box",
			0x0A: "hifi", 0x0B: "vcr", 0x0C: "video-camera", 0x0D: "camcorder",
			0x0E: "video-monitor", 0x0F: "video-display", 0x10: "video-conferencing", 0x12: "gaming",
		})
	case majorPeripheral:
		return peripheralTypeKey(minor)
	case majorImaging:
		switch {
		case minor&0x08 != 0:
			return "dropdown-bluetooth-type-printer"
		case minor&0x04 != 0:
			return "dropdown-bluetooth-type-scanner"
		case minor&0x02 != 0:
			return "dropdown-bluetooth-type-camera"
		case minor&0x01 != 0:
			return "dropdown-bluetooth-type-display"
		}
		return "dropdown-bluetooth-type-imaging"
	case majorWearable:
		return pick(minor, "wearable", map[uint32]string{
			0x01: "wrist-watch", 0x02: "pager", 0x03: "jacket", 0x04: "helmet", 0x05: "glasses",
		})
	case majorToy:
		return pick(minor, "toy", map[uint32]string{
			0x01: "robot", 0x02: "vehicle", 0x03: "doll", 0x04: "controller", 0x05: "game",
		})
	case majorHealth:
		return "dropdown-bluetooth-type-health"
	}
	return "dropdown-bluetooth-type-unknown"
}

// pick maps a minor class through table, falling back to the major
// class's own key.
func pick(minor uint32, fallback string, table map[uint32]string) string {
	if name, ok := table[minor]; ok {
		return "dropdown-bluetooth-type-" + name
	}
	return "dropdown-bluetooth-type-" + fallback
}

func peripheralTypeKey(minor uint32) string {
	switch minor >> 4 {
	case 0x01:
		return "dropdown-bluetooth-type-keyboard"
	case 0x02:
		return "dropdown-bluetooth-type-mouse"
	case 0x03:
		return "dropdown-bluetooth-type-combo-keyboard"
	}
	return pick(minor&0x0F, "peripheral", map[uint32]string{
		0x01: "joystick", 0x02: "gamepad", 0x03: "remote", 0x04: "sensing",
		0x05: "tablet", 0x06: "card-reader",
	})
}

// formatPasskey zero-pads a passkey to six digits.
func formatPasskey(passkey uint32) string { return fmt.Sprintf("%06d", passkey) }

const btBaseUUIDSuffix = "-0000-1000-8000-00805f9b34fb"

// serviceNameKey maps a service UUID to its Fluent key via the SIG
// assigned 16-bit numbers (service_name_key).
func serviceNameKey(uuid string) string {
	lower := strings.ToLower(uuid)
	hexPart, ok := strings.CutPrefix(lower, "0000")
	if ok {
		hexPart, ok = strings.CutSuffix(hexPart, btBaseUUIDSuffix)
	}
	short, err := strconv.ParseUint(hexPart, 16, 16)
	if !ok || err != nil {
		return "dropdown-bluetooth-service-proprietary"
	}
	name := "unknown"
	switch s := uint16(short); {
	case s == 0x1101:
		name = "serial-port"
	case s == 0x1102:
		name = "lan-access"
	case s == 0x1103:
		name = "dialup-networking"
	case s == 0x1105:
		name = "object-push"
	case s == 0x1106:
		name = "file-transfer"
	case s == 0x1108 || s == 0x1112:
		name = "headset"
	case s == 0x110a:
		name = "audio-source"
	case s == 0x110b:
		name = "audio-sink"
	case s == 0x110c || s == 0x110e || s == 0x110f:
		name = "remote-control"
	case s == 0x110d:
		name = "audio-distribution"
	case s == 0x111e || s == 0x111f:
		name = "handsfree"
	case s >= 0x1115 && s <= 0x1117:
		name = "network-access"
	case s == 0x1124:
		name = "input-device"
	case s == 0x112d:
		name = "sim-access"
	case s == 0x112f || s == 0x1130:
		name = "phonebook"
	case s >= 0x1132 && s <= 0x1134:
		name = "messaging"
	}
	return "dropdown-bluetooth-service-" + name
}

// deviceDisplay is DeviceDisplayInfo: what the pairing card shows.
type deviceDisplay struct {
	name    string
	icon    string
	typeKey string
}

// unknownDisplay is DeviceDisplayInfo::default, for a request about a
// device the model has not seen.
var unknownDisplay = deviceDisplay{name: "-", icon: "ld-bluetooth-symbolic", typeKey: "dropdown-bluetooth-type-unknown"}

// resolveDeviceDisplay is resolve_device_display.
func resolveDeviceDisplay(d bluetooth.Device) deviceDisplay {
	name := d.Alias
	if name == "" {
		name = "-"
		if d.Name != nil {
			name = *d.Name
		}
	}
	return deviceDisplay{name: name, icon: deviceIcon(d.Icon, d.Class), typeKey: deviceTypeKey(d.Icon, d.Class)}
}

// deviceCategory orders the rows: connected, paired, then the rest.
type deviceCategory uint8

const (
	categoryConnected deviceCategory = iota
	categoryPaired
	categoryAvailable
)

// deviceSnapshot is one row's data (DeviceSnapshot).
type deviceSnapshot struct {
	path      dbus.ObjectPath
	name      string
	icon      string
	typeKey   string
	battery   *uint8
	connected bool
	paired    bool
	category  deviceCategory
}

// categorizeDevice is categorize_device: nil for a device with nothing
// to call it, and for an unnamed unpaired one (an alias BlueZ derived
// from the address).
func categorizeDevice(d bluetooth.Device) *deviceSnapshot {
	category := categoryAvailable
	switch {
	case d.Connected:
		category = categoryConnected
	case d.Paired:
		category = categoryPaired
	}
	name := d.Alias
	switch {
	case name == "" && d.Name == nil:
		return nil
	case name == "":
		name = *d.Name
	case category == categoryAvailable && d.Name == nil:
		return nil
	}
	return &deviceSnapshot{
		path:      d.Path,
		name:      name,
		icon:      deviceIcon(d.Icon, d.Class),
		typeKey:   deviceTypeKey(d.Icon, d.Class),
		battery:   d.BatteryPercentage,
		connected: d.Connected,
		paired:    d.Paired,
		category:  category,
	}
}

// splitDeviceLists is build_split_device_lists: my devices (connected
// and paired) and the rest, each by category then name.
func splitDeviceLists(devices []bluetooth.Device) (mine, available []deviceSnapshot) {
	for _, d := range devices {
		snap := categorizeDevice(d)
		if snap == nil {
			continue
		}
		if snap.category == categoryAvailable {
			available = append(available, *snap)
		} else {
			mine = append(mine, *snap)
		}
	}
	order := func(a, b deviceSnapshot) int {
		return cmp.Or(cmp.Compare(a.category, b.category), strings.Compare(a.name, b.name))
	}
	slices.SortFunc(mine, order)
	slices.SortFunc(available, order)
	return mine, available
}

package bar

import (
	"strings"
	"testing"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/bluetooth"
)

func classOf(major, minor uint32) *uint32 {
	c := major<<8 | minor<<2
	return &c
}

func TestDeviceIconMatchesRust(t *testing.T) {
	for _, c := range []struct {
		hint  *string
		class *uint32
		want  string
	}{
		{new("audio-headphones"), nil, "ld-headphones-symbolic"},
		{new("audio-headset"), nil, "ld-headphones-symbolic"},
		{new("audio-speakers"), nil, "ld-speaker-symbolic"},
		{new("input-keyboard"), nil, "ld-keyboard-symbolic"},
		{new("input-mouse"), nil, "ld-mouse-symbolic"},
		{new("input-gaming"), nil, "ld-gamepad-2-symbolic"},
		{new("video-display"), nil, "ld-monitor-symbolic"},
		{new("phone"), nil, "ld-smartphone-symbolic"},
		{nil, classOf(majorComputer, 0), "ld-monitor-symbolic"},
		{nil, classOf(majorComputer, 0x03), "ld-laptop-symbolic"},
		{nil, classOf(majorPhone, 0), "ld-smartphone-symbolic"},
		{nil, classOf(majorAV, 0x06), "ld-headphones-symbolic"},
		{nil, classOf(majorAV, 0x09), "tb-device-tv-symbolic"},
		{nil, classOf(majorAV, 0x05), "ld-speaker-symbolic"},
		{nil, classOf(majorAV, 0x04), "ld-mic-symbolic"},
		{nil, classOf(majorPeripheral, 0x01<<4), "ld-keyboard-symbolic"},
		{nil, classOf(majorPeripheral, 0x02<<4), "ld-mouse-symbolic"},
		{nil, classOf(majorPeripheral, 0x02), "ld-gamepad-2-symbolic"},
		{nil, classOf(majorWearable, 0), "ld-watch-symbolic"},
		// The hint wins over the class; an unknown hint falls through.
		{new("phone"), classOf(majorComputer, 0), "ld-smartphone-symbolic"},
		{new("unknown-device"), classOf(majorPhone, 0), "ld-smartphone-symbolic"},
		{nil, nil, "ld-bluetooth-symbolic"},
		{new("alien-device"), new(uint32(0xFF00)), "ld-bluetooth-symbolic"},
	} {
		if got := deviceIcon(c.hint, c.class); got != c.want {
			t.Errorf("deviceIcon(%v, %v) = %q, want %q", c.hint, c.class, got, c.want)
		}
	}
}

func TestDeviceTypeKeyMatchesRust(t *testing.T) {
	for _, c := range []struct {
		hint  *string
		class *uint32
		want  string
	}{
		{nil, classOf(majorPeripheral, 0x01), "joystick"},
		{nil, classOf(majorPeripheral, 0x01<<4), "keyboard"},
		{nil, classOf(majorAV, 0x0B), "vcr"},
		{nil, classOf(majorAV, 0x10), "video-conferencing"},
		{nil, classOf(majorAV, 0x02), "handsfree"},
		{nil, classOf(majorAV, 0x09), "set-top-box"},
		{nil, classOf(majorAV, 0x3F), "audio-video"},
		{nil, classOf(majorComputer, 0), "computer"},
		{nil, classOf(majorComputer, 0x03), "laptop"},
		{nil, classOf(majorComputer, 0x07), "computer-tablet"},
		{nil, classOf(majorPhone, 0x03), "smartphone"},
		{nil, classOf(majorWearable, 0x01), "wrist-watch"},
		{nil, classOf(majorWearable, 0x05), "glasses"},
		{nil, classOf(majorToy, 0x01), "robot"},
		{nil, classOf(majorNetwork, 0), "network"},
		{nil, classOf(majorHealth, 0), "health"},
		{nil, classOf(majorImaging, 0x04), "scanner"},
		{new("input-keyboard"), nil, "keyboard"},
		{nil, nil, "unknown"},
	} {
		if got := deviceTypeKey(c.hint, c.class); got != "dropdown-bluetooth-type-"+c.want {
			t.Errorf("deviceTypeKey(%v, %v) = %q, want %s", c.hint, c.class, got, c.want)
		}
		if got := btText(deviceTypeKey(c.hint, c.class)); got == deviceTypeKey(c.hint, c.class) || strings.HasPrefix(got, "No localization") {
			t.Errorf("type key %q has no text: %q", deviceTypeKey(c.hint, c.class), got)
		}
	}
}

func TestBatteryLevelIcon(t *testing.T) {
	for percent, want := range map[uint8]string{
		0: "tb-battery-vertical-symbolic", 5: "tb-battery-vertical-symbolic",
		6: "tb-battery-vertical-1-symbolic", 25: "tb-battery-vertical-1-symbolic",
		26: "tb-battery-vertical-2-symbolic", 50: "tb-battery-vertical-2-symbolic",
		51: "tb-battery-vertical-3-symbolic", 75: "tb-battery-vertical-3-symbolic",
		76: "tb-battery-vertical-4-symbolic", 100: "tb-battery-vertical-4-symbolic",
	} {
		if got := batteryLevelIcon(percent); got != want {
			t.Errorf("%d%% = %q, want %q", percent, got, want)
		}
	}
}

func TestServiceNameKey(t *testing.T) {
	for uuid, want := range map[string]string{
		"0000110b-0000-1000-8000-00805f9b34fb": "audio-sink",
		"0000110A-0000-1000-8000-00805F9B34FB": "audio-source",
		"0000111f-0000-1000-8000-00805f9b34fb": "handsfree",
		"00001116-0000-1000-8000-00805f9b34fb": "network-access",
		"00001133-0000-1000-8000-00805f9b34fb": "messaging",
		"00001800-0000-1000-8000-00805f9b34fb": "unknown",
		"6e400001-b5a3-f393-e0a9-e50e24dcca9e": "proprietary",
		"0000zzzz-0000-1000-8000-00805f9b34fb": "proprietary",
	} {
		if got := serviceNameKey(uuid); got != "dropdown-bluetooth-service-"+want {
			t.Errorf("%s = %q, want %s", uuid, got, want)
		}
	}
}

func TestFormatPasskeyPads(t *testing.T) {
	if formatPasskey(42) != "000042" || formatPasskey(999999) != "999999" {
		t.Error("passkeys are six zero-padded digits")
	}
}

func TestBtText(t *testing.T) {
	// The en-US wording comes from the shared Fluent bundle, not a Go
	// table: one placeholder key and one plain key.
	if got := plain(btText("dropdown-bluetooth-battery", "percent", uint8(80))); got != "80%" {
		t.Errorf("battery = %q", got)
	}
	if got := btText("dropdown-bluetooth-forget"); got != i18n.T("dropdown-bluetooth-forget") {
		t.Errorf("forget = %q, want the bundle's text", got)
	}
	if got := plain(btText("dropdown-bluetooth-pairing-entering", "entered", 2, "total", 6)); got != "2 of 6 digits entered" {
		t.Errorf("entering = %q", got)
	}
	// An unknown key renders the loader's "No localization" marker, as
	// the Rust lookup does.
	if got := btText("dropdown-bluetooth-nope"); got != `No localization for id: "dropdown-bluetooth-nope"` {
		t.Errorf("unknown key = %q", got)
	}
}

func TestCategorizeDevice(t *testing.T) {
	name := new("Name")
	cases := []struct {
		d    bluetooth.Device
		want *deviceCategory
		name string
	}{
		{bluetooth.Device{Alias: "A", Name: name, Connected: true}, new(categoryConnected), "A"},
		{bluetooth.Device{Alias: "A", Name: name, Paired: true}, new(categoryPaired), "A"},
		{bluetooth.Device{Alias: "A", Name: name}, new(categoryAvailable), "A"},
		// No alias falls back to the name.
		{bluetooth.Device{Name: name}, new(categoryAvailable), "Name"},
		// An unnamed unpaired device (alias derived from the address)
		// is hidden; a paired one is kept by its alias.
		{bluetooth.Device{Alias: "AA-BB"}, nil, ""},
		{bluetooth.Device{Alias: "AA-BB", Paired: true}, new(categoryPaired), "AA-BB"},
		{bluetooth.Device{}, nil, ""},
	}
	for i, c := range cases {
		got := categorizeDevice(c.d)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("case %d: got %+v, want hidden", i, got)
		case c.want != nil && (got == nil || got.category != *c.want || got.name != c.name):
			t.Errorf("case %d: got %+v, want %v %q", i, got, *c.want, c.name)
		}
	}
}

func TestSplitDeviceListsSorts(t *testing.T) {
	name := new("n")
	mine, avail := splitDeviceLists([]bluetooth.Device{
		{Alias: "Zed", Name: name, Paired: true},
		{Alias: "Bee", Name: name},
		{Alias: "Yak", Name: name, Connected: true},
		{Alias: "Ant", Name: name, Paired: true},
		{Alias: "Ape", Name: name},
	})
	var got []string
	for _, s := range mine {
		got = append(got, s.name)
	}
	if len(got) != 3 || got[0] != "Yak" || got[1] != "Ant" || got[2] != "Zed" {
		t.Errorf("mine = %v, want connected first then paired by name", got)
	}
	if len(avail) != 2 || avail[0].name != "Ape" || avail[1].name != "Bee" {
		t.Errorf("available = %+v", avail)
	}
}

func TestResolveDeviceDisplay(t *testing.T) {
	if d := resolveDeviceDisplay(bluetooth.Device{}); d.name != "-" || d.icon != "ld-bluetooth-symbolic" {
		t.Errorf("empty device display = %+v", d)
	}
	if d := resolveDeviceDisplay(bluetooth.Device{Name: new("Pods")}); d.name != "Pods" {
		t.Errorf("name fallback = %+v", d)
	}
}

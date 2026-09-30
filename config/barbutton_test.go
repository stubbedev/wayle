package config

import (
	"strings"
	"testing"
)

// buttonSchema is one module's bar-button defaults as its Rust schema
// (crates/wayle-config/src/schemas/modules/*/mod.rs) and bar module
// (auto_icon_color) declare them.
type buttonSchema struct {
	icon, label, iconBg, buttonBg, border string
	auto                                  CssToken
	labelShow                             bool
	labelMax                              int
}

func TestButtonDefaultsMatchTheRustSchemas(t *testing.T) {
	d := Defaults()
	bse := "bg-surface-elevated"
	for _, tc := range []struct {
		module string
		got    ButtonConfig
		want   buttonSchema
	}{
		{"battery", d.Battery.Button, buttonSchema{"auto", "yellow", "yellow", bse, "yellow", TokenYellow, true, 0}},
		{"bluetooth", d.Bluetooth.Button, buttonSchema{"auto", "blue", "blue", bse, "blue", TokenBlue, true, 15}},
		{"brightness", d.Brightness.Button, buttonSchema{"auto", "yellow", "yellow", bse, "yellow", TokenYellow, true, 0}},
		{"clock", d.Clock.Button, buttonSchema{"auto", "accent", "accent", bse, "border-accent", TokenAccent, true, 0}},
		{"cpu", d.CPU.Button, buttonSchema{"auto", "blue", "blue", bse, "blue", TokenBlue, true, 0}},
		{"hyprsunset", d.Hyprsunset.Button, buttonSchema{"auto", "yellow", "yellow", bse, "yellow", TokenYellow, true, 0}},
		{"idle-inhibit", d.IdleInhibit.Button, buttonSchema{"auto", "green", "green", bse, "green", TokenGreen, true, 0}},
		{"keybind-mode", d.KeybindMode.Button, buttonSchema{"auto", "blue", "blue", bse, "blue", TokenBlue, true, 0}},
		{"keyboard-input", d.KeyboardInput.Button, buttonSchema{"auto", "yellow", "yellow", bse, "yellow", TokenYellow, true, 0}},
		{"mail", d.Mail.Button, buttonSchema{"auto", "auto", bse, bse, "blue", TokenBlue, true, 0}},
		{"media", d.Media.Button, buttonSchema{"auto", "blue", "blue", bse, "blue", TokenBlue, true, 35}},
		{"microphone", d.Microphone.Button, buttonSchema{"auto", "red", "red", bse, "red", TokenRed, true, 0}},
		{"netstat", d.Netstat.Button, buttonSchema{"auto", "red", "red", bse, "red", TokenRed, true, 0}},
		{"network", d.Network.Button, buttonSchema{"auto", "accent", "accent", bse, "accent", TokenAccent, true, 15}},
		{"notifications", d.Notification.Button, buttonSchema{"auto", "green", "green", bse, "green", TokenGreen, true, 0}},
		{"power", d.Power.Button, buttonSchema{"auto", "fg-default", "red", bse, "red", TokenRed, false, 0}},
		{"power-profiles", d.PowerProfiles.Button, buttonSchema{"auto", "auto", bse, bse, "blue", TokenBlue, false, 0}},
		{"ram", d.RAM.Button, buttonSchema{"auto", "green", "green", bse, "green", TokenGreen, true, 0}},
		{"recorder", d.Recorder.Button, buttonSchema{"auto", "red", "red", bse, "red", TokenRed, true, 0}},
		{"storage", d.Storage.Button, buttonSchema{"auto", "yellow", "yellow", bse, "yellow", TokenYellow, true, 0}},
		{"treeman", d.Treeman.Button, buttonSchema{"auto", "accent", "accent", bse, "border-accent", TokenAccent, true, 0}},
		{"volume", d.Volume.Button, buttonSchema{"auto", "red", "red", bse, "red", TokenRed, true, 0}},
		{"weather", d.Weather.Button, buttonSchema{"auto", "accent", "accent", bse, "border-accent", TokenAccent, true, 0}},
		{"window-title", d.WindowTitle.Button, buttonSchema{"auto", "blue", "blue", bse, "blue", TokenBlue, true, 50}},
		{"world-clock", d.WorldClock.Button, buttonSchema{"auto", "yellow", "yellow", bse, "yellow", TokenYellow, true, 0}},
		{"custom", DefaultsCustomModule().Button, buttonSchema{"auto", "auto", "auto", bse, "auto", TokenAccent, true, 0}},
	} {
		w := tc.want
		want := ButtonConfig{
			IconShow: true, LabelShow: w.labelShow, LabelMaxLength: w.labelMax,
			Colors:        buttonColors(w.icon, w.label, w.iconBg, w.buttonBg, w.border),
			AutoIconColor: w.auto,
		}
		want.Defaults = want.Colors
		if tc.got != want {
			t.Errorf("%s: button defaults =\n%+v\nwant\n%+v", tc.module, tc.got, want)
		}
	}
	for name, c := range map[string]ContainerConfig{"cava": d.Cava.Container, "systray": d.Systray.Container} {
		if c != DefaultsContainer("bg-surface-elevated", "border-accent") || c.BorderShow {
			t.Errorf("%s: container defaults = %+v", name, c)
		}
	}
}

// The legacy fields the modules had before ButtonConfig agree with it
// by default, so a reader of either sees the schema value.
func TestLegacyFieldsAgreeWithButtonDefaults(t *testing.T) {
	d := Defaults()
	for name, pair := range map[string][2]bool{
		"battery":        {d.Battery.LabelShow, d.Battery.Button.LabelShow},
		"volume":         {d.Volume.LabelShow, d.Volume.Button.LabelShow},
		"media":          {d.Media.LabelShow, d.Media.Button.LabelShow},
		"power-profiles": {d.PowerProfiles.LabelShow, d.PowerProfiles.Button.LabelShow},
		"window-title":   {d.WindowTitle.LabelShow, d.WindowTitle.Button.LabelShow},
		"cpu":            {d.CPU.LabelShow, d.CPU.Button.LabelShow},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: LabelShow %v != Button.LabelShow %v", name, pair[0], pair[1])
		}
	}
	if d.Media.LabelMaxLength != d.Media.Button.LabelMaxLength || d.WindowTitle.LabelMaxLength != d.WindowTitle.Button.LabelMaxLength {
		t.Error("LabelMaxLength disagrees with Button.LabelMaxLength")
	}
	if d.Battery.Icon.Color != d.Battery.Button.Colors.Icon || d.Battery.Icon.Show != d.Battery.Button.IconShow {
		t.Error("battery Icon disagrees with its Button")
	}
}

func TestApplyButtonDecodesEveryKey(t *testing.T) {
	path := writeConfig(t, `
[modules.battery]
border-show = true
border-color = "#123456"
icon-show = false
icon-color = "red"
icon-bg-color = "transparent"
label-show = false
label-color = "fg-muted"
label-max-length = 12
button-bg-color = "bg-hover"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b := cfg.Battery.Button
	want := DefaultsBattery().Button
	want.BorderShow, want.IconShow, want.LabelShow, want.LabelMaxLength = true, false, false, 12
	want.Colors = ButtonColors{
		Icon: mustColor("red"), Label: mustColor("fg-muted"), IconBg: mustColor("transparent"),
		ButtonBg: mustColor("bg-hover"), Border: mustColor("#123456"),
	}
	if b != want {
		t.Errorf("button =\n%+v\nwant\n%+v", b, want)
	}
	if b.Defaults != DefaultsBattery().Button.Defaults {
		t.Error("decoding must not move the schema defaults")
	}
	// The legacy fields follow the decoded keys.
	if cfg.Battery.LabelShow || cfg.Battery.Icon.Show || cfg.Battery.Icon.Color.Token != TokenRed {
		t.Errorf("legacy fields = %v / %+v, want the decoded keys", cfg.Battery.LabelShow, cfg.Battery.Icon)
	}
}

func TestApplyButtonMirrorsLegacyFieldsPerModule(t *testing.T) {
	path := writeConfig(t, `
[modules.media]
label-show = false
label-max-length = 7
[modules.recorder]
icon-show = false
icon-color = "green"
[modules.hyprsunset]
icon-color = "blue"
[modules.power-profiles]
icon-show = false
[[modules.custom]]
id = "a"
label-max-length = 3
icon-color = "#ff00ff"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Media.LabelShow || cfg.Media.LabelMaxLength != 7 {
		t.Errorf("media legacy = %v/%d", cfg.Media.LabelShow, cfg.Media.LabelMaxLength)
	}
	if cfg.Recorder.Icon.Show || cfg.Recorder.Icon.Color.Token != TokenGreen {
		t.Errorf("recorder icon = %+v", cfg.Recorder.Icon)
	}
	for name, icon := range cfg.Recorder.Icons {
		if icon.Show {
			t.Errorf("recorder %s icon still shown", name)
		}
	}
	if cfg.Hyprsunset.IconOn.Color.Token != TokenBlue || cfg.Hyprsunset.IconOff.Color.Token != TokenBlue {
		t.Errorf("hyprsunset icons = %+v / %+v", cfg.Hyprsunset.IconOn, cfg.Hyprsunset.IconOff)
	}
	for name, icon := range cfg.PowerProfiles.Icons {
		if icon.Show {
			t.Errorf("power-profiles %s icon still shown", name)
		}
	}
	custom := cfg.Custom[0]
	if custom.LabelMaxLength != 3 || custom.Icon.Color.Hex != "#ff00ff" || custom.Button.Colors.Icon.Hex != "#ff00ff" {
		t.Errorf("custom = %+v", custom)
	}
	// ConfigProperty::new: the custom module's fallback is its value.
	if custom.Button.Defaults != custom.Button.Colors {
		t.Errorf("custom defaults = %+v, want the configured colors", custom.Button.Defaults)
	}
}

func TestApplyButtonHonorsTheModuleKeySet(t *testing.T) {
	// The power schema declares only border-show, border-color,
	// icon-color, and icon-bg-color; the rest are not keys there.
	path := writeConfig(t, `
[modules.power]
border-show = true
icon-color = "blue"
icon-bg-color = "green"
label-show = true
label-color = "red"
icon-show = false
button-bg-color = "bg-hover"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	b := cfg.Power.Button
	if !b.BorderShow || b.Colors.Icon.Token != TokenBlue || b.Colors.IconBg.Token != TokenGreen {
		t.Errorf("declared keys not applied: %+v", b)
	}
	if b.LabelShow || !b.IconShow || b.Colors.Label.Token != TokenFgDefault || b.Colors.ButtonBg.Token != TokenBgSurfaceElevated {
		t.Errorf("undeclared keys must be ignored: %+v", b)
	}
}

func TestApplyButtonRejectsBadValues(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"[modules.volume]\nicon-bg-color = \"nope\"", "nope"},
		{"[modules.volume]\nlabel-color = \"#12\"", "#12"},
		{"[modules.media]\nlabel-max-length = -1", "label-max-length"},
		{"[modules.media]\nlabel-max-length = 4294967296", "label-max-length"},
		{"[modules.clock]\nborder-color = \"sideways\"", "sideways"},
		{"[modules.volume]\nborder-show = \"yes\"", "border-show"},
		{"[[modules.custom]]\nid = \"a\"\nbutton-bg-color = \"nope\"", "nope"},
		{"[modules.cava]\nbutton-bg-color = \"nope\"", "nope"},
		{"[modules.systray]\nborder-color = \"#zz\"", "#zz"},
	} {
		path := writeConfig(t, tc.body+"\n")
		if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want %q", tc.body, err, tc.want)
		}
	}
}

func TestApplyContainerDecodes(t *testing.T) {
	path := writeConfig(t, `
[modules.cava]
border-show = true
border-color = "red"
button-bg-color = "transparent"
[modules.systray]
button-bg-color = "#101010"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	c := cfg.Cava.Container
	if !c.BorderShow || c.BorderColor.Token != TokenRed || c.Background.Kind != ColorTransparent {
		t.Errorf("cava container = %+v", c)
	}
	if c.DefaultBackground.Token != TokenBgSurfaceElevated || c.DefaultBorderColor.Token != TokenBorderAccent {
		t.Errorf("container defaults moved: %+v", c)
	}
	if cfg.Systray.Container.Background.Hex != "#101010" || cfg.Systray.Container.BorderShow {
		t.Errorf("systray container = %+v", cfg.Systray.Container)
	}
}

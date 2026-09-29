package config

import (
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
)

// Bar is the bar chrome: per-monitor layout, spacing, and placement.
// The fields carry the BarConfig keys the Go shell consumes; the button
// styling keys follow with the bar button component.
type Bar struct {
	Location          Location
	Layer             Layer
	Exclusive         bool
	BG                ColorValue
	BackgroundOpacity int
	BorderColor       ColorValue
	BorderLocation    BorderLocation
	BorderWidth       int
	// ButtonLabelSize is the module label font size: a multiplier of
	// the 1.04rem button-label base, or absolute pixels.
	ButtonLabelSize Size
	ModuleGap       Size
	Padding         Size
	PaddingEnds     Size
	InsetEdge       Size
	InsetEnds       Size
	Rounding        RoundingLevel
	Scale           float64
	Layout          []BarLayout

	// Button group container styling.
	ButtonGroupModuleGap      Size
	ButtonGroupPadding        Size
	ButtonGroupBackground     ColorValue
	ButtonGroupOpacity        int
	ButtonGroupBorderColor    ColorValue
	ButtonGroupBorderLocation BorderLocation
	ButtonGroupBorderWidth    int
	ButtonGroupRounding       RoundingLevel
}

// BarLayout is the bar layout for one monitor. Monitor is a connector
// name ("DP-1") or "*" for every monitor without an exact entry;
// Extends inherits the unset sections of another layout by its monitor
// value.
type BarLayout struct {
	Monitor string
	Extends string
	Show    bool
	Left    []BarItem
	Center  []BarItem
	Right   []BarItem
}

// ClockConfig is the clock module configuration; the format is strftime.
type ClockConfig struct {
	Click  ClickConfig
	Format string
}

// GeneralConfig is the cross-shell general section.
type GeneralConfig struct {
	FontSans string
	FontMono string
}

// Config is the loaded user configuration.
type Config struct {
	Bar                Bar
	Clock              ClockConfig
	Battery            BatteryConfig
	Brightness         BrightnessConfig
	Volume             VolumeConfig
	Media              MediaConfig
	Network            NetworkConfig
	Microphone         MicrophoneConfig
	Bluetooth          BluetoothConfig
	KeyboardInput      KeyboardInputConfig
	WindowTitle        WindowTitleConfig
	CPU                CPUConfig
	RAM                RAMConfig
	Storage            StorageConfig
	Weather            WeatherConfig
	WorldClock         WorldClockConfig
	Netstat            NetstatConfig
	Mail               MailConfig
	Power              PowerConfig
	KeybindMode        KeybindModeConfig
	PowerProfiles      PowerProfilesConfig
	Hyprsunset         HyprsunsetConfig
	Custom             []CustomModuleConfig
	Cava               CavaConfig
	Separator          SeparatorConfig
	HyprlandWorkspaces HyprlandWorkspacesConfig
	General            GeneralConfig
}

// Defaults returns the schema defaults for every modeled section.
func Defaults() *Config {
	return &Config{
		Bar: Bar{
			Location:                  LocationTop,
			Layer:                     LayerTop,
			Exclusive:                 true,
			BG:                        mustColor("bg-surface"),
			BackgroundOpacity:         100,
			BorderColor:               mustColor("border-accent"),
			BorderLocation:            BorderNone,
			BorderWidth:               1,
			ButtonLabelSize:           Size{Value: 1.0, Unit: SizeMultiplier},
			ModuleGap:                 Size{Value: 0.5, Unit: SizeMultiplier},
			Padding:                   Size{Value: 0.35, Unit: SizeMultiplier},
			PaddingEnds:               Size{Value: 0.5, Unit: SizeMultiplier},
			InsetEdge:                 Size{Value: 0, Unit: SizeMultiplier},
			InsetEnds:                 Size{Value: 0, Unit: SizeMultiplier},
			Rounding:                  RoundingNone,
			Scale:                     1.0,
			ButtonGroupModuleGap:      Size{Value: 0.25, Unit: SizeMultiplier},
			ButtonGroupPadding:        Size{Value: 0.0, Unit: SizeMultiplier},
			ButtonGroupBackground:     mustColor("bg-elevated"),
			ButtonGroupOpacity:        100,
			ButtonGroupBorderColor:    mustColor("border-accent"),
			ButtonGroupBorderLocation: BorderNone,
			ButtonGroupBorderWidth:    1,
			ButtonGroupRounding:       RoundingSm,
		},
		Clock: ClockConfig{
			Click:  DefaultsClick(map[string]string{"left-click": "dropdown:calendar", "right-click": "dropdown:weather"}),
			Format: "%a %b %d %I:%M %p",
		},
		Cava:               DefaultsCava(),
		Battery:            DefaultsBattery(),
		Brightness:         DefaultsBrightness(),
		Volume:             DefaultsVolume(),
		Media:              DefaultsMedia(),
		Network:            DefaultsNetwork(),
		Microphone:         DefaultsMicrophone(),
		Bluetooth:          DefaultsBluetooth(),
		KeyboardInput:      DefaultsKeyboardInput(),
		WindowTitle:        DefaultsWindowTitle(),
		CPU:                DefaultsSysinfoCpu(),
		RAM:                DefaultsSysinfoRam(),
		Storage:            DefaultsSysinfoStorage(),
		Weather:            DefaultsWeather(),
		WorldClock:         DefaultsWorldClock(),
		Netstat:            DefaultsNetstat(),
		Mail:               DefaultsMail(),
		Power:              DefaultsPower(),
		KeybindMode:        DefaultsKeybindMode(),
		PowerProfiles:      DefaultsPowerProfiles(),
		Hyprsunset:         DefaultsHyprsunset(),
		Separator:          DefaultsSeparator(),
		HyprlandWorkspaces: DefaultsHyprlandWorkspaces(),
		General: GeneralConfig{
			FontSans: "Inter",
			FontMono: "JetBrains Mono",
		},
	}
}

// mustColor panics only on a typo in these literal defaults; user
// config never flows through it.
func mustColor(s string) ColorValue {
	cv, err := ParseColorValue(s)
	if err != nil {
		panic(err)
	}
	return cv
}

// fileDoc mirrors the top-level TOML document. Sections the Go shell
// does not model yet (osd, launcher, modules beyond the clock, ...) are
// ignored here exactly as serde ignores unknown fields in Rust.
type fileDoc struct {
	Bar     *toml.Primitive `toml:"bar"`
	Modules *struct {
		Clock              *toml.Primitive `toml:"clock"`
		Cava               *toml.Primitive `toml:"cava"`
		Battery            *toml.Primitive `toml:"battery"`
		Brightness         *toml.Primitive `toml:"brightness"`
		Volume             *toml.Primitive `toml:"volume"`
		Media              *toml.Primitive `toml:"media"`
		Network            *toml.Primitive `toml:"network"`
		Microphone         *toml.Primitive `toml:"microphone"`
		Bluetooth          *toml.Primitive `toml:"bluetooth"`
		KeyboardLayout     *toml.Primitive `toml:"keyboard-input"`
		WindowTitle        *toml.Primitive `toml:"window-title"`
		CPU                *toml.Primitive `toml:"cpu"`
		RAM                *toml.Primitive `toml:"ram"`
		Storage            *toml.Primitive `toml:"storage"`
		Weather            *toml.Primitive `toml:"weather"`
		WorldClock         *toml.Primitive `toml:"world-clock"`
		Netstat            *toml.Primitive `toml:"netstat"`
		Mail               *toml.Primitive `toml:"mail"`
		Power              *toml.Primitive `toml:"power"`
		KeybindMode        *toml.Primitive `toml:"keybind-mode"`
		PowerProfiles      *toml.Primitive `toml:"power-profiles"`
		Hyprsunset         *toml.Primitive `toml:"hyprsunset"`
		Custom             *[]customDoc    `toml:"custom"`
		Separator          *toml.Primitive `toml:"separator"`
		HyprlandWorkspaces *toml.Primitive `toml:"hyprland-workspaces"`
	} `toml:"modules"`
	General *toml.Primitive `toml:"general"`
}

// barDoc mirrors the [bar] table; the leaf values defer through
// tomlValue where the schema allows number|string unions, and layout is
// handled by BarItem's UnmarshalTOML through the slice element decode.
type barDoc struct {
	Location                  string      `toml:"location"`
	Layer                     string      `toml:"layer"`
	Exclusive                 bool        `toml:"exclusive"`
	BG                        string      `toml:"bg"`
	BackgroundOpacity         *int        `toml:"background-opacity"`
	BorderColor               string      `toml:"border-color"`
	BorderLocation            string      `toml:"border-location"`
	BorderWidth               *int        `toml:"border-width"`
	ButtonLabelSize           tomlValue   `toml:"button-label-size"`
	ModuleGap                 tomlValue   `toml:"module-gap"`
	ButtonGroupModuleGap      tomlValue   `toml:"button-group-module-gap"`
	ButtonGroupPadding        tomlValue   `toml:"button-group-padding"`
	ButtonGroupBackground     string      `toml:"button-group-background"`
	ButtonGroupOpacity        *int        `toml:"button-group-opacity"`
	ButtonGroupBorderColor    string      `toml:"button-group-border-color"`
	ButtonGroupBorderLocation string      `toml:"button-group-border-location"`
	ButtonGroupBorderWidth    *int        `toml:"button-group-border-width"`
	ButtonGroupRounding       string      `toml:"button-group-rounding"`
	Padding                   tomlValue   `toml:"padding"`
	PaddingEnds               tomlValue   `toml:"padding-ends"`
	InsetEdge                 tomlValue   `toml:"inset-edge"`
	InsetEnds                 tomlValue   `toml:"inset-ends"`
	Rounding                  string      `toml:"rounding"`
	Scale                     *float64    `toml:"scale"`
	Layout                    []BarLayout `toml:"layout"`
}

type generalDoc struct {
	FontSans string `toml:"font-sans"`
	FontMono string `toml:"font-mono"`
}

// tomlValue defers a leaf's decode so Size can accept number|string.
type tomlValue struct {
	value any
}

func (t *tomlValue) UnmarshalTOML(value any) error {
	t.value = value
	return nil
}

// applyTOML overlays a config.toml document onto the defaults. Every
// failure is a load error; the receiver keeps the pre-failure state.
func (c *Config) applyTOML(data []byte) error {
	var doc fileDoc
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return err
	}
	if doc.Bar != nil {
		bar := barDoc{}
		if err := md.PrimitiveDecode(*doc.Bar, &bar); err != nil {
			return err
		}
		applied, err := bar.toBar()
		if err != nil {
			return err
		}
		c.Bar = applied
	}
	if doc.Modules != nil && doc.Modules.Clock != nil {
		clock := struct {
			Format string `toml:"format"`
		}{}
		if err := md.PrimitiveDecode(*doc.Modules.Clock, &clock); err != nil {
			return err
		}
		clicks, err := applyClicks(md, *doc.Modules.Clock, c.Clock.Click)
		if err != nil {
			return err
		}
		c.Clock.Click = clicks
		c.Clock.Format = clock.Format
	}
	if doc.Modules != nil && doc.Modules.CPU != nil {
		cpu, err := applyCpu(md, *doc.Modules.CPU)
		if err != nil {
			return err
		}
		c.CPU = cpu
	}
	if doc.Modules != nil && doc.Modules.RAM != nil {
		ram, err := applyRam(md, *doc.Modules.RAM)
		if err != nil {
			return err
		}
		c.RAM = ram
	}
	if doc.Modules != nil && doc.Modules.Weather != nil {
		w, err := applyWeather(md, *doc.Modules.Weather)
		if err != nil {
			return err
		}
		c.Weather = w
	}
	if doc.Modules != nil && doc.Modules.Hyprsunset != nil {
		hs, err := applyHyprsunset(md, *doc.Modules.Hyprsunset)
		if err != nil {
			return err
		}
		c.Hyprsunset = hs
	}
	if doc.Modules != nil && doc.Modules.PowerProfiles != nil {
		pp, err := applyPowerProfiles(md, *doc.Modules.PowerProfiles)
		if err != nil {
			return err
		}
		c.PowerProfiles = pp
	}
	if doc.Modules != nil && doc.Modules.KeybindMode != nil {
		km, err := applyKeybindMode(md, *doc.Modules.KeybindMode)
		if err != nil {
			return err
		}
		c.KeybindMode = km
	}
	if doc.Modules != nil && doc.Modules.Power != nil {
		pw, err := applyPower(md, *doc.Modules.Power)
		if err != nil {
			return err
		}
		c.Power = pw
	}
	if doc.Modules != nil && doc.Modules.Mail != nil {
		ml, err := applyMail(md, *doc.Modules.Mail)
		if err != nil {
			return err
		}
		c.Mail = ml
	}
	if doc.Modules != nil && doc.Modules.Netstat != nil {
		ns, err := applyNetstat(md, *doc.Modules.Netstat)
		if err != nil {
			return err
		}
		c.Netstat = ns
	}
	if doc.Modules != nil && doc.Modules.WorldClock != nil {
		wc, err := applyWorldClock(md, *doc.Modules.WorldClock)
		if err != nil {
			return err
		}
		c.WorldClock = wc
	}
	if doc.Modules != nil && doc.Modules.Storage != nil {
		st, err := applyStorage(md, *doc.Modules.Storage)
		if err != nil {
			return err
		}
		c.Storage = st
	}
	if doc.Modules != nil && doc.Modules.WindowTitle != nil {
		wt, err := applyWindowTitle(md, *doc.Modules.WindowTitle)
		if err != nil {
			return err
		}
		c.WindowTitle = wt
	}
	if doc.Modules != nil && doc.Modules.KeyboardLayout != nil {
		ki, err := applyKeyboardInput(md, *doc.Modules.KeyboardLayout)
		if err != nil {
			return err
		}
		c.KeyboardInput = ki
	}
	if doc.Modules != nil && doc.Modules.Custom != nil {
		defs, err := applyCustomDefinitions(*doc.Modules.Custom)
		if err != nil {
			return err
		}
		c.Custom = defs
	}
	if doc.Modules != nil && doc.Modules.Bluetooth != nil {
		bt, err := applyBluetooth(md, *doc.Modules.Bluetooth)
		if err != nil {
			return err
		}
		c.Bluetooth = bt
	}
	if doc.Modules != nil && doc.Modules.Microphone != nil {
		mic, err := applyMicrophone(md, *doc.Modules.Microphone)
		if err != nil {
			return err
		}
		c.Microphone = mic
	}
	if doc.Modules != nil && doc.Modules.Network != nil {
		net, err := applyNetwork(md, *doc.Modules.Network)
		if err != nil {
			return err
		}
		c.Network = net
	}
	if doc.Modules != nil && doc.Modules.Media != nil {
		med, err := applyMedia(md, *doc.Modules.Media)
		if err != nil {
			return err
		}
		c.Media = med
	}
	if doc.Modules != nil && doc.Modules.Volume != nil {
		vol, err := applyVolume(md, *doc.Modules.Volume)
		if err != nil {
			return err
		}
		c.Volume = vol
	}
	if doc.Modules != nil && doc.Modules.Brightness != nil {
		br, err := applyBrightness(md, *doc.Modules.Brightness)
		if err != nil {
			return err
		}
		c.Brightness = br
	}
	if doc.Modules != nil && doc.Modules.Cava != nil {
		cava, err := applyCava(md, *doc.Modules.Cava)
		if err != nil {
			return err
		}
		c.Cava = cava
	}
	if doc.Modules != nil && doc.Modules.Battery != nil {
		b, err := applyBattery(md, *doc.Modules.Battery)
		if err != nil {
			return err
		}
		c.Battery = b
	}
	if doc.Modules != nil && doc.Modules.Separator != nil {
		sep, err := applySeparator(md, *doc.Modules.Separator)
		if err != nil {
			return err
		}
		c.Separator = sep
	}
	if doc.Modules != nil && doc.Modules.HyprlandWorkspaces != nil {
		hw, err := applyHyprlandWorkspaces(md, *doc.Modules.HyprlandWorkspaces)
		if err != nil {
			return err
		}
		c.HyprlandWorkspaces = hw
	}
	if doc.General != nil {
		general := generalDoc{}
		if err := md.PrimitiveDecode(*doc.General, &general); err != nil {
			return err
		}
		c.General = GeneralConfig(general)
	}
	return nil
}

func (b barDoc) toBar() (Bar, error) {
	bar := Defaults().Bar
	if b.Location != "" {
		bar.Location = Location(b.Location)
	}
	if b.Layer != "" {
		bar.Layer = Layer(b.Layer)
	}
	bar.Exclusive = b.Exclusive
	if b.BG != "" {
		cv, err := ParseColorValue(b.BG)
		if err != nil {
			return Bar{}, fmt.Errorf("bar: bg: %w", err)
		}
		bar.BG = cv
	}
	if b.BorderColor != "" {
		cv, err := ParseColorValue(b.BorderColor)
		if err != nil {
			return Bar{}, fmt.Errorf("bar: border-color: %w", err)
		}
		bar.BorderColor = cv
	}
	if b.BorderLocation != "" {
		bar.BorderLocation = BorderLocation(b.BorderLocation)
	}
	if b.BorderWidth != nil {
		if *b.BorderWidth < 0 || *b.BorderWidth > 255 {
			return Bar{}, fmt.Errorf("bar: border-width %d outside 0-255", *b.BorderWidth)
		}
		bar.BorderWidth = *b.BorderWidth
	}
	if b.ButtonLabelSize.value != nil {
		if err := bar.ButtonLabelSize.unmarshal(b.ButtonLabelSize.value, "button-label-size"); err != nil {
			return Bar{}, err
		}
	}
	if b.ModuleGap.value != nil {
		if err := bar.ModuleGap.unmarshal(b.ModuleGap.value, "module-gap"); err != nil {
			return Bar{}, err
		}
	}
	if b.Padding.value != nil {
		if err := bar.Padding.unmarshal(b.Padding.value, "padding"); err != nil {
			return Bar{}, err
		}
	}
	if b.PaddingEnds.value != nil {
		if err := bar.PaddingEnds.unmarshal(b.PaddingEnds.value, "padding-ends"); err != nil {
			return Bar{}, err
		}
	}
	if b.InsetEdge.value != nil {
		if err := bar.InsetEdge.unmarshal(b.InsetEdge.value, "inset-edge"); err != nil {
			return Bar{}, err
		}
	}
	if b.InsetEnds.value != nil {
		if err := bar.InsetEnds.unmarshal(b.InsetEnds.value, "inset-ends"); err != nil {
			return Bar{}, err
		}
	}
	if b.ButtonGroupModuleGap.value != nil {
		if err := bar.ButtonGroupModuleGap.unmarshal(b.ButtonGroupModuleGap.value, "button-group-module-gap"); err != nil {
			return Bar{}, err
		}
	}
	if b.ButtonGroupPadding.value != nil {
		if err := bar.ButtonGroupPadding.unmarshal(b.ButtonGroupPadding.value, "button-group-padding"); err != nil {
			return Bar{}, err
		}
	}
	if b.ButtonGroupBackground != "" {
		cv, err := ParseColorValue(b.ButtonGroupBackground)
		if err != nil {
			return Bar{}, fmt.Errorf("bar: button-group-background: %w", err)
		}
		bar.ButtonGroupBackground = cv
	}
	if b.ButtonGroupOpacity != nil {
		bar.ButtonGroupOpacity = *b.ButtonGroupOpacity
	}
	if b.ButtonGroupBorderColor != "" {
		cv, err := ParseColorValue(b.ButtonGroupBorderColor)
		if err != nil {
			return Bar{}, fmt.Errorf("bar: button-group-border-color: %w", err)
		}
		bar.ButtonGroupBorderColor = cv
	}
	if b.ButtonGroupBorderLocation != "" {
		bar.ButtonGroupBorderLocation = BorderLocation(b.ButtonGroupBorderLocation)
	}
	if b.ButtonGroupBorderWidth != nil {
		if *b.ButtonGroupBorderWidth < 0 || *b.ButtonGroupBorderWidth > 255 {
			return Bar{}, fmt.Errorf("bar: button-group-border-width %d outside 0-255", *b.ButtonGroupBorderWidth)
		}
		bar.ButtonGroupBorderWidth = *b.ButtonGroupBorderWidth
	}
	if b.ButtonGroupRounding != "" {
		bar.ButtonGroupRounding = RoundingLevel(b.ButtonGroupRounding)
	}
	if b.Rounding != "" {
		bar.Rounding = RoundingLevel(b.Rounding)
	}
	if b.BackgroundOpacity != nil {
		bar.BackgroundOpacity = *b.BackgroundOpacity
	}
	if b.Scale != nil {
		bar.Scale = *b.Scale
	}
	bar.Layout = b.Layout

	if !validLocations[bar.Location] {
		return Bar{}, fmt.Errorf("bar: invalid location %q (want top|bottom|left|right)", bar.Location)
	}
	if !validLayers[bar.Layer] {
		return Bar{}, fmt.Errorf("bar: invalid layer %q (want background|bottom|top|overlay)", bar.Layer)
	}
	if !validRounding[bar.Rounding] {
		return Bar{}, fmt.Errorf("bar: invalid rounding %q (want none|sm|md|lg|full)", bar.Rounding)
	}
	if !validBorderLocations[bar.BorderLocation] {
		return Bar{}, fmt.Errorf("bar: invalid border-location %q (want none|top|bottom|left|right|all)", bar.BorderLocation)
	}
	if !validBorderLocations[bar.ButtonGroupBorderLocation] {
		return Bar{}, fmt.Errorf("bar: invalid button-group-border-location %q (want none|top|bottom|left|right|all)", bar.ButtonGroupBorderLocation)
	}
	if !validRounding[bar.ButtonGroupRounding] {
		return Bar{}, fmt.Errorf("bar: invalid button-group-rounding %q (want none|sm|md|lg|full)", bar.ButtonGroupRounding)
	}
	if bar.ButtonGroupOpacity < 0 || bar.ButtonGroupOpacity > 100 {
		return Bar{}, fmt.Errorf("bar: button-group-opacity %d outside 0-100", bar.ButtonGroupOpacity)
	}
	if bar.BackgroundOpacity < 0 || bar.BackgroundOpacity > 100 {
		return Bar{}, fmt.Errorf("bar: background-opacity %d outside 0-100", bar.BackgroundOpacity)
	}
	if bar.Scale < 0.25 || bar.Scale > 3.0 {
		return Bar{}, fmt.Errorf("bar: scale %v outside 0.25-3.0", bar.Scale)
	}
	for i := range bar.Layout {
		if err := validateLayout(&bar.Layout[i]); err != nil {
			return Bar{}, err
		}
	}
	return bar, nil
}

func validateLayout(l *BarLayout) error {
	if l.Monitor == "" {
		return errors.New("bar: layout entry missing monitor")
	}
	if l.Extends == l.Monitor {
		return fmt.Errorf("bar: layout %q extends itself", l.Monitor)
	}
	return nil
}

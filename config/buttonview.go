package config

import "reflect"

// Bar-button views: the bar_button key set (wayle-derive's
// BAR_BUTTON fields) read from a module section by key, so every
// module carrying the keys gets the same view without a per-module
// accessor. The keys interleave with each module's own keys in the
// Rust declaration order, so they stay flat fields on the sections.

// ButtonColors are the five color slots of a bar button.
type ButtonColors struct {
	Icon     ColorValue // icon-color
	Label    ColorValue // label-color
	IconBg   ColorValue // icon-bg-color
	ButtonBg ColorValue // button-bg-color
	Border   ColorValue // border-color
}

// ButtonConfig is one module's bar-button keys plus their schema
// defaults (resolve_color's fallback under a non-wayle provider).
type ButtonConfig struct {
	BorderShow     bool
	IconShow       bool
	LabelShow      bool
	LabelMaxLength int
	Colors         ButtonColors
	Defaults       ButtonColors
	// AutoIconColor is the token an auto icon color resolves to under
	// the basic variant: a widget constant per module in Rust
	// (BarButtonColors::auto_icon_color), never read from a file.
	AutoIconColor CssToken
}

// ContainerConfig is a bar_container module's keys (cava, systray):
// border-show, border-color, and button-bg-color, the container
// background, plus the schema colors.
type ContainerConfig struct {
	BorderShow         bool
	Background         ColorValue
	BorderColor        ColorValue
	DefaultBackground  ColorValue
	DefaultBorderColor ColorValue
}

// ModuleButton is the bar-button view of the [modules.<name>] section
// named by its schema key; ok is false for an unknown module. A key
// the section lacks reads as off for border-show and as shown for
// icon-show and label-show (a module without the key always shows
// that part).
func (m *ModulesConfig) ModuleButton(name string) (ButtonConfig, bool) {
	cur, def, ok := moduleSection(m, name)
	if !ok {
		return ButtonConfig{}, false
	}
	return buttonView(cur, def, name), true
}

// Button is a custom module's bar-button view; custom modules
// resolve an auto icon color to the accent.
func (c CustomModuleDefinition) Button() ButtonConfig {
	return buttonView(reflect.ValueOf(c), reflect.ValueOf(DefaultsCustomModuleDefinition()), "custom")
}

// buttonView reads the key set from a section and its defaults.
func buttonView(cur, def reflect.Value, module string) ButtonConfig {
	return ButtonConfig{
		BorderShow:     keyBool(cur, "border-show", false),
		IconShow:       keyBool(cur, "icon-show", true),
		LabelShow:      keyBool(cur, "label-show", true),
		LabelMaxLength: keyInt(cur, "label-max-length"),
		Colors:         buttonColorsOf(cur),
		Defaults:       buttonColorsOf(def),
		AutoIconColor:  autoIconColor(module),
	}
}

// ModuleContainer is the bar_container view of a module section.
func (m *ModulesConfig) ModuleContainer(name string) (ContainerConfig, bool) {
	cur, def, ok := moduleSection(m, name)
	if !ok {
		return ContainerConfig{}, false
	}
	return ContainerConfig{
		BorderShow:         keyBool(cur, "border-show", false),
		Background:         keyColor(cur, "button-bg-color"),
		BorderColor:        keyColor(cur, "border-color"),
		DefaultBackground:  keyColor(def, "button-bg-color"),
		DefaultBorderColor: keyColor(def, "border-color"),
	}, true
}

// moduleSection finds the named section in m and in the defaults.
func moduleSection(m *ModulesConfig, name string) (cur, def reflect.Value, ok bool) {
	v := valueOf(m)
	for _, f := range fieldsOf(v.Type()) {
		if f.key == name {
			defaults := DefaultsModules()
			return v.FieldByIndex(f.index), valueOf(&defaults).FieldByIndex(f.index), true
		}
	}
	return reflect.Value{}, reflect.Value{}, false
}

// keyField is the section field with the given key.
func keyField(section reflect.Value, key string) (reflect.Value, bool) {
	for _, f := range fieldsOf(section.Type()) {
		if f.key == key {
			return section.FieldByIndex(f.index), true
		}
	}
	return reflect.Value{}, false
}

func keyBool(section reflect.Value, key string, missing bool) bool {
	if f, ok := keyField(section, key); ok && f.Kind() == reflect.Bool {
		return f.Bool()
	}
	return missing
}

func keyInt(section reflect.Value, key string) int {
	if f, ok := keyField(section, key); ok && f.CanUint() {
		return int(f.Uint())
	}
	return 0
}

func keyColor(section reflect.Value, key string) ColorValue {
	if f, ok := keyField(section, key); ok {
		if c, ok := reflect.TypeAssert[ColorValue](f); ok {
			return c
		}
	}
	return ColorValue{Kind: ColorAuto}
}

func buttonColorsOf(section reflect.Value) ButtonColors {
	return ButtonColors{
		Icon:     keyColor(section, "icon-color"),
		Label:    keyColor(section, "label-color"),
		IconBg:   keyColor(section, "icon-bg-color"),
		ButtonBg: keyColor(section, "button-bg-color"),
		Border:   keyColor(section, "border-color"),
	}
}

// moduleAutoIconColors is each bar module's auto_icon_color (the
// BarButtonColors each module builds in its crate); modules not listed
// use the accent.
var moduleAutoIconColors = map[string]CssToken{
	"battery":        TokenYellow,
	"bluetooth":      TokenBlue,
	"brightness":     TokenYellow,
	"cpu":            TokenBlue,
	"dashboard":      TokenYellow,
	"hyprsunset":     TokenYellow,
	"idle-inhibit":   TokenGreen,
	"keybind-mode":   TokenBlue,
	"keyboard-input": TokenYellow,
	"mail":           TokenBlue,
	"media":          TokenBlue,
	"microphone":     TokenRed,
	"netstat":        TokenRed,
	"notifications":  TokenGreen,
	"power-profiles": TokenBlue,
	"power":          TokenRed,
	"ram":            TokenGreen,
	"recorder":       TokenRed,
	"storage":        TokenYellow,
	"volume":         TokenRed,
	"window-title":   TokenBlue,
	"world-clock":    TokenYellow,
}

// autoIconColor is the module's auto icon token; clock, custom,
// network, screenshot, treeman, and weather use the accent.
func autoIconColor(module string) CssToken {
	if t, ok := moduleAutoIconColors[module]; ok {
		return t
	}
	return TokenAccent
}

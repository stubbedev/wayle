package config

import (
	"reflect"
	"slices"
	"sort"
)

// The config reference pages (`wayle config docs`) document every
// schema the Rust crate registers with register_module!: a section's
// own schema (schema_for! of its type), the groups its fields are laid
// out in, and its defaults. This file is that registry over the Go
// types; the pages themselves are rendered by internal/configdocs.

// DocGroupRule is how a docs group claims fields (GroupRule).
type DocGroupRule uint8

// Group rules.
const (
	// DocCatchAll takes every field no other group claimed.
	DocCatchAll DocGroupRule = iota
	// DocByType takes fields whose type is the named schema type.
	DocByType
	// DocPrefix takes fields whose name starts with the argument.
	DocPrefix
	// DocStandalone takes the one field named by the argument.
	DocStandalone
)

// DocGroup is one H2 section of a reference page (ConfigGroup).
type DocGroup struct {
	Title string
	Rule  DocGroupRule
	// Arg is the type name, prefix, or field name the rule matches.
	Arg string
}

func docGeneral() DocGroup { return DocGroup{Title: "General"} }
func docColors() DocGroup  { return DocGroup{Title: "Colors", Rule: DocByType, Arg: "ColorValue"} }
func docClick() DocGroup {
	return DocGroup{Title: "Click actions", Rule: DocByType, Arg: "ClickAction"}
}

func docPrefix(title, prefix string) DocGroup {
	return DocGroup{Title: title, Rule: DocPrefix, Arg: prefix}
}

func docStandalone(title, field string) DocGroup {
	return DocGroup{Title: title, Rule: DocStandalone, Arg: field}
}

// docStandard is GroupDefaults::standard.
func docStandard() []DocGroup { return []DocGroup{docGeneral()} }

// docBarButton is GroupDefaults::bar_button.
func docBarButton() []DocGroup {
	return []DocGroup{docGeneral(), docColors(), docClick(), docPrefix("Dropdown", "dropdown-")}
}

// docDefault is one field's default as its encoded tree.
type docDefault struct {
	key   string
	value any
}

// DocSection is one documented schema (a ModuleInfo with its groups).
type DocSection struct {
	Name string
	// LayoutID is the bar layout name a module goes by; "" for a
	// top-level section.
	LayoutID string
	// ArrayEntry: the section is one entry of an array of tables
	// (custom modules, mail accounts, toast presets).
	ArrayEntry bool
	Groups     []DocGroup
	// Schema is schema_for! of the section's type: description,
	// properties (see PropertyOrder), and its own $defs.
	Schema Schema
	// Required are the fields without a default, in declaration order.
	Required []string

	// defaults are the fields with one, each as its encoded tree, for
	// the TOML block.
	defaults []docDefault
}

// docSpec registers one section: its default value and layout.
type docSpec struct {
	name, layout string
	array        bool
	groups       []DocGroup
	value        func(c *Config) reflect.Value
}

func field(get func(c *Config) any) func(c *Config) reflect.Value {
	return func(c *Config) reflect.Value { return reflect.ValueOf(get(c)).Elem() }
}

// standalone is a type's default outside the tree (an array entry):
// its zero value with its setDefaults applied.
func standalone[T any]() func(*Config) reflect.Value {
	return func(*Config) reflect.Value {
		v := zeroOf(reflect.TypeFor[T]())
		if d, ok := reflect.TypeAssert[defaulter](v.Addr()); ok {
			d.setDefaults()
		}
		return v
	}
}

// docSpecs are the register_module! entries.
func docSpecs() []docSpec {
	bar := docBarButton
	m := func(name string, groups func() []DocGroup, get func(c *Config) any) docSpec {
		return docSpec{name: name, layout: name, groups: groups(), value: field(get)}
	}
	top := func(name string, groups []DocGroup, get func(c *Config) any) docSpec {
		return docSpec{name: name, groups: groups, value: field(get)}
	}
	return []docSpec{
		top("animations", docStandard(), func(c *Config) any { return &c.Animations }),
		top("bar", []DocGroup{docGeneral(), docColors(), docPrefix("Buttons", "button-"), docPrefix("Dropdowns", "dropdown-")}, func(c *Config) any { return &c.Bar }),
		top("dropdowns", docStandard(), func(c *Config) any { return &c.Dropdowns }),
		top("general", docStandard(), func(c *Config) any { return &c.General }),
		top("greeter", docStandard(), func(c *Config) any { return &c.Greeter }),
		top("launcher", docStandard(), func(c *Config) any { return &c.Launcher }),
		top("lock", docStandard(), func(c *Config) any { return &c.Lock }),
		top("osd", docStandard(), func(c *Config) any { return &c.Osd }),
		top("share-picker", docStandard(), func(c *Config) any { return &c.SharePicker }),
		top("styling", []DocGroup{
			docGeneral(), docPrefix("Theme provider", "theme-"), docPrefix("Matugen", "matugen-"),
			docPrefix("Wallust", "wallust-"), docPrefix("Pywal", "pywal-"), docStandalone("Palette", "palette"),
		}, func(c *Config) any { return &c.Styling }),
		top("wallpaper", []DocGroup{docGeneral(), docPrefix("Cycling", "cycling-"), docStandalone("Per-monitor overrides", "monitors")},
			func(c *Config) any { return &c.Wallpaper }),
		{name: "toast-preset", array: true, groups: docStandard(), value: standalone[ToastPreset]()},
		{name: "mail-account", array: true, groups: docStandard(), value: standalone[MailAccount]()},
		{name: "custom", layout: "custom-<id>", array: true, groups: []DocGroup{
			docGeneral(), docColors(), docClick(), docPrefix("Icons", "icon-"), docPrefix("Restart", "restart-"),
		}, value: standalone[CustomModuleDefinition]()},
		{
			name: "dropdown-dashboard-user-session", layout: "user-session", groups: docBarButton(),
			value: field(func(c *Config) any { return &c.Dashboard.UserSession }),
		},
		m("battery", bar, func(c *Config) any { return &c.Battery }),
		m("bluetooth", bar, func(c *Config) any { return &c.Bluetooth }),
		m("brightness", bar, func(c *Config) any { return &c.Brightness }),
		m("cava", docStandard, func(c *Config) any { return &c.Cava }),
		m("clock", bar, func(c *Config) any { return &c.Clock }),
		m("cpu", bar, func(c *Config) any { return &c.CPU }),
		m("dashboard", bar, func(c *Config) any { return &c.Dashboard }),
		m("hyprland-workspaces", docStandard, func(c *Config) any { return &c.HyprlandWorkspaces }),
		m("hyprsunset", bar, func(c *Config) any { return &c.Hyprsunset }),
		m("idle-inhibit", bar, func(c *Config) any { return &c.IdleInhibit }),
		m("keybind-mode", bar, func(c *Config) any { return &c.KeybindMode }),
		m("keyboard-input", bar, func(c *Config) any { return &c.KeyboardInput }),
		m("mail", bar, func(c *Config) any { return &c.Mail }),
		m("mango-workspaces", docStandard, func(c *Config) any { return &c.MangoWorkspaces }),
		m("media", bar, func(c *Config) any { return &c.Media }),
		m("microphone", bar, func(c *Config) any { return &c.Microphone }),
		m("netstat", bar, func(c *Config) any { return &c.Netstat }),
		m("network", bar, func(c *Config) any { return &c.Network }),
		m("niri-workspaces", docStandard, func(c *Config) any { return &c.NiriWorkspaces }),
		m("notifications", bar, func(c *Config) any { return &c.Notification }),
		m("power", bar, func(c *Config) any { return &c.Power }),
		m("power-profiles", bar, func(c *Config) any { return &c.PowerProfiles }),
		m("ram", bar, func(c *Config) any { return &c.RAM }),
		m("recorder", bar, func(c *Config) any { return &c.Recorder }),
		m("screenshot", bar, func(c *Config) any { return &c.Screenshot }),
		m("separator", docStandard, func(c *Config) any { return &c.Separator }),
		m("storage", bar, func(c *Config) any { return &c.Storage }),
		m("sway-workspaces", docStandard, func(c *Config) any { return &c.SwayWorkspaces }),
		m("systray", docStandard, func(c *Config) any { return &c.Systray }),
		m("treeman", bar, func(c *Config) any { return &c.Treeman }),
		m("volume", bar, func(c *Config) any { return &c.Volume }),
		m("weather", bar, func(c *Config) any { return &c.Weather }),
		m("window-title", bar, func(c *Config) any { return &c.WindowTitle }),
		m("world-clock", bar, func(c *Config) any { return &c.WorldClock }),
	}
}

// DocSections is every documented schema, sorted by name as the Rust
// registry orders them.
func DocSections() []DocSection {
	cfg := Defaults()
	specs := docSpecs()
	out := make([]DocSection, 0, len(specs))
	for _, sp := range specs {
		v := sp.value(cfg)
		schema := sectionSchema(v, sp.array)
		sec := DocSection{Name: sp.name, LayoutID: sp.layout, ArrayEntry: sp.array, Groups: sp.groups, Schema: schema}
		props, _ := schema["properties"].(Schema)
		for _, f := range fieldsOf(v.Type()) {
			prop, _ := props[f.key].(Schema)
			if _, has := prop["default"]; !has {
				sec.Required = append(sec.Required, f.key)
				continue
			}
			sec.defaults = append(sec.defaults, docDefault{key: f.key, value: encodeValueMode(v.FieldByIndex(f.index), true)})
		}
		out = append(out, sec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// sectionSchema is schema_for! of v's type alone: its own $defs, the
// container defaults taken from v. An array entry (a custom module, a
// mail account) is no config container: it sits in a slice, so its
// fields keep their own required-ness, as in the whole-config schema.
func sectionSchema(v reflect.Value, entry bool) Schema {
	g := &schemaGen{
		defs:     map[string]Schema{},
		ids:      map[string]string{},
		names:    map[string]bool{},
		defaults: map[reflect.Type]reflect.Value{},
	}
	if !entry && isContainer(v.Type()) {
		collectContainerDefaults(v, g.defaults)
	}
	schema := g.structSchema(v.Type())
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = schemaName(v.Type())
	if len(g.defs) > 0 {
		defs := Schema{}
		for k, d := range g.defs {
			defs[k] = d
		}
		schema["$defs"] = defs
	}
	return schema
}

// DefaultsTOML is the page's defaults block (render_default_toml's
// table): each default as schemars put it in the schema (f32 widened to
// f64) converted to TOML, under [name], [modules.name], or
// [[modules.name]]. A None default is skipped, and one holding a None
// anywhere inside is left out, as serde_json's null has no TOML form.
// "" when nothing remains.
func (s DocSection) DefaultsTOML() string {
	section := newTable()
	for _, d := range s.defaults {
		if !hasAbsent(d.value) {
			section.set(d.key, widen(d.value))
		}
	}
	if len(section.keys) == 0 {
		return ""
	}
	root := newTable()
	switch {
	case s.ArrayEntry:
		modules := newTable()
		modules.set(s.Name, []any{section})
		root.set("modules", modules)
	case s.LayoutID != "":
		modules := newTable()
		modules.set(s.Name, section)
		root.set("modules", modules)
	default:
		root.set(s.Name, section)
	}
	out, err := tomlPretty(root)
	if err != nil {
		return ""
	}
	return out
}

// hasAbsent reports a None anywhere in an encoded tree.
func hasAbsent(v any) bool {
	switch t := v.(type) {
	case absent:
		return true
	case *table:
		for _, k := range t.keys {
			if hasAbsent(t.vals[k]) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(t, hasAbsent)
	}
	return false
}

// widen is the tree as serde_json holds it: every float an f64.
func widen(v any) any {
	switch t := v.(type) {
	case float32:
		return float64(t)
	case *table:
		out := newTable()
		for _, k := range t.keys {
			out.set(k, widen(t.vals[k]))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, c := range t {
			out[i] = widen(c)
		}
		return out
	}
	return v
}

// JSONText is v as serde_json writes it: compact, and a float always
// with a fraction or exponent (render_json_literal's numbers).
func JSONText(v any) string { return marshalJSON(v) }

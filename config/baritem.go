package config

import (
	"errors"
	"slices"
	"strings"
)

// BarItem is one entry in a bar layout section: a bare module name, a
// module with a per-instance CSS class, or a named group of modules
// (the untagged BarItem/ModuleRef enums in
// crates/wayle-config/src/schemas/bar/types/mod.rs).
type BarItem struct {
	// Module is the module ("clock", "battery", "custom-submap").
	Module BarModule
	// Class is an extra styling class for this instance; empty for
	// bare entries and groups.
	Class string
	// Group is non-nil when the item is a group container.
	Group *BarGroup
}

// IsGroup reports whether the item is a group container.
func (b BarItem) IsGroup() bool { return b.Group != nil }

// BarGroup is a set of modules sharing a visual container; its members
// are module references, never nested groups.
type BarGroup struct {
	Name    string
	Modules []BarItem
}

// UnmarshalConfig implements Unmarshaler with serde's untagged order:
// a {module, class} table, then a module name, then a {name, modules}
// group. Anything else — including a table missing a required key or
// naming an unknown module — is serde's untagged error.
func (b *BarItem) UnmarshalConfig(v any) error {
	if ref, ok := decodeModuleRef(v); ok {
		*b = ref
		return nil
	}
	if group, ok := decodeGroup(v); ok {
		*b = BarItem{Group: group}
		return nil
	}
	return errUntagged("BarItem")
}

// decodeModuleRef is ModuleRef: Classed, then Plain.
func decodeModuleRef(v any) (BarItem, bool) {
	if isTable(v) {
		rawModule, hasModule := lookup(v, "module")
		rawClass, hasClass := lookup(v, "class")
		class, classOK := rawClass.(string)
		var module BarModule
		if hasModule && hasClass && classOK && module.UnmarshalConfig(rawModule) == nil {
			return BarItem{Module: module, Class: class}, true
		}
		return BarItem{}, false
	}
	var module BarModule
	if module.UnmarshalConfig(v) != nil {
		return BarItem{}, false
	}
	return BarItem{Module: module}, true
}

func decodeGroup(v any) (*BarGroup, bool) {
	rawName, hasName := lookup(v, "name")
	rawModules, hasModules := lookup(v, "modules")
	name, nameOK := rawName.(string)
	list, listOK := rawModules.([]any)
	if !hasName || !hasModules || !nameOK || !listOK {
		return nil, false
	}
	group := &BarGroup{Name: name, Modules: make([]BarItem, 0, len(list))}
	for _, entry := range list {
		ref, ok := decodeModuleRef(entry)
		if !ok {
			return nil, false
		}
		group.Modules = append(group.Modules, ref)
	}
	return group, true
}

// MarshalConfig implements Marshaler: a bare name, a {module, class}
// table, or a {name, modules} group.
func (b BarItem) MarshalConfig() any {
	if b.Group != nil {
		modules := make([]any, len(b.Group.Modules))
		for i, m := range b.Group.Modules {
			modules[i] = m.MarshalConfig()
		}
		t := newTable()
		t.set("name", b.Group.Name)
		t.set("modules", modules)
		return t
	}
	if b.Class != "" {
		t := newTable()
		t.set("module", string(b.Module))
		t.set("class", b.Class)
		return t
	}
	return string(b.Module)
}

const (
	barItemDoc   = "One entry in a bar layout section (`left`, `center`, or `right`).\n\nThree shapes are accepted, all interchangeable in the same array:\n\n- A plain module name: `\"clock\"`\n- A module with a CSS class for per-instance styling: `{ module = \"clock\", class = \"primary\" }`\n- A named group that wraps several modules in a shared container, addressable by CSS ID\n\n## Examples\n\n```toml\n[[bar.layout]]\nmonitor = \"*\"\n\n# Plain module\nleft = [\"dashboard\"]\n\n# Mix of plain and classed modules on the same side\ncenter = [\"clock\", { module = \"clock\", class = \"secondary\" }]\n\n# Named group (renders inside a GTK container with CSS ID `#status`)\nright = [{ name = \"status\", modules = [\"battery\", \"network\", \"volume\"] }]\n\n# Groups can hold classed modules too\n[[bar.layout]]\nmonitor = \"DP-2\"\nleft = [{ name = \"clocks\", modules = [\n  { module = \"clock\", class = \"local\" },\n  { module = \"world-clock\", class = \"remote\" }\n]}]\n```"
	moduleRefDoc = "Reference to a module, optionally with a custom CSS class.\n\n## Examples\n\n```toml\n# Plain module (just the name)\nleft = [\"clock\"]\n\n# Module with custom CSS class\nleft = [{ module = \"clock\", class = \"primary-clock\" }]\n```"
)

func (BarItem) configSchema(g *schemaGen) Schema {
	moduleRef := g.define("derived:ModuleRef", "ModuleRef", func() Schema {
		classed := g.define("derived:ClassedModule", "ClassedModule", func() Schema {
			return Schema{
				"description": "A module with an associated CSS class for custom styling.",
				"type":        "object",
				"properties": Schema{
					"module": withDescription(g.subschema(typeOf[BarModule]()), "The module type."),
					"class":  Schema{"type": "string", "description": "CSS class added to the module's GTK widget."},
				},
				"required": []any{"module", "class"},
			}
		})
		return Schema{
			"description": moduleRefDoc,
			"anyOf": []any{
				withDescription(classed, "Module with a custom CSS class."),
				withDescription(g.subschema(typeOf[BarModule]()), "Plain module reference."),
			},
		}
	})
	group := g.define("derived:BarGroup", "BarGroup", func() Schema {
		return Schema{
			"description": "Named group of modules. The name becomes a CSS ID selector.",
			"type":        "object",
			"properties": Schema{
				"name":    Schema{"type": "string", "description": "Unique name for CSS targeting (becomes `#name` selector)."},
				"modules": Schema{"type": "array", "items": moduleRef, "description": "Modules contained in this group."},
			},
			"required": []any{"name", "modules"},
		}
	})
	return Schema{
		"description": barItemDoc,
		"anyOf": []any{
			withDescription(moduleRef, "A single module (plain or with custom CSS class)."),
			withDescription(group, "A named group of modules with shared visual container."),
		},
	}
}

func withDescription(s Schema, desc string) Schema {
	out := copySchema(s)
	out["description"] = desc
	return out
}

// BarModule names a bar module: a built-in, or `custom-<id>` for a
// [[modules.custom]] definition.
type BarModule string

// The built-in modules the layout accepts (BUILTIN_MODULES).
const (
	ModuleBattery            BarModule = "battery"
	ModuleBluetooth          BarModule = "bluetooth"
	ModuleBrightness         BarModule = "brightness"
	ModuleCava               BarModule = "cava"
	ModuleClock              BarModule = "clock"
	ModuleCPU                BarModule = "cpu"
	ModuleDashboard          BarModule = "dashboard"
	ModuleHyprlandWorkspaces BarModule = "hyprland-workspaces"
	ModuleHyprsunset         BarModule = "hyprsunset"
	ModuleIdleInhibit        BarModule = "idle-inhibit"
	ModuleKeybindMode        BarModule = "keybind-mode"
	ModuleKeyboardInput      BarModule = "keyboard-input"
	ModuleMail               BarModule = "mail"
	ModuleMedia              BarModule = "media"
	ModuleMangoWorkspaces    BarModule = "mango-workspaces"
	ModuleMicrophone         BarModule = "microphone"
	ModuleNetstat            BarModule = "netstat"
	ModuleNetwork            BarModule = "network"
	ModuleNiriWorkspaces     BarModule = "niri-workspaces"
	ModuleNotifications      BarModule = "notifications"
	ModulePower              BarModule = "power"
	ModulePowerProfiles      BarModule = "power-profiles"
	ModuleRAM                BarModule = "ram"
	ModuleRecorder           BarModule = "recorder"
	ModuleScreenshot         BarModule = "screenshot"
	ModuleSeparator          BarModule = "separator"
	ModuleStorage            BarModule = "storage"
	ModuleSwayWorkspaces     BarModule = "sway-workspaces"
	ModuleSystray            BarModule = "systray"
	ModuleTreeman            BarModule = "treeman"
	ModuleUpdates            BarModule = "updates"
	ModuleVolume             BarModule = "volume"
	ModuleWeather            BarModule = "weather"
	ModuleWindowTitle        BarModule = "window-title"
	ModuleWorldClock         BarModule = "world-clock"
)

// BuiltinModules is the built-in module list in schema order.
var BuiltinModules = []BarModule{
	ModuleBattery, ModuleBluetooth, ModuleBrightness, ModuleCava, ModuleClock,
	ModuleCPU, ModuleDashboard, ModuleHyprlandWorkspaces, ModuleHyprsunset,
	ModuleIdleInhibit, ModuleKeybindMode, ModuleKeyboardInput, ModuleMail,
	ModuleMedia, ModuleMangoWorkspaces, ModuleMicrophone, ModuleNetstat,
	ModuleNetwork, ModuleNiriWorkspaces, ModuleNotifications, ModulePower,
	ModulePowerProfiles, ModuleRAM, ModuleRecorder, ModuleScreenshot,
	ModuleSeparator, ModuleStorage, ModuleSwayWorkspaces, ModuleSystray,
	ModuleTreeman, ModuleUpdates, ModuleVolume, ModuleWeather,
	ModuleWindowTitle, ModuleWorldClock,
}

const customModulePrefix = "custom-"

// CustomID is the [[modules.custom]] id of a custom-<id> module.
func (m BarModule) CustomID() (string, bool) {
	return strings.CutPrefix(string(m), customModulePrefix)
}

// UnmarshalConfig implements Unmarshaler: a built-in name or a
// non-empty custom-<id>.
func (m *BarModule) UnmarshalConfig(v any) error {
	s, ok := v.(string)
	if !ok {
		return invalidType(v, "a string")
	}
	if id, custom := strings.CutPrefix(s, customModulePrefix); custom {
		if id == "" {
			return errors.New("custom module ID cannot be empty")
		}
		*m = BarModule(s)
		return nil
	}
	if !slices.Contains(BuiltinModules, BarModule(s)) {
		names := make([]string, len(BuiltinModules))
		for i, b := range BuiltinModules {
			names[i] = string(b)
		}
		return errors.New("unknown variant `" + s + "`, " + expectedOneOf(names))
	}
	*m = BarModule(s)
	return nil
}

func (BarModule) configSchema(*schemaGen) Schema {
	names := make([]any, len(BuiltinModules))
	for i, b := range BuiltinModules {
		names[i] = string(b)
	}
	return Schema{
		"description": "Bar module name. Built-in modules or custom modules with a `custom-<id>` pattern.",
		"anyOf": []any{
			Schema{"enum": names},
			Schema{
				"type":        "string",
				"pattern":     "^custom-[a-z0-9-]+$",
				"description": "Custom module ID (e.g., 'custom-gpu-temp')",
			},
		},
	}
}

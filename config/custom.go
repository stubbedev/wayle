package config

// CustomModuleDefinition is ported from crates/wayle-config/src/schemas/modules/custom/mod.rs.
//
// User-defined module that runs a shell command and renders the output in the bar.
//
// Full walkthrough with examples in `docs/guide/custom-modules.md`.
type CustomModuleDefinition struct {
	// Unique identifier for this module.
	//
	// Referenced in bar layouts as `custom-<id>`. Must be unique across
	// all custom module definitions.
	//
	// ## Example
	//
	// ```toml
	// [[modules.custom]]
	// id = "gpu-temp"
	//
	// # Reference in layout:
	// # layout = ["custom-gpu-temp", "clock"]
	// ```
	Id string `cfg:"id,required"`
	// Shell command to execute.
	//
	// The command runs via `sh -c` and should output to stdout.
	// Stderr is discarded. Commands have a 30-second timeout.
	//
	// ## Output Parsing
	//
	// - If output starts with `{` or `[`: parsed as JSON
	// - Otherwise: treated as plain text
	//
	// ## Behavior by Mode
	//
	// - **poll**: Executed every `interval-ms` milliseconds
	// - **watch**: Spawned once, each stdout line triggers a display update.
	//   Restarts are controlled by `restart-policy`.
	Command *string `cfg:"command,default"`
	// Execution mode for the command.
	//
	// | Mode | Behavior |
	// |------|----------|
	// | `poll` | Run command every `interval-ms` (default) |
	// | `watch` | Spawn long-running process, update on each stdout line |
	//
	// Use `poll` for commands that return current state and exit.
	// Use `watch` for commands that stream updates (e.g., `pactl subscribe`).
	Mode ExecutionMode `cfg:"mode,default"`
	// Polling interval in milliseconds.
	//
	// Only applies to `poll` mode. Ignored in `watch` mode.
	//
	// Set to `0` for manual polling mode: no timer is started. In manual
	// mode, the command still runs once at startup.
	IntervalMs uint64 `cfg:"interval-ms,default"`
	// Restart policy for watch mode.
	//
	// Only applies to `watch` mode. Ignored in `poll` mode.
	//
	// | Policy | Behavior |
	// |--------|----------|
	// | `never` | Do not restart after exit |
	// | `on-exit` | Restart after any exit |
	// | `on-failure` | Restart only after non-zero/signal exit |
	RestartPolicy RestartPolicy `cfg:"restart-policy,default"`
	// Base restart delay in milliseconds for watch mode.
	//
	// Only applies to `watch` mode. Ignored in `poll` mode.
	//
	// Used when `restart-policy` is `on-exit` or `on-failure`.
	// Delay increases exponentially on rapid failures, capped at 30 seconds.
	RestartIntervalMs RestartDelay `cfg:"restart-interval-ms,default"`
	// Format string for the label using Jinja2 template syntax.
	//
	// ## Variables
	//
	// - `{{ output }}` - Raw command output
	// - `{{ field }}` - JSON field access
	// - `{{ nested.field }}` - Nested field access
	// - `{{ items.0 }}` - Array index access
	//
	// ## Filters
	//
	// - `{{ val | default('fallback') }}` - Fallback for missing values
	// - `{{ "%02d" | format(val) }}` - Zero-padding
	// - `{{ val | upper }}`, `| lower`, `| trim` - String transforms
	//
	// ## Examples
	//
	// - `"{{ output }}°C"` - Plain text: "72°C"
	// - `"{{ percentage }}%"` - JSON field: "75%"
	// - `"{{ data.temp }}°C"` - Nested: "22°C"
	//
	// If JSON output contains a `text` field, it overrides this format.
	Format string `cfg:"format,default"`
	// Format string for the tooltip (hover text).
	//
	// Supports the same Jinja2 syntax as `format`. If not set, no tooltip is shown.
	// If JSON output contains a `tooltip` field, it overrides this format.
	//
	// ## Example
	//
	// ```toml
	// format = "{{ percentage }}%"
	// tooltip-format = "Volume: {{ percentage }}% on {{ device }}"
	// ```
	TooltipFormat *string `cfg:"tooltip-format,default"`
	// Hide module when output is empty, "0", or "false".
	//
	// When enabled, the module (including its gap in the bar layout) is
	// completely hidden if the output indicates an empty/disabled state.
	HideIfEmpty bool `cfg:"hide-if-empty,default"`
	// Static symbolic icon name.
	//
	// Used when `icon-names` and `icon-map` don't provide a match.
	// Should be a symbolic icon name from the icon theme (e.g., `"ld-gpu-symbolic"`).
	//
	// ## Example
	//
	// ```toml
	// icon-name = "ld-temperature-symbolic"
	// ```
	IconName string `cfg:"icon-name,default"`
	// Array of icon names indexed by percentage (0-100).
	//
	// Requires JSON output with a `percentage` field (0-100).
	// The array is divided evenly across the percentage range.
	//
	// ## Resolution
	//
	// For N icons, icon at index `floor(percentage * N / 101)` is selected:
	//
	// - 4 icons: 0-24% → [0], 25-49% → [1], 50-74% → [2], 75-100% → [3]
	// - 5 icons: 0-19% → [0], 20-39% → [1], 40-59% → [2], 60-79% → [3], 80-100% → [4]
	//
	// ## Example
	//
	// ```toml
	// icon-names = [
	//   "battery-empty-symbolic",
	//   "battery-caution-symbolic",
	//   "battery-low-symbolic",
	//   "battery-good-symbolic",
	//   "battery-full-symbolic"
	// ]
	// ```
	IconNames *[]string `cfg:"icon-names,default"`
	// Map of icon names keyed by the `alt` field value.
	//
	// Requires JSON output with an `alt` field. The `alt` value is looked up
	// in this map. Use `"default"` as a fallback key.
	//
	// **Priority**: `icon-map[alt]` takes precedence over `icon-names[percentage]`,
	// allowing state-specific icons to override percentage-based icons.
	//
	// ## Example
	//
	// ```toml
	// # Volume with muted state override
	// icon-names = ["vol-0", "vol-33", "vol-66", "vol-100"]
	// icon-map = { "muted" = "audio-volume-muted-symbolic" }
	//
	// # Output: {"percentage": 50, "alt": "muted"}
	// # Result: Uses "audio-volume-muted-symbolic" (alt match beats percentage)
	//
	// # Output: {"percentage": 50}
	// # Result: Uses "vol-33" (percentage-based, no alt)
	// ```
	IconMap *map[string]string `cfg:"icon-map,default"`
	// Format string for dynamic CSS classes.
	//
	// Supports the same Jinja2 syntax as `format`. The formatted result is
	// split on whitespace and each word is added as a CSS class.
	//
	// Combined with the `class` field from JSON output (if present).
	//
	// ## Example
	//
	// ```toml
	// class-format = "volume-{{ alt }}"
	// # Output: {"alt": "muted"} → adds class "volume-muted"
	// ```
	ClassFormat *string `cfg:"class-format,default"`
	// Display module icon.
	IconShow bool `cfg:"icon-show,default"`
	// Icon foreground color.
	IconColor ColorValue `cfg:"icon-color,default"`
	// Icon container background color.
	IconBgColor ColorValue `cfg:"icon-bg-color,default"`
	// Display text label.
	LabelShow bool `cfg:"label-show,default"`
	// Label text color.
	LabelColor ColorValue `cfg:"label-color,default"`
	// Maximum label length in characters before truncation.
	//
	// When exceeded, label is truncated with ellipsis. Set to `0` to disable.
	LabelMaxLength uint32 `cfg:"label-max-length,default"`
	// Button background color.
	ButtonBgColor ColorValue `cfg:"button-bg-color,default"`
	// Display border around button.
	BorderShow bool `cfg:"border-show,default"`
	// Border color.
	BorderColor ColorValue `cfg:"border-color,default"`
	// Map of per-state color overrides keyed by the `alt` field value.
	//
	// Mirrors `icon-map`: the `alt` value from JSON output is looked up in
	// this map and the colors set for that state override the module's
	// static colors, letting a single widget cycle its colors alongside its
	// icon as its state changes. Use `"default"` as a fallback key. Any
	// color left unset for the matched state falls back to the module's
	// static color of the same name.
	//
	// ## Example
	//
	// ```toml
	// icon-map = { muted = "audio-volume-muted-symbolic" }
	// color-map = { muted = { icon-color = "red" }, default = { icon-color = "green" } }
	//
	// # Output: {"percentage": 50, "alt": "muted"}
	// # Result: muted icon rendered in red
	// ```
	ColorMap *map[string]StateColors `cfg:"color-map,default"`
	// Action performed on left click.
	//
	// Supports shell commands and dropdown toggles:
	// - `"pavucontrol"` — runs a shell command
	// - `"dropdown:audio"` — toggles the named dropdown panel
	// - `""` — no action (default)
	//
	// If `on-action` is set, it runs after shell commands complete.
	LeftClick ClickAction `cfg:"left-click,default"`
	// Action performed on right click.
	//
	// Supports shell commands and dropdown toggles (see `left-click`).
	// If `on-action` is set, it runs after shell commands complete.
	RightClick ClickAction `cfg:"right-click,default"`
	// Action performed on middle click.
	//
	// Supports shell commands and dropdown toggles (see `left-click`).
	// If `on-action` is set, it runs after shell commands complete.
	MiddleClick ClickAction `cfg:"middle-click,default"`
	// Action performed on scroll up.
	//
	// Supports shell commands and dropdown toggles (see `left-click`).
	// Scroll events are debounced (50ms) to coalesce rapid scrolls.
	// If `on-action` is set, it runs after shell commands complete.
	ScrollUp ClickAction `cfg:"scroll-up,default"`
	// Action performed on scroll down.
	//
	// Supports shell commands and dropdown toggles (see `left-click`).
	// Scroll events are debounced (50ms) to coalesce rapid scrolls.
	// If `on-action` is set, it runs after shell commands complete.
	ScrollDown ClickAction `cfg:"scroll-down,default"`
	// Shell command to run after any click/scroll action completes.
	//
	// Executes after the action handler finishes, and its output updates
	// the display immediately. Useful for reflecting state changes without
	// waiting for the next poll interval.
	//
	// ## Example
	//
	// ```toml
	// # Volume control with immediate feedback
	// scroll-up = "pactl set-sink-volume @DEFAULT_SINK@ +5%"
	// scroll-down = "pactl set-sink-volume @DEFAULT_SINK@ -5%"
	// on-action = '''
	// vol=$(pactl get-sink-volume @DEFAULT_SINK@ | grep -oP '\d+(?=%)' | head -1)
	// echo "{\"percentage\": $vol}"
	// '''
	// ```
	OnAction *string `cfg:"on-action,default"`
}

// DefaultsCustomModuleDefinition returns the schema defaults.
func DefaultsCustomModuleDefinition() CustomModuleDefinition {
	return CustomModuleDefinition{
		Mode:              ExecutionModePoll,
		IntervalMs:        5000,
		RestartPolicy:     RestartPolicyNever,
		RestartIntervalMs: 1000,
		Format:            "{{ output }}",
		HideIfEmpty:       false,
		IconName:          "",
		IconShow:          true,
		IconColor:         mustColor("auto"),
		IconBgColor:       mustColor("auto"),
		LabelShow:         true,
		LabelColor:        mustColor("auto"),
		LabelMaxLength:    0,
		ButtonBgColor:     mustColor("bg-surface-elevated"),
		BorderShow:        false,
		BorderColor:       mustColor("auto"),
		LeftClick:         ClickAction{},
		RightClick:        ClickAction{},
		MiddleClick:       ClickAction{},
		ScrollUp:          ClickAction{},
		ScrollDown:        ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c CustomModuleDefinition) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// ExecutionMode is ported from crates/wayle-config/src/schemas/modules/custom/types.rs.
//
// Execution mode for custom module commands.
type ExecutionMode string

// ExecutionMode values.
const (
	// Run command at regular intervals defined by `interval-ms`.
	//
	// Best for commands that complete quickly and return current state
	// (e.g., reading a file, querying system status).
	ExecutionModePoll ExecutionMode = "poll"
	// Spawn long-running process and update display on each stdout line.
	//
	// Best for event-driven updates without polling overhead
	// (e.g., `pactl subscribe`, `inotifywait`, `tail -f`).
	// Configure `restart-policy` to control restarts after exit.
	ExecutionModeWatch ExecutionMode = "watch"
)

var _ = registerEnum(ExecutionModePoll, ExecutionModeWatch)

// RestartPolicy is ported from crates/wayle-config/src/schemas/modules/custom/types.rs.
//
// Restart behavior for watch-mode custom modules.
type RestartPolicy string

// RestartPolicy values.
const (
	// Never restart after exit.
	RestartPolicyNever RestartPolicy = "never"
	// Restart after any exit code (success or failure).
	RestartPolicyOnExit RestartPolicy = "on-exit"
	// Restart only after non-zero exit codes or signal termination.
	RestartPolicyOnFailure RestartPolicy = "on-failure"
)

var _ = registerEnum(RestartPolicyNever, RestartPolicyOnExit, RestartPolicyOnFailure)

// StateColors is ported from crates/wayle-config/src/schemas/modules/custom/types.rs.
//
// Per-state color overrides for a custom module.
//
// Values are keyed in `color-map` by the JSON output's `alt` field, mirroring
// `icon-map`. Any field left unset falls back to the module's static color of
// the same name. Use the `"default"` key as a fallback state.
type StateColors struct {
	// Icon foreground color for this state.
	IconColor *ColorValue `cfg:"icon-color"`
	// Icon container background color for this state.
	IconBgColor *ColorValue `cfg:"icon-bg-color"`
	// Label text color for this state.
	LabelColor *ColorValue `cfg:"label-color"`
	// Button background color for this state.
	ButtonBgColor *ColorValue `cfg:"button-bg-color"`
	// Border color for this state.
	BorderColor *ColorValue `cfg:"border-color"`
}

func (c *CustomModuleDefinition) setDefaults() { *c = DefaultsCustomModuleDefinition() }

func (CustomModuleDefinition) noStructDefault() {}

// CustomByID finds one [[modules.custom]] definition by id.
func (c *Config) CustomByID(id string) (CustomModuleDefinition, bool) {
	for _, def := range c.Custom {
		if def.Id == id {
			return def, true
		}
	}
	return CustomModuleDefinition{}, false
}

// Icon is the definition's static icon.
func (c CustomModuleDefinition) Icon() IconConfig {
	return IconWith(c.IconShow, c.IconName, c.IconColor)
}

// CommandOrEmpty is the command, empty when unset.
func (c CustomModuleDefinition) CommandOrEmpty() string {
	if c.Command == nil {
		return ""
	}
	return *c.Command
}

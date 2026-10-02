package config

import (
	"errors"
	"io/fs"
	"path/filepath"
)

// GreeterConfig is the [greeter] section
// (crates/wayle-config/src/schemas/greeter/mod.rs).
//
// Greeter (display manager): the pre-login screen `wayle-greeter` renders as
// a greetd greeter.
//
// The greeter reads the system config (`/etc/wayle/config.toml`), so these
// settings take effect there — copy or symlink your user config if you want
// the login screen to follow it.
type GreeterConfig struct {
	// Background is background-mode, background-image, and
	// background-color.
	Background Background `cfg:",inline"`
	// Show a clock above the login form.
	ShowClock bool `cfg:"show-clock"`
	// `strftime` format for the greeter time.
	ClockFormat StrftimeFormat `cfg:"clock-format"`
	// `strftime` format for the greeter date.
	DateFormat StrftimeFormat `cfg:"date-format"`
	// Show clickable avatars for the machine's login users.
	ShowUserList bool `cfg:"show-user-list"`
	// Show the shutdown/reboot buttons at the bottom of the screen.
	ShowPowerButtons bool `cfg:"show-power-buttons"`
	// Xcursor theme used on the login screen (empty = system default).
	CursorTheme string `cfg:"cursor-theme"`
	// Logical cursor size on the login screen. Scaled automatically per
	// display, so HiDPI outputs get a matching high-resolution cursor.
	CursorSize uint32 `cfg:"cursor-size"`
	// CursorThemeExplicit and CursorSizeExplicit record that a layer
	// set the key: explicit values beat cursor auto-detection, schema
	// defaults do not (ConfigProperty's config/runtime source).
	CursorThemeExplicit bool `cfg:"-"`
	CursorSizeExplicit  bool `cfg:"-"`
}

// DefaultsGreeter returns the schema defaults.
func DefaultsGreeter() GreeterConfig {
	return GreeterConfig{
		Background:       defaultBackground(),
		ShowClock:        true,
		ClockFormat:      mustStrftime("%H:%M"),
		DateFormat:       mustStrftime("%A, %B %-d"),
		ShowUserList:     true,
		ShowPowerButtons: true,
		CursorSize:       24,
	}
}

// GreeterConfigPath is the system config the greeter reads when no
// --config is given: pre-login there is no $HOME to discover from.
const GreeterConfigPath = "/etc/wayle/config.toml"

// RuntimeOverlayPath is the runtime.toml beside a config file: where
// `wayle-greeter apply-config` writes the settings GUI's overrides so
// they never clobber a hand-written config.toml.
func RuntimeOverlayPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "runtime.toml")
}

// LoadGreeter is wayle-greeter's config::load: the file at path with
// its imports on the config layer, the sibling runtime.toml on the
// runtime layer. A file that fails to load leaves its layer out (the
// errors come back joined beside a usable config), so the greeter
// always renders.
func LoadGreeter(path string, sink DiagnosticSink) (*Config, error) {
	var errs []error
	configTree, err := loadTree(path)
	if err != nil {
		errs = append(errs, err)
		configTree = nil
	}
	runtimeTree, err := readRuntime(RuntimeOverlayPath(path))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, err)
	}
	cfg, _, err := build(configTree, runtimeTree, nil, sink)
	if err != nil {
		errs = append(errs, err)
	}
	for _, tree := range []any{configTree, runtimeTree} {
		if hasPath(tree, "greeter.cursor-theme") {
			cfg.Greeter.CursorThemeExplicit = true
		}
		if hasPath(tree, "greeter.cursor-size") {
			cfg.Greeter.CursorSizeExplicit = true
		}
	}
	return cfg, errors.Join(errs...)
}

// GreeterApplyKeys are the [greeter] keys wayle-settings pushes to the
// login screen and wayle-greeter apply-config accepts (apply.rs
// ALLOWED_KEYS), in the settings app's order.
var GreeterApplyKeys = []string{
	"background-mode", "background-image", "background-color",
	"show-clock", "clock-format", "date-format",
	"show-user-list", "show-power-buttons",
	"cursor-theme", "cursor-size",
}

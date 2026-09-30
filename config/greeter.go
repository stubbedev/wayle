package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// GreeterConfig is the [greeter] section (wayle-config's
// GreeterConfig): the pre-login screen wayle-greeter renders for
// greetd, read from the system config.
type GreeterConfig struct {
	Background Background
	Clock      ClockFormats
	// ShowUserList shows clickable avatars for the login users.
	ShowUserList bool
	// ShowPowerButtons shows shutdown and reboot at the bottom.
	ShowPowerButtons bool
	// CursorTheme is the Xcursor theme; "" is the system default.
	CursorTheme string
	// CursorSize is the logical cursor size.
	CursorSize uint32
	// CursorThemeExplicit and CursorSizeExplicit record that a config
	// layer set the key: explicit values beat cursor auto-detection,
	// schema defaults do not (the Rust ConfigProperty config/runtime
	// layers).
	CursorThemeExplicit bool
	CursorSizeExplicit  bool
}

// DefaultsGreeter returns the schema defaults: a black fill, the
// clock, the user list, the power buttons, and a 24px system cursor.
func DefaultsGreeter() GreeterConfig {
	return GreeterConfig{
		Background:       Background{Mode: BackgroundColor, Color: mustHex("#000000")},
		Clock:            defaultClockFormats(),
		ShowUserList:     true,
		ShowPowerButtons: true,
		CursorSize:       24,
	}
}

// applyGreeter overlays [greeter] onto base, key by key, so a later
// layer (runtime.toml) only replaces what it sets.
func applyGreeter(md toml.MetaData, prim toml.Primitive, base GreeterConfig) (GreeterConfig, error) {
	cfg := base
	var doc struct {
		screenDoc
		ShowUserList     *bool   `toml:"show-user-list"`
		ShowPowerButtons *bool   `toml:"show-power-buttons"`
		CursorTheme      *string `toml:"cursor-theme"`
		CursorSize       *uint32 `toml:"cursor-size"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return base, fmt.Errorf("greeter: %w", err)
	}
	if err := doc.apply("greeter", &cfg.Background, &cfg.Clock); err != nil {
		return base, err
	}
	setIf(doc.ShowUserList, &cfg.ShowUserList)
	setIf(doc.ShowPowerButtons, &cfg.ShowPowerButtons)
	if doc.CursorTheme != nil {
		cfg.CursorTheme = *doc.CursorTheme
		cfg.CursorThemeExplicit = true
	}
	if doc.CursorSize != nil {
		cfg.CursorSize = *doc.CursorSize
		cfg.CursorSizeExplicit = true
	}
	return cfg, nil
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

// LoadGreeter loads the greeter's config (wayle-greeter's config::load):
// the file at path over the defaults, then the [greeter] table of the
// sibling runtime.toml over that, key by key. Failures come back as a
// joined error beside a usable config — the greeter logs them and
// always renders.
func LoadGreeter(path string) (*Config, error) {
	cfg, err := LoadFile(path)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	if err := cfg.applyGreeterOverlay(RuntimeOverlayPath(path)); err != nil {
		errs = append(errs, err)
	}
	return cfg, errors.Join(errs...)
}

// applyGreeterOverlay overlays runtime.toml's [greeter] table; a
// missing file is no overlay, not an error.
func (c *Config) applyGreeterOverlay(path string) error {
	data, err := os.ReadFile(path) //nolint:gosec // the overlay sits beside the config the operator named
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	var doc struct {
		Greeter *toml.Primitive `toml:"greeter"`
	}
	md, err := toml.Decode(string(data), &doc)
	if err != nil {
		return fmt.Errorf("config: %s: %w", path, err)
	}
	if doc.Greeter == nil {
		return nil
	}
	g, err := applyGreeter(md, *doc.Greeter, c.Greeter)
	if err != nil {
		return fmt.Errorf("config: %s: %w", path, err)
	}
	c.Greeter = g
	return nil
}

// applyScreens overlays the [lock] and [greeter] sections that are
// present.
func (c *Config) applyScreens(md toml.MetaData, lock, greeter *toml.Primitive) error {
	if lock != nil {
		l, err := applyLock(md, *lock, c.Lock)
		if err != nil {
			return err
		}
		c.Lock = l
	}
	if greeter != nil {
		g, err := applyGreeter(md, *greeter, c.Greeter)
		if err != nil {
			return err
		}
		c.Greeter = g
	}
	return nil
}

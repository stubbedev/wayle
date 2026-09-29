// Package config loads wayle's user configuration: the same files the
// Rust shell reads, with the same discovery order, key names, defaults,
// and failure behavior. It carries only the sections the Go shell
// consumes so far; unknown keys and sections are ignored, while known
// keys with bad values are load errors.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Dir returns the wayle configuration directory:
// $XDG_CONFIG_HOME/wayle, or ~/.config/wayle when XDG_CONFIG_HOME is
// unset. Both variables unset is an error — there is nowhere to look.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "wayle"), nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", errors.New("config: neither XDG_CONFIG_HOME nor HOME is set")
	}
	return filepath.Join(home, ".config", "wayle"), nil
}

// DiscoverMain resolves the user's main config file inside dir,
// preferring YAML over TOML: config.yaml, config.yml, then config.toml.
// When no config file exists yet, the config.toml path is returned, so
// fresh installs and explicit missing-file handling agree on one path.
func DiscoverMain(dir string) string {
	for _, name := range []string{"config.yaml", "config.yml"} {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join(dir, "config.toml")
}

// Load reads the user's config from the platform location. The error
// carries the reason the file could not be applied; the returned
// config is always usable (defaults, or defaults overlaid by whatever
// applied before the failure), so callers log the error and continue —
// the Rust shell's `using defaults, config.toml failed` behavior.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFile(DiscoverMain(dir))
}

// LoadFile parses one main config file. A missing file is the
// fresh-install case: defaults, no error. Anything unreadable or
// invalid is returned as the error alongside the fallback defaults.
func LoadFile(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path) //nolint:gosec // the path is resolved by DiscoverMain, never user input
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := cfg.applyTOML(data); err != nil {
		return cfg, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}

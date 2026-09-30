package config

import (
	"errors"

	"github.com/BurntSushi/toml"
)

// PowerConfig is the power module configuration: an icon button whose
// :menu opens the session command list, whose bindings run them.
type PowerConfig struct {
	Click   ClickConfig
	Icon    IconConfig
	Lock    string
	Logout  string
	Reboot  string
	Shutoff string
	Suspend string
}

// DefaultsPower returns the schema defaults.
func DefaultsPower() PowerConfig {
	return PowerConfig{
		Click:   DefaultsClick(map[string]string{"left-click": ":menu"}),
		Icon:    DefaultsIcon(true, "ld-power-symbolic"),
		Lock:    "loginctl lock-session",
		Logout:  "loginctl terminate-session $XDG_SESSION_ID",
		Reboot:  "systemctl reboot",
		Shutoff: "systemctl poweroff",
		Suspend: "systemctl suspend",
	}
}

// applyPower overlays [modules.power].
func applyPower(md toml.MetaData, prim toml.Primitive) (PowerConfig, error) {
	cfg := DefaultsPower()
	var doc struct {
		IconShow  *bool       `toml:"icon-show"`
		IconName  *string     `toml:"icon-name"`
		IconColor *ColorValue `toml:"icon-color"`
		Lock      *string     `toml:"lock-command"`
		Logout    *string     `toml:"logout-command"`
		Reboot    *string     `toml:"reboot-command"`
		Shutoff   *string     `toml:"shutdown-command"`
		Suspend   *string     `toml:"suspend-command"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	if doc.IconShow != nil {
		cfg.Icon.Show = *doc.IconShow
	}
	if doc.IconName != nil {
		cfg.Icon.Name = *doc.IconName
	}
	if doc.IconColor != nil {
		cfg.Icon.Color = *doc.IconColor
	}
	if doc.Lock != nil {
		cfg.Lock = *doc.Lock
	}
	if doc.Logout != nil {
		cfg.Logout = *doc.Logout
	}
	if doc.Reboot != nil {
		cfg.Reboot = *doc.Reboot
	}
	if doc.Shutoff != nil {
		cfg.Shutoff = *doc.Shutoff
	}
	if doc.Suspend != nil {
		cfg.Suspend = *doc.Suspend
	}
	if cfg.Lock == "" && cfg.Logout == "" && cfg.Reboot == "" && cfg.Shutoff == "" && cfg.Suspend == "" {
		return cfg, errors.New("power: every command is empty")
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}

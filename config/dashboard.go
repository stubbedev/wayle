package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// SessionAction is one button of the dashboard's user-session row
// (schemas/bar/dropdowns/dashboard/user_session.rs).
type SessionAction string

// Session actions, by their config spelling.
const (
	SessionLock     SessionAction = "lock"
	SessionLogout   SessionAction = "log-out"
	SessionReboot   SessionAction = "reboot"
	SessionPowerOff SessionAction = "power-off"
)

// ParseSessionAction reads one actions entry; anything but the four
// spellings is an error.
func ParseSessionAction(raw string) (SessionAction, error) {
	switch a := SessionAction(raw); a {
	case SessionLock, SessionLogout, SessionReboot, SessionPowerOff:
		return a, nil
	}
	return "", fmt.Errorf("unknown session action %q (want lock, log-out, reboot, or power-off)", raw)
}

// DashboardConfig is [modules.dashboard]: the distro-icon button that
// opens the dashboard dropdown, the dropdown's session commands, its
// severity thresholds, and the user-session row
// (schemas/modules/dashboard/mod.rs).
type DashboardConfig struct {
	Click ClickConfig
	// IconOverride replaces the auto-detected distro icon when set.
	IconOverride string
	IconColor    ColorValue
	IconBgColor  ColorValue
	BorderShow   bool
	BorderColor  ColorValue

	LockCommand     string
	LogoutCommand   string
	RebootCommand   string
	PowerOffCommand string

	// Ring thresholds: percent for CPU/RAM/disk usage, degrees Celsius
	// for the CPU temperature, percent at or below for the battery.
	UsageWarning    float64
	UsageError      float64
	TempWarning     float64
	TempError       float64
	BatteryWarning  float64
	BatteryCritical float64

	// SessionActions are the user-session row's buttons, in order.
	SessionActions []SessionAction
}

// DefaultsDashboard returns the schema defaults.
func DefaultsDashboard() DashboardConfig {
	return DashboardConfig{
		Click:           DefaultsClick(map[string]string{"left-click": "dropdown:dashboard"}),
		IconColor:       mustColor("auto"),
		IconBgColor:     mustColor("yellow"),
		BorderColor:     mustColor("yellow"),
		LockCommand:     "loginctl lock-session",
		LogoutCommand:   "loginctl terminate-session $XDG_SESSION_ID",
		RebootCommand:   "systemctl reboot",
		PowerOffCommand: "systemctl poweroff",
		UsageWarning:    60,
		UsageError:      85,
		TempWarning:     65,
		TempError:       85,
		BatteryWarning:  30,
		BatteryCritical: 15,
		SessionActions:  []SessionAction{SessionLock, SessionLogout, SessionReboot, SessionPowerOff},
	}
}

// applyDashboard overlays [modules.dashboard] and its
// [modules.dashboard.user-session] table.
func applyDashboard(md toml.MetaData, prim toml.Primitive) (DashboardConfig, error) {
	cfg := DefaultsDashboard()
	var doc struct {
		IconOverride    *string     `toml:"icon-override"`
		IconColor       *ColorValue `toml:"icon-color"`
		IconBgColor     *ColorValue `toml:"icon-bg-color"`
		BorderShow      *bool       `toml:"border-show"`
		BorderColor     *ColorValue `toml:"border-color"`
		LockCommand     *string     `toml:"dropdown-lock-command"`
		LogoutCommand   *string     `toml:"dropdown-logout-command"`
		RebootCommand   *string     `toml:"dropdown-reboot-command"`
		PowerOffCommand *string     `toml:"dropdown-poweroff-command"`
		UsageWarning    *float64    `toml:"usage-warning"`
		UsageError      *float64    `toml:"usage-error"`
		TempWarning     *float64    `toml:"temp-warning"`
		TempError       *float64    `toml:"temp-error"`
		BatteryWarning  *float64    `toml:"battery-warning"`
		BatteryCritical *float64    `toml:"battery-critical"`
		UserSession     *struct {
			Actions *[]string `toml:"actions"`
		} `toml:"user-session"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	for _, s := range []struct {
		raw  *string
		dest *string
	}{
		{doc.IconOverride, &cfg.IconOverride},
		{doc.LockCommand, &cfg.LockCommand},
		{doc.LogoutCommand, &cfg.LogoutCommand},
		{doc.RebootCommand, &cfg.RebootCommand},
		{doc.PowerOffCommand, &cfg.PowerOffCommand},
	} {
		if s.raw != nil {
			*s.dest = *s.raw
		}
	}
	for _, c := range []struct {
		raw  *ColorValue
		dest *ColorValue
	}{
		{doc.IconColor, &cfg.IconColor},
		{doc.IconBgColor, &cfg.IconBgColor},
		{doc.BorderColor, &cfg.BorderColor},
	} {
		if c.raw != nil {
			*c.dest = *c.raw
		}
	}
	for _, f := range []struct {
		raw  *float64
		dest *float64
	}{
		{doc.UsageWarning, &cfg.UsageWarning},
		{doc.UsageError, &cfg.UsageError},
		{doc.TempWarning, &cfg.TempWarning},
		{doc.TempError, &cfg.TempError},
		{doc.BatteryWarning, &cfg.BatteryWarning},
		{doc.BatteryCritical, &cfg.BatteryCritical},
	} {
		if f.raw != nil {
			*f.dest = *f.raw
		}
	}
	if doc.BorderShow != nil {
		cfg.BorderShow = *doc.BorderShow
	}
	if doc.UserSession != nil && doc.UserSession.Actions != nil {
		actions := make([]SessionAction, 0, len(*doc.UserSession.Actions))
		for _, raw := range *doc.UserSession.Actions {
			a, err := ParseSessionAction(raw)
			if err != nil {
				return cfg, fmt.Errorf("dashboard: user-session actions: %w", err)
			}
			actions = append(actions, a)
		}
		cfg.SessionActions = actions
	}
	clicks, err := applyClicks(md, prim, cfg.Click)
	if err != nil {
		return cfg, err
	}
	cfg.Click = clicks
	return cfg, nil
}

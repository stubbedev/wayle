package config

// DashboardConfig is ported from crates/wayle-config/src/schemas/modules/dashboard/mod.rs.
//
// Quick-access button with a distro icon; opens the dashboard dropdown.
type DashboardConfig struct {
	// Override the auto-detected distro icon.
	IconOverride string `cfg:"icon-override"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Shell command for the lock button in the dashboard dropdown.
	DropdownLockCommand string `cfg:"dropdown-lock-command"`
	// Shell command for the logout button in the dashboard dropdown.
	DropdownLogoutCommand string `cfg:"dropdown-logout-command"`
	// Shell command for the reboot button in the dashboard dropdown.
	DropdownRebootCommand string `cfg:"dropdown-reboot-command"`
	// Shell command for the power-off button in the dashboard dropdown.
	DropdownPoweroffCommand string `cfg:"dropdown-poweroff-command"`
	// CPU/RAM/disk usage percent at which the dashboard rings turn warning.
	UsageWarning float32 `cfg:"usage-warning"`
	// CPU/RAM/disk usage percent at which the dashboard rings turn error.
	UsageError float32 `cfg:"usage-error"`
	// CPU temperature (°C) at which the dashboard temp ring turns warning.
	TempWarning float32 `cfg:"temp-warning"`
	// CPU temperature (°C) at which the dashboard temp ring turns error.
	TempError float32 `cfg:"temp-error"`
	// Battery percent at or below which the dashboard battery shows warning.
	BatteryWarning float32 `cfg:"battery-warning"`
	// Battery percent at or below which the dashboard battery shows critical.
	BatteryCritical float32 `cfg:"battery-critical"`
	// User session configuration
	UserSession UserSessionConfig `cfg:"user-session"`
}

// DefaultsDashboard returns the schema defaults.
func DefaultsDashboard() DashboardConfig {
	return DashboardConfig{
		IconOverride:            "",
		BorderShow:              false,
		BorderColor:             mustColor("yellow"),
		IconColor:               mustColor("auto"),
		IconBgColor:             mustColor("yellow"),
		RightClick:              ClickAction{},
		MiddleClick:             ClickAction{},
		ScrollUp:                ClickAction{},
		ScrollDown:              ClickAction{},
		LeftClick:               ParseClickAction("dropdown:dashboard"),
		DropdownLockCommand:     "loginctl lock-session",
		DropdownLogoutCommand:   "loginctl terminate-session $XDG_SESSION_ID",
		DropdownRebootCommand:   "systemctl reboot",
		DropdownPoweroffCommand: "systemctl poweroff",
		UsageWarning:            60,
		UsageError:              85,
		TempWarning:             65,
		TempError:               85,
		BatteryWarning:          30,
		BatteryCritical:         15,
		UserSession: UserSessionConfig{
			Actions: []SessionAction{
				SessionActionLock,
				SessionActionLogOut,
				SessionActionReboot,
				SessionActionPowerOff,
			},
		},
	}
}

// Clicks returns the five input bindings.
func (c DashboardConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// UserSessionConfig is ported from crates/wayle-config/src/schemas/modules/dashboard/mod.rs.
//
// Settings for user session the in dashboard
// ## Examples
//
// ```toml
// [modules.dashboard.user-session]
// actions = [ "lock", "log-out", "reboot", "power-off" ]
// ```
type UserSessionConfig struct {
	// Session actions to show on dashboard
	Actions []SessionAction `cfg:"actions"`
}

// DefaultsUserSession returns the schema defaults.
func DefaultsUserSession() UserSessionConfig {
	return UserSessionConfig{
		Actions: []SessionAction{
			SessionActionLock,
			SessionActionLogOut,
			SessionActionReboot,
			SessionActionPowerOff,
		},
	}
}

// SessionAction is ported from crates/wayle-config/src/schemas/bar/dropdowns/dashboard/user_session.rs.
//
// One action the dashboard session actions
type SessionAction string

// SessionAction values.
const (
	// Lock the session
	SessionActionLock SessionAction = "lock"
	// Logout of the current session
	SessionActionLogOut SessionAction = "log-out"
	// Reboot the machine
	SessionActionReboot SessionAction = "reboot"
	// Power off the machine
	SessionActionPowerOff SessionAction = "power-off"
)

var _ = registerEnum(SessionActionLock, SessionActionLogOut, SessionActionReboot, SessionActionPowerOff)

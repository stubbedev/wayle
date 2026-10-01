package config

// PowerConfig is ported from crates/wayle-config/src/schemas/modules/power/mod.rs.
//
// Shutdown, reboot, and logout menu.
type PowerConfig struct {
	// Icon name to display.
	IconName string `cfg:"icon-name"`
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
	// Action on left click. Default opens wayle's native power menu (`:menu`).
	LeftClick ClickAction `cfg:"left-click"`
	// Command run by the power menu's Lock button.
	LockCommand string `cfg:"lock-command"`
	// Command run by the power menu's Log out button.
	LogoutCommand string `cfg:"logout-command"`
	// Command run by the power menu's Suspend button.
	SuspendCommand string `cfg:"suspend-command"`
	// Command run by the power menu's Reboot button.
	RebootCommand string `cfg:"reboot-command"`
	// Command run by the power menu's Shut down button.
	ShutdownCommand string `cfg:"shutdown-command"`
	// Show the Lock button in the power menu.
	ShowLock bool `cfg:"show-lock"`
	// Show the Log out button in the power menu.
	ShowLogout bool `cfg:"show-logout"`
	// Show the Suspend button in the power menu.
	ShowSuspend bool `cfg:"show-suspend"`
	// Show the Reboot button in the power menu.
	ShowReboot bool `cfg:"show-reboot"`
	// Show the Shut down button in the power menu.
	ShowShutdown bool `cfg:"show-shutdown"`
}

// DefaultsPower returns the schema defaults.
func DefaultsPower() PowerConfig {
	return PowerConfig{
		IconName:        "ld-power-symbolic",
		BorderShow:      false,
		BorderColor:     mustColor("red"),
		IconColor:       mustColor("auto"),
		IconBgColor:     mustColor("red"),
		RightClick:      ClickAction{},
		MiddleClick:     ClickAction{},
		ScrollUp:        ClickAction{},
		ScrollDown:      ClickAction{},
		LeftClick:       ParseClickAction(":menu"),
		LockCommand:     "loginctl lock-session",
		LogoutCommand:   "loginctl terminate-session $XDG_SESSION_ID",
		SuspendCommand:  "systemctl suspend",
		RebootCommand:   "systemctl reboot",
		ShutdownCommand: "systemctl poweroff",
		ShowLock:        true,
		ShowLogout:      true,
		ShowSuspend:     true,
		ShowReboot:      true,
		ShowShutdown:    true,
	}
}

// Clicks returns the five input bindings.
func (c PowerConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

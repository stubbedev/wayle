package config

// LockConfig is the [lock] section
// (crates/wayle-config/src/schemas/lock/mod.rs).
//
// Lock screen: a secure session lock rendered by Wayle via `ext-session-lock-v1`.
//
// When `enabled`, Wayle locks the session in response to the logind `Lock`
// signal (`loginctl lock-session`), the `wayle lock` CLI, or the shell IPC
// `lock()` method. The lock surface grabs all input and survives client
// crashes (the compositor shows a solid color), unlike a layer-shell overlay.
type LockConfig struct {
	// Let Wayle handle session locking. When off, lock requests are ignored
	// and an external locker (e.g. hyprlock) stays responsible.
	Enabled bool `cfg:"enabled"`
	// Lock the session as soon as the shell starts. For autologin setups
	// (e.g. greetd with no password at boot) where the lock screen is the
	// access gate: the session comes up locked and requires the password
	// before use. Off by default — a normal login has already authenticated.
	LockOnStart bool `cfg:"lock-on-start"`
	// Background is background-mode, background-image, and
	// background-color.
	Background Background `cfg:",inline"`
	// Gaussian blur radius applied to image/wallpaper backgrounds (0 = none).
	Blur uint32 `cfg:"blur"`
	// Show a clock on the lock screen.
	ShowClock bool `cfg:"show-clock"`
	// `strftime` format for the lock-screen time.
	ClockFormat StrftimeFormat `cfg:"clock-format"`
	// `strftime` format for the lock-screen date.
	DateFormat StrftimeFormat `cfg:"date-format"`
	// Grace window after locking during which the screen unlocks without a
	// password (milliseconds, `0` = always require the password).
	GracePeriodMS uint32 `cfg:"grace-period-ms"`
	// Maximum failed password attempts before further input is blocked
	// (`0` = unlimited). The screen stays locked regardless.
	MaxAttempts uint32 `cfg:"max-attempts"`
	// Show the failed-attempt count on the lock screen.
	ShowFailedAttempts bool `cfg:"show-failed-attempts"`
	// Black out the lock screen after this idle time (milliseconds, `0` =
	// never). This is a visual blank that hides the clock/prompt and dismisses
	// on any key; true display power-off (DPMS) is left to your idle daemon.
	BlankTimeoutMS uint32 `cfg:"blank-timeout-ms"`
	// PAM service name used to authenticate the unlock. Distro-dependent:
	// `system-auth` (Arch/Fedora), `login`, or a custom `/etc/pam.d` entry.
	PamService string `cfg:"pam-service"`
}

// DefaultsLock returns the schema defaults.
func DefaultsLock() LockConfig {
	return LockConfig{
		Enabled:            true,
		Background:         defaultBackground(),
		ShowClock:          true,
		ClockFormat:        mustStrftime("%H:%M"),
		DateFormat:         mustStrftime("%A, %B %-d"),
		ShowFailedAttempts: true,
		PamService:         "system-auth",
	}
}

// Background is the background keys the lock screen and the greeter
// share, with the same descriptions in both schemas.
type Background struct {
	// How the background is drawn: solid color, an image, or the wallpaper.
	Mode LockBackground `cfg:"background-mode"`
	// Background image path (used when `background-mode = "image"`).
	Image string `cfg:"background-image"`
	// Background fill color (used when `background-mode = "color"`).
	Color HexColor `cfg:"background-color"`
}

// defaultBackground is the shared schema default: a black fill.
func defaultBackground() Background {
	return Background{Mode: BackgroundColor, Color: mustHex("#000000")}
}

// ImagePath resolves the file the background shows: the configured
// image for image mode, the wallpaper for wallpaper mode, and "" for
// color mode (or an unset path, which draws the color).
func (b Background) ImagePath(wallpaper string) string {
	switch b.Mode {
	case BackgroundImage:
		return b.Image
	case BackgroundWallpaper:
		return wallpaper
	}
	return ""
}

// LockBackground is how the lock screen and the greeter draw their
// background (lock/types.rs).
//
// How the lock screen background is rendered.
type LockBackground string

// Background modes.
const (
	// Solid color fill (`background-color`).
	BackgroundColor LockBackground = "color"
	// A specific image file (`background-image`).
	BackgroundImage LockBackground = "image"
	// Reuse the current desktop wallpaper.
	BackgroundWallpaper LockBackground = "wallpaper"
)

var _ = registerEnum(BackgroundColor, BackgroundImage, BackgroundWallpaper)

package config

import (
	"fmt"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/strftime"
)

// LockBackground is how the lock screen (and the greeter) draws its
// background: the LockBackground schema enum.
type LockBackground uint8

// Background modes.
const (
	// BackgroundColor paints a solid fill (background-color).
	BackgroundColor LockBackground = iota
	// BackgroundImage shows a specific file (background-image).
	BackgroundImage
	// BackgroundWallpaper reuses the desktop wallpaper.
	BackgroundWallpaper
)

// String returns the schema name.
func (b LockBackground) String() string {
	switch b {
	case BackgroundImage:
		return "image"
	case BackgroundWallpaper:
		return "wallpaper"
	}
	return "color"
}

// UnmarshalText decodes the schema name; anything else is an error.
func (b *LockBackground) UnmarshalText(text []byte) error {
	switch string(text) {
	case "color":
		*b = BackgroundColor
	case "image":
		*b = BackgroundImage
	case "wallpaper":
		*b = BackgroundWallpaper
	default:
		return fmt.Errorf("config: unknown background-mode %q (want color, image, or wallpaper)", text)
	}
	return nil
}

// Background is the background keys the lock screen and the greeter
// share: background-mode, background-image, background-color.
type Background struct {
	Mode  LockBackground
	Image string
	Color HexColor
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

// ClockFormats is the clock keys the lock screen and the greeter
// share: show-clock and the compiled clock-format and date-format.
type ClockFormats struct {
	Show bool
	Time *strftime.Layout
	Date *strftime.Layout
}

// defaultClockFormats is the shared schema default: shown, "%H:%M",
// "%A, %B %-d".
func defaultClockFormats() ClockFormats {
	return ClockFormats{Show: true, Time: mustLayout("%H:%M"), Date: mustLayout("%A, %B %-d")}
}

func mustLayout(format string) *strftime.Layout {
	l, err := strftime.Compile(format)
	if err != nil {
		panic(err)
	}
	return l
}

// screenDoc is the TOML shape of the shared background and clock keys.
type screenDoc struct {
	BackgroundMode  *LockBackground `toml:"background-mode"`
	BackgroundImage *string         `toml:"background-image"`
	BackgroundColor *HexColor       `toml:"background-color"`
	ShowClock       *bool           `toml:"show-clock"`
	ClockFormat     *string         `toml:"clock-format"`
	DateFormat      *string         `toml:"date-format"`
}

// apply overlays the present keys; a bad strftime format is a load
// error, like every other bad value.
func (d screenDoc) apply(section string, bg *Background, clock *ClockFormats) error {
	if d.BackgroundMode != nil {
		bg.Mode = *d.BackgroundMode
	}
	if d.BackgroundImage != nil {
		bg.Image = *d.BackgroundImage
	}
	if d.BackgroundColor != nil {
		bg.Color = *d.BackgroundColor
	}
	if d.ShowClock != nil {
		clock.Show = *d.ShowClock
	}
	for _, f := range []struct {
		src *string
		dst **strftime.Layout
		key string
	}{
		{d.ClockFormat, &clock.Time, "clock-format"},
		{d.DateFormat, &clock.Date, "date-format"},
	} {
		if f.src == nil {
			continue
		}
		l, err := strftime.Compile(*f.src)
		if err != nil {
			return fmt.Errorf("%s: %s: %w", section, f.key, err)
		}
		*f.dst = l
	}
	return nil
}

// LockConfig is the [lock] section (wayle-config's LockConfig): the
// ext-session-lock screen the shell raises on logind Lock, `wayle
// lock`, and the shell IPC Lock method.
type LockConfig struct {
	// Enabled lets wayle handle locking; off leaves it to an external
	// locker and ignores lock requests.
	Enabled bool
	// LockOnStart locks once when a login session's shell first
	// starts (the autologin gate).
	LockOnStart bool
	Background  Background
	// Blur is the gaussian blur radius for image and wallpaper
	// backgrounds; 0 is none.
	Blur  uint32
	Clock ClockFormats
	// GracePeriodMS unlocks without a password within this long after
	// locking; 0 always requires the password.
	GracePeriodMS uint32
	// MaxAttempts blocks input after this many failures; 0 is
	// unlimited. The screen stays locked regardless.
	MaxAttempts uint32
	// ShowFailedAttempts shows the failure count on the screen.
	ShowFailedAttempts bool
	// BlankTimeoutMS blacks the screen out after this much inactivity;
	// 0 never blanks.
	BlankTimeoutMS uint32
	// PamService is the PAM service the unlock authenticates against.
	PamService string
}

// DefaultsLock returns the schema defaults: enabled, never on start, a
// black fill, and no grace window, attempt cap, or blanking.
func DefaultsLock() LockConfig {
	return LockConfig{
		Enabled:            true,
		Background:         Background{Mode: BackgroundColor, Color: mustHex("#000000")},
		Clock:              defaultClockFormats(),
		ShowFailedAttempts: true,
		PamService:         "system-auth",
	}
}

// applyLock overlays [lock] onto base.
func applyLock(md toml.MetaData, prim toml.Primitive, base LockConfig) (LockConfig, error) {
	cfg := base
	var doc struct {
		screenDoc
		Enabled            *bool   `toml:"enabled"`
		LockOnStart        *bool   `toml:"lock-on-start"`
		Blur               *uint32 `toml:"blur"`
		GracePeriodMS      *uint32 `toml:"grace-period-ms"`
		MaxAttempts        *uint32 `toml:"max-attempts"`
		ShowFailedAttempts *bool   `toml:"show-failed-attempts"`
		BlankTimeoutMS     *uint32 `toml:"blank-timeout-ms"`
		PamService         *string `toml:"pam-service"`
	}
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return base, fmt.Errorf("lock: %w", err)
	}
	if err := doc.apply("lock", &cfg.Background, &cfg.Clock); err != nil {
		return base, err
	}
	setIf(&cfg.Enabled, doc.Enabled)
	setIf(&cfg.LockOnStart, doc.LockOnStart)
	setIf(&cfg.Blur, doc.Blur)
	setIf(&cfg.GracePeriodMS, doc.GracePeriodMS)
	setIf(&cfg.MaxAttempts, doc.MaxAttempts)
	setIf(&cfg.ShowFailedAttempts, doc.ShowFailedAttempts)
	setIf(&cfg.BlankTimeoutMS, doc.BlankTimeoutMS)
	setIf(&cfg.PamService, doc.PamService)
	return cfg, nil
}

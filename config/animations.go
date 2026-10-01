package config

import (
	"fmt"
	"math"
)

// AnimationsConfig is the [animations] section
// (crates/wayle-config/src/schemas/animations/mod.rs).
//
// Enter/exit and change animations for transient surfaces.
type AnimationsConfig struct {
	// Enable enter/exit animations (OSD, toasts, notifications) and
	// icon-change crossfades. When disabled, surfaces appear instantly.
	Enabled bool `cfg:"enabled"`
	// Animation duration in milliseconds.
	Duration uint32 `cfg:"duration"`
	// Transition style used for enter/exit of the OSD, toasts, and
	// notification cards. Base fallback for every surface and direction.
	Transition AnimationType `cfg:"transition"`
	// Global enter transition. Unset → `transition`.
	Enter *AnimationType `cfg:"enter"`
	// Global exit transition. Unset → `transition`.
	Exit *AnimationType `cfg:"exit"`
	// Global enter duration in ms. Unset → `duration`.
	EnterDuration *uint32 `cfg:"enter-duration"`
	// Global exit duration in ms. Unset → `duration`.
	ExitDuration *uint32 `cfg:"exit-duration"`
	// Duration in ms for in-place dropdown transitions: hover highlights and
	// the page/stack crossfades inside dropdowns. `enabled = false` removes
	// them entirely.
	InteractionDuration uint32 `cfg:"interaction-duration"`
	// Base duration in ms for general UI micro-transitions (hover, focus, and
	// color fades) driven by the CSS `--duration-*` token family. Fast,
	// normal, and slow speeds are derived from this. `enabled = false` zeroes
	// them for an instant UI.
	UIDuration uint32 `cfg:"ui-duration"`
	// Run looping status indicators: spinners, network/bluetooth scan
	// animations, the recording pulse, and the clock blink. Disable (or set
	// `enabled = false`) for a fully static UI.
	Indicators bool `cfg:"indicators"`
	// Per-surface override for notification popup cards.
	Notifications SurfaceAnimation `cfg:"notifications"`
	// Per-surface override for the OSD (volume/brightness/toggle/toast).
	Osd SurfaceAnimation `cfg:"osd"`
	// Per-surface override for bar widget dropdown foldouts.
	Dropdown SurfaceAnimation `cfg:"dropdown"`
	// Per-surface override for the screen-share picker overlay.
	SharePicker SurfaceAnimation `cfg:"share-picker"`
	// Per-surface override for the wallpaper crossfade between images.
	Wallpaper SurfaceAnimation `cfg:"wallpaper"`
	// Per-surface override for the power menu overlay.
	Power SurfaceAnimation `cfg:"power"`
	// Per-surface override for portal dialogs (access/account/app-chooser/
	// launcher-install prompts).
	Dialog SurfaceAnimation `cfg:"dialog"`
	// Per-surface override for the portal file-chooser surface.
	FileChooser SurfaceAnimation `cfg:"file-chooser"`
	// Per-surface override for the portal print surface.
	Print SurfaceAnimation `cfg:"print"`
	// Per-surface override for the lock screen reveal.
	Lock SurfaceAnimation `cfg:"lock"`
	// Per-surface override for the application launcher.
	Launcher SurfaceAnimation `cfg:"launcher"`
}

// DefaultsAnimations returns the schema defaults.
func DefaultsAnimations() AnimationsConfig {
	return AnimationsConfig{
		Enabled:             true,
		Duration:            200,
		Transition:          AnimationFade,
		InteractionDuration: 150,
		UIDuration:          250,
		Indicators:          true,
	}
}

// AnimSurface is a transient surface an animation applies to.
type AnimSurface int

// Surfaces.
const (
	// AnimNotifications is the notification popup cards.
	AnimNotifications AnimSurface = iota
	// AnimOsd is the OSD overlay (volume/brightness/toggle/toast).
	AnimOsd
	// AnimDropdown is the bar widget dropdown foldouts.
	AnimDropdown
	// AnimSharePicker is the screen-share picker overlay.
	AnimSharePicker
	// AnimWallpaper is the wallpaper crossfade between images.
	AnimWallpaper
	// AnimPower is the power menu overlay.
	AnimPower
	// AnimDialog is the portal dialogs (access / account / app-chooser /
	// launcher install).
	AnimDialog
	// AnimFileChooser is the portal file-chooser surface.
	AnimFileChooser
	// AnimPrint is the portal print surface.
	AnimPrint
	// AnimLock is the lock screen reveal.
	AnimLock
	// AnimLauncher is the application launcher overlay.
	AnimLauncher
)

func (a AnimationsConfig) surface(s AnimSurface) SurfaceAnimation {
	switch s {
	case AnimOsd:
		return a.Osd
	case AnimDropdown:
		return a.Dropdown
	case AnimSharePicker:
		return a.SharePicker
	case AnimWallpaper:
		return a.Wallpaper
	case AnimPower:
		return a.Power
	case AnimDialog:
		return a.Dialog
	case AnimFileChooser:
		return a.FileChooser
	case AnimPrint:
		return a.Print
	case AnimLock:
		return a.Lock
	case AnimLauncher:
		return a.Launcher
	}
	return a.Notifications
}

// TransitionFor is the resolved transition for a surface and
// direction: none when disabled, else the surface override, the
// global enter/exit, then `transition`.
func (a AnimationsConfig) TransitionFor(s AnimSurface, exiting bool) AnimationType {
	if !a.Enabled {
		return AnimationNone
	}
	sa := a.surface(s)
	perSurface, global := sa.Enter, a.Enter
	if exiting {
		perSurface, global = sa.Exit, a.Exit
	}
	switch {
	case perSurface != nil:
		return *perSurface
	case global != nil:
		return *global
	}
	return a.Transition
}

// DurationFor is the resolved duration in ms for a surface and
// direction; 0 when disabled.
func (a AnimationsConfig) DurationFor(s AnimSurface, exiting bool) uint32 {
	if !a.Enabled {
		return 0
	}
	sa := a.surface(s)
	perSurface, global := sa.EnterDuration, a.EnterDuration
	if exiting {
		perSurface, global = sa.ExitDuration, a.ExitDuration
	}
	switch {
	case perSurface != nil:
		return *perSurface
	case global != nil:
		return *global
	}
	return a.Duration
}

// InteractionDurationMs is the in-place dropdown transition duration;
// 0 when disabled.
func (a AnimationsConfig) InteractionDurationMs() uint32 {
	if a.Enabled {
		return a.InteractionDuration
	}
	return 0
}

// CSSOverrides is the :root block bridging the config into the
// stylesheet: the --duration-* family from ui-duration (0.3/0.6/1/1.4
// scaled) and, with indicators off, every looping keyframe frozen.
func (a AnimationsConfig) CSSOverrides() string {
	ui := uint32(0)
	if a.Enabled {
		ui = a.UIDuration
	}
	scaled := func(factor float32) uint32 {
		return uint32(math.Round(float64(float32(ui) * factor)))
	}
	frozen := ""
	if !a.Enabled || !a.Indicators {
		frozen = "    --cfg-anim-spin: 0s;\n    --cfg-anim-media-spin: 0s;\n    " +
			"--cfg-anim-scan: 0s;\n    --cfg-anim-blink: 0s;\n    --cfg-anim-pulse: 0s;\n"
	}
	return fmt.Sprintf(":root {\n    --duration-super-fast: %dms;\n    --duration-fast: %dms;\n    "+
		"--duration-normal: %dms;\n    --duration-slow: %dms;\n%s}",
		scaled(0.3), scaled(0.6), ui, scaled(1.4), frozen)
}

// AnimationType is a transition style
// (crates/wayle-config/src/schemas/animations/types.rs).
//
// Enter/exit transition style for transient surfaces (OSD, toasts,
// notifications). Driven per frame by the custom revealer
// (`GskTransform` + opacity).
type AnimationType string

// Transition styles.
const (
	// No transition; surfaces appear and disappear instantly.
	AnimationNone AnimationType = "none"
	// Cross-fade opacity in and out.
	AnimationFade AnimationType = "fade"
	// Slide in from / out to the top edge.
	AnimationSlideUp AnimationType = "slide-up"
	// Slide in from / out to the bottom edge.
	AnimationSlideDown AnimationType = "slide-down"
	// Slide in from / out to the left edge.
	AnimationSlideLeft AnimationType = "slide-left"
	// Slide in from / out to the right edge.
	AnimationSlideRight AnimationType = "slide-right"
	// Slide in from / out to the top edge with a rotating swing.
	AnimationSwingUp AnimationType = "swing-up"
	// Slide in from / out to the bottom edge with a rotating swing.
	AnimationSwingDown AnimationType = "swing-down"
	// Slide in from / out to the left edge with a rotating swing.
	AnimationSwingLeft AnimationType = "swing-left"
	// Slide in from / out to the right edge with a rotating swing.
	AnimationSwingRight AnimationType = "swing-right"
	// Scale in from a shrunken state with an elastic overshoot, then settle.
	AnimationBounce AnimationType = "bounce"
	// Suck in toward / out to the surface's anchored edge (macOS "genie"
	// minimize, approximated with an affine scale-and-slide). Auto-orients to
	// the edge the surface sits on.
	AnimationGenie AnimationType = "genie"
	// Scale in smoothly from the center with no overshoot.
	AnimationZoom AnimationType = "zoom"
	// Spin in: rotate into place while scaling up and fading in.
	AnimationRotate AnimationType = "rotate"
	// Card flip: open out from a vertical edge (horizontal scale through zero).
	AnimationFlip AnimationType = "flip"
)

var _ = registerEnum(
	AnimationNone, AnimationFade,
	AnimationSlideUp, AnimationSlideDown, AnimationSlideLeft, AnimationSlideRight,
	AnimationSwingUp, AnimationSwingDown, AnimationSwingLeft, AnimationSwingRight,
	AnimationBounce, AnimationGenie, AnimationZoom, AnimationRotate, AnimationFlip,
)

// SurfaceAnimation is one surface's override.
//
// Per-surface enter/exit animation override. Any field left unset falls back
// to the global `[animations]` enter/exit, then to `transition`/`duration`.
type SurfaceAnimation struct {
	// Enter transition for this surface. Unset → global `enter`, then `transition`.
	Enter *AnimationType `cfg:"enter"`
	// Exit transition for this surface. Unset → global `exit`, then `transition`.
	Exit *AnimationType `cfg:"exit"`
	// Enter duration in ms. Unset → global `enter-duration`, then `duration`.
	EnterDuration *uint32 `cfg:"enter-duration"`
	// Exit duration in ms. Unset → global `exit-duration`, then `duration`.
	ExitDuration *uint32 `cfg:"exit-duration"`
}

func (SurfaceAnimation) configValue() {}

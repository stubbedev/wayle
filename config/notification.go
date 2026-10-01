package config

// NotificationConfig is ported from crates/wayle-config/src/schemas/modules/notification/mod.rs.
//
// Notification center: icon in the bar, dropdown with history, DND toggle.
type NotificationConfig struct {
	// Enable the notifications service and module.
	Enabled bool `cfg:"enabled"`
	// Icon shown when no notifications and DND is off.
	IconName string `cfg:"icon-name"`
	// Icon shown when notifications exist.
	IconUnread string `cfg:"icon-unread"`
	// Icon shown when Do Not Disturb is active.
	IconDnd string `cfg:"icon-dnd"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display notification count label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click. Default toggles Do Not Disturb.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
	// Glob patterns for app names whose notifications are blocked entirely.
	//
	// Matched notifications are silently dropped.
	// Supports `*` (any characters) and `?` (single character).
	//
	// Examples: `["notify-send", "*chromium*", "Vivaldi*"]`
	Blocklist []string `cfg:"blocklist"`
	// How notification icons are resolved.
	//
	// | Mode | Per-notification image | No image provided |
	// |------|----------------------|-------------------|
	// | `automatic` | Shows the image | Mapped icon |
	// | `mapped` | Ignored | Mapped icon |
	// | `application` | Shows the image | App's generic icon, then mapped fallback |
	IconSource IconSource `cfg:"icon-source"`
	// Screen position for popup notifications.
	PopupPosition PopupPosition `cfg:"popup-position"`
	// Maximum number of popups visible at once.
	PopupMaxVisible uint32 `cfg:"popup-max-visible"`
	// Order in which popups stack on screen.
	PopupStackingOrder StackingOrder `cfg:"popup-stacking-order"`
	// Maximum popup display duration in milliseconds.
	//
	// Applications may request a shorter timeout, which takes precedence.
	PopupDuration uint32 `cfg:"popup-duration"`
	// Pause popup auto-dismiss timer on hover.
	PopupHoverPause bool `cfg:"popup-hover-pause"`
	// Horizontal margin from screen edges. Accepts a scale multiplier or
	// pixels (e.g. `"12px"`).
	PopupMarginX Size `cfg:"popup-margin-x"`
	// Vertical margin from screen edges. Accepts a scale multiplier or pixels
	// (e.g. `"12px"`).
	PopupMarginY Size `cfg:"popup-margin-y"`
	// Gap between stacked popups: a multiplier of the default 8px (`1.0` =
	// default) or absolute pixels.
	PopupGap Size `cfg:"popup-gap"`
	// Target monitor: "primary" or a connector name like "DP-1".
	PopupMonitor PopupMonitor `cfg:"popup-monitor"`
	// Layer-shell layer popup notifications are placed on.
	//
	// When `general.tearing-mode` is enabled, `overlay` is demoted to `top`
	// to allow fullscreen tearing.
	PopupLayer Layer `cfg:"popup-layer"`
	// What happens when the close button on a popup is clicked.
	PopupCloseBehavior PopupCloseBehavior `cfg:"popup-close-behavior"`
	// Display drop shadow on popup cards.
	PopupShadow bool `cfg:"popup-shadow"`
	// Minimum urgency level that displays a colored urgency bar.
	PopupUrgencyBar UrgencyBarThreshold `cfg:"popup-urgency-bar"`
	// Dynamic color thresholds based on notification count.
	//
	// Entries are checked in order; the last matching entry wins for each
	// color slot. Use `above` for high-value warnings (e.g., many unread
	// notifications).
	//
	// ## Example
	//
	// ```toml
	// [[modules.notifications.thresholds]]
	// above = 5
	// icon-color = "status-warning"
	// label-color = "status-warning"
	//
	// [[modules.notifications.thresholds]]
	// above = 20
	// icon-color = "status-error"
	// label-color = "status-error"
	// ```
	Thresholds []ThresholdEntry `cfg:"thresholds"`
}

// DefaultsNotification returns the schema defaults.
func DefaultsNotification() NotificationConfig {
	return NotificationConfig{
		Enabled:            true,
		IconName:           "ld-bell-symbolic",
		IconUnread:         "ld-bell-dot-symbolic",
		IconDnd:            "ld-bell-off-symbolic",
		BorderShow:         false,
		BorderColor:        mustColor("green"),
		IconShow:           true,
		IconColor:          mustColor("auto"),
		IconBgColor:        mustColor("green"),
		LabelShow:          true,
		LabelColor:         mustColor("green"),
		LabelMaxLength:     0,
		ButtonBgColor:      mustColor("bg-surface-elevated"),
		LeftClick:          ParseClickAction("dropdown:notification"),
		RightClick:         ParseClickAction("wayle notify dnd"),
		MiddleClick:        ClickAction{},
		ScrollUp:           ClickAction{},
		ScrollDown:         ClickAction{},
		Blocklist:          []string{},
		IconSource:         IconSourceAutomatic,
		PopupPosition:      PopupPositionTopRight,
		PopupMaxVisible:    5,
		PopupStackingOrder: StackingOrderNewestFirst,
		PopupDuration:      5000,
		PopupHoverPause:    true,
		PopupMarginX:       Size{Value: 0, Unit: SizeMultiplier},
		PopupMarginY:       Size{Value: 0, Unit: SizeMultiplier},
		PopupGap:           Size{Value: 1, Unit: SizeMultiplier},
		PopupLayer:         LayerOverlay,
		PopupCloseBehavior: PopupCloseBehaviorDismiss,
		PopupShadow:        true,
		PopupUrgencyBar:    UrgencyBarThresholdLow,
		Thresholds:         []ThresholdEntry{},
	}
}

// Clicks returns the five input bindings.
func (c NotificationConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// PopupPosition is ported from crates/wayle-config/src/schemas/modules/notification/types.rs.
//
// Screen position for notification popups.
type PopupPosition string

// PopupPosition values.
const (
	// Top-left corner.
	PopupPositionTopLeft PopupPosition = "top-left"
	// Top-center edge.
	PopupPositionTopCenter PopupPosition = "top-center"
	// Top-right corner.
	PopupPositionTopRight PopupPosition = "top-right"
	// Bottom-left corner.
	PopupPositionBottomLeft PopupPosition = "bottom-left"
	// Bottom-center edge.
	PopupPositionBottomCenter PopupPosition = "bottom-center"
	// Bottom-right corner.
	PopupPositionBottomRight PopupPosition = "bottom-right"
	// Center-left edge.
	PopupPositionCenterLeft PopupPosition = "center-left"
	// Center-right edge.
	PopupPositionCenterRight PopupPosition = "center-right"
)

var _ = registerEnum(PopupPositionTopLeft, PopupPositionTopCenter, PopupPositionTopRight, PopupPositionBottomLeft, PopupPositionBottomCenter, PopupPositionBottomRight, PopupPositionCenterLeft, PopupPositionCenterRight)

// StackingOrder is ported from crates/wayle-config/src/schemas/modules/notification/types.rs.
//
// Order in which popups are stacked on screen.
type StackingOrder string

// StackingOrder values.
const (
	// Newest notifications appear closest to the configured position.
	StackingOrderNewestFirst StackingOrder = "newest-first"
	// Oldest notifications appear closest to the configured position.
	StackingOrderOldestFirst StackingOrder = "oldest-first"
)

var _ = registerEnum(StackingOrderNewestFirst, StackingOrderOldestFirst)

// PopupCloseBehavior is ported from crates/wayle-config/src/schemas/modules/notification/types.rs.
//
// Behavior when the close button is clicked on a popup card.
type PopupCloseBehavior string

// PopupCloseBehavior values.
const (
	// Hide the popup; notification stays in history.
	PopupCloseBehaviorDismiss PopupCloseBehavior = "dismiss"
	// Remove the notification entirely.
	PopupCloseBehaviorRemove PopupCloseBehavior = "remove"
)

var _ = registerEnum(PopupCloseBehaviorDismiss, PopupCloseBehaviorRemove)

// UrgencyBarThreshold is ported from crates/wayle-config/src/schemas/modules/notification/types.rs.
//
// Minimum urgency level that shows a colored urgency bar on popup cards.
//
// All urgency levels at or above the threshold display the bar.
// For example, `Normal` shows bars on both normal and critical popups.
type UrgencyBarThreshold string

// UrgencyBarThreshold values.
const (
	// Show urgency bars on all popups.
	UrgencyBarThresholdLow UrgencyBarThreshold = "low"
	// Show urgency bars on normal and critical popups.
	UrgencyBarThresholdNormal UrgencyBarThreshold = "normal"
	// Show urgency bars on critical popups only.
	UrgencyBarThresholdCritical UrgencyBarThreshold = "critical"
	// Never show urgency bars.
	UrgencyBarThresholdNone UrgencyBarThreshold = "none"
)

var _ = registerEnum(UrgencyBarThresholdLow, UrgencyBarThresholdNormal, UrgencyBarThresholdCritical, UrgencyBarThresholdNone)

// IconSource is ported from crates/wayle-config/src/schemas/modules/notification/types.rs.
//
// Source for resolving notification icons.
type IconSource string

// IconSource values.
const (
	// Use per-notification images when provided, otherwise Wayle's mapped icon.
	IconSourceAutomatic IconSource = "automatic"
	// Always use Wayle's mapped icons regardless of what the app provides.
	IconSourceMapped IconSource = "mapped"
	// Use the full application icon chain, falling back to mapped if unavailable.
	IconSourceApplication IconSource = "application"
)

var _ = registerEnum(IconSourceAutomatic, IconSourceMapped, IconSourceApplication)

// Popup spacing bases (in rem) the margin and gap multipliers scale.
const (
	PopupMarginBaseRem = 0.75
	PopupGapBaseRem    = 0.5
)

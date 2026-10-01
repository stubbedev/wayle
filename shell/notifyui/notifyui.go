// Package notifyui is what the notification popups and the bar's
// notification dropdown share (wayle-shell-core notification_icons.rs):
// icon resolution, urgency classes, relative times, and body text.
package notifyui

import (
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/service/notifications"
)

// FallbackIcon is the bell every unresolved notification shows.
const FallbackIcon = "ld-bell-symbolic"

// Icon is a resolved notification icon: a theme name or an image file
// (ResolvedIcon). Exactly one is set.
type Icon struct {
	Name string
	File string
}

// ResolveIcon is resolve_icon: Mapped is the curated icon for the app
// name; Automatic prefers the image; Application prefers the image,
// then the app icon, then the desktop entry; each falls back to Mapped,
// and Mapped to the bell.
func ResolveIcon(source config.IconSource, n *notifications.Notification) Icon {
	switch source {
	case config.IconSourceAutomatic:
		if icon, ok := iconString(n.ImagePath); ok {
			return icon
		}
	case config.IconSourceApplication:
		if icon, ok := iconString(n.ImagePath); ok {
			return icon
		}
		if icon, ok := iconString(n.AppIcon); ok {
			return icon
		}
		if n.DesktopEntry != "" {
			return Icon{Name: n.DesktopEntry}
		}
	}
	return mappedIcon(n.AppName)
}

// iconString is try_icon_string: an absolute path (file:// stripped) is
// a file, unless it is a -symbolic.svg, which resolves to its theme
// name so it recolors; anything else is a theme name.
func iconString(value string) (Icon, bool) {
	if value == "" {
		return Icon{}, false
	}
	path := strings.TrimPrefix(value, "file://")
	if !strings.HasPrefix(path, "/") {
		return Icon{Name: value}, true
	}
	if stem, ok := strings.CutSuffix(filepath.Base(path), ".svg"); ok && strings.HasSuffix(stem, "-symbolic") {
		return Icon{Name: stem}, true
	}
	return Icon{File: path}, true
}

func mappedIcon(appName string) Icon {
	if appName != "" {
		if name, ok := appicons.Lookup(appName); ok {
			return Icon{Name: name}
		}
	}
	return Icon{Name: FallbackIcon}
}

// UrgencyClass is urgency_css_class.
func UrgencyClass(u notifications.Urgency) string {
	switch u {
	case notifications.UrgencyLow:
		return "low"
	case notifications.UrgencyCritical:
		return "critical"
	}
	return "normal"
}

// UrgencyBarVisible is urgency_bar_visible.
func UrgencyBarVisible(u notifications.Urgency, threshold config.UrgencyBarThreshold) bool {
	switch threshold {
	case config.UrgencyBarThresholdLow:
		return true
	case config.UrgencyBarThresholdNormal:
		return u >= notifications.UrgencyNormal
	case config.UrgencyBarThresholdCritical:
		return u >= notifications.UrgencyCritical
	}
	return false
}

// Age is RelativeTime: under a minute is just now, under an hour is
// whole minutes, else whole hours.
type Age struct {
	Minutes int64 // set below an hour
	Hours   int64 // set from an hour
}

// JustNow reports the under-a-minute case.
func (a Age) JustNow() bool { return a.Minutes == 0 && a.Hours == 0 }

// RelativeTime is relative_time from ts to now.
func RelativeTime(now, ts time.Time) Age {
	d := now.Sub(ts)
	minutes := int64(d / time.Minute)
	switch {
	case minutes < 1:
		return Age{}
	case minutes < 60:
		return Age{Minutes: minutes}
	}
	return Age{Hours: int64(d / time.Hour)}
}

// BodyText is the text a markup body shows (sanitize_markup through a
// markup label): well-formed markup renders its character data, tags
// and entities resolved; anything that fails to parse shows literally.
func BodyText(body string) string {
	dec := xml.NewDecoder(strings.NewReader("<markup>" + body + "</markup>"))
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return b.String()
		}
		if err != nil {
			return body
		}
		if text, ok := tok.(xml.CharData); ok {
			b.Write(text)
		}
	}
}

// NewIcon builds the widget for a resolved icon at size pixels: a file
// image, or a theme icon tinted when its name is symbolic.
func NewIcon(icon Icon, size int, tint render.Color) *widget.Icon {
	if icon.File != "" {
		return widget.NewFileIcon(icon.File, size)
	}
	w := widget.NewThemeIcon(icon.Name, size)
	if strings.HasSuffix(icon.Name, "-symbolic") {
		w.SetTint(tint)
	}
	return w
}

// IsFileIcon reports whether an icon draws as a picture rather than a
// recolored glyph (an image, or a theme name that is not -symbolic):
// the card's icon box takes the file-icon class for it.
func IsFileIcon(icon Icon) bool {
	return icon.File != "" || !strings.HasSuffix(icon.Name, "-symbolic")
}

// VisibleActions are the actions a card draws as buttons: all but the
// default one, which the card itself invokes on a click.
func VisibleActions(n *notifications.Notification) []notifications.Action {
	var out []notifications.Action
	for _, a := range n.Actions {
		if a.ID != notifications.DefaultActionID {
			out = append(out, a)
		}
	}
	return out
}

// ActionsPerRow is how many action buttons share a row (MAX_PER_ROW).
const ActionsPerRow = 3

// TimeLabel is time_to_string / format_time_label in a surface's own
// strings: domain is "notification-popup" or "notification-dropdown",
// whose -time-just-now, -time-minutes-ago and -time-hours-ago messages
// it reads.
func TimeLabel(age Age, domain string) string {
	switch {
	case age.JustNow():
		return i18n.T(domain + "-time-just-now")
	case age.Hours > 0:
		return i18n.T(domain+"-time-hours-ago", i18n.Str("hours", strconv.FormatInt(age.Hours, 10)))
	}
	return i18n.T(domain+"-time-minutes-ago", i18n.Str("minutes", strconv.FormatInt(age.Minutes, 10)))
}

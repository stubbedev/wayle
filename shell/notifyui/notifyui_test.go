package notifyui

import (
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/notifications"
)

func TestIconString(t *testing.T) {
	for in, want := range map[string]Icon{
		"file:///usr/share/icon.png":           {File: "/usr/share/icon.png"},
		"/usr/share/icon.png":                  {File: "/usr/share/icon.png"},
		"firefox":                              {Name: "firefox"},
		"/opt/icons/si-gmail-symbolic.svg":     {Name: "si-gmail-symbolic"},
		"file:///x/tb-brand-edge-symbolic.svg": {Name: "tb-brand-edge-symbolic"},
		"/opt/icons/gmail.svg":                 {File: "/opt/icons/gmail.svg"},
		"/opt/icons/si-gmail-symbolic.png":     {File: "/opt/icons/si-gmail-symbolic.png"},
	} {
		got, ok := iconString(in)
		if !ok || got != want {
			t.Errorf("iconString(%q) = %+v %v, want %+v", in, got, ok, want)
		}
	}
	if _, ok := iconString(""); ok {
		t.Error("empty string resolved")
	}
}

func TestResolveIcon(t *testing.T) {
	full := &notifications.Notification{
		AppName: "firefox", AppIcon: "app-icon", ImagePath: "/img.png", DesktopEntry: "org.app",
	}
	mapped := Icon{Name: "si-firefox-symbolic"}
	if got := ResolveIcon(config.IconSourceMapped, full); got != mapped {
		t.Errorf("mapped = %+v", got)
	}
	if got := ResolveIcon(config.IconSourceAutomatic, full); got != (Icon{File: "/img.png"}) {
		t.Errorf("automatic = %+v", got)
	}
	if got := ResolveIcon(config.IconSourceApplication, full); got != (Icon{File: "/img.png"}) {
		t.Errorf("application = %+v", got)
	}
	noImage := *full
	noImage.ImagePath = ""
	if got := ResolveIcon(config.IconSourceAutomatic, &noImage); got != mapped {
		t.Errorf("automatic without image = %+v, want mapped", got)
	}
	if got := ResolveIcon(config.IconSourceApplication, &noImage); got != (Icon{Name: "app-icon"}) {
		t.Errorf("application without image = %+v, want the app icon", got)
	}
	noImage.AppIcon = ""
	if got := ResolveIcon(config.IconSourceApplication, &noImage); got != (Icon{Name: "org.app"}) {
		t.Errorf("application, desktop entry = %+v", got)
	}
	noImage.DesktopEntry = ""
	if got := ResolveIcon(config.IconSourceApplication, &noImage); got != mapped {
		t.Errorf("application, nothing = %+v, want mapped", got)
	}
	unknown := &notifications.Notification{AppName: "zzz-no-such-app"}
	if got := ResolveIcon(config.IconSourceMapped, unknown); got != (Icon{Name: FallbackIcon}) {
		t.Errorf("unknown app = %+v, want the bell", got)
	}
	if got := ResolveIcon(config.IconSourceMapped, &notifications.Notification{}); got != (Icon{Name: FallbackIcon}) {
		t.Errorf("no app = %+v, want the bell", got)
	}
}

func TestUrgency(t *testing.T) {
	low, normal, crit := notifications.UrgencyLow, notifications.UrgencyNormal, notifications.UrgencyCritical
	if UrgencyClass(low) != "low" || UrgencyClass(normal) != "normal" || UrgencyClass(crit) != "critical" {
		t.Error("urgency classes")
	}
	for _, tc := range []struct {
		th   config.UrgencyBarThreshold
		want [3]bool
	}{
		{config.UrgencyBarThresholdNone, [3]bool{false, false, false}},
		{config.UrgencyBarThresholdLow, [3]bool{true, true, true}},
		{config.UrgencyBarThresholdNormal, [3]bool{false, true, true}},
		{config.UrgencyBarThresholdCritical, [3]bool{false, false, true}},
	} {
		for i, u := range []notifications.Urgency{low, normal, crit} {
			if got := UrgencyBarVisible(u, tc.th); got != tc.want[i] {
				t.Errorf("UrgencyBarVisible(%v, %s) = %v", u, tc.th, got)
			}
		}
	}
}

func TestRelativeTime(t *testing.T) {
	now := time.Unix(100000, 0)
	if a := RelativeTime(now, now.Add(-30*time.Second)); !a.JustNow() {
		t.Errorf("30s = %+v", a)
	}
	if a := RelativeTime(now, now.Add(-30*time.Minute)); a.Minutes != 30 || a.JustNow() {
		t.Errorf("30m = %+v", a)
	}
	if a := RelativeTime(now, now.Add(-150*time.Minute)); a.Hours != 2 || a.Minutes != 0 {
		t.Errorf("2.5h = %+v", a)
	}
	// A timestamp from the future reads as just now.
	if a := RelativeTime(now, now.Add(time.Hour)); !a.JustNow() {
		t.Errorf("future = %+v", a)
	}
}

func TestBodyMarkup(t *testing.T) {
	for in, want := range map[string]string{
		// Well-formed markup passes through with its tags, the label
		// rendering it; anything else escapes to the literal characters.
		"<b>bold</b> and <i>italic</i>": "<b>bold</b> and <i>italic</i>",
		"a &amp; b &lt;c&gt;":           "a &amp; b &lt;c&gt;",
		"NixOS Package & Module":        "NixOS Package &amp; Module",
		"<b>unclosed":                   "&lt;b&gt;unclosed",
		"plain":                         "plain",
		"":                              "",
	} {
		if got := BodyMarkup(in); got != want {
			t.Errorf("BodyMarkup(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewIcon(t *testing.T) {
	tint := render.RGB(1, 2, 3)
	if w := NewIcon(Icon{Name: "ld-bell-symbolic"}, 16, tint); w.Name() != "ld-bell-symbolic" || w.Tint() != tint {
		t.Errorf("symbolic: name %q tint %v", w.Name(), w.Tint())
	}
	if w := NewIcon(Icon{Name: "firefox"}, 16, tint); w.Tint() != 0 {
		t.Error("a full-color theme icon was tinted")
	}
	if w := NewIcon(Icon{File: "/no/such.png"}, 16, tint); w.Name() != "" || w.Tint() != 0 {
		t.Errorf("file icon: name %q tint %v", w.Name(), w.Tint())
	}
}

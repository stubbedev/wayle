package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestCompositorWorkspacesDefaultsDifferOnlyInTrailingEmpty(t *testing.T) {
	sway, niri := DefaultsSwayWorkspaces(), DefaultsNiriWorkspaces()
	if sway.HideTrailingEmpty || !niri.HideTrailingEmpty {
		t.Fatalf("hide-trailing-empty: sway %v niri %v, want false/true", sway.HideTrailingEmpty, niri.HideTrailingEmpty)
	}
	niri.HideTrailingEmpty = false
	if !reflect.DeepEqual(sway, niri) {
		t.Fatalf("defaults differ beyond hide-trailing-empty:\n sway %+v\n niri %+v", sway, niri)
	}
	if sway.LabelStrategy != LabelNameOrIndex || sway.Click.LeftClick.Kind != WorkspaceClickFocusThis ||
		sway.Click.ScrollUp.Kind != WorkspaceClickFocusPrevious || sway.Click.ScrollDown.Kind != WorkspaceClickFocusNext ||
		sway.Click.MiddleClick.Kind != WorkspaceClickNone {
		t.Fatalf("schema defaults wrong: %+v", sway)
	}
}

func TestLoadFileAppliesNiriWorkspaces(t *testing.T) {
	path := writeConfig(t, `
[modules.niri-workspaces]
monitor-specific = false
hide-trailing-empty = false
display-mode = "icon"
label-strategy = "index-and-name"
urgent-mode = "application"
active-indicator = "underline"
divider = "|"
app-icons-show = true
app-icons-dedupe = false
icon-gap = 1
label-size = "1.5"
workspace-padding = "3px"
workspace-ignore = ["scratch*"]
active-color = "#ff0000"
workspace-map = { web = { label = "W", icon = "ld-globe-symbolic", color = "#00ff00", extra = "ignored" } }
app-icon-map = { "app:*firefox*" = "ld-globe-symbolic" }
left-click = "dropdown:calendar"
middle-click = "focus:last"
right-click = "notify-send hi"
scroll-up = ""
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	n := cfg.NiriWorkspaces
	if n.MonitorSpecific || n.HideTrailingEmpty || n.DisplayMode != DisplayModeIcon ||
		n.LabelStrategy != LabelIndexAndName || n.UrgentMode != UrgentApplication ||
		n.ActiveIndicator != ActiveUnderline || n.Divider != "|" || !n.AppIconsShow || n.AppIconsDedupe {
		t.Fatalf("scalars not applied: %+v", n)
	}
	if n.IconGap != (Size{Value: 1, Unit: SizeMultiplier}) {
		t.Errorf("icon-gap = %+v, want an integer scale", n.IconGap)
	}
	if n.LabelSize != (Size{Value: 1.5, Unit: SizeMultiplier}) {
		t.Errorf("label-size = %+v, want the bare-number string as a scale", n.LabelSize)
	}
	if n.WorkspacePad != (Size{Value: 3, Unit: SizePixels}) {
		t.Errorf("workspace-padding = %+v", n.WorkspacePad)
	}
	if len(n.WorkspaceIgnore) != 1 || n.WorkspaceIgnore[0] != "scratch*" {
		t.Errorf("workspace-ignore = %v", n.WorkspaceIgnore)
	}
	web, ok := n.WorkspaceMap["web"]
	if !ok || !web.LabelSet || web.Label != "W" || web.Icon != "ld-globe-symbolic" || !web.ColorSet {
		t.Errorf("workspace-map[web] = %+v", web)
	}
	if n.AppIconMap["app:*firefox*"] != "ld-globe-symbolic" {
		t.Errorf("app-icon-map = %v", n.AppIconMap)
	}
	if n.Click.LeftClick != (WorkspaceClickAction{Kind: WorkspaceClickDropdown, Arg: "calendar"}) ||
		n.Click.MiddleClick.Kind != WorkspaceClickFocusLast ||
		n.Click.RightClick != (WorkspaceClickAction{Kind: WorkspaceClickShell, Arg: "notify-send hi"}) ||
		n.Click.ScrollUp.Kind != WorkspaceClickNone ||
		n.Click.ScrollDown.Kind != WorkspaceClickFocusNext {
		t.Errorf("clicks = %+v", n.Click)
	}
	// The niri table leaves sway (and hyprland) at their defaults.
	if !reflect.DeepEqual(cfg.SwayWorkspaces, DefaultsSwayWorkspaces()) {
		t.Errorf("niri config leaked into sway: %+v", cfg.SwayWorkspaces)
	}
}

func TestLoadFileSwayWorkspacesDoesNotTouchHyprland(t *testing.T) {
	path := writeConfig(t, "[modules.sway-workspaces]\nmonitor-specific = false\nlabel-strategy = \"index\"\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.SwayWorkspaces.MonitorSpecific || cfg.SwayWorkspaces.LabelStrategy != LabelIndex {
		t.Errorf("sway config not applied: %+v", cfg.SwayWorkspaces)
	}
	if !cfg.HyprlandWorkspaces.Shared.MonitorSpecific {
		t.Error("sway-workspaces overwrote hyprland-workspaces")
	}
}

func TestLoadFileRejectsBadCompositorWorkspaces(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`label-strategy = "names"`, "label-strategy"},
		{`urgent-mode = "loud"`, "urgent-mode"},
		{`display-mode = "text"`, "display-mode"},
		{`active-indicator = "glow"`, "active-indicator"},
		{`active-color = "not-a-color"`, "active-color"},
		{`label-size = "huge"`, "label-size"},
		{`workspace-map = { web = { color = 5 } }`, "workspace-map"},
	} {
		for _, module := range []string{"sway-workspaces", "niri-workspaces"} {
			path := writeConfig(t, "[modules."+module+"]\n"+tc.body+"\n")
			_, err := LoadFile(path)
			if err == nil || !strings.Contains(err.Error(), module) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s %s: err = %v, want a %s error naming the module", module, tc.body, err, tc.want)
			}
		}
	}
}

func TestParseWorkspaceClickActionRoundTrips(t *testing.T) {
	for _, raw := range []string{"", "focus:this", "focus:next", "focus:previous", "focus:last", "dropdown:audio", "wpctl set-mute @DEFAULT_SINK@ toggle"} {
		if got := ParseWorkspaceClickAction(raw).String(); got != raw {
			t.Errorf("%q round-trips to %q", raw, got)
		}
	}
	if got := ParseWorkspaceClickAction("focus:nowhere"); got.Kind != WorkspaceClickShell {
		t.Errorf("unknown focus target = %+v, want a shell command like the Rust from_str", got)
	}
}

func TestMangoWorkspacesDefaults(t *testing.T) {
	m := DefaultsMangoWorkspaces()
	if !m.HideEmpty || m.MinTagCount != 0 {
		t.Fatalf("tag rules = %+v", m)
	}
	if m.Shared.WorkspacePad != (Size{Value: 0.5, Unit: SizeMultiplier}) || m.Shared.Click != DefaultWorkspaceClicks() {
		t.Fatalf("shared = %+v", m.Shared)
	}
}

func TestLoadFileAppliesMangoWorkspaces(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[modules.mango-workspaces]
hide-empty = false
min-tag-count = 5
tag-padding = "2px"
display-mode = "icon"
tag-map = { "3" = { icon = "ld-globe-symbolic", label = "web" } }
right-click = "focus:last"
`))
	if err != nil {
		t.Fatal(err)
	}
	m := cfg.MangoWorkspaces
	if m.HideEmpty || m.MinTagCount != 5 || m.Shared.WorkspacePad != (Size{Value: 2, Unit: SizePixels}) ||
		m.Shared.DisplayMode != DisplayModeIcon || m.Shared.WorkspaceMap["3"].Icon != "ld-globe-symbolic" ||
		m.Shared.Click.RightClick.Kind != WorkspaceClickFocusLast {
		t.Fatalf("mango = %+v", m)
	}
}

func TestLoadFileRejectsBadMangoWorkspaces(t *testing.T) {
	for _, body := range []string{"min-tag-count = 300", "min-tag-count = -1", `urgent-mode = "x"`, `tag-padding = "wide"`, `tag-map = { "1" = { color = "nope" } }`} {
		_, err := LoadFile(writeConfig(t, "[modules.mango-workspaces]\n"+body+"\n"))
		if err == nil || !strings.Contains(err.Error(), "mango-workspaces") {
			t.Errorf("%s: err = %v", body, err)
		}
	}
}

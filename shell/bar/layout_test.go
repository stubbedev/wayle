package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestFindLayoutExactMonitorBeatsStar(t *testing.T) {
	layouts := []config.BarLayout{
		{Monitor: "*", Center: []config.BarItem{{Module: "clock"}}},
		{Monitor: "DP-1", Center: []config.BarItem{{Module: "battery"}}},
	}
	layout, ok := FindLayout(layouts, "DP-1")
	if !ok {
		t.Fatal("FindLayout DP-1: want a layout, got none")
	}
	if len(layout.Center) != 1 || layout.Center[0].Module != "battery" {
		t.Fatalf("DP-1 center = %+v, want battery (exact match wins over *)", layout.Center)
	}
}

func TestFindLayoutStarFallback(t *testing.T) {
	layouts := []config.BarLayout{{Monitor: "*", Center: []config.BarItem{{Module: "clock"}}}}
	layout, ok := FindLayout(layouts, "HDMI-A-2")
	if !ok {
		t.Fatal("FindLayout HDMI-A-2: want the * layout, got none")
	}
	if len(layout.Center) != 1 || layout.Center[0].Module != "clock" {
		t.Fatalf("fallback center = %+v, want clock", layout.Center)
	}
}

func TestFindLayoutUnmatchedMonitorGetsNoBar(t *testing.T) {
	layouts := []config.BarLayout{{Monitor: "DP-1", Center: []config.BarItem{{Module: "clock"}}}}
	if _, ok := FindLayout(layouts, "DP-9"); ok {
		t.Fatal("FindLayout DP-9: want no layout, got one")
	}
}

func TestFindLayoutInheritsEmptySectionsFromExtends(t *testing.T) {
	layouts := []config.BarLayout{
		{Monitor: "DP-1", Extends: new("*"), Left: []config.BarItem{{Module: "battery"}}},
		{Monitor: "*", Center: []config.BarItem{{Module: "clock"}}, Right: []config.BarItem{{Module: "power"}}},
	}
	layout, ok := FindLayout(layouts, "DP-1")
	if !ok {
		t.Fatal("FindLayout DP-1: want a layout, got none")
	}
	if len(layout.Left) != 1 || layout.Left[0].Module != "battery" {
		t.Errorf("left = %+v, want DP-1's own battery", layout.Left)
	}
	if len(layout.Center) != 1 || layout.Center[0].Module != "clock" {
		t.Errorf("center = %+v, want inherited clock", layout.Center)
	}
	if len(layout.Right) != 1 || layout.Right[0].Module != "power" {
		t.Errorf("right = %+v, want inherited power", layout.Right)
	}
}

func TestFindLayoutExtendsKeepsOwnNonEmptySections(t *testing.T) {
	layouts := []config.BarLayout{
		{Monitor: "DP-1", Extends: new("*"), Left: []config.BarItem{{Module: "battery"}}, Right: []config.BarItem{{Module: "volume"}}},
		{Monitor: "*", Center: []config.BarItem{{Module: "clock"}}, Right: []config.BarItem{{Module: "power"}}},
	}
	layout, _ := FindLayout(layouts, "DP-1")
	if len(layout.Right) != 1 || layout.Right[0].Module != "volume" {
		t.Errorf("right = %+v, want DP-1's own volume (own sections are never overwritten)", layout.Right)
	}
}

func TestFindLayoutCircularExtendsTerminates(t *testing.T) {
	layouts := []config.BarLayout{
		{Monitor: "DP-1", Extends: new("DP-2"), Left: []config.BarItem{{Module: "battery"}}},
		{Monitor: "DP-2", Extends: new("DP-1"), Center: []config.BarItem{{Module: "clock"}}},
	}
	layout, ok := FindLayout(layouts, "DP-1")
	if !ok {
		t.Fatal("circular extends: want a layout, got none")
	}
	if len(layout.Left) != 1 {
		t.Errorf("left = %+v, want DP-1's own battery", layout.Left)
	}
	if len(layout.Center) != 1 || layout.Center[0].Module != "clock" {
		t.Errorf("center = %+v, want clock inherited from DP-2", layout.Center)
	}
}

func TestFindLayoutMissingExtendsParentIsASkip(t *testing.T) {
	layouts := []config.BarLayout{{Monitor: "DP-1", Extends: new("ghost"), Left: []config.BarItem{{Module: "battery"}}}}
	layout, ok := FindLayout(layouts, "DP-1")
	if !ok {
		t.Fatal("missing extends parent: want a layout, got none")
	}
	if len(layout.Left) != 1 || len(layout.Center) != 0 {
		t.Errorf("layout = %+v, want own battery and nothing else", layout)
	}
}

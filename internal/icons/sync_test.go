package icons

import (
	"maps"
	"slices"
	"testing"
)

func iconsIn(text string) []string {
	found := map[string]bool{}
	ScanStrings(text, found)
	return slices.Sorted(maps.Keys(found))
}

func TestScanStrings(t *testing.T) {
	for text, want := range map[string][]string{
		"use ld-bell-symbolic and tb-alert-triangle-symbolic together": {"ld-bell-symbolic", "tb-alert-triangle-symbolic"},
		"ld-alert-triangle-symbolic":                                   {"ld-alert-triangle-symbolic"},
		"tbf-circle-symbolic":                                          {"tbf-circle-symbolic"},
		"cm-offline-symbolic,si-gmail-symbolic;md-home_app-symbolic":   {"cm-offline-symbolic", "md-home_app-symbolic", "si-gmail-symbolic"},
		"xy-foo-symbolic":                                              nil, // unknown prefix
		"ld-bell ld-home":                                              nil, // no -symbolic
		"ld--symbolic":                                                 nil, // empty slug
		"ld-symbolic":                                                  nil,
		"foo.ld-bell-symbolic.svg":                                     {"ld-bell-symbolic"},
	} {
		if got := iconsIn(text); !slices.Equal(got, want) {
			t.Errorf("ScanStrings(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestScanValueWalksNestedTables(t *testing.T) {
	v := map[string]any{
		"modules": map[string]any{
			"notification": map[string]any{"icon-name": "ld-bell-symbolic", "count": int64(3)},
			"power":        map[string]any{"icon-name": "ld-power-symbolic"},
		},
		"bar": map[string]any{"layout": []any{map[string]any{"right": []any{"tb-x-symbolic", true}}}},
	}
	found := map[string]bool{}
	ScanValue(v, found)
	if got, want := slices.Sorted(maps.Keys(found)), []string{"ld-bell-symbolic", "ld-power-symbolic", "tb-x-symbolic"}; !slices.Equal(got, want) {
		t.Errorf("ScanValue = %v, want %v", got, want)
	}
}

func TestFindMissing(t *testing.T) {
	referenced := map[string]bool{
		"ld-bell-symbolic": true, "tb-alert-triangle-symbolic": true, "ld-power-symbolic": true,
		"cm-offline-symbolic": true, "ld-arrow-left-symbolic": true,
		"xy-foo-symbolic": true, "ld-bell": true,
	}
	installed := map[string]bool{"ld-bell-symbolic": true}
	got := FindMissing(referenced, installed)
	want := []MissingIcon{
		{Name: "cm-offline-symbolic", Slug: "offline"}, // user-imported: no source
		{Name: "ld-arrow-left-symbolic", Source: "lucide", Slug: "arrow-left"},
		{Name: "ld-power-symbolic", Source: "lucide", Slug: "power"},
		{Name: "tb-alert-triangle-symbolic", Source: "tabler", Slug: "alert-triangle"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("FindMissing =\n%+v\nwant\n%+v", got, want)
	}
}

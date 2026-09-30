package main

import "testing"

func TestParseScreenshot(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		mode, target string
	}{
		{[]string{"region"}, "region", ""},
		{[]string{"window"}, "window", ""},
		{[]string{"output"}, "output", ""},
		{[]string{"output", "DP-1"}, "output", "DP-1"},
	} {
		mode, target, err := parseScreenshot(tc.args)
		if err != nil || mode != tc.mode || target != tc.target {
			t.Errorf("%v = %q %q %v", tc.args, mode, target, err)
		}
	}
	for _, bad := range [][]string{
		nil,
		{"screen"}, // the daemon's composite mode is not a CLI subcommand
		{"region", "extra"},
		{"output", "DP-1", "DP-2"},
		{"selfie"},
	} {
		if _, _, err := parseScreenshot(bad); err == nil {
			t.Errorf("%v parsed", bad)
		}
	}
}

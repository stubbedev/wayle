package main

import (
	"strings"
	"testing"

	"github.com/stubbedev/wayle/internal/dbustest"
)

func TestScreenshotSubcommandsReachTheService(t *testing.T) {
	bus := dbustest.Start(t)
	bus.UseAsSessionBus(t)
	for _, args := range [][]string{{"region"}, {"window"}, {"output"}, {"output", "DP-1"}} {
		_, stderr, code := runCaptured(t, false, append([]string{"screenshot"}, args...)...)
		if code != 1 || !strings.Contains(stderr, "Screenshot service not running. Start wayle shell first.") {
			t.Errorf("screenshot %v: code %d stderr %q, want the not-running error from the bus", args, code, stderr)
		}
	}
}

func TestScreenshotRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"screen"}, // the daemon's composite mode is not a CLI subcommand
		{"region", "extra"},
		{"output", "DP-1", "DP-2"},
		{"selfie"},
	} {
		if _, _, code := runCaptured(t, false, append([]string{"screenshot"}, args...)...); code != 2 {
			t.Errorf("screenshot %v exited %d, want the usage error 2", args, code)
		}
	}
}

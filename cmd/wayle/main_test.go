package main

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points every per-user location at a scratch home so no
// test (or a daemon it starts, such as the notification service's
// persisted do-not-disturb flag) touches the real user's files.
func TestMain(m *testing.M) {
	// The launcher session tests re-exec the test binary as the CLI.
	if os.Getenv("WAYLE_TEST_CLI") == "1" {
		main()
		os.Exit(0)
	}
	home, err := os.MkdirTemp("", "wayle-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for key, dir := range map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": home + "/config",
		"XDG_DATA_HOME":   home + "/data",
		"XDG_STATE_HOME":  home + "/state",
		"XDG_CACHE_HOME":  home + "/cache",
		"XDG_RUNTIME_DIR": home + "/run",
	} {
		_ = os.Setenv(key, dir)
	}
	_ = os.MkdirAll(home+"/run", 0o700)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

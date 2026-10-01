package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	return filepath.Join(home, "wayle")
}

// runConfigOut runs `wayle config args...` through the command tree;
// a failure comes back as the handler's error text.
func runConfigOut(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout, stderr, code := runCaptured(t, false, append([]string{"config"}, args...)...)
	if code != 0 {
		return stdout, errors.New(strings.TrimSuffix(strings.TrimPrefix(stderr, "Error: "), "\n"))
	}
	return stdout, nil
}

func TestConfigSetGetReset(t *testing.T) {
	dir := configDir(t)
	out, err := runConfigOut(t, "set", "bar.location", "bottom")
	if err != nil || out != "Set bar.location = \"bottom\"\n" {
		t.Fatalf("set: %q %v", out, err)
	}
	runtime, _ := os.ReadFile(filepath.Join(dir, "runtime.toml"))
	if string(runtime) != "[bar]\nlocation = \"bottom\"\n" {
		t.Errorf("runtime.toml = %q", runtime)
	}
	if out, _ := runConfigOut(t, "get", "bar.location"); out != "bottom\n" {
		t.Errorf("get = %q", out)
	}
	if out, _ := runConfigOut(t, "get", "bar.border-width"); out != "1\n" {
		t.Errorf("get int = %q", out)
	}
	if out, err := runConfigOut(t, "reset", "bar.location"); err != nil || out != "Reset bar.location (now using: \"top\")\n" {
		t.Errorf("reset: %q %v", out, err)
	}
	if out, _ := runConfigOut(t, "reset", "bar.location"); out != "No runtime override at bar.location\n" {
		t.Errorf("second reset = %q", out)
	}
}

func TestConfigSetAndGetFailures(t *testing.T) {
	configDir(t)
	if _, err := runConfigOut(t, "set", "bar.location", "sideways"); err == nil ||
		!strings.HasPrefix(err.Error(), "Failed to set config at 'bar.location': invalid value for 'bar.location': unknown variant `sideways`") {
		t.Errorf("bad value: %v", err)
	}
	if _, err := runConfigOut(t, "set", "bar.nope", "1"); err == nil ||
		err.Error() != "Failed to read back value: invalid config field 'nope' in bar.nope: field not found" {
		t.Errorf("unknown path: %v", err)
	}
	if _, err := runConfigOut(t, "get", "nope"); err == nil ||
		err.Error() != "Failed to get config at 'nope': invalid config field 'nope' in nope: field not found" {
		t.Errorf("get unknown: %v", err)
	}
	if _, err := runConfigOut(t, "reset", "bar"); err == nil || err.Error() != "cannot reset 'bar': empty path" {
		t.Errorf("reset section: %v", err)
	}
	if _, err := runConfigOut(t, "frobnicate"); err == nil {
		t.Error("unknown command: want an error")
	}
}

func TestConfigSchemaAndDefault(t *testing.T) {
	dir := configDir(t)
	out, err := runConfigOut(t, "schema", "--stdout")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal([]byte(out), &schema); err != nil || schema["title"] != "Config" {
		t.Fatalf("schema --stdout: %v %v", err, schema["title"])
	}
	out, err = runConfigOut(t, "schema")
	want := "Written:\n  " + filepath.Join(dir, "schema.json") + "\n  " + filepath.Join(dir, "tombi.toml") + "\n"
	if err != nil || out != want {
		t.Errorf("schema: %q %v", out, err)
	}
	if tombi, _ := os.ReadFile(filepath.Join(dir, "tombi.toml")); !strings.Contains(string(tombi), `include = ["config.toml", "runtime.toml"]`) {
		t.Errorf("tombi.toml = %q", tombi)
	}
	out, err = runConfigOut(t, "default")
	if err != nil || out != "Written:\n  "+filepath.Join(dir, "config.toml.example")+"\n" {
		t.Errorf("default: %q %v", out, err)
	}
	example, _ := os.ReadFile(filepath.Join(dir, "config.toml.example"))
	if !strings.HasPrefix(string(example), "imports = []\n") {
		t.Errorf("example = %q", example[:min(len(example), 40)])
	}
	if out, _ := runConfigOut(t, "default", "--stdout"); out != string(example)+"\n" {
		t.Error("default --stdout differs from the written example")
	}
}

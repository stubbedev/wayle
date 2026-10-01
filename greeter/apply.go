package greeter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/stubbedev/wayle/config"
)

// allowedKeys are the [greeter] keys wayle-settings may push to the
// system config (apply.rs ALLOWED_KEYS).
var allowedKeys = map[string]bool{
	"background-mode": true, "background-image": true, "background-color": true,
	"show-clock": true, "clock-format": true, "date-format": true,
	"show-user-list": true, "show-power-buttons": true,
	"cursor-theme": true, "cursor-size": true,
}

// RunApplyConfig is `wayle-greeter apply-config [--config PATH]
// <STAGED.toml>`, run as root through pkexec by wayle-settings: it
// copies only the allowlisted [greeter] keys of the staged file into
// runtime.toml beside the system config, never touching config.toml.
// It returns the exit code and writes its messages to stdout/stderr.
func RunApplyConfig(args []string, stdout, stderr io.Writer) int {
	configPath := config.GreeterConfigPath
	staged := ""
	for rest := args; len(rest) > 0; {
		arg := rest[0]
		rest = rest[1:]
		switch arg {
		case "--config":
			if len(rest) == 0 {
				return applyFail(stderr, "--config requires a path")
			}
			configPath, rest = rest[0], rest[1:]
		default:
			if staged != "" {
				return applyFail(stderr, "unexpected extra argument")
			}
			staged = arg
		}
	}
	if staged == "" {
		return applyFail(stderr, "usage: wayle-greeter apply-config [--config PATH] <STAGED.toml>")
	}
	dest, err := applyConfig(staged, configPath, asPkexecCaller)
	if err != nil {
		return applyFail(stderr, err.Error())
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s\n", dest)
	return 0
}

func applyFail(stderr io.Writer, msg string) int {
	_, _ = fmt.Fprintf(stderr, "wayle-greeter apply-config: %s\n", msg)
	return 1
}

// applyConfig merges the staged greeter keys into runtime.toml and
// returns its path. readAsCaller performs the one privileged read a
// user controls - the background image - with the caller's rights.
func applyConfig(staged, configPath string, readAsCaller func(func() ([]byte, error)) ([]byte, error)) (string, error) {
	text, err := os.ReadFile(staged) //nolint:gosec // the staged file wayle-settings names
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", staged, err)
	}
	var table map[string]any
	if _, err := toml.Decode(string(text), &table); err != nil {
		return "", fmt.Errorf("invalid TOML: %w", err)
	}
	// Accept a [greeter] table or the bare keys at top level.
	source := table
	if inner, ok := table["greeter"].(map[string]any); ok {
		source = inner
	}
	greeter := map[string]any{}
	for k, v := range source {
		if allowedKeys[k] {
			greeter[k] = v
		}
	}
	if len(greeter) == 0 {
		return "", errors.New("no recognised greeter keys in staged file")
	}
	if err := relocateBackgroundImage(greeter, configPath, readAsCaller); err != nil {
		return "", err
	}

	dest := config.RuntimeOverlayPath(configPath)
	root := map[string]any{}
	if existing, err := os.ReadFile(dest); err == nil { //nolint:gosec // the system runtime overlay
		if _, err := toml.Decode(string(existing), &root); err != nil {
			return "", fmt.Errorf("existing %s is invalid: %w", dest, err)
		}
	}
	root["greeter"] = greeter
	var out bytes.Buffer
	if err := toml.NewEncoder(&out).Encode(root); err != nil {
		return "", fmt.Errorf("serialize failed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil { //nolint:gosec // /etc/wayle is world-readable config
		return "", fmt.Errorf("cannot create %s: %w", filepath.Dir(dest), err)
	}
	if err := os.WriteFile(dest, out.Bytes(), 0o644); err != nil { //nolint:gosec // the greeter user reads it
		return "", fmt.Errorf("cannot write %s: %w", dest, err)
	}
	return dest, nil
}

// relocateBackgroundImage copies an image-mode background next to the
// system config and points the key there: the greeter runs as the
// unprivileged greeter user and cannot read the picking user's home.
// Other modes leave the key alone. The read runs with the pkexec
// caller's rights, so a caller cannot point background-image at a
// root-only file (/etc/shadow) and get a world-readable copy.
func relocateBackgroundImage(greeter map[string]any, configPath string, readAsCaller func(func() ([]byte, error)) ([]byte, error)) error {
	if mode, _ := greeter["background-mode"].(string); mode != "image" {
		return nil
	}
	src, _ := greeter["background-image"].(string)
	if src == "" {
		return nil
	}
	data, err := readAsCaller(func() ([]byte, error) { return os.ReadFile(src) }) //nolint:gosec // read with the caller's rights
	if err != nil {
		return fmt.Errorf("cannot read background image %s: %w", src, err)
	}
	ext := filepath.Ext(src)
	if ext == "" {
		ext = ".img"
	}
	dest := filepath.Join(filepath.Dir(configPath), "greeter-background"+ext)
	if err := os.WriteFile(dest, data, 0o644); err != nil { //nolint:gosec // world-readable so the greeter user can load it
		return fmt.Errorf("cannot write %s: %w", dest, err)
	}
	_ = os.Chmod(dest, 0o644) //nolint:gosec // world-readable on purpose
	greeter["background-image"] = dest
	return nil
}

// asPkexecCaller runs read with the effective uid dropped to
// PKEXEC_UID, restoring root after; unchanged when PKEXEC_UID is unset
// (invoked directly as root). Go's Seteuid applies to every thread.
func asPkexecCaller(read func() ([]byte, error)) ([]byte, error) {
	uid, err := strconv.Atoi(os.Getenv("PKEXEC_UID"))
	if err != nil {
		return read()
	}
	if err := syscall.Seteuid(uid); err != nil {
		return nil, fmt.Errorf("drop to caller uid %d: %w", uid, err)
	}
	data, readErr := read()
	if err := syscall.Seteuid(0); err != nil {
		return nil, fmt.Errorf("restore root: %w", err)
	}
	return data, readErr
}

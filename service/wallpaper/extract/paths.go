package extract

import (
	"errors"
	"os"
	"path/filepath"
)

// errNoHome is the lookup failure when neither the XDG variable nor
// HOME is set.
var errNoHome = errors.New("neither the XDG base directory nor HOME is set")

// xdgBase resolves an XDG base directory: $env, else $HOME/fallback.
func xdgBase(env, fallback string) (string, error) {
	if dir := os.Getenv(env); dir != "" {
		return dir, nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return "", errNoHome
	}
	return filepath.Join(home, fallback), nil
}

// wayleDir resolves $base/wayle and creates it when absent, as
// ConfigPaths::cache_dir / data_dir do.
func wayleDir(env, fallback string) (string, error) {
	base, err := xdgBase(env, fallback)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "wayle")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // matches create_dir_all's default mode
		return "", err
	}
	return dir, nil
}

// cacheDir is $XDG_CACHE_HOME/wayle (~/.cache/wayle), created on demand.
func cacheDir() (string, error) { return wayleDir("XDG_CACHE_HOME", ".cache") }

// dataDir is $XDG_DATA_HOME/wayle (~/.local/share/wayle), created on demand.
func dataDir() (string, error) { return wayleDir("XDG_DATA_HOME", ".local/share") }

// MatugenColorsPath is where wayle saves matugen's JSON output
// ($XDG_CACHE_HOME/wayle/matugen-colors.json); the wayle cache dir is
// created on demand.
func MatugenColorsPath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "matugen-colors.json"), nil
}

// WallustColorsPath is the wallust template's target
// ($XDG_CACHE_HOME/wayle/wallust-colors.json); the wayle cache dir is
// created on demand.
func WallustColorsPath() (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "wallust-colors.json"), nil
}

// PywalColorsPath is pywal's own output ($XDG_CACHE_HOME/wal/colors.json);
// nothing is created, pywal owns that directory.
func PywalColorsPath() (string, error) {
	base, err := xdgBase("XDG_CACHE_HOME", ".cache")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "wal", "colors.json"), nil
}

// ColorsPath is the cache file the given tool leaves its palette in;
// false for None.
func ColorsPath(t Tool) (string, bool, error) {
	var (
		path string
		err  error
	)
	switch t {
	case Matugen:
		path, err = MatugenColorsPath()
	case Wallust:
		path, err = WallustColorsPath()
	case Pywal:
		path, err = PywalColorsPath()
	default:
		return "", false, nil
	}
	return path, err == nil, err
}

package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Secrets from .env files (crates/wayle-config/src/infrastructure/
// secrets.rs): `.env` and `.*.env` in the config directory load into
// the environment at startup and on change, and a config value of
// "$NAME" resolves to that variable. Like dotenvy::from_path, a
// variable already set in the environment is never overridden.

// isEnvFile reports whether path names a secrets file (".env",
// ".api.env", ...).
func isEnvFile(path string) bool {
	name := filepath.Base(path)
	return strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".env")
}

func envFiles(dir string) []string {
	var files []string
	if st, err := os.Stat(filepath.Join(dir, ".env")); err == nil && st.Mode().IsRegular() {
		files = append(files, filepath.Join(dir, ".env"))
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".*.env"))
	files = append(files, matches...)
	sort.Strings(files)
	return files
}

// LoadEnvFiles loads the config directory's .env files.
func LoadEnvFiles(dir string) { loadEnvFiles(dir, false) }

// ReloadEnvFiles is LoadEnvFiles after a change.
func ReloadEnvFiles(dir string) { loadEnvFiles(dir, true) }

func loadEnvFiles(dir string, reload bool) {
	files := envFiles(dir)
	for _, path := range files {
		if err := loadEnvFile(path); err != nil {
			log.Printf("config: cannot load env file %s: %v", path, err)
		}
	}
	if reload && len(files) > 0 {
		log.Printf("config: secrets reloaded (%d files)", len(files))
	}
}

func loadEnvFile(path string) error {
	f, err := os.Open(path) //nolint:gosec // a .env file in the config dir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		key, value, ok := parseEnvLine(scan.Text())
		if !ok {
			continue
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scan.Err()
}

// parseEnvLine reads KEY=VALUE, optionally `export`-prefixed, with a
// single- or double-quoted value or a bare one ending at a comment.
func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	switch {
	case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
		value = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(value[1 : len(value)-1])
	case len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'':
		value = value[1 : len(value)-1]
	default:
		if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
	}
	return key, value, key != ""
}

// ResolveSecret resolves a "$NAME" config value from the environment;
// any other value is literal. An unset variable is (_, false) and
// logs, as secrets::resolve warns.
func ResolveSecret(value string) (string, bool) {
	name, ref := strings.CutPrefix(value, "$")
	if !ref {
		return value, true
	}
	resolved, ok := os.LookupEnv(name)
	if !ok {
		log.Printf("config: environment variable %s not set", name)
		return "", false
	}
	return resolved, true
}

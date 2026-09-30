package styling

import (
	"log"
	"os"
	"path/filepath"

	"github.com/stubbedev/wayle/internal/scss"
)

// userStylesTemplate is the starter index.scss, byte for byte the one
// ensure_user_styles_scaffold writes.
const userStylesTemplate = "// Custom Wayle styles. Anything here overrides the built-in styling.\n" +
	"// Use @import \"name\" to bring in _name.scss from this folder.\n"

// UserCSS compiles the user's optional overrides at
// <configDir>/styles/index.scss: lib.rs's user_css. An absent entry
// file is no overrides; a read or compile failure is logged and yields
// "" too, so a broken user stylesheet leaves the rest of the bundle
// intact.
func UserCSS(configDir string) string {
	css, err := TryUserCSS(configDir)
	if err != nil {
		log.Printf("styling: user styles compilation failed: %v", err)
		return ""
	}
	return css
}

// TryUserCSS is UserCSS surfacing the failure: lib.rs's try_user_css.
// ("", nil) means the entry file does not exist. <configDir>/styles/ is
// the load path, so index.scss can @import or @use sibling partials.
func TryUserCSS(configDir string) (string, error) {
	stylesDir := filepath.Join(configDir, "styles")
	entry := filepath.Join(stylesDir, "index.scss")
	if _, err := os.Stat(entry); err != nil {
		// Path::exists: any stat failure reads as absent.
		return "", nil
	}
	return scss.Compile(entry, stylesDir)
}

// UserStylesDir returns <configDir>/styles/ when it is a directory:
// lib.rs's user_styles_dir.
func UserStylesDir(configDir string) (string, bool) {
	dir := filepath.Join(configDir, "styles")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, true
	}
	return "", false
}

// EnsureUserStylesScaffold creates <configDir>/styles/ and a starter
// index.scss when either is missing: lib.rs's
// ensure_user_styles_scaffold. It is a no-op once both exist, and never
// overwrites the user's file; failures are logged, not returned, so
// scaffolding never blocks startup.
func EnsureUserStylesScaffold(configDir string) {
	stylesDir := filepath.Join(configDir, "styles")
	if err := os.MkdirAll(stylesDir, 0o755); err != nil { //nolint:gosec // the config dir is the user's own, as in the Rust shell
		log.Printf("styling: cannot create user styles directory %s: %v", stylesDir, err)
		return
	}
	entry := filepath.Join(stylesDir, "index.scss")
	if _, err := os.Stat(entry); err == nil {
		return
	}
	if err := os.WriteFile(entry, []byte(userStylesTemplate), 0o644); err != nil { //nolint:gosec // a user-editable stylesheet, readable like the rest of the config
		log.Printf("styling: cannot create user styles/index.scss %s: %v", entry, err)
	}
}

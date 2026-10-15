package greeter

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/atomicfile"
)

// embeddedIcons are the glyphs the greeter shows (the power buttons,
// the session picker's arrow and checkmark): pre-login there is no
// icon theme to rely on.
//
//go:embed assets/*.svg
var embeddedIcons embed.FS

// installIcons is install_icons: the embedded glyphs written to
// $XDG_RUNTIME_DIR/wayle-greeter-icons (the temp dir without one) and
// that directory put first on the icon search path, so the widgets load
// them by name and recolor them as symbolic.
func installIcons() error {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "wayle-greeter-icons")
	if err := writeIcons(dir); err != nil {
		return err
	}
	widget.SetIconSearchPaths(append([]string{dir}, widget.IconSearchPaths()...))
	widget.RefreshIcons()
	return nil
}

// writeIcons materializes the embedded glyphs into dir.
func writeIcons(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // the session runtime dir, as install_icons uses it
		return fmt.Errorf("greeter: icon dir %s: %w", dir, err)
	}
	entries, err := embeddedIcons.ReadDir("assets")
	if err != nil {
		return fmt.Errorf("greeter: embedded icons: %w", err)
	}
	for _, e := range entries {
		data, err := embeddedIcons.ReadFile("assets/" + e.Name())
		if err != nil {
			return fmt.Errorf("greeter: embedded icon %s: %w", e.Name(), err)
		}
		if err := atomicfile.Write(filepath.Join(dir, e.Name()), data, 0o600); err != nil {
			return fmt.Errorf("greeter: icon %s: %w", e.Name(), err)
		}
	}
	return nil
}

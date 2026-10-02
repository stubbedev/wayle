// Package apptheme is what every wayle GTK-era surface loads at start
// (bootstrap.rs, wayle-settings app/css.rs): the Rust stylesheet
// bundle with its hot reload, the stylesheet font resolver, and the
// wayle icon registry on the icon search path.
package apptheme

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// Priority is the bundle's provider priority: bootstrap.rs loads it at
// STYLE_PROVIDER_PRIORITY_USER + 100, above the per-widget variable
// providers (STYLE_PROVIDER_PRIORITY_USER).
const Priority = widget.StylePriorityUser + 100

// Theme owns the stylesheet: the Rust bundle — the compiled SCSS, the
// theme's :root variables, and the user's styles/index.scss — attached
// to the roots that take it (the Go-painted surfaces keep their
// programmatic styling), plus the resolved palette the Go-painted
// surfaces read.
type Theme struct {
	cfg     *config.Config
	Sheet   *widget.Stylesheet
	palette config.Palette
	// userStamp fingerprints the user styles tree for the hot-reload
	// poll.
	userStamp string
}

// New resolves the palette and compiles the bundle. A provider or
// user-stylesheet failure logs and falls back (the configured palette,
// no user CSS), the Rust shell's behavior.
func New(cfg *config.Config) *Theme {
	t := &Theme{cfg: cfg}
	if dir, err := config.Dir(); err == nil {
		styling.EnsureUserStylesScaffold(dir)
	}
	t.Sheet = widget.NewStylesheet(t.Bundle(), Priority)
	t.userStamp = userStylesStamp()
	return t
}

// Bundle builds init_css_provider's CSS (bootstrap.rs): static, theme,
// the [animations] overrides (the --duration-* tokens and, with
// indicators off, the frozen looping animations), then user styles,
// resolving the palette on the way.
func (t *Theme) Bundle() string {
	palette, err := styling.ResolvePalette(t.cfg.Styling.ActivePalette(), t.cfg.Styling)
	if err != nil {
		log.Printf("styling: %v", err)
	}
	t.palette = palette
	theme := styling.ThemeCSS(palette, t.cfg.General, t.cfg.Bar, t.cfg.Styling)
	user := ""
	if dir, err := config.Dir(); err == nil {
		user = styling.UserCSS(dir)
	} else {
		log.Printf("styling: cannot resolve config dir; user styles disabled: %v", err)
	}
	return styling.StaticCSS + "\n" + theme + "\n" + t.cfg.Animations.CSSOverrides() + "\n" + user
}

// Reload recompiles the bundle into the attached stylesheet: every
// root restyles on the next frame.
func (t *Theme) Reload() {
	t.Sheet.Load(t.Bundle())
}

// SetConfig recompiles the bundle for a new config snapshot.
func (t *Theme) SetConfig(cfg *config.Config) {
	t.cfg = cfg
	t.Reload()
}

// RenderPalette is the resolved palette as render colors, for the
// surfaces the stylesheet does not reach (OSD, popups, dropdowns).
func (t *Theme) RenderPalette() *styling.Palette {
	p, err := styling.PaletteFromHex(t.palette)
	if err != nil {
		log.Printf("styling: palette: %v", err)
		return styling.Default()
	}
	return p
}

// Attach styles one root.
func (t *Theme) Attach(root interface{ AttachStylesheet(*widget.Stylesheet) }) {
	root.AttachStylesheet(t.Sheet)
}

// WatchUserStyles reloads the bundle when a .scss or .css file under
// the user styles tree changed (watcher.rs): the tree is watched on the
// loop (app.WatchFiles), and the stamp filters out changes to other
// files. Without inotify it polls once a second. A failed compile keeps
// the stylesheet running: UserCSS logs and contributes nothing.
func (t *Theme) WatchUserStyles(application *app.Application) {
	t.watchStyles(application.WatchFiles, func(fn func()) { application.Every(time.Second, fn) })
}

// watchStyles is WatchUserStyles over its two mechanisms.
func (t *Theme) watchStyles(watch func(paths []string, recursive bool, fn func()) (func(), error), poll func(fn func())) {
	check := func() {
		if stamp := userStylesStamp(); stamp != t.userStamp {
			t.userStamp = stamp
			t.Reload()
		}
	}
	if dir, err := config.Dir(); err == nil {
		if styles, ok := styling.UserStylesDir(dir); ok {
			if _, err := watch([]string{styles}, true, check); err == nil {
				return
			}
		}
	}
	poll(check)
}

// userStylesStamp fingerprints every stylesheet under the user styles
// directory by path, size, and modification time; empty when there is
// none.
func userStylesStamp() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	styles, ok := styling.UserStylesDir(dir)
	if !ok {
		return ""
	}
	var b strings.Builder
	_ = filepath.WalkDir(styles, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".scss" && ext != ".css" {
			return nil
		}
		if info, err := d.Info(); err == nil {
			b.WriteString(path)
			b.WriteByte(0)
			b.WriteString(info.ModTime().String())
			b.WriteByte(0)
			b.WriteString(strconv.FormatInt(info.Size(), 10))
			b.WriteByte(0)
		}
		return nil
	})
	return b.String()
}

// faceKey keys the resolved-face cache.
type faceKey struct {
	family string
	weight int
}

// faceCache memoizes FontResolver: a label compares faces by identity,
// so one family and weight must resolve to one face (loop goroutine
// only, like every style read).
var faceCache = map[faceKey]render.Font{}

// FontResolver resolves the stylesheet's font-family and font-weight
// declarations through the system font store, so the bundle's
// --weight-* tokens and a user's font-family take effect. A family the
// store lacks declines, keeping the label's own face.
func FontResolver(family string, weight int) (render.Font, bool) {
	if weight == 0 {
		weight = 400
	}
	key := faceKey{family, weight}
	if f, ok := faceCache[key]; ok {
		return f, f != nil
	}
	face, err := app.FontWeighted(family, 14, weight, false)
	if err != nil {
		faceCache[key] = nil
		return nil, false
	}
	f := render.Font(app.FontFallback(face))
	faceCache[key] = f
	return f, true
}

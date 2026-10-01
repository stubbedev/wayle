package bar

import (
	"log"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/launcheripc"
	engine "github.com/stubbedev/wayle/service/launcher"
	"github.com/stubbedev/wayle/shell/launcher"
	"github.com/stubbedev/wayle/styling"
)

// startLauncher serves the launcher socket from the shell, as the Rust
// shell's launcher service does: `wayle launcher` sessions open the
// surface on the UI loop, which reads the live config on every session
// (so a reload reaches the next one). The returned stop closes the
// socket.
func startLauncher(application *app.Application, outputs func() []*app.Output, current func() *config.Config, ctx ModuleContext, theme *barTheme, font render.Font, palette *styling.Palette) func() {
	ink, _ := palette.Token(config.TokenFgDefault)
	deps := launcher.Deps{
		Invoke: application.Invoke,
		After: func(d time.Duration, fn func()) func() {
			t := time.AfterFunc(d, func() { application.Invoke(fn) })
			return func() { t.Stop() }
		},
		Config: current,
		Open: func(cfg app.LayerConfig) (launcher.Window, error) {
			w, err := application.NewLayer(cfg)
			if err != nil {
				return nil, err
			}
			return w, nil
		},
		Font:          font,
		Ink:           ink,
		Sheet:         theme.sheet,
		SheetPriority: barThemePriority,
		MonitorWidth: func() int {
			if outs := outputs(); len(outs) > 0 {
				return int(outs[0].LogicalW)
			}
			return 0
		},
		Copy: func(text string) {
			if c := application.Clipboard(); c != nil {
				if err := c.WriteText(text); err != nil {
					log.Printf("launcher: copy: %v", err)
				}
			}
		},
		HeldMods:    func() engine.MouseModifiers { return mouseMods(application.HeldMods()) },
		OpenHistory: engine.OpenHistory,
	}
	if ctx.Clipboard != nil {
		deps.Clipboard = ctx.Clipboard
	}
	stop, err := launcheripc.NewServer(launcher.New(deps)).Listen()
	if err != nil {
		log.Printf("launcher: the launcher socket is unavailable: %v", err)
		return func() {}
	}
	return stop
}

// mouseMods maps the held keyboard modifiers onto a pointer binding's.
func mouseMods(m app.Mods) engine.MouseModifiers {
	var out engine.MouseModifiers
	if m&app.ModCtrl != 0 {
		out |= engine.MouseControl
	}
	if m&app.ModShift != 0 {
		out |= engine.MouseShift
	}
	if m&app.ModAlt != 0 {
		out |= engine.MouseAlt
	}
	return out
}

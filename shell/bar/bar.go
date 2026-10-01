package bar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/service/bluetooth"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/service/clipboard"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/network"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/service/sni"
	"github.com/stubbedev/wayle/service/treeman"
	"github.com/stubbedev/wayle/service/upower"
	"github.com/stubbedev/wayle/shell/lock"
	"github.com/stubbedev/wayle/shell/osd"
	"github.com/stubbedev/wayle/shell/popups"
	wallpapershell "github.com/stubbedev/wayle/shell/wallpaper"
	"github.com/stubbedev/wayle/styling"
)

// Run loads the user config and shows the bar. A config failure logs
// and falls back to defaults — the Rust shell's behavior — rather than
// refusing to start.
func Run() error {
	svc, err := config.Open()
	if err != nil {
		log.Printf("wayle: config: %v", err)
	}
	if svc == nil {
		return RunWith(config.Defaults())
	}
	defer svc.Close()
	return run(svc.Config(), svc)
}

// RunWith shows the bar from a prepared config: one layer surface per
// output the layout gives a visible bar, then the gelm loop.
func RunWith(cfg *config.Config) error { return run(cfg, nil) }

// run shows the bar; with a service it follows config reloads.
func run(cfg *config.Config, svc *config.Service) error {
	sess, err := app.Connect()
	if err != nil {
		return fmt.Errorf("bar: connect: %w", err)
	}
	defer sess.Close()

	application := app.NewApplication(sess)
	// Programmatic copies (the screenshot host) and the ctrl+c/v keys
	// share one clipboard.
	if application.Clipboard() == nil {
		application.SetClipboard(app.NewClipboard(sess))
	}
	// The bars load the Rust stylesheet bundle; the surfaces it does not
	// reach (OSD, popups, dropdowns) paint from the same resolved palette.
	theme := newBarTheme(cfg)
	theme.watchUserStyles(application)
	widget.SetFaceResolver(fontResolver)
	palette := theme.renderPalette()
	applyPalette(palette)

	style := computeStyle(cfg, palette)
	// Bar modules leave their normal ink to the stylesheet
	// (--bar-btn-label-color): a zero fg is "unset" to gelm, so only a
	// module's deliberate state color stays programmatic.
	moduleStyle := style
	moduleStyle.fg = 0

	face, err := app.Font(cfg.General.FontSans, style.labelPx)
	if err != nil {
		return fmt.Errorf("bar: font %q: %w", cfg.General.FontSans, err)
	}
	font := app.FontFallback(face)
	baseCtx := ModuleContext{Config: cfg, App: application, Font: font, Style: &moduleStyle, Theme: theme}
	// The clipboard history starts with the shell rather than when the
	// launcher first opens, so it covers the session; a compositor
	// without data-control simply has none (bootstrap/mod.rs).
	if clip, err := clipboard.Start(application, clipboard.DefaultHistory()); err == nil {
		baseCtx.Clipboard = clip
	}
	if hyprland.IsRunning() {
		if conn, err := hyprland.Connect(); err == nil {
			baseCtx.Hyprland = conn
		}
	}
	if battery, err := upower.NewSystem(); err == nil {
		defer func() { _ = battery.Close() }()
		baseCtx.Battery = battery
	}
	backlights := brightness.NewSystem(cfg.Brightness.EnableExternal)
	defer func() { _ = backlights.Close() }()
	baseCtx.Brightness = backlights
	audio := connectAudio()
	if audio != nil {
		defer func() { _ = audio.Close() }()
		baseCtx.Pulse = audio
	}
	if bt, err := bluetooth.NewSystem(); err == nil {
		defer func() { _ = bt.Close() }()
		baseCtx.Bluetooth = bt
	}
	if nm, err := network.NewSystem(); err == nil {
		defer func() { _ = nm.Close() }()
		baseCtx.Network = nm
	}
	if svc, stop, err := startNetworkService(); err == nil {
		defer stop()
		baseCtx.Networking = svc
	} else {
		log.Printf("network: %v", err)
	}
	if pp, err := powerprofiles.NewSystem(); err == nil {
		defer func() { _ = pp.Close() }()
		baseCtx.PowerProfiles = pp
	}
	if media, err := mpris.NewSession(cfg.Media.PlayersIgnored, cfg.Media.PlayerPriority); err == nil {
		defer func() { _ = media.Close() }()
		baseCtx.Media = media
	}
	// The idle-inhibit service owns its state here and serves it on the
	// session bus for the `wayle idle` CLI.
	inhibitState := idleinhibit.NewState(cfg.IdleInhibit.StartupDuration)
	baseCtx.IdleInhibit = inhibitState
	baseCtx.Treeman = treeman.New("treeman")
	// The notification service owns the well-known name on the session
	// bus; other senders deliver through it.
	notifSvc := notifications.NewService()
	baseCtx.Notifications = notifSvc
	recState := recorder.NewState(recorder.WfRecorder{}, time.Duration(cfg.Recorder.StartDelayMs)*time.Millisecond)
	baseCtx.Recorder = recState
	sniStore := sni.NewStore()
	baseCtx.SNI = sniStore
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		defer func() { _ = conn.Close() }()
		server, err := notifications.Serve(conn, notifSvc)
		if err != nil {
			log.Printf("notifications: daemon: %v", err)
		} else {
			defer func() { _ = server.Release() }()
		}
		if release, err := recorder.NewDaemon(recState).Export(conn); err == nil {
			defer release()
		} else {
			log.Printf("recorder: daemon: %v", err)
		}
		if audio != nil {
			if release, err := pulse.NewDaemon(audio).Export(conn); err == nil {
				defer release()
			} else {
				log.Printf("audio: daemon: %v", err)
			}
		}
	}
	stopMail := startMail(&baseCtx)
	defer stopMail()
	sniHost, err := sni.NewHost(sniStore)
	if err == nil {
		defer func() { _ = sniHost.Close() }()
		baseCtx.Tray = NewTrayService(sniHost)
	} else {
		log.Printf("systray: host: %v", err)
	}
	defer serveCLIDaemons(baseCtx, sniHost)()
	customUpd := newCustomUpdates()
	baseCtx.CustomUpdates = customUpd
	baseCtx.Attachers = &[]interface{ Attach(app.Host) }{}
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		defer func() { _ = conn.Close() }()
		daemon := idleinhibit.NewDaemon(inhibitState)
		if release, err := daemon.Export(conn); err == nil {
			defer release()
		} else {
			log.Printf("idle-inhibit: daemon: %v", err)
		}
	}

	outputs := sess.Outputs()
	if len(outputs) == 0 {
		return errors.New("bar: no output to draw on")
	}
	// Wallpapers render on their own Background layers; hotplugged
	// outputs join once their connector name is known.
	wall, stopWallpaper := wallpapershell.Launch(application, outputs, cfg, &sess.OnOutputIdentity, &sess.OnOutputRemoved)
	defer stopWallpaper()
	// A fresh color extraction re-resolves the provider palette and
	// recompiles the bundle, the Rust shell's theme hot-apply.
	if wall != nil {
		ticks, stopTicks := wall.Service().Extracted()
		defer stopTicks()
		go func() {
			for range ticks {
				application.Invoke(theme.reload)
			}
		}()
	}
	osdSrv := osd.New(application, cfg.Osd, font, palette)
	captureSvc := startCapture(application, sess.Outputs, cfg, palette, baseCtx.Hyprland, font, style.labelPx)
	defer captureSvc.close()
	baseCtx.Screenshot = captureSvc.trigger
	// rt is what the bars are built from; a config reload swaps it and
	// rebuilds them.
	rt := &barRuntime{ctx: baseCtx, style: style}
	rt.mount(application, cfg, font)
	// openBar builds one output's bar layer; `wayle panel show` and a
	// reload reopen bars through it.
	openBar := func(output *app.Output, layout config.BarLayout) (barLayer, error) {
		ctx := rt.ctx
		ctx.Connector = output.Name
		ctx.Attachers = &[]interface{ Attach(app.Host) }{}
		// Each bar is its own generation: closing it (panel hide, a
		// reload) ends its modules.
		ctx.gen = newMountGen()
		lc := layerConfigFor(ctx, layout, output.Name, logicalSize(output))
		lc.Output = output
		layer, err := application.NewLayer(*lc)
		if err != nil {
			ctx.gen.retire()
			return nil, err
		}
		for _, a := range *ctx.Attachers {
			a.Attach(layer)
		}
		rt.ctx.Dropdowns.attachHost(output.Name, layer)
		return barWindow{layer, ctx.gen}, nil
	}
	bars := newBarSet(openBar)
	for _, output := range outputs {
		layout, show := barLayoutFor(cfg, output.Name)
		var layer barLayer
		if show {
			if layer, err = openBar(output, layout); err != nil {
				return err
			}
		}
		bars.add(output, layout, layer)
		osdSrv.AttachOutput(output.Name, output)
	}
	// Notification popups render on one monitor, bar or not.
	var popupHost *popups.Popups
	if cfg.Notification.Enabled {
		if output := popups.Output(outputs, cfg.Notification.PopupMonitor); output != nil {
			popupHost = popups.New(application, notifSvc, cfg.Notification, font, palette, output)
			go popupHost.Run()
		}
	}
	current := &atomic.Pointer[config.Config]{}
	current.Store(cfg)
	if svc != nil {
		// A reload rebuilds the bars from the new snapshot (the Rust
		// modules re-render from their property watches) and hands the
		// OSD and popups their new sections.
		cancel := svc.Subscribe(func(_, next *config.Config) {
			application.Invoke(func() {
				current.Store(next)
				rt.style = computeStyle(next, palette)
				rt.mount(application, next, font)
				bars.reload(func(connector string) (config.BarLayout, bool) { return barLayoutFor(next, connector) })
				osdSrv.SetConfig(next.Osd)
				if popupHost != nil {
					popupHost.SetConfig(next.Notification)
				}
			})
		})
		defer cancel()
	}
	if cfg.Osd.Enabled {
		go watchOsd(current.Load, baseCtx, osdSrv)
	}
	// The ext-session-lock screen and its triggers (logind, and `wayle
	// lock` through Shell1).
	lockScreen, stopLock := lock.Start(application, cfg, lock.Fonts(cfg.General.FontSans, font), palette)
	defer stopLock()
	defer serveShellIPC(application, bars, func() bool {
		application.Invoke(lockScreen.Lock)
		return true
	})()
	// The widget socket carries out-of-process pushes (`wayle toast`).
	widgetSrv := widgetipc.NewServer()
	if stop, err := widgetSrv.Listen(); err == nil {
		defer stop()
	} else {
		log.Printf("widget socket: %v", err)
	}
	go func() {
		for req := range widgetSrv.Toasts() {
			if err := osdSrv.ShowToast(req); err != nil {
				log.Printf("toast: %v", err)
			}
		}
	}()
	go func() {
		for update := range widgetSrv.Updates() {
			customUpd.dispatch(update.ID, update.Output)
		}
	}()
	return application.Run()
}

// brightnessOsdIcon picks the brightness flash's glyph from the
// percentage.
func brightnessOsdIcon(percent float64) string {
	switch {
	case percent <= 0:
		return "ld-sun-off-symbolic"
	case percent < 50:
		return "ld-sun-dim-symbolic"
	}
	return "ld-sun-symbolic"
}

// connectAudio starts the audio service (the Rust bootstrap's
// AudioService with its daemon). A missing sound server leaves the
// shell without audio, logged, rather than failing it.
func connectAudio() *pulse.Service {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	audio, err := pulse.Connect(ctx, "")
	if err != nil {
		log.Printf("audio: %v", err)
		return nil
	}
	return audio
}

// microphoneOsdIcon resolves the input flash's glyph.
func microphoneOsdIcon(cfg config.MicrophoneConfig, dev pulse.Device) string {
	if dev.Muted {
		return cfg.IconMuted
	}
	return cfg.Icon().Name
}

// watchOsd follows the pulse and brightness subscriptions and flashes
// the OSD on their changes, deduplicating repeats like osd/watchers.rs's
// last_volume/last_brightness tracking.
func watchOsd(current func() *config.Config, ctx ModuleContext, server *osd.Osd) {
	bctx := context.Background()
	var lastVolume, lastInput float64
	var lastMuted, lastInputMuted bool
	var lastBrightness float64
	handlePulse := func() {
		if ctx.Pulse == nil {
			return
		}
		// Repeats dedupe on the rounded level and mute, the Rust
		// last_volume snapshot.
		if out, err := ctx.Pulse.DefaultSink(bctx); err == nil {
			sink := out.Device
			if level := volumePercent(sink); level != lastVolume || sink.Muted != lastMuted {
				lastVolume, lastMuted = level, sink.Muted
				server.Show(osd.Event{
					Kind:  "volume",
					Icon:  volumeIconName(current().Volume, sink),
					Label: "Output",
					Value: sink.Volume.AveragePercentage(),
					Muted: sink.Muted,
				})
			}
		}
		if in, err := ctx.Pulse.DefaultSource(bctx); err == nil {
			source := in.Device
			if level := volumePercent(source); level != lastInput || source.Muted != lastInputMuted {
				lastInput, lastInputMuted = level, source.Muted
				server.Show(osd.Event{
					Kind:  "input-volume",
					Icon:  microphoneOsdIcon(current().Microphone, source),
					Label: "Input",
					Value: source.Volume.AveragePercentage(),
					Muted: source.Muted,
				})
			}
		}
	}
	handleBrightness := func() {
		if ctx.Brightness == nil {
			return
		}
		devices, err := ctx.Brightness.Devices(bctx)
		if err != nil || len(devices) == 0 {
			return
		}
		percent := devices[0].Percentage()
		if percent != lastBrightness {
			lastBrightness = percent
			server.Show(osd.Event{
				Kind:  "brightness",
				Icon:  brightnessOsdIcon(percent),
				Label: i18n.T("osd-brightness"),
				Value: percent,
			})
		}
	}
	stops := make([]func(), 0, 2)
	if ctx.Pulse != nil {
		if ticks, stop, err := ctx.Pulse.Subscribe(bctx); err == nil {
			stops = append(stops, stop)
			go func() {
				for range ticks {
					handlePulse()
				}
			}()
		}
	}
	if ctx.Brightness != nil {
		if ticks, stop, err := ctx.Brightness.Subscribe(bctx); err == nil {
			stops = append(stops, stop)
			go func() {
				for range ticks {
					handleBrightness()
				}
			}()
		}
	}
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	select {}
}

// layerConfigFor measures the tree for one output (its logical size)
// and maps it onto a layer surface config: anchors from the location,
// margins from the insets, the exclusive zone from the bar's thickness
// (the Rust shell's auto exclusive zone), and the wayle-bar namespace.
// The compositor stretches only the doubly anchored axis, so a top or
// bottom bar sizes its height and a side bar its width. The caller pins
// the output.
func layerConfigFor(ctx ModuleContext, layout config.BarLayout, connector string, output widget.Size) *app.LayerConfig {
	root := buildRoot(ctx, layout, connector)
	// The surface is transparent and carries no layer margins: the
	// window's CSS margins (insets and the shadow margin) sit inside it
	// and the stylesheet paints the bar, as GTK sizes a layer window by
	// its margin box.
	vertical := ctx.Config.Bar.Location.IsVertical()
	thickness := measureThickness(root, output, vertical)
	var width, height uint32
	if vertical {
		width = uint32(thickness)
	} else {
		height = uint32(thickness)
	}
	return &app.LayerConfig{
		Layer:         LayerFor(ctx.Config.Bar.Layer),
		Anchor:        AnchorsFor(ctx.Config.Bar.Location),
		Width:         width,
		Height:        height,
		ExclusiveZone: exclusiveZone(ctx.Config.Bar.Exclusive, thickness),
		Namespace:     "wayle-bar-" + connector,
		Root:          root,
	}
}

// buildRoot assembles one bar the way bar/mod.rs's view does, so the
// compiled bar SCSS styles it:
//
//	window.bar.<location>.<connector>[.floating]   (the --bar-* variables inline)
//	└ centerbox
//	  ├ box.bar-section.bar-left
//	  ├ box.bar-section.bar-center
//	  └ box.bar-section.bar-right
//
// The window's CSS margins carry the insets and the shadow margin, its
// border and background the bar chrome; the sections' margins carry the
// padding (bar/_container.scss). Expanding fillers stand in for
// GtkCenterBox's centering.
func buildRoot(ctx ModuleContext, layout config.BarLayout, connector string) widget.Widget {
	cfg := ctx.Config
	axis := widget.Row
	if cfg.Bar.Location.IsVertical() {
		axis = widget.Column
	}
	root := widget.NewBox(axis, 0, 0)
	root.SetElement("window")
	root.AddClass(rootClasses(connector, cfg)...)
	root.SetInlineStyle(inlineDecls(styling.BarCSS(cfg.Bar, cfg.Styling.ColorExtractor.ThemeProvider)))
	if ctx.Theme != nil {
		ctx.Theme.attach(root)
	}
	center := widget.NewBox(axis, 0, 0)
	for i, part := range []struct {
		class string
		items []config.BarItem
	}{
		{"bar-left", layout.Left},
		{"bar-center", layout.Center},
		{"bar-right", layout.Right},
	} {
		section := CreateAll(part.items, ctx)
		section.AddClass("bar-section", part.class)
		center.Append(section, false)
		if i < 2 {
			center.Append(widget.NewBox(axis, 0, 0), true)
		}
	}
	root.Append(center, true)
	return root
}

// measureThickness resolves the bar's content-driven thickness: the
// natural height of the tree at the output's width, or for a side bar
// the natural width at the output's height. A priming arrange runs
// first: gelm links a widget to its container at arrange time, and the
// bar stylesheet (attached to the root) reaches a widget only through
// those links, so the unprimed tree would measure unstyled.
func measureThickness(root widget.Widget, output widget.Size, vertical bool) int {
	con := widget.Constraints{Max: widget.Size{W: output.W, H: 1 << 16}}
	if vertical {
		con = widget.Constraints{Max: widget.Size{W: 1 << 16, H: output.H}}
	}
	size := root.Measure(con)
	if vertical {
		root.Arrange(render.Rect{W: size.W, H: output.H})
	} else {
		root.Arrange(render.Rect{W: output.W, H: size.H})
	}
	// The links drop the children's measure caches but not the root's.
	if inv, ok := root.(interface{ InvalidateLayout() }); ok {
		inv.InvalidateLayout()
	}
	size = root.Measure(con)
	if vertical {
		return size.W
	}
	return size.H
}

// exclusiveZone mirrors gtk4-layer-shell's auto exclusive zone: an
// exclusive bar reserves its own thickness along the docked edge, a
// non-exclusive one reserves nothing.
func exclusiveZone(exclusive bool, thickness int) int32 {
	if !exclusive {
		return 0
	}
	return int32(thickness)
}

// logicalSize converts an output's current mode to logical pixels at
// the output's scale; the compositor's fractional-scale preference
// overrides this live once the surface exists. Bar scale is a UI scale
// for sizes, not a device scale, and never divides here.
func logicalSize(output *app.Output) widget.Size {
	scale := max(output.Scale, 1)
	return widget.Size{W: output.ModeW / scale, H: output.ModeH / scale}
}

// applyPalette seeds gelm's widget theme from the wayle palette, so
// widgets the stylesheet does not target still match the tokens: the
// mapping follows the token table (bg-base, bg-surface, fg-default,
// fg-on-accent) with the border-default token for widget strokes.
func applyPalette(palette *styling.Palette) {
	border, _ := palette.Token(config.TokenBorderDefault)
	widget.SetTheme(&widget.Theme{
		Bg:        palette.Bg,
		Surface:   palette.Surface,
		Text:      palette.Fg,
		TextMuted: palette.FgMuted,
		Accent:    palette.Primary,
		OnAccent:  palette.Surface,
		Border:    border,
	})
}

// barRuntime is the state a mount generation of bars is built from:
// the module context (with its dropdown registry) and the style.
type barRuntime struct {
	ctx   ModuleContext
	style barStyle
}

// mount starts a generation for cfg: the previous one is retired, the
// dropdowns are rebuilt, and the context carries the new snapshot.
func (r *barRuntime) mount(application *app.Application, cfg *config.Config, font render.Font) {
	if r.ctx.gen != nil {
		r.ctx.gen.retire()
	}
	r.ctx.Config = cfg
	r.ctx.Style = &r.style
	r.ctx.gen = newMountGen()
	r.ctx.Dropdowns = newDropdownRegistry(application, cfg, font, &r.style, r.ctx)
}

// barLayoutFor is the output's layout and whether it shows a bar.
func barLayoutFor(cfg *config.Config, connector string) (config.BarLayout, bool) {
	layout, ok := FindLayout(cfg.Bar.Layout, connector)
	return layout, ok && layout.Show
}

// barWindow is an open bar: closing it closes the layer and ends the
// generation its modules run in.
type barWindow struct {
	layer *app.LayerWindow
	gen   *mountGen
}

func (w barWindow) Close() {
	w.layer.Close()
	w.gen.retire()
}

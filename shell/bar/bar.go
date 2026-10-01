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
	"github.com/stubbedev/wayle/internal/desktopnotify"
	"github.com/stubbedev/wayle/internal/icons"
	"github.com/stubbedev/wayle/internal/spawn"
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
	"github.com/stubbedev/wayle/service/weather"
	"github.com/stubbedev/wayle/shell/layering"
	"github.com/stubbedev/wayle/shell/lock"
	"github.com/stubbedev/wayle/shell/osd"
	"github.com/stubbedev/wayle/shell/popups"
	"github.com/stubbedev/wayle/shell/powermenu"
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
	// Surfaces animate their content through [animations] (shell/reveal),
	// as the Rust shell's revealers do; gelm's own surface fades would
	// stack on top of them.
	application.SetSurfaceMotion(false)
	// current is the live config snapshot the long-lived services read.
	current := &atomic.Pointer[config.Config]{}
	current.Store(cfg)
	// osdRef is the OSD once it exists; the recorder toasts through it.
	osdRef := &atomic.Pointer[osd.Osd]{}
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
	if registry, err := icons.NewRegistry(); err == nil {
		defer initIcons(registry, gelmIcons)()
	} else {
		log.Printf("wayle: icon registry init failed: %v", err)
	}
	// rt is what the bars are built from; a config reload or a new
	// palette re-derives it and rebuilds them.
	rt := &barRuntime{theme: theme, palette: new(styling.Palette)}
	if err := rt.derive(cfg); err != nil {
		return err
	}
	palette, style, font := rt.palette, rt.style, rt.font
	baseCtx := ModuleContext{Config: cfg, App: application, Font: font, Style: &rt.moduleStyle, Theme: theme}
	if svc != nil {
		baseCtx.SetConfig = configSetter(svc)
	}
	baseCtx.Toast = func(label, icon string) {
		if o := osdRef.Load(); o != nil {
			_ = o.ShowToast(widgetipc.ToastRequest{Label: &label, Icon: &icon})
		}
	}
	ink, _ := palette.Token(config.TokenFgDefault)
	baseCtx.PowerMenu = powermenu.New(powermenu.Deps{
		Config: current.Load,
		Open: func(cfg app.LayerConfig) (powermenu.Window, error) {
			w, err := application.NewLayer(cfg)
			if err != nil {
				return nil, err
			}
			return w, nil
		},
		Run:   spawn.Quiet,
		Font:  font,
		Ink:   ink,
		Sheet: theme.sheet,
	})
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
		// Every Rust bar warms all its dropdowns, so the bluetooth
		// dropdown's pairing watcher runs whether or not a bluetooth
		// module is placed: a request still reaches the user.
		startBtPairingNotifier(bt)
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
	// The weather service polls for the whole session (bootstrap/weather.rs);
	// after its first fetch it only polls while a module or dropdown
	// follows it.
	weatherSvc := weather.New(weatherSettings(cfg.Weather))
	defer weatherSvc.Close()
	baseCtx.Weather = weatherSvc
	// The notification service owns the well-known name on the session
	// bus; other senders deliver through it.
	notifSvc := startNotifications(cfg.Notification)
	baseCtx.Notifications = notifSvc
	sniStore := sni.NewStore()
	baseCtx.SNI = sniStore
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		defer func() { _ = conn.Close() }()
		if notifSvc != nil {
			server, err := notifications.Serve(conn, notifSvc)
			if err != nil {
				log.Printf("notifications: daemon: %v", err)
			} else {
				defer func() { _ = server.Release() }()
			}
		}
		// The recorder captures through the ScreenCast portal on this
		// bus and reports through the OSD and desktop notifications.
		recState := recorder.NewState(recorder.GstEngine{Conn: conn},
			func() config.RecorderConfig { return current.Load().Recorder },
			recorder.Hooks{Toast: func(label, icon string, ms uint32) {
				if o := osdRef.Load(); o != nil {
					_ = o.ShowToast(widgetipc.ToastRequest{Label: &label, Icon: &icon, DurationMS: &ms})
				}
			}, Notify: desktopnotify.Notify})
		baseCtx.Recorder = recState
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
	wall, stopWallpaper := wallpapershell.Launch(application, outputs, cfg, sess)
	defer stopWallpaper()
	osdSrv := osd.New(application, cfg.Osd, cfg.General, cfg.Animations, font, palette)
	osdRef.Store(osdSrv)
	captureSvc := startCapture(application, sess.Outputs, cfg, palette, baseCtx.Hyprland, font, style.labelPx)
	defer captureSvc.close()
	baseCtx.Screenshot = captureSvc.trigger
	rt.ctx = baseCtx
	rt.mount(application, cfg)
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
	// plugOutput gives a named output its bar and OSD face; startup
	// outputs and hotplugged ones (once their connector name is known)
	// take the same path, the Rust SyncMonitors. A bar that cannot open
	// is logged, not fatal.
	plugOutput := func(output *app.Output) {
		if output.Name == "" {
			return
		}
		layout, show := barLayoutFor(current.Load(), output.Name)
		bars.plug(output, layout, show)
		osdSrv.AttachOutput(output.Name, output)
	}
	for _, output := range outputs {
		plugOutput(output)
	}
	defer sess.WatchOutputIdentity(plugOutput)()
	defer sess.WatchOutputs(nil, func(output *app.Output) {
		bars.unplug(output)
		osdSrv.DetachOutput(output.Name)
		rt.ctx.Dropdowns.detachHost(output.Name)
	})()
	// Notification popups render on one monitor, bar or not.
	var popupHost *popups.Popups
	if notifSvc != nil {
		if output := popups.Output(outputs, cfg.Notification.PopupMonitor); output != nil {
			popupHost = popups.New(application, notifSvc, cfg, font, palette, theme.sheet, output)
			go popupHost.Run()
		}
	}
	// restyle re-derives the palette, styles, and font from cfg and
	// rebuilds the bars from them (the Rust modules re-render from their
	// property watches, and the CSS watcher recompiles the bundle).
	restyle := func(cfg *config.Config) {
		if err := rt.derive(cfg); err != nil {
			log.Printf("%v; keeping the previous font", err)
		}
		rt.mount(application, cfg)
		bars.reload(func(connector string) (config.BarLayout, bool) { return barLayoutFor(cfg, connector) })
	}
	// lockScreen starts below; a reload reaches it once it exists.
	var lockScreen *lock.Screen
	if svc != nil {
		// A reload recompiles the stylesheet for the new snapshot,
		// rebuilds the bars, and hands the OSD and popups their new
		// sections.
		cancel := svc.Subscribe(func(old, next *config.Config) {
			application.Invoke(func() {
				current.Store(next)
				theme.setConfig(next)
				if barsAffected(old, next) {
					restyle(next)
				} else {
					// The open bars stay; dropdowns opened from now on
					// read the new snapshot.
					rt.ctx.Config = next
					rt.ctx.Dropdowns.setConfig(next)
				}
				osdSrv.SetConfig(next.Osd, next.General, next.Animations)
				applyNotificationConfig(notifSvc, next.Notification)
				weatherSvc.Configure(weatherSettings(next.Weather))
				if wall != nil {
					wall.SetConfig(next)
				}
				if popupHost != nil {
					popupHost.SetConfig(next)
				}
				if lockScreen != nil {
					lockScreen.SetConfig(next)
				}
			})
		})
		defer cancel()
		// A secrets reload re-resolves the weather API keys
		// (spawn_secrets_reload_watcher).
		defer svc.SubscribeSecrets(func() { weatherSvc.Configure(weatherSettings(current.Load().Weather)) })()
	}
	// A fresh color extraction re-resolves the provider palette and
	// recompiles the bundle, the Rust shell's theme hot-apply.
	if wall != nil {
		ticks, stopTicks := wall.Service().Extracted()
		defer stopTicks()
		go func() {
			for range ticks {
				application.Invoke(func() {
					// The bundle recompiles regardless (the Rust CSS
					// watcher); the bars rebuild only for a new palette, so
					// an extraction that changed nothing (a monitor plugged
					// in) leaves them open.
					theme.reload()
					if rt.paletteStale() {
						restyle(current.Load())
					}
				})
			}
		}()
	}
	// The OSD watcher runs whether or not the OSD is enabled: the OSD
	// drops events while disabled, so a reload can turn it on or off
	// (the Rust OsdEnabledChanged).
	go watchOsd(current.Load, baseCtx, osdSrv)
	// The ext-session-lock screen and its triggers (logind, and `wayle
	// lock` through Shell1).
	lockScreen, stopLock := lock.Start(application, current.Load(), lock.Fonts(cfg.General.FontSans, font), palette)
	defer stopLock()
	defer serveShellIPC(application, bars, func() bool {
		application.Invoke(lockScreen.Lock)
		return true
	})()
	// The launcher surface serves `wayle launcher` sessions.
	defer startLauncher(application, sess.Outputs, current.Load, rt.ctx, theme, font, palette)()
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
		Layer:         layering.For(ctx.Config.General, ctx.Config.Bar.Layer),
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
// the module context (with its dropdown registry), the theme, and what
// derive resolves from a config snapshot.
type barRuntime struct {
	ctx   ModuleContext
	theme *barTheme
	// palette is the one resolved palette every Go-painted surface
	// (OSD, popups, lock, capture) holds; derive updates it in place,
	// on the loop.
	palette *styling.Palette
	// style is the dropdowns'; moduleStyle is the modules', which
	// leave their normal ink to the stylesheet (--bar-btn-label-color):
	// a zero fg is "unset" to gelm, so only a module's deliberate state
	// color stays programmatic.
	style, moduleStyle barStyle
	font               render.Font
}

// derive resolves the palette, styles, and font for cfg from the
// theme's compiled palette. A font that does not load is an error and
// leaves the previous font in place.
func (r *barRuntime) derive(cfg *config.Config) error {
	*r.palette = *r.theme.renderPalette()
	applyPalette(r.palette)
	r.style = computeStyle(cfg, r.palette)
	r.moduleStyle = r.style
	r.moduleStyle.fg = 0
	face, err := app.Font(cfg.General.FontSans, r.style.labelPx)
	if err != nil {
		return fmt.Errorf("bar: font %q: %w", cfg.General.FontSans, err)
	}
	r.font = app.FontFallback(face)
	return nil
}

// paletteStale reports whether the theme's compiled palette differs
// from the one derive last resolved.
func (r *barRuntime) paletteStale() bool { return *r.theme.renderPalette() != *r.palette }

// mount starts a generation for cfg: the previous one is retired, the
// dropdowns are rebuilt, and the context carries the new snapshot.
func (r *barRuntime) mount(application *app.Application, cfg *config.Config) {
	if r.ctx.gen != nil {
		r.ctx.gen.retire()
	}
	r.ctx.Config = cfg
	r.ctx.Font = r.font
	r.ctx.Style = &r.moduleStyle
	r.ctx.gen = newMountGen()
	r.ctx.Dropdowns = newDropdownRegistry(application, cfg, r.font, &r.style, r.ctx)
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

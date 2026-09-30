package bar

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	"github.com/stubbedev/wayle/shell/osd"
	"github.com/stubbedev/wayle/shell/popups"
	wallpapershell "github.com/stubbedev/wayle/shell/wallpaper"
	"github.com/stubbedev/wayle/styling"
)

// Run loads the user config and shows the bar. A config failure logs
// and falls back to defaults — the Rust shell's behavior — rather than
// refusing to start.
func Run() error {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("wayle: using defaults, config failed:\n%v", err)
	}
	return RunWith(cfg)
}

// RunWith shows the bar from a prepared config: one layer surface per
// output the layout gives a visible bar, then the gelm loop.
func RunWith(cfg *config.Config) error {
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
	backlights := brightness.NewSystem(cfg.Brightness.EnableExt)
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
	recState := recorder.NewState(recorder.WfRecorder{}, time.Duration(cfg.Recorder.StartDelayMS)*time.Millisecond)
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
	dropdowns := newDropdownRegistry(application, cfg, font, &style, baseCtx)
	baseCtx.Dropdowns = dropdowns
	captureSvc := startCapture(application, sess.Outputs, cfg, palette, baseCtx.Hyprland, font, style.labelPx)
	defer captureSvc.close()
	baseCtx.Screenshot = captureSvc.trigger
	// openBar builds one output's bar layer; `wayle panel show` reopens
	// a hidden bar through it.
	openBar := func(output *app.Output, layout config.BarLayout) (*app.LayerWindow, error) {
		ctx := baseCtx
		ctx.Connector = output.Name
		ctx.Attachers = &[]interface{ Attach(app.Host) }{}
		lc, err := layerConfigFor(ctx, layout, output.Name, logicalWidth(output.ModeW, output.Scale))
		if err != nil {
			return nil, err
		}
		lc.Output = output
		layer, err := application.NewLayer(*lc)
		if err != nil {
			return nil, err
		}
		for _, a := range *ctx.Attachers {
			a.Attach(layer)
		}
		dropdowns.attachHost(output.Name, layer)
		return layer, nil
	}
	bars := newBarSet(func(o *app.Output, l config.BarLayout) (barLayer, error) { return openBar(o, l) })
	for _, output := range outputs {
		layout, ok := FindLayout(cfg.Bar.Layout, output.Name)
		if !ok || !layout.Show {
			continue
		}
		layer, err := openBar(output, layout)
		if err != nil {
			return err
		}
		bars.add(output, layout, layer)
		osdSrv.AttachOutput(output.Name, output)
	}
	// Notification popups render on one monitor, bar or not.
	if cfg.Notification.Enabled {
		if output := popups.Output(outputs, cfg.Notification.PopupMonitor); output != nil {
			p := popups.New(application, notifSvc, cfg.Notification, font, palette, output)
			go p.Run()
		}
	}
	defer serveShellIPC(application, bars)()
	if cfg.Osd.Enabled {
		go watchOsd(cfg, baseCtx, osdSrv)
	}
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
	return cfg.Icon.Name
}

// watchOsd follows the pulse and brightness subscriptions and flashes
// the OSD on their changes, deduplicating repeats like osd/watchers.rs's
// last_volume/last_brightness tracking.
func watchOsd(cfg *config.Config, ctx ModuleContext, server *osd.Osd) {
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
					Icon:  volumeIconName(cfg.Volume, sink),
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
					Icon:  microphoneOsdIcon(cfg.Microphone, source),
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

// layerConfigFor measures the tree for one output and maps it onto a
// layer surface config: anchors from the location, margins from the
// insets, the exclusive zone from the bar height (the Rust shell's auto
// exclusive zone), and the wayle-bar namespace. The caller pins the
// output.
func layerConfigFor(ctx ModuleContext, layout config.BarLayout, connector string, width int) (*app.LayerConfig, error) {
	root, err := buildRoot(ctx, layout, connector)
	if err != nil {
		return nil, err
	}
	// The surface is transparent and carries no layer margins: the
	// window's CSS margins (insets and the shadow margin) sit inside it
	// and the stylesheet paints the bar, as GTK sizes a layer window by
	// its margin box.
	height := measureHeight(root, width)
	return &app.LayerConfig{
		Layer:         LayerFor(ctx.Config.Bar.Layer),
		Anchor:        AnchorsFor(ctx.Config.Bar.Location),
		Height:        uint32(height),
		ExclusiveZone: exclusiveZone(ctx.Config.Bar.Exclusive, height),
		Namespace:     "wayle-bar-" + connector,
		Root:          root,
	}, nil
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
func buildRoot(ctx ModuleContext, layout config.BarLayout, connector string) (widget.Widget, error) {
	cfg := ctx.Config
	axis := widget.Row
	if cfg.Bar.Location.IsVertical() {
		axis = widget.Column
	}
	root := widget.NewBox(axis, 0, 0)
	root.SetElement("window")
	root.AddClass(rootClasses(connector, cfg)...)
	root.SetInlineStyle(inlineDecls(styling.BarCSS(cfg.Bar, cfg.ColorExtractor.ThemeProvider)))
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
		section, err := CreateAll(part.items, ctx)
		if err != nil {
			return nil, err
		}
		section.AddClass("bar-section", part.class)
		center.Append(section, false)
		if i < 2 {
			center.Append(widget.NewBox(axis, 0, 0), true)
		}
	}
	root.Append(center, true)
	return root, nil
}

// measureHeight resolves the bar's content-driven height: the natural
// height of the tree at the output's width. A priming arrange runs
// first: gelm links a widget to its container at arrange time, and the
// bar stylesheet (attached to the root) reaches a widget only through
// those links, so the unprimed tree would measure unstyled.
func measureHeight(root widget.Widget, width int) int {
	con := widget.Constraints{Max: widget.Size{W: width, H: 1 << 16}}
	size := root.Measure(con)
	root.Arrange(render.Rect{W: width, H: size.H})
	// The links drop the children's measure caches but not the root's.
	if inv, ok := root.(interface{ InvalidateLayout() }); ok {
		inv.InvalidateLayout()
	}
	return root.Measure(con).H
}

// exclusiveZone mirrors gtk4-layer-shell's auto exclusive zone: an
// exclusive bar reserves its own height along the docked edge, a
// non-exclusive one reserves nothing.
func exclusiveZone(exclusive bool, height int) int32 {
	if !exclusive {
		return 0
	}
	return int32(height)
}

// logicalWidth converts an output's current mode to logical pixels at
// the output's scale; the compositor's fractional-scale preference
// overrides this live once the surface exists. Bar scale is a UI scale
// for sizes, not a device scale, and never divides here.
func logicalWidth(modeW, outputScale int) int {
	return modeW / max(outputScale, 1)
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

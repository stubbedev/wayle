package bar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
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
	palette := styling.Default()
	applyPalette(palette)

	style := computeStyle(cfg, palette)
	loadBarStylesheet(style)

	face, err := app.Font(cfg.General.FontSans, style.labelPx)
	if err != nil {
		return fmt.Errorf("bar: font %q: %w", cfg.General.FontSans, err)
	}
	font := app.FontFallback(face)
	baseCtx := ModuleContext{Config: cfg, App: application, Font: font, Style: &style}
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
		baseCtx.NetworkService = svc
	} else {
		log.Printf("network: %v", err)
	}
	if pp, err := powerprofiles.NewSystem(); err == nil {
		defer func() { _ = pp.Close() }()
		baseCtx.PowerProfiles = pp
	}
	if media, err := mpris.NewSession(); err == nil {
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
		if release, err := exportShellIPC(conn); err == nil {
			defer release()
		} else {
			log.Printf("shell ipc: %v", err)
		}
	}
	stopMail := startMail(&baseCtx)
	defer stopMail()
	if host, err := sni.NewHost(sniStore); err == nil {
		defer func() { _ = host.Close() }()
	} else {
		log.Printf("systray: host: %v", err)
	}
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
	_, stopWallpaper := wallpapershell.Launch(application, outputs, cfg, &sess.OnOutputIdentity, &sess.OnOutputRemoved)
	defer stopWallpaper()
	osdSrv := osd.New(application, cfg.Osd, font, palette)
	dropdowns := newDropdownRegistry(application, cfg, font, &style, baseCtx)
	baseCtx.Dropdowns = dropdowns
	for _, output := range outputs {
		layout, ok := FindLayout(cfg.Bar.Layout, output.Name)
		if !ok || !layout.Show {
			continue
		}
		ctx := baseCtx
		ctx.Connector = output.Name
		ctx.Attachers = &[]interface{ Attach(app.Host) }{}
		lc, err := layerConfigFor(ctx, layout, output.Name, logicalWidth(output.ModeW, output.Scale))
		if err != nil {
			return err
		}
		lc.Output = output
		layer, err := application.NewLayer(*lc)
		if err != nil {
			return err
		}
		for _, a := range *ctx.Attachers {
			a.Attach(layer)
		}
		dropdowns.attachHost(output.Name, layer)
		osdSrv.AttachOutput(output.Name, output)
		// Notification popups render on the configured monitor only.
		if cfg.Notification.Enabled && cfg.Notification.PopupMonitor == output.Name {
			p := popups.New(application, notifSvc, cfg.Notification, font, palette, output)
			go p.Run()
		}
	}
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
		for raw := range widgetSrv.Updates() {
			var params struct {
				ID     string `json:"id"`
				Output string `json:"output"`
			}
			if err := json.Unmarshal(raw, &params); err != nil {
				continue
			}
			customUpd.dispatch(params.ID, params.Output)
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
				Label: "Brightness",
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
	height := measureHeight(root, width)
	margins := ctx.Style.margins(ctx.Config.Bar.Location)
	return &app.LayerConfig{
		Layer:         LayerFor(ctx.Config.Bar.Layer),
		Anchor:        AnchorsFor(ctx.Config.Bar.Location),
		Height:        uint32(height),
		ExclusiveZone: exclusiveZone(ctx.Config.Bar.Exclusive, height),
		Margin: app.Margins{
			Top:    margins[0],
			Right:  margins[1],
			Bottom: margins[2],
			Left:   margins[3],
		},
		Namespace:  "wayle-bar-" + connector,
		Root:       root,
		Background: ctx.Style.bg,
	}, nil
}

// buildRoot assembles one bar: the row of sections with wayle's
// section margins, the per-side border strips over it, and the root
// classes the stylesheet targets.
func buildRoot(ctx ModuleContext, layout config.BarLayout, connector string) (widget.Widget, error) {
	content, err := buildContent(ctx, layout)
	if err != nil {
		return nil, err
	}
	addRootClasses(content, connector, ctx.Config)

	if !ctx.Style.borders.any() {
		return content, nil
	}
	root := widget.NewOverlay()
	root.Append(content)
	root.Append(newBorder(ctx.Style.borders, ctx.Style.border))
	addRootClasses(root, connector, ctx.Config)
	return root, nil
}

// buildContent lays out the left, center, and right sections the way
// bar/_container.scss does: padding on the cross axis of every
// section, padding-ends on the outer ends, and expanding fillers
// around the center.
func buildContent(ctx ModuleContext, layout config.BarLayout) (*widget.Box, error) {
	horizontal := ctx.Config.Bar.Location == config.LocationTop ||
		ctx.Config.Bar.Location == config.LocationBottom
	row := widget.NewBox(widget.Row, ctx.Style.moduleGap, 0)

	appendSection := func(items []config.BarItem, leading, trailing bool) error {
		if len(items) == 0 {
			return nil
		}
		section, err := CreateAll(items, ctx)
		if err != nil {
			return err
		}
		var left, top, right, bottom int
		if horizontal {
			top, bottom = ctx.Style.padding, ctx.Style.padding
			if leading {
				left = ctx.Style.paddingEnds
			}
			if trailing {
				right = ctx.Style.paddingEnds
			}
		} else {
			left, right = ctx.Style.padding, ctx.Style.padding
			if leading {
				top = ctx.Style.paddingEnds
			}
			if trailing {
				bottom = ctx.Style.paddingEnds
			}
		}
		row.Append(newInset(section, left, top, right, bottom), false)
		return nil
	}

	if err := appendSection(layout.Left, true, false); err != nil {
		return nil, err
	}
	if len(layout.Center) > 0 {
		row.Append(widget.NewBox(widget.Row, 0, 0), true)
		if err := appendSection(layout.Center, false, false); err != nil {
			return nil, err
		}
		row.Append(widget.NewBox(widget.Row, 0, 0), true)
	}
	if err := appendSection(layout.Right, false, true); err != nil {
		return nil, err
	}
	return row, nil
}

// measureHeight resolves the bar's content-driven height: the natural
// height of the tree at the output's width.
func measureHeight(root widget.Widget, width int) int {
	size := root.Measure(widget.Constraints{Max: widget.Size{W: width, H: 1 << 16}})
	return size.H
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

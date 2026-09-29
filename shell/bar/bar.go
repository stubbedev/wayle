package bar

import (
	"errors"
	"fmt"
	"log"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/brightness"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/upower"
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
	if hyprland.IsRunning() {
		if conn, err := hyprland.Connect(); err == nil {
			baseCtx.Hyprland = conn
		}
	}
	if battery, err := upower.NewSystem(); err == nil {
		defer func() { _ = battery.Close() }()
		baseCtx.Battery = battery
	}
	baseCtx.Brightness = brightness.NewSysfs()
	baseCtx.Pulse = pulse.New()
	if media, err := mpris.NewSession(); err == nil {
		defer func() { _ = media.Close() }()
		baseCtx.Media = media
	}

	outputs := sess.Outputs()
	if len(outputs) == 0 {
		return errors.New("bar: no output to draw on")
	}
	for _, output := range outputs {
		layout, ok := FindLayout(cfg.Bar.Layout, output.Name)
		if !ok || !layout.Show {
			continue
		}
		ctx := baseCtx
		ctx.Connector = output.Name
		lc, err := layerConfigFor(ctx, layout, output.Name, logicalWidth(output.ModeW, output.Scale))
		if err != nil {
			return err
		}
		lc.Output = output
		if _, err := application.NewLayer(*lc); err != nil {
			return err
		}
	}
	return application.Run()
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

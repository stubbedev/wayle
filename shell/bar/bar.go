package bar

import (
	"errors"
	"fmt"
	"log"
	"math"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
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
	theme := widget.DarkTheme()
	widget.SetTheme(theme)

	face, err := app.Font(cfg.General.FontSans, labelPx)
	if err != nil {
		return fmt.Errorf("bar: font %q: %w", cfg.General.FontSans, err)
	}
	font := app.FontFallback(face)
	ctx := ModuleContext{Config: cfg, App: application, Font: font, Theme: theme}

	outputs := sess.Outputs()
	if len(outputs) == 0 {
		return errors.New("bar: no output to draw on")
	}
	for _, output := range outputs {
		layout, ok := FindLayout(cfg.Bar.Layout, output.Name)
		if !ok || !layout.Show {
			continue
		}
		cfg, err := layerConfigFor(ctx, layout, output.Name, logicalWidth(output.ModeW, output.Scale, cfg.Bar.Scale))
		if err != nil {
			return err
		}
		cfg.Output = output
		if _, err := application.NewLayer(*cfg); err != nil {
			return err
		}
	}
	return application.Run()
}

// layerConfigFor measures the tree for one output and maps it onto a
// layer surface config: anchors from the location, the exclusive zone
// from the bar height (the Rust shell's auto exclusive zone), and the
// wayle-bar namespace. The caller pins the output.
func layerConfigFor(ctx ModuleContext, layout config.BarLayout, connector string, width int) (*app.LayerConfig, error) {
	root, err := buildTree(ctx, layout)
	if err != nil {
		return nil, err
	}
	height := measureHeight(root, width)
	return &app.LayerConfig{
		Layer:         LayerFor(ctx.Config.Bar.Layer),
		Anchor:        AnchorsFor(ctx.Config.Bar.Location),
		Height:        uint32(height),
		ExclusiveZone: exclusiveZone(ctx.Config.Bar.Exclusive, height),
		Namespace:     "wayle-bar-" + connector,
		Root:          root,
		Background:    withAlpha(ctx.Theme.Surface, ctx.Config.Bar.BackgroundOpacity),
	}, nil
}

// buildTree assembles one bar's row: the left, center, and right
// sections with expanding fillers around the center. The shared group
// container and per-side padding land with the styling port; the
// padding-ends value is carried by the config until then.
func buildTree(ctx ModuleContext, layout config.BarLayout) (widget.Widget, error) {
	root := widget.NewBox(widget.Row, 0, int(math.Round(ctx.Config.Bar.Padding.Px(labelPx))))
	left, err := CreateAll(layout.Left, ctx)
	if err != nil {
		return nil, err
	}
	center, err := CreateAll(layout.Center, ctx)
	if err != nil {
		return nil, err
	}
	right, err := CreateAll(layout.Right, ctx)
	if err != nil {
		return nil, err
	}
	root.Append(left, false)
	if len(layout.Center) > 0 {
		root.Append(widget.NewBox(widget.Row, 0, 0), true)
		root.Append(center, false)
		root.Append(widget.NewBox(widget.Row, 0, 0), true)
	}
	root.Append(right, false)
	return root, nil
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
// the configured scale; the compositor's fractional-scale preference
// overrides this live once the surface exists.
func logicalWidth(modeW, outputScale int, scaleFactor float64) int {
	scale := outputScale
	if configured := int(scaleFactor); configured > scale {
		scale = configured
	}
	if scale < 1 {
		scale = 1
	}
	return modeW / scale
}

// withAlpha rewrites a color's alpha byte from a 0-100 percentage.
func withAlpha(c render.Color, opacity int) render.Color {
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 100 {
		opacity = 100
	}
	return c&0x00FFFFFF | render.Color(uint8(opacity*255/100))<<24
}

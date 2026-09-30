package bar

import (
	"errors"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/styling"
)

// screenshotModule is the screenshot capture button
// (wayle-bar-hardware/src/modules/screenshot): an icon and an optional
// static label whose bindings run `wayle screenshot region|output|window`
// (in-process through the builtin) by default.
type screenshotModule struct {
	root widget.Widget
}

func newScreenshot(ctx ModuleContext) (Module, error) {
	cfg := ctx.Config.Screenshot
	var label *widget.Label
	if cfg.LabelShow && cfg.Label != "" {
		color := ctx.Style.fg
		if resolved, ok := styling.ResolveColor(cfg.LabelColor, ctx.Style.palette); ok {
			color = resolved
		}
		label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, truncateLabel(cfg.Label, cfg.LabelMaxLength), color)
	}
	icon := moduleIcon(ctx, cfg.Icon)
	switch {
	case icon == nil && label == nil:
		return nil, errors.New("screenshot: neither the icon nor a label is shown")
	case label == nil:
		return &screenshotModule{root: icon}, nil
	}
	return &screenshotModule{root: assembleModule(ctx, icon, label)}, nil
}

func (m *screenshotModule) Root() widget.Widget { return m.root }

// screenshotBuiltin reads a `screenshot [mode [target]]` builtin verb
// the way dropdown_registry.rs's try_screenshot does: the mode defaults
// to region, the target to none. ok is false for any other verb.
func screenshotBuiltin(verb string) (mode, target string, ok bool) {
	parts := strings.Fields(verb)
	if len(parts) == 0 || parts[0] != "screenshot" {
		return "", "", false
	}
	mode = "region"
	if len(parts) > 1 {
		mode = parts[1]
	}
	if len(parts) > 2 {
		target = parts[2]
	}
	return mode, target, true
}

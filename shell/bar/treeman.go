package bar

import (
	"context"
	"strconv"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/treeman"
	"github.com/stubbedev/wayle/styling"
)

// treemanLabel is helpers.rs's format_label: the bucket counts land
// on their placeholders.
func treemanLabel(format string, status *treeman.Status) string {
	out := replaceTemplateVar(format, "total", treemanCount(status.Total))
	out = replaceTemplateVar(out, "stable", treemanCount(status.Stable))
	out = replaceTemplateVar(out, "up", treemanCount(status.Up))
	out = replaceTemplateVar(out, "down", treemanCount(status.Down))
	out = replaceTemplateVar(out, "failed", treemanCount(status.Failed))
	return out
}

func treemanCount(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// treeman is the module: the worktree health aggregate, with the icon
// following the worst bucket.
type treemanModule struct {
	ctx   ModuleContext
	src   treeman.Source
	label *widget.Label
	icon  widget.Widget
	root  widget.Widget
	stop  func()
}

func newTreeman(ctx ModuleContext) (Module, error) {
	m := &treemanModule{ctx: ctx, src: ctx.Treeman, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.Treeman.Icon)
	m.root = assembleModule(ctx, ctx.Config.Treeman.Icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	if ctx.App == nil {
		// Headless construction renders the resting state; no loop to
		// follow events on.
		return m, nil
	}
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.Treeman.Subscribe(context.Background())
	if err != nil {
		return nil, err
	}
	m.stop = stop
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
	}()
	return m, nil
}

// refresh re-reads treeman status and restyles label and icon.
func (m *treemanModule) refresh() error {
	cfg := m.ctx.Config.Treeman
	status, err := m.src.Read(context.Background())
	if err != nil {
		return err
	}
	if status == nil {
		m.label.SetText("")
		return nil
	}
	text := ""
	if cfg.LabelShow {
		text = treemanLabel(cfg.Format, status)
	}
	if cfg.HideIfEmpty && status.Total == 0 {
		text = ""
	}
	m.label.SetText(text)
	m.label.SetColor(treemanColor(cfg, status.WorstBucket(), m.ctx.Style.palette, m.ctx.Style.fg))
	if setter, ok := m.icon.(interface {
		SetThemeName(name string)
	}); ok {
		name := cfg.Icons[config.TreemanBucketStable].Name
		switch status.WorstBucket() {
		case treeman.BucketFailed:
			name = cfg.Icons[config.TreemanBucketFailed].Name
		case treeman.BucketDown:
			name = cfg.Icons[config.TreemanBucketDown].Name
		case treeman.BucketUp:
			name = cfg.Icons[config.TreemanBucketUp].Name
		}
		setter.SetThemeName(name)
	}
	return nil
}

// treemanColor resolves the bucket's configured color, the bar fg
// otherwise.
func treemanColor(cfg config.TreemanConfig, bucket treeman.Bucket, palette *styling.Palette, fallback render.Color) render.Color {
	name := config.TreemanBucketStable
	switch bucket {
	case treeman.BucketFailed:
		name = config.TreemanBucketFailed
	case treeman.BucketDown:
		name = config.TreemanBucketDown
	case treeman.BucketUp:
		name = config.TreemanBucketUp
	}
	color, ok := cfg.Colors[name]
	if !ok {
		return fallback
	}
	if resolved, ok := styling.ResolveColor(color, palette); ok {
		return resolved
	}
	return fallback
}

func (m *treemanModule) Root() widget.Widget { return m.root }

// Stop releases the event subscription.
func (m *treemanModule) Stop() {
	if m.stop != nil {
		m.stop()
	}
}

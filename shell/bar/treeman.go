package bar

import (
	"context"
	"strconv"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/treeman"
)

// treemanLabel is helpers.rs's format_label: plain replacements of
// the exact "{{ name }}" spellings, in order, not a template.
func treemanLabel(format string, status *treeman.Status) string {
	out := strings.ReplaceAll(format, "{{ total }}", treemanCount(status.Total))
	out = strings.ReplaceAll(out, "{{ stable }}", treemanCount(status.Stable))
	out = strings.ReplaceAll(out, "{{ up }}", treemanCount(status.Up))
	out = strings.ReplaceAll(out, "{{ down }}", treemanCount(status.Down))
	return strings.ReplaceAll(out, "{{ failed }}", treemanCount(status.Failed))
}

func treemanCount(v uint32) string { return strconv.FormatUint(uint64(v), 10) }

// treemanButton is the watcher's update: the label ("--" without a
// status), the icon following the worst bucket, the severity class on
// the module root, and whether hide-if-empty collapses it.
type treemanButton struct {
	label, icon, severity string
	visible               bool
}

func treemanDisplay(cfg config.TreemanConfig, status *treeman.Status) treemanButton {
	v := treemanButton{label: "--", icon: cfg.IconName, visible: !cfg.HideIfEmpty}
	if status == nil {
		return v
	}
	v.label = treemanLabel(cfg.Format, status)
	v.visible = !cfg.HideIfEmpty || status.Total > 0
	switch status.WorstBucket() {
	case treeman.BucketFailed:
		v.icon, v.severity = cfg.IconFailed, "failed"
	case treeman.BucketDown:
		v.icon, v.severity = cfg.IconTearingDown, "tearing-down"
	case treeman.BucketUp:
		v.icon, v.severity = cfg.IconPreparing, "preparing"
	}
	return v
}

// treeman is the module: the worktree health aggregate.
type treemanModule struct {
	ctx   ModuleContext
	src   treeman.Source
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget
	stop  func()
	// chrome is the module root the severity class and visibility go
	// on; view is what was last shown, applied when the chrome arrives.
	chrome moduleChrome
	view   treemanButton
}

func newTreeman(ctx ModuleContext) (Module, error) {
	m := &treemanModule{ctx: ctx, src: ctx.Treeman, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.Treeman.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	if ctx.App == nil {
		// Headless construction renders the resting state; no loop to
		// follow events on.
		return m, nil
	}
	ticks, stop, err := ctx.Treeman.Subscribe(ctx.Life())
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

// refresh re-reads the treeman status and shows it.
func (m *treemanModule) refresh() error {
	cfg := m.ctx.Config.Treeman
	status, err := m.src.Read(context.Background())
	if err != nil {
		return err
	}
	v := treemanDisplay(cfg, status)
	text := v.label
	if !cfg.LabelShow {
		text = ""
	}
	m.label.SetText(text)
	if m.icon != nil {
		m.icon.SetThemeName(v.icon)
	}
	m.show(v)
	return nil
}

// show moves the severity class and visibility on the module root.
func (m *treemanModule) show(v treemanButton) {
	prev := m.view.severity
	m.view = v
	if m.chrome == nil {
		return
	}
	m.chrome.SetVisible(v.visible)
	if prev == v.severity {
		return
	}
	if prev != "" {
		m.chrome.RemoveClass(prev)
	}
	if v.severity != "" {
		m.chrome.AddClass(v.severity)
	}
}

func (m *treemanModule) setChrome(c moduleChrome) {
	m.chrome = c
	v := m.view
	m.view.severity = ""
	m.show(v)
}

func (m *treemanModule) Root() widget.Widget { return m.root }

// Stop releases the event subscription.
func (m *treemanModule) Stop() {
	if m.stop != nil {
		m.stop()
	}
}

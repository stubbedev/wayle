package bar

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sysinfo"
)

// sysinfoLabel renders the {{ percent }} template the level modules
// use, zero-padded to two digits like the Rust {:02.0} format.
func sysinfoLabel(format string, percent float64) string {
	padded := strconv.Itoa(int(percent))
	if len(padded) < 2 {
		padded = "0" + padded
	}
	return replaceTemplateVar(format, "percent", padded)
}

// pollModule is the shared poll loop of cpu/ram/storage: read on an
// interval, render the label, icon, and threshold colors.
type pollModule struct {
	ctx    ModuleContext
	cfg    config.SysinfoConfig
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
	read   func() (float64, error)
	cancel context.CancelFunc
}

func newPollModule(ctx ModuleContext, cfg config.SysinfoConfig, read func() (float64, error)) (*pollModule, error) {
	if ctx.App == nil {
		return nil, errors.New("poll: requires the application loop")
	}
	m := &pollModule{ctx: ctx, cfg: cfg, read: read}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, cfg.Icon)
	m.root = assembleModule(ctx, m.icon, m.label)
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if cfg.PollMs > 0 {
		go func() {
			timer := time.NewTimer(0)
			defer timer.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-timer.C:
				}
				if percent, err := m.read(); err == nil {
					m.ctx.Invoke(func() { m.render(percent) })
				}
				timer.Reset(time.Duration(cfg.PollMs) * time.Millisecond)
			}
		}()
	} else if percent, err := read(); err == nil {
		m.render(percent)
	}
	return m, nil
}

// Root is the icon beside the label (the label alone without an icon).
func (m *pollModule) Root() widget.Widget { return m.root }

// Stop ends the poll loop.
func (m *pollModule) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// render is the shared renderer: the format with the zero-padded
// percent, then the threshold colors over the defaults.
func (m *pollModule) render(percent float64) {
	text := ""
	if m.cfg.LabelShow {
		text = sysinfoLabel(m.cfg.Format, percent)
	}
	m.label.SetText(text)
	applyThresholds(m.ctx, percent, m.cfg.Thresholds, m.label, m.icon, m.cfg.Icon.Color)
}

// newCpu builds the cpu module: usage percent from /proc/stat deltas.
func newCpu(ctx ModuleContext) (Module, error) {
	var prev sysinfo.CpuSample
	first := true
	read := func() (float64, error) {
		sample, err := sysinfo.ReadCpuSample()
		if err != nil {
			return 0, err
		}
		if first {
			first = false
			prev = sample
			return 0, nil
		}
		percent := sample.Usage(prev)
		prev = sample
		return percent, nil
	}
	return newPollModule(ctx, ctx.Config.CPU, read)
}

// newRam builds the ram module: used/total from /proc/meminfo.
func newRam(ctx ModuleContext) (Module, error) {
	read := func() (float64, error) {
		mem, err := sysinfo.ReadMemory()
		if err != nil {
			return 0, err
		}
		return mem.UsagePercent(), nil
	}
	return newPollModule(ctx, ctx.Config.RAM, read)
}

// newStorage builds the storage module: used/total on one mount point.
func newStorage(ctx ModuleContext) (Module, error) {
	path := ctx.Config.Storage.Path
	return newPollModule(ctx, ctx.Config.Storage, func() (float64, error) {
		return sysinfo.ReadStoragePercent(path)
	})
}

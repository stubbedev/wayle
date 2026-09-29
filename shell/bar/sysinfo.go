package bar

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/sysinfo"
	"github.com/stubbedev/wayle/styling"
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

// sysinfoThreshold applies the first matching threshold's color.
func sysinfoThreshold(percent float64, thresholds []config.ThresholdEntry, palette *styling.Palette) (render.Color, bool) {
	return thresholdColor(percent, thresholds, palette)
}

// pollModule is the shared poll loop of cpu/ram/storage: read on an
// interval, render through a formatter.
type pollModule struct {
	ctx    ModuleContext
	label  *widget.Label
	read   func() (float64, error)
	render func(m ModuleContext, label *widget.Label, percent float64)
	cancel context.CancelFunc
}

func newPollModule(ctx ModuleContext, pollMs int, read func() (float64, error), render func(ModuleContext, *widget.Label, float64)) (*pollModule, error) {
	if ctx.App == nil {
		return nil, errors.New("poll: requires the application loop")
	}
	m := &pollModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg), read: read, render: render}
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if pollMs > 0 {
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
					m.ctx.Invoke(func() { m.render(m.ctx, m.label, percent) })
				}
				timer.Reset(time.Duration(pollMs) * time.Millisecond)
			}
		}()
	} else if percent, err := read(); err == nil {
		render(ctx, m.label, percent)
	}
	return m, nil
}

func (m *pollModule) Root() widget.Widget { return m.label }

// Stop ends the poll loop.
func (m *pollModule) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// renderSysinfo is the shared label renderer: the format with the
// zero-padded percent, then the threshold color over the default fg.
func renderSysinfo(cfg config.SysinfoConfig, ctx ModuleContext, label *widget.Label, percent float64) {
	text := ""
	if cfg.LabelShow {
		text = sysinfoLabel(cfg.Format, percent)
	}
	label.SetText(text)
	if color, ok := sysinfoThreshold(percent, cfg.Thresholds, ctx.Style.palette); ok {
		label.SetColor(color)
	} else {
		label.SetColor(ctx.Style.fg)
	}
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
	return newPollModule(ctx, ctx.Config.CPU.PollMs, read, func(ctx ModuleContext, label *widget.Label, percent float64) {
		renderSysinfo(ctx.Config.CPU, ctx, label, percent)
	})
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
	return newPollModule(ctx, ctx.Config.RAM.PollMs, read, func(ctx ModuleContext, label *widget.Label, percent float64) {
		renderSysinfo(ctx.Config.RAM, ctx, label, percent)
	})
}

// newStorage builds the storage module: used/total on one mount point.
func newStorage(ctx ModuleContext) (Module, error) {
	path := ctx.Config.Storage.Path
	read := func() (float64, error) {
		return sysinfo.ReadStoragePercent(path)
	}
	return newPollModule(ctx, ctx.Config.Storage.PollMs, read, func(ctx ModuleContext, label *widget.Label, percent float64) {
		renderSysinfo(ctx.Config.Storage, ctx, label, percent)
	})
}

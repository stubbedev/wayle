package bar

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/sysinfo"
)

// pad2 is Rust's "{:02.0}" of an f32: whole number, zero-padded to two.
func pad2(v float32) string {
	s := strconv.FormatFloat(float64(v), 'f', 0, 32)
	if len(s) < 2 {
		s = "0" + s
	}
	return s
}

func fixed(v float64, precision int) string { return strconv.FormatFloat(v, 'f', precision, 64) }

const (
	kib = 1024.0
	mib = kib * 1024
	gib = mib * 1024
	tib = gib * 1024
)

// bytesIn is ByteSize::as_kib/as_mib/... at a Rust "{:.N}" precision:
// the byte columns of the ram, storage, and netstat contexts.
func bytesIn(b uint64, unit float64, precision int) string { return fixed(float64(b)/unit, precision) }

// The ram and storage precisions (helpers.rs gib/tib/mib).
func gibStr(b uint64) string { return bytesIn(b, gib, 1) }
func tibStr(b uint64) string { return bytesIn(b, tib, 2) }
func mibStr(b uint64) string { return bytesIn(b, mib, 0) }

// cpuContext is cpu helpers.rs's format_label context.
func cpuContext(cpu sysinfo.CPU) map[string]any {
	tempC := float32(0)
	if cpu.HasTemperature {
		tempC = cpu.TemperatureC
	}
	return map[string]any{
		"percent":      pad2(cpu.UsagePercent),
		"freq_ghz":     fixed(float64(cpu.BusiestCoreFreqMHz)/1000, 1),
		"avg_freq_ghz": fixed(float64(cpu.AvgFrequencyMHz)/1000, 1),
		"max_freq_ghz": fixed(float64(cpu.MaxFrequencyMHz)/1000, 1),
		"temp_c":       pad2(tempC),
		"temp_f":       pad2(tempC*9/5 + 32),
	}
}

// memoryPercents are the f32 usage figures of polling/memory.rs.
func memoryPercents(m sysinfo.Memory) (usage, swap float32) {
	if m.Total > 0 {
		usage = float32(m.Used()) / float32(m.Total) * 100
	}
	if m.SwapTotal > 0 {
		swap = float32(m.SwapTotal-m.SwapFree) / float32(m.SwapTotal) * 100
	}
	return usage, swap
}

// ramContext is ram helpers.rs's format_label context.
func ramContext(m sysinfo.Memory) map[string]any {
	usage, swap := memoryPercents(m)
	return map[string]any{
		"percent":        pad2(usage),
		"used_gib":       gibStr(m.Used()),
		"total_gib":      gibStr(m.Total),
		"available_gib":  gibStr(m.Available),
		"swap_percent":   pad2(swap),
		"swap_used_gib":  gibStr(m.SwapTotal - m.SwapFree),
		"swap_total_gib": gibStr(m.SwapTotal),
	}
}

// storageContext is storage helpers.rs's format_label context.
func storageContext(s sysinfo.Storage) map[string]any {
	fs := s.Filesystem
	if s.Multiple {
		fs = i18n.T("bar-storage-multiple")
	}
	return map[string]any{
		"percent":  pad2(s.UsagePercent),
		"used_tib": tibStr(s.UsedBytes), "used_gib": gibStr(s.UsedBytes), "used_mib": mibStr(s.UsedBytes), "used_auto": sysinfo.AutoBytes(s.UsedBytes),
		"total_tib": tibStr(s.TotalBytes), "total_gib": gibStr(s.TotalBytes), "total_mib": mibStr(s.TotalBytes), "total_auto": sysinfo.AutoBytes(s.TotalBytes),
		"free_tib": tibStr(s.AvailableBytes), "free_gib": gibStr(s.AvailableBytes), "free_mib": mibStr(s.AvailableBytes), "free_auto": sysinfo.AutoBytes(s.AvailableBytes),
		"filesystem": fs,
	}
}

// reading is one poll: the template context and the value thresholds
// evaluate, or ok false when there is nothing to show (storage with no
// matching mount point keeps its last label).
type reading struct {
	vars  map[string]any
	value float64
	ok    bool
}

// pollModule is the shared poll loop of cpu/ram/storage: read on an
// interval, render the format, and apply the threshold colors.
type pollModule struct {
	buttonRef
	ctx    ModuleContext
	cfg    pollConfig
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
	read   func() (reading, error)
	cancel context.CancelFunc
}

// pollConfig is the keys the cpu, ram, and storage sections share.
type pollConfig struct {
	format     string
	thresholds []config.ThresholdEntry
	icon       config.IconConfig
	pollMs     uint64
}

func newPollModule(ctx ModuleContext, cfg pollConfig, initial string, read func() (reading, error)) (*pollModule, error) {
	if ctx.App == nil {
		return nil, errors.New("poll: requires the application loop")
	}
	m := &pollModule{ctx: ctx, cfg: cfg, read: read}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, initial, ctx.Style.fg)
	m.icon = moduleIcon(ctx, cfg.icon)
	m.root = assembleModule(ctx, m.icon, m.label)
	runCtx, cancel := context.WithCancel(ctx.Life())
	m.cancel = cancel
	if cfg.pollMs == 0 {
		if r, err := read(); err == nil {
			m.render(r)
		}
		return m, nil
	}
	go func() {
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-timer.C:
			}
			if r, err := m.read(); err == nil {
				m.ctx.Invoke(func() { m.render(r) })
			}
			timer.Reset(time.Duration(cfg.pollMs) * time.Millisecond)
		}
	}()
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

// render applies one reading: the rendered format, then the threshold
// colors over the defaults.
func (m *pollModule) render(r reading) {
	if !r.ok {
		return
	}
	m.label.SetText(jinja.RenderOr(m.cfg.format, r.vars))
	m.thresholds(r.value, m.cfg.thresholds)
}

// newCpu builds the cpu module (polling/cpu.rs and helpers.rs).
func newCpu(ctx ModuleContext) (Module, error) {
	c := ctx.Config.CPU
	reader := &sysinfo.CPUReader{Sensor: c.TempSensor}
	read := func() (reading, error) {
		cpu, err := reader.Read()
		if err != nil {
			return reading{}, err
		}
		return reading{vars: cpuContext(cpu), value: float64(cpu.UsagePercent), ok: true}, nil
	}
	initial := jinja.RenderOr(c.Format, cpuContext(sysinfo.CPU{}))
	return newPollModule(ctx, pollConfig{c.Format, c.Thresholds, c.Icon(), c.PollIntervalMs}, initial, read)
}

// newRam builds the ram module (polling/memory.rs and helpers.rs).
func newRam(ctx ModuleContext) (Module, error) {
	c := ctx.Config.RAM
	read := func() (reading, error) {
		mem, err := sysinfo.ReadMemory()
		if err != nil {
			return reading{}, err
		}
		usage, _ := memoryPercents(mem)
		return reading{vars: ramContext(mem), value: float64(usage), ok: true}, nil
	}
	initial := jinja.RenderOr(c.Format, ramContext(sysinfo.Memory{}))
	return newPollModule(ctx, pollConfig{c.Format, c.Thresholds, c.Icon(), c.PollIntervalMs}, initial, read)
}

// newStorage builds the storage module: the configured mount points
// together, "--" until one matches.
func newStorage(ctx ModuleContext) (Module, error) {
	c := ctx.Config.Storage
	paths := c.MountPoint.Paths
	read := func() (reading, error) {
		s, ok := sysinfo.AggregateStorage(sysinfo.Disks(), paths)
		if !ok {
			return reading{}, nil
		}
		return reading{vars: storageContext(s), value: float64(s.UsagePercent), ok: true}, nil
	}
	return newPollModule(ctx, pollConfig{c.Format, c.Thresholds, c.Icon(), c.PollIntervalMs}, "--", read)
}

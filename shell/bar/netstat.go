package bar

import (
	"context"
	"errors"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/jinja"
	"github.com/stubbedev/wayle/service/sysinfo"
)

// netstatLabel is helpers.rs's format_label over one interface's
// rates (kib "{:.0}", mib "{:.1}", gib "{:.2}", and the bytesize display).
func netstatLabel(format string, rate sysinfo.Rate) string {
	return jinja.RenderOr(format, map[string]any{
		"down_kib": bytesIn(rate.RxPerSec, kib, 0), "down_mib": bytesIn(rate.RxPerSec, mib, 1),
		"down_gib": bytesIn(rate.RxPerSec, gib, 2), "down_auto": sysinfo.AutoBytes(rate.RxPerSec),
		"up_kib": bytesIn(rate.TxPerSec, kib, 0), "up_mib": bytesIn(rate.TxPerSec, mib, 1),
		"up_gib": bytesIn(rate.TxPerSec, gib, 2), "up_auto": sysinfo.AutoBytes(rate.TxPerSec),
		"interface": rate.Interface,
	})
}

// netstat is the module: per-interface traffic rates.
type netstatModule struct {
	ctx     ModuleContext
	label   *widget.Label
	cancel  context.CancelFunc
	prev    map[string]sysinfo.NetTotals
	current sysinfo.Rate
}

func newNetstat(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("netstat: requires the application loop")
	}
	cfg := ctx.Config.Netstat
	m := &netstatModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	runCtx, cancel := context.WithCancel(ctx.Life())
	m.cancel = cancel
	if int(cfg.PollIntervalMs) > 0 {
		go func() {
			interval := time.Duration(int(cfg.PollIntervalMs)) * time.Millisecond
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
				}
				m.sample(interval)
			}
		}()
	}
	return m, nil
}

// sample reads the counters and re-renders; the first pass primes the
// previous snapshot without a rate.
func (m *netstatModule) sample(elapsed time.Duration) {
	cfg := m.ctx.Config.Netstat
	totals, err := sysinfo.ReadNetTotals()
	if err != nil {
		return
	}
	name, ok := sysinfo.SelectInterface(totals, cfg.Interface)
	if !ok {
		m.ctx.Invoke(func() { m.label.SetText("") })
		return
	}
	now := totals[name]
	prev, seen := m.prev[name]
	m.prev = totals
	if !seen {
		m.ctx.Invoke(func() { m.label.SetText("") })
		return
	}
	rate, err := sysinfo.NetRate(name, prev, now, elapsed.Seconds())
	if err != nil {
		return
	}
	m.current = rate
	m.ctx.Invoke(func() {
		text := ""
		if cfg.LabelShow {
			text = netstatLabel(cfg.Format, rate)
		}
		m.label.SetText(text)
	})
}

func (m *netstatModule) Root() widget.Widget { return m.label }

// Stop ends the sampling loop.
func (m *netstatModule) Stop() { m.cancel() }

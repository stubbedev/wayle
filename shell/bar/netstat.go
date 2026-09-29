package bar

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/service/sysinfo"
)

// netstatLabel is helpers.rs's format_label: nine placeholders over
// one interface's rates.
func netstatLabel(format string, rate sysinfo.Rate) string {
	out := replaceTemplateVar(format, "down_kib", byteKib(rate.RxPerSec))
	out = replaceTemplateVar(out, "down_mib", byteMib(rate.RxPerSec))
	out = replaceTemplateVar(out, "down_gib", byteGib(rate.RxPerSec))
	out = replaceTemplateVar(out, "down_auto", sysinfo.AutoBytes(rate.RxPerSec))
	out = replaceTemplateVar(out, "up_kib", byteKib(rate.TxPerSec))
	out = replaceTemplateVar(out, "up_mib", byteMib(rate.TxPerSec))
	out = replaceTemplateVar(out, "up_gib", byteGib(rate.TxPerSec))
	out = replaceTemplateVar(out, "up_auto", sysinfo.AutoBytes(rate.TxPerSec))
	out = replaceTemplateVar(out, "interface", rate.Interface)
	return out
}

// byteKib is the bytesize crate's {:.0} KiB render.
func byteKib(bytes uint64) string {
	const kib = 1024
	return strconv.FormatUint(bytes/kib, 10)
}

// byteMib is {:.1} MiB; byteGib is {:.2} GiB.
func byteMib(bytes uint64) string {
	return floatDiv(bytes, 1024*1024, 1)
}

func byteGib(bytes uint64) string {
	return floatDiv(bytes, 1024*1024*1024, 2)
}

func floatDiv(bytes uint64, divisor float64, precision int) string {
	return strconv.FormatFloat(float64(bytes)/divisor, 'f', precision, 64)
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
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	if cfg.PollMs > 0 {
		go func() {
			interval := time.Duration(cfg.PollMs) * time.Millisecond
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

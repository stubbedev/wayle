package bar

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/strftime"
)

// worldClockRender is helpers.rs's format_world_clock: every
// {{ tz('Zone', 'strftime') }} renders the current instant in that
// zone; plain text passes through; a bad zone renders empty (the Rust
// warns and formats to "").
func worldClockRender(format string, now time.Time) string {
	var out strings.Builder
	rest := format
	for {
		start := strings.Index(rest, "{{")
		if start < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end := strings.Index(rest[start:], "}}")
		if end < 0 {
			out.WriteString(rest)
			return out.String()
		}
		end += start
		out.WriteString(rest[:start])
		call := strings.TrimSpace(rest[start+2 : end])
		if zoneID, layout, ok := parseTzCall(call); ok {
			out.WriteString(renderTz(zoneID, layout, now))
		}
		rest = rest[end+2:]
	}
}

// parseTzCall reads tz('Zone', 'strftime'); anything else is not a
// recognized call and renders as nothing.
func parseTzCall(call string) (string, string, bool) {
	const prefix = "tz("
	if !strings.HasPrefix(call, prefix) || !strings.HasSuffix(call, ")") {
		return "", "", false
	}
	inner := call[len(prefix) : len(call)-1]
	zone, args, found := strings.Cut(inner, ",")
	if !found {
		return "", "", false
	}
	zoneID, ok := singleQuoted(zone)
	if !ok {
		return "", "", false
	}
	layout, ok := singleQuoted(args)
	return zoneID, layout, ok
}

// singleQuoted extracts the '…' span of one argument.
func singleQuoted(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < 2 || arg[0] != '\'' {
		return "", false
	}
	end := strings.IndexByte(arg[1:], '\'')
	if end < 0 {
		return "", false
	}
	return arg[1 : 1+end], true
}

// renderTz formats now in the zone; an unknown zone renders empty.
func renderTz(zoneID, layout string, now time.Time) string {
	location, err := time.LoadLocation(zoneID)
	if err != nil {
		return ""
	}
	formatted, err := strftime.Compile(layout)
	if err != nil {
		return ""
	}
	return formatted.Format(now.In(location))
}

// worldClock is the module: the format re-renders every second.
type worldClockModule struct {
	ctx   ModuleContext
	label *widget.Label
	stop  func()
}

func newWorldClock(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("world-clock: requires the application loop")
	}
	cfg := ctx.Config.WorldClock
	m := &worldClockModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.apply(cfg.Format)
	runCtx, cancel := context.WithCancel(ctx.Life())
	m.stop = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
			m.ctx.Invoke(func() { m.apply(m.ctx.Config.WorldClock.Format) })
		}
	}()
	return m, nil
}

// apply renders the current format; an unchanged label is a no-op for
// the damage collector.
func (m *worldClockModule) apply(format string) {
	text := ""
	if m.ctx.Config.WorldClock.LabelShow {
		text = worldClockRender(format, time.Now())
	}
	m.label.SetText(text)
}

func (m *worldClockModule) Root() widget.Widget {
	return assembleModule(m.ctx, moduleIcon(m.ctx, m.ctx.Config.WorldClock.Icon()), m.label)
}

// Stop ends the render ticker.
func (m *worldClockModule) Stop() { m.stop() }

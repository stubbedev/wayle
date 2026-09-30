package main

import (
	"fmt"
	"strings"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/rustparse"
	"github.com/stubbedev/wayle/service/idleinhibit"
)

// idleCommand is wayle/src/cli/idle/commands.rs.
func idleCommand() *cli.Command {
	adjust := []*cli.Arg{{ID: "value", ValueName: "VALUE", Required: true, Help: "+N to add, -N to subtract, N to set absolute"}}
	return &cli.Command{
		Name:  "idle",
		About: "Idle inhibit control commands",
		Subcommands: []*cli.Command{
			{
				Name:  "on",
				About: "Enable idle inhibition",
				Args: []*cli.Arg{
					{ID: "minutes", ValueName: "MINUTES", Value: cli.U32, Help: "Duration in minutes (omit to use default duration)"},
					{ID: "indefinite", Short: 'i', Long: "indefinite", Help: "Force indefinite mode (ignore default duration)"},
				},
				Run: withIdle(idleOn),
			},
			{Name: "off", About: "Disable idle inhibition", Run: withIdle(idleOff)},
			{Name: "duration", About: "Adjust timer duration (upper limit)", AllowHyphenValues: true, Args: adjust, Run: withIdle(idleAdjuster{"duration", "Duration"}.run)},
			{Name: "remaining", About: "Adjust remaining time on active timer", AllowHyphenValues: true, Args: adjust, Run: withIdle(idleAdjuster{"remaining", "Remaining"}.run)},
			{Name: "status", About: "Show current idle inhibit status", Run: withIdle(idleStatus)},
			{
				Name:  "toggle",
				About: "Toggle idle inhibition on/off",
				Args: []*cli.Arg{
					{ID: "indefinite", Short: 'i', Long: "indefinite", Help: "Use indefinite mode when enabling"},
				},
				Run: withIdle(idleToggle),
			},
		},
	}
}

func withIdle(run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return withDaemon("IdleInhibit", idleinhibit.ServiceName, idleinhibit.ServicePath, idleinhibit.ServiceName, run)
}

// idleEnable is on.rs / toggle.rs's enable tail: the duration read is
// best-effort, as Rust's unwrap_or(0).
func idleEnable(m *cli.Matches, p *daemonProxy, indefinite bool) error {
	if err := p.call("enable idle inhibit", "Enable", nil, indefinite); err != nil {
		return err
	}
	var duration uint32
	_ = p.prop("get duration", "Duration", &duration)
	if indefinite || duration == 0 {
		fmt.Fprintln(m.Stdout(), "Enabled (indefinite)")
	} else {
		fmt.Fprintf(m.Stdout(), "Enabled for %d minutes\n", duration)
	}
	return nil
}

func idleOn(m *cli.Matches, p *daemonProxy) error {
	if minutes, ok := cli.Value[uint32](m, "minutes"); ok {
		if err := p.call("set duration", "SetDuration", nil, minutes); err != nil {
			return err
		}
	}
	return idleEnable(m, p, m.Flag("indefinite"))
}

func idleOff(m *cli.Matches, p *daemonProxy) error {
	if err := p.call("disable idle inhibit", "Disable", nil); err != nil {
		return err
	}
	fmt.Fprintln(m.Stdout(), "Disabled")
	return nil
}

func idleToggle(m *cli.Matches, p *daemonProxy) error {
	var active bool
	if err := p.prop("get active state", "Active", &active); err != nil {
		return err
	}
	if active {
		return idleOff(m, p)
	}
	return idleEnable(m, p, m.Flag("indefinite"))
}

// idleAdjuster is duration.rs / remaining.rs: "+N" adds, "-N"
// subtracts, a bare N sets.
type idleAdjuster struct {
	noun   string // "duration" / "remaining"
	member string // "Duration" / "Remaining"
}

func (a idleAdjuster) run(m *cli.Matches, p *daemonProxy) error {
	raw, _ := cli.Value[string](m, "value")
	value := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-"):
		digits := value
		if value[0] == '+' {
			digits = value[1:]
		}
		delta, err := rustparse.Int(digits, 32)
		if err != nil {
			return cliMessage("Invalid delta: " + value)
		}
		if err := p.call("adjust "+a.noun, "Adjust"+a.member, nil, int32(delta)); err != nil {
			return err
		}
		if value[0] == '+' {
			fmt.Fprintf(m.Stdout(), "Added %d minutes to %s\n", delta, a.noun)
		} else {
			fmt.Fprintf(m.Stdout(), "Subtracted %d minutes from %s\n", -delta, a.noun)
		}
	default:
		minutes, err := rustparse.Uint(value, 32)
		if err != nil {
			return cliMessage("Invalid minutes: " + value)
		}
		if err := p.call("set "+a.noun, "Set"+a.member, nil, uint32(minutes)); err != nil {
			return err
		}
		fmt.Fprintf(m.Stdout(), "Set %s to %d minutes\n", a.noun, minutes)
	}
	return nil
}

func idleStatus(m *cli.Matches, p *daemonProxy) error {
	var active bool
	var duration uint32
	if err := p.prop("get active state", "Active", &active); err != nil {
		return err
	}
	if err := p.prop("get duration", "Duration", &duration); err != nil {
		return err
	}
	if !active {
		label := "indefinite"
		if duration != 0 {
			label = fmt.Sprintf("%d min", duration)
		}
		fmt.Fprintf(m.Stdout(), "Inactive (duration: %s)\n", label)
		return nil
	}
	if duration == 0 {
		fmt.Fprintln(m.Stdout(), "Active (indefinite)")
		return nil
	}
	var remaining uint32
	if err := p.prop("get remaining", "Remaining", &remaining); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Active (%d:%02d remaining, %d min duration)\n", remaining/60, remaining%60, duration)
	return nil
}

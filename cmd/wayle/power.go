package main

import (
	"fmt"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/powerprofiles"
)

// powerCommand is wayle/src/cli/power/commands.rs.
func powerCommand() *cli.Command {
	return &cli.Command{
		Name:  "power",
		About: "Power profile commands",
		Subcommands: []*cli.Command{
			{Name: "status", About: "Show current power profile", Run: withPower(powerStatus)},
			{
				Name:  "set",
				About: "Set power profile",
				Args: []*cli.Arg{
					{ID: "profile", ValueName: "PROFILE", Required: true, Help: "Profile name (power-saver, balanced, performance)"},
				},
				Run: withPower(powerSet),
			},
			{Name: "cycle", About: "Cycle to next power profile", Run: withPower(powerCycle)},
			{Name: "list", About: "List available power profiles", Run: withPower(powerList)},
		},
	}
}

// withPower is power/proxy.rs's connect around a handler.
func withPower(run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return withDaemon("Power profiles", powerprofiles.ServiceName, powerprofiles.ServicePath, powerprofiles.ServiceName, run)
}

func powerStatus(m *cli.Matches, p *daemonProxy) error {
	var active, degraded string
	if err := p.prop("get active profile", "ActiveProfile", &active); err != nil {
		return err
	}
	if err := p.prop("get degradation status", "PerformanceDegraded", &degraded); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Active profile: %s\n", active)
	if degraded != "" {
		fmt.Fprintf(m.Stdout(), "Performance degraded: %s\n", degraded)
	}
	return nil
}

func powerSet(m *cli.Matches, p *daemonProxy) error {
	profile, _ := cli.Value[string](m, "profile")
	if err := p.call("set profile", "SetProfile", nil, profile); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Profile set to: %s\n", profile)
	return nil
}

func powerCycle(m *cli.Matches, p *daemonProxy) error {
	if err := p.call("cycle profile", "Cycle", nil); err != nil {
		return err
	}
	var active string
	if err := p.prop("get active profile", "ActiveProfile", &active); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Profile: %s\n", active)
	return nil
}

func powerList(m *cli.Matches, p *daemonProxy) error {
	var profiles []string
	if err := p.call("list profiles", "ListProfiles", []any{&profiles}); err != nil {
		return err
	}
	var active string
	if err := p.prop("get active profile", "ActiveProfile", &active); err != nil {
		return err
	}
	fmt.Fprintln(m.Stdout(), "Available profiles:")
	for _, profile := range profiles {
		marker := ""
		if profile == active {
			marker = " *"
		}
		fmt.Fprintf(m.Stdout(), "  %s%s\n", profile, marker)
	}
	return nil
}

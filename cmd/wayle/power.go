package main

import "github.com/stubbedev/wayle/internal/cli"

// powerCommand is wayle/src/cli/power/commands.rs.
func powerCommand() *cli.Command {
	return &cli.Command{
		Name:  "power",
		About: "Power profile commands",
		Subcommands: []*cli.Command{
			{Name: "status", About: "Show current power profile", Run: notPorted("power status")},
			{
				Name:  "set",
				About: "Set power profile",
				Args: []*cli.Arg{
					{ID: "profile", ValueName: "PROFILE", Required: true, Help: "Profile name (power-saver, balanced, performance)"},
				},
				Run: notPorted("power set"),
			},
			{Name: "cycle", About: "Cycle to next power profile", Run: notPorted("power cycle")},
			{Name: "list", About: "List available power profiles", Run: notPorted("power list")},
		},
	}
}

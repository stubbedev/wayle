package main

import "github.com/stubbedev/wayle/internal/cli"

// systrayCommand is wayle/src/cli/systray/commands.rs.
func systrayCommand() *cli.Command {
	return &cli.Command{
		Name:  "systray",
		About: "System tray commands",
		Subcommands: []*cli.Command{
			{Name: "list", About: "List all system tray items", Run: notPorted("systray list")},
			{
				Name:  "activate",
				About: "Activate a tray item by ID",
				Args: []*cli.Arg{
					{ID: "id", ValueName: "ID", Required: true, Help: "Tray item ID to activate"},
				},
				Run: notPorted("systray activate"),
			},
			{Name: "status", About: "Show system tray status", Run: notPorted("systray status")},
		},
	}
}

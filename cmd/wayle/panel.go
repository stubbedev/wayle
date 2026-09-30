package main

import "github.com/stubbedev/wayle/internal/cli"

// panelCommand is wayle/src/cli/panel/commands.rs.
func panelCommand() *cli.Command {
	monitor := func(verb string) []*cli.Arg {
		return []*cli.Arg{{ID: "monitor", Help: `Monitor connector name (e.g., "DP-1"). Omit to ` + verb + " all"}}
	}
	return &cli.Command{
		Name:  "panel",
		About: "Panel management commands",
		Subcommands: []*cli.Command{
			{Name: "start", About: "Start the panel daemon", Run: notPorted("panel start")},
			{Name: "stop", About: "Stop the panel daemon", Run: notPorted("panel stop")},
			{Name: "restart", About: "Restart the panel daemon", Run: notPorted("panel restart")},
			{Name: "status", About: "Check panel status", Run: notPorted("panel status")},
			{Name: "settings", About: "Open panel settings", Run: notPorted("panel settings")},
			{Name: "inspect", About: "Open GTK Inspector for debugging", Run: notPorted("panel inspect")},
			{Name: "hide", About: "Hide the bar on a monitor", Args: monitor("hide"), Run: notPorted("panel hide")},
			{Name: "show", About: "Show the bar on a monitor", Args: monitor("show"), Run: notPorted("panel show")},
			{Name: "toggle", About: "Toggle bar visibility on a monitor", Args: monitor("toggle"), Run: notPorted("panel toggle")},
		},
	}
}

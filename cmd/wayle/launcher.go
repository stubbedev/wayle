package main

import "github.com/stubbedev/wayle/internal/cli"

// launcherCommand is app.rs's Launcher: every argument, flags
// included, passes through raw to the rofi-compatible parser.
func launcherCommand() *cli.Command {
	return &cli.Command{
		Name:            "launcher",
		About:           "Application launcher / dmenu with rofi-compatible flags",
		DisableHelpFlag: true,
		Args: []*cli.Arg{
			{ID: "args", Multiple: true, TrailingVarArg: true, Help: "Raw rofi-style args (single-dash long flags, e.g. `-show drun`)"},
		},
		Run: notPorted("launcher"),
	}
}

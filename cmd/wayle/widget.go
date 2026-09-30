package main

import "github.com/stubbedev/wayle/internal/cli"

// widgetCommand is wayle/src/cli/widget/commands.rs.
func widgetCommand() *cli.Command {
	return &cli.Command{
		Name:  "widget",
		About: "Widget control commands",
		Subcommands: []*cli.Command{
			{
				Name:      "update",
				About:     "Push an output update to a bar widget by its config id",
				LongAbout: "Push an output update to a bar widget by its config id.\n\nThe output is interpreted exactly like the widget's own command output: plain text, or JSON with `text`/`alt`/`percentage`/`class`/`tooltip` fields (driving icon/color cycling).",
				Args: []*cli.Arg{
					{ID: "id", Required: true, Help: "Config id of the target widget (e.g. a custom module's `id`)"},
					{ID: "output", Required: true, Help: "Output payload (plain text, or a JSON object)"},
				},
				Run: notPorted("widget update"),
			},
		},
	}
}

package main

import (
	"context"
	"time"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/widgetipc"
)

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
				Run: runWidgetUpdate,
			},
		},
	}
}

// socketTimeout bounds one widget-socket exchange.
const socketTimeout = 5 * time.Second

// runWidgetUpdate is widget/update.rs.
func runWidgetUpdate(m *cli.Matches) error {
	id, _ := cli.Value[string](m, "id")
	output, _ := cli.Value[string](m, "output")
	ctx, cancel := context.WithTimeout(context.Background(), socketTimeout)
	defer cancel()
	return widgetipc.SendWidgetUpdate(ctx, id, output)
}

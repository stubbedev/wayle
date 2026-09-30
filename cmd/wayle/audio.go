package main

import "github.com/stubbedev/wayle/internal/cli"

// audioCommand is wayle/src/cli/audio/commands.rs.
func audioCommand() *cli.Command {
	level := func() []*cli.Arg {
		return []*cli.Arg{{ID: "level", ValueName: "LEVEL", Help: "Volume level (0-100) or relative adjustment (+5, -10)"}}
	}
	return &cli.Command{
		Name:  "audio",
		About: "Audio control commands",
		Subcommands: []*cli.Command{
			{Name: "output-volume", About: "Get or set output volume level", AllowHyphenValues: true, Args: level(), Run: notPorted("audio output-volume")},
			{Name: "output-mute", About: "Toggle output mute state", Run: notPorted("audio output-mute")},
			{Name: "input-volume", About: "Get or set input volume level", AllowHyphenValues: true, Args: level(), Run: notPorted("audio input-volume")},
			{Name: "input-mute", About: "Toggle input mute state", Run: notPorted("audio input-mute")},
			{Name: "sinks", About: "List available audio sinks (outputs)", Run: notPorted("audio sinks")},
			{Name: "sources", About: "List available audio sources (inputs)", Run: notPorted("audio sources")},
			{Name: "status", About: "Show current audio status", Run: notPorted("audio status")},
		},
	}
}

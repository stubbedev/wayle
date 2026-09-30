package main

import "github.com/stubbedev/wayle/internal/cli"

// mediaCommand is wayle/src/cli/media/commands.rs.
func mediaCommand() *cli.Command {
	player := func(help string) *cli.Arg {
		return &cli.Arg{ID: "player", ValueName: "PLAYER_ID", Help: help}
	}
	const playerHelp = "Player identifier (number or partial name match)"
	return &cli.Command{
		Name:  "media",
		About: "Media player control commands",
		Subcommands: []*cli.Command{
			{Name: "list", About: "List all available media players", Run: notPorted("media list")},
			{Name: "play-pause", About: "Toggle play/pause for a media player", Args: []*cli.Arg{player(playerHelp)}, Run: notPorted("media play-pause")},
			{Name: "next", About: "Skip to next track", Args: []*cli.Arg{player(playerHelp)}, Run: notPorted("media next")},
			{Name: "previous", About: "Go to previous track", Args: []*cli.Arg{player(playerHelp)}, Run: notPorted("media previous")},
			{
				Name:  "shuffle",
				About: "Toggle or set shuffle mode",
				Args: []*cli.Arg{
					{ID: "state", ValueName: "SHUFFLE_STATE", Help: "Shuffle state", Value: cli.Enum(
						cli.PossibleValue{Name: "on", Help: "Enable shuffle"},
						cli.PossibleValue{Name: "off", Help: "Disable shuffle"},
						cli.PossibleValue{Name: "toggle", Help: "Toggle shuffle state"},
					)},
					player(playerHelp),
				},
				Run: notPorted("media shuffle"),
			},
			{
				Name:  "loop",
				About: "Set loop/repeat mode",
				Args: []*cli.Arg{
					{ID: "mode", Required: true, Help: "Loop mode", Value: cli.Enum(
						cli.PossibleValue{Name: "none", Help: "No looping"},
						cli.PossibleValue{Name: "track", Help: "Loop current track"},
						cli.PossibleValue{Name: "playlist", Help: "Loop entire playlist"},
					)},
					player(playerHelp),
				},
				Run: notPorted("media loop"),
			},
			{Name: "active", About: "Get or set the active media player", Args: []*cli.Arg{player("Player to set as active (number or partial name match)")}, Run: notPorted("media active")},
			{Name: "info", About: "Display detailed information about a media player", Args: []*cli.Arg{player(playerHelp)}, Run: notPorted("media info")},
		},
	}
}

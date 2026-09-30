package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/rustparse"
	"github.com/stubbedev/wayle/service/mpris"
)

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
			{Name: "list", About: "List all available media players", Run: withMedia(mediaList)},
			{Name: "play-pause", About: "Toggle play/pause for a media player", Args: []*cli.Arg{player(playerHelp)}, Run: withMedia(mediaTransport("toggle play/pause", "PlayPause"))},
			{Name: "next", About: "Skip to next track", Args: []*cli.Arg{player(playerHelp)}, Run: withMedia(mediaTransport("skip to next track", "Next"))},
			{Name: "previous", About: "Go to previous track", Args: []*cli.Arg{player(playerHelp)}, Run: withMedia(mediaTransport("go to previous track", "Previous"))},
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
				Run: withMedia(mediaShuffle),
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
				Run: withMedia(mediaLoop),
			},
			{Name: "active", About: "Get or set the active media player", Args: []*cli.Arg{player("Player to set as active (number or partial name match)")}, Run: withMedia(mediaActive)},
			{Name: "info", About: "Display detailed information about a media player", Args: []*cli.Arg{player(playerHelp)}, Run: withMedia(mediaInfo)},
		},
	}
}

func withMedia(run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return withDaemon("Media", mpris.DaemonName, mpris.DaemonPath, mpris.DaemonName, run)
}

func listPlayers(p *daemonProxy) ([]mpris.PlayerRow, error) {
	var players []mpris.PlayerRow
	err := p.call("list players", "ListPlayers", []any{&players})
	return players, err
}

// resolvePlayer is media/resolve.rs: a 1-based index or a
// case-insensitive substring of the id or identity; "" or absent means
// the active player.
func resolvePlayer(p *daemonProxy, input string) (string, error) {
	if input == "" {
		return "", nil
	}
	players, err := listPlayers(p)
	if err != nil {
		return "", err
	}
	if len(players) == 0 {
		return "", cliMessage("No media players available")
	}
	if index, err := rustparse.Uint(input, 64); err == nil {
		if index == 0 {
			return "", cliMessage("Player numbers start at 1")
		}
		if index <= uint64(len(players)) {
			return players[index-1].ID, nil
		}
		return "", cliMessage(fmt.Sprintf("Player %d not found (available: 1-%d)", index, len(players)))
	}
	needle := strings.ToLower(input)
	for _, pl := range players {
		if strings.Contains(strings.ToLower(pl.ID), needle) || strings.Contains(strings.ToLower(pl.Identity), needle) {
			return pl.ID, nil
		}
	}
	available := make([]string, len(players))
	for i, pl := range players {
		available[i] = fmt.Sprintf("%d. %s", i+1, pl.Identity)
	}
	return "", cliMessage(fmt.Sprintf("No player matching '%s'\nAvailable players:\n  %s", input, strings.Join(available, "\n  ")))
}

func playerArg(m *cli.Matches) string {
	player, _ := cli.Value[string](m, "player")
	return player
}

func mediaList(m *cli.Matches, p *daemonProxy) error {
	players, err := listPlayers(p)
	if err != nil {
		return err
	}
	if len(players) == 0 {
		fmt.Fprintln(m.Stdout(), "No media players found")
		return nil
	}
	fmt.Fprintln(m.Stdout(), "Available media players:")
	for i, pl := range players {
		status := "Stopped"
		if pl.State == "Playing" || pl.State == "Paused" {
			status = pl.State
		}
		fmt.Fprintf(m.Stdout(), "  %d. %s (%s) [%s]\n", i+1, pl.Identity, pl.ID, status)
	}
	return nil
}

// mediaTransport is play_pause.rs / next.rs / previous.rs.
func mediaTransport(op, method string) func(*cli.Matches, *daemonProxy) error {
	return func(m *cli.Matches, p *daemonProxy) error {
		player, err := resolvePlayer(p, playerArg(m))
		if err != nil {
			return err
		}
		return p.call(op, method, nil, player)
	}
}

func mediaShuffle(m *cli.Matches, p *daemonProxy) error {
	player, err := resolvePlayer(p, playerArg(m))
	if err != nil {
		return err
	}
	state, ok := cli.Value[string](m, "state")
	if !ok {
		state = "toggle"
	}
	return p.call("set shuffle", "SetShuffle", nil, player, state)
}

func mediaLoop(m *cli.Matches, p *daemonProxy) error {
	player, err := resolvePlayer(p, playerArg(m))
	if err != nil {
		return err
	}
	mode, _ := cli.Value[string](m, "mode")
	if err := p.call("set loop mode", "SetLoopStatus", nil, player, mode); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Loop mode set to %s\n", mode)
	return nil
}

func playerInfo(p *daemonProxy, player string) (map[string]string, error) {
	var info map[string]string
	err := p.call("get player info", "GetPlayerInfo", []any{&info}, player)
	return info, err
}

func infoOr(info map[string]string, key, fallback string) string {
	if v, ok := info[key]; ok {
		return v
	}
	return fallback
}

func mediaActive(m *cli.Matches, p *daemonProxy) error {
	if input, set := cli.Value[string](m, "player"); set {
		player, err := resolvePlayer(p, input)
		if err != nil {
			return err
		}
		if err := p.call("set active player", "SetActivePlayer", nil, player); err != nil {
			return err
		}
		fmt.Fprintf(m.Stdout(), "Set active player to: %s\n", player)
		return nil
	}
	var active string
	if err := p.call("get active player", "GetActivePlayer", []any{&active}); err != nil {
		return err
	}
	if active == "" {
		fmt.Fprintln(m.Stdout(), "No active player set")
		return nil
	}
	info, err := playerInfo(p, active)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Active player: %s - %s [%s]\n",
		infoOr(info, "identity", "Unknown"), infoOr(info, "title", "Unknown"), infoOr(info, "playback_state", "Unknown"))
	return nil
}

// mediaInfo is info.rs.
func mediaInfo(m *cli.Matches, p *daemonProxy) error {
	player, err := resolvePlayer(p, playerArg(m))
	if err != nil {
		return err
	}
	info, err := playerInfo(p, player)
	if err != nil {
		return err
	}
	lines := []string{
		"Player: " + infoOr(info, "identity", "Unknown"),
		"Status: " + infoOr(info, "playback_state", "Unknown"),
		"Title: " + infoOr(info, "title", "Unknown"),
		"Artist: " + infoOr(info, "artist", "Unknown"),
		"Album: " + infoOr(info, "album", "Unknown"),
	}
	if raw, ok := info["length_us"]; ok {
		if us, err := strconv.ParseUint(raw, 10, 64); err == nil {
			secs := us / 1_000_000
			lines = append(lines, fmt.Sprintf("Length: %02d:%02d", secs/60, secs%60))
		}
	}
	lines = append(lines,
		"Volume: "+infoOr(info, "volume", "0")+"%",
		"Shuffle: "+infoOr(info, "shuffle_mode", "Unknown"),
		"Loop: "+infoOr(info, "loop_mode", "Unknown"),
	)
	var caps []string
	for _, c := range []struct{ key, name string }{{"can_seek", "Seek"}, {"can_go_next", "Next"}, {"can_go_previous", "Previous"}} {
		if info[c.key] == "true" {
			caps = append(caps, c.name)
		}
	}
	if len(caps) > 0 {
		lines = append(lines, "Capabilities: "+strings.Join(caps, ", "))
	}
	fmt.Fprintln(m.Stdout(), strings.Join(lines, "\n"))
	return nil
}

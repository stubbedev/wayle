package mpris

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Wayle's own interface over the media service, which `wayle media`
// drives (wayle-media/src/dbus).
const (
	DaemonName = "com.wayle.Media1"
	DaemonPath = dbus.ObjectPath("/com/wayle/Media")
)

const daemonTimeout = 25 * time.Second

// Daemon is the com.wayle.Media1 object (MediaDaemon).
type Daemon struct {
	c *Controller
}

func daemonCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), daemonTimeout)
}

// resolve is resolve_player: "" is the active player.
func (d *Daemon) resolve(player string) (string, *dbus.Error) {
	if player != "" {
		return player, nil
	}
	if active := d.c.Active(); active != "" {
		return active, nil
	}
	return "", dbusx.Failed("No active player")
}

func failed(err error) *dbus.Error {
	if err == nil {
		return nil
	}
	return dbusx.Failed(err.Error())
}

func (d *Daemon) control(player string, run func(context.Context, string) error) *dbus.Error {
	name, derr := d.resolve(player)
	if derr != nil {
		return derr
	}
	ctx, cancel := daemonCtx()
	defer cancel()
	return failed(run(ctx, name))
}

// PlayPause toggles playback.
func (d *Daemon) PlayPause(player string) *dbus.Error { return d.control(player, d.c.PlayPause) }

// Next skips forward.
func (d *Daemon) Next(player string) *dbus.Error { return d.control(player, d.c.Next) }

// Previous skips back.
func (d *Daemon) Previous(player string) *dbus.Error { return d.control(player, d.c.Previous) }

// Seek moves to positionUS within the track.
func (d *Daemon) Seek(player string, positionUS int64) *dbus.Error {
	return d.control(player, func(ctx context.Context, name string) error {
		return d.c.Seek(ctx, name, positionUS)
	})
}

// SetShuffle takes on, off, or toggle (any case).
func (d *Daemon) SetShuffle(player, state string) *dbus.Error {
	name, derr := d.resolve(player)
	if derr != nil {
		return derr
	}
	ctx, cancel := daemonCtx()
	defer cancel()
	switch strings.ToLower(state) {
	case "on":
		return failed(d.c.SetShuffle(ctx, name, true))
	case "off":
		return failed(d.c.SetShuffle(ctx, name, false))
	case "toggle":
		return failed(d.c.ToggleShuffle(ctx, name))
	default:
		return dbusx.InvalidArgs("Invalid shuffle state: " + state + ". Expected: on, off, toggle")
	}
}

// SetLoopStatus takes none, track, or playlist (any case).
func (d *Daemon) SetLoopStatus(player, mode string) *dbus.Error {
	name, derr := d.resolve(player)
	if derr != nil {
		return derr
	}
	var status string
	switch strings.ToLower(mode) {
	case "none":
		status = "None"
	case "track":
		status = "Track"
	case "playlist":
		status = "Playlist"
	default:
		return dbusx.InvalidArgs("Invalid loop mode: " + mode + ". Expected: none, track, playlist")
	}
	ctx, cancel := daemonCtx()
	defer cancel()
	return failed(d.c.SetLoop(ctx, name, status))
}

// PlayerRow is one ListPlayers row: (id, identity, playback state).
type PlayerRow struct {
	ID       string
	Identity string
	State    string
}

// ListPlayers lists the players.
func (d *Daemon) ListPlayers() ([]PlayerRow, *dbus.Error) {
	ctx, cancel := daemonCtx()
	defer cancel()
	players := d.c.Players()
	rows := make([]PlayerRow, len(players))
	for i, name := range players {
		info := d.c.Info(ctx, name)
		rows[i] = PlayerRow{name, info.Identity, info.PlaybackState}
	}
	return rows, nil
}

// GetActivePlayer names the active player, "" when none.
func (d *Daemon) GetActivePlayer() (string, *dbus.Error) { return d.c.Active(), nil }

// SetActivePlayer picks the active player; "" clears it.
func (d *Daemon) SetActivePlayer(player string) *dbus.Error { return failed(d.c.SetActive(player)) }

// GetPlayerInfo is the player's snapshot as a string map.
func (d *Daemon) GetPlayerInfo(player string) (map[string]string, *dbus.Error) {
	name, derr := d.resolve(player)
	if derr != nil {
		return nil, derr
	}
	ctx, cancel := daemonCtx()
	defer cancel()
	info := d.c.Info(ctx, name)
	out := map[string]string{
		"id":              info.ID,
		"identity":        info.Identity,
		"playback_state":  info.PlaybackState,
		"loop_mode":       info.LoopMode,
		"shuffle_mode":    info.ShuffleMode,
		"volume":          strconv.FormatFloat(info.Volume, 'f', 0, 64),
		"can_go_next":     strconv.FormatBool(info.CanGoNext),
		"can_go_previous": strconv.FormatBool(info.CanGoPrevious),
		"can_seek":        strconv.FormatBool(info.CanSeek),
		"can_loop":        strconv.FormatBool(info.CanLoop),
		"can_shuffle":     strconv.FormatBool(info.CanShuffle),
		"title":           info.Title,
		"artist":          info.Artist,
		"album":           info.Album,
	}
	if info.HasArtURL {
		out["art_url"] = info.ArtURL
	}
	if info.LengthUS > 0 {
		out["length_us"] = strconv.FormatUint(info.LengthUS, 10)
	}
	return out, nil
}

// ServeDaemon exports the interface over the controller.
func ServeDaemon(conn *dbus.Conn, c *Controller) (func(), error) {
	d := &Daemon{c: c}
	return dbusx.Serve(conn, dbusx.Service{
		Name:      DaemonName,
		Path:      DaemonPath,
		Interface: DaemonName,
		Methods:   d,
		Properties: dbusx.Getters{
			"ActivePlayer": func() any { return c.Active() },
			"PlayerCount":  func() any { return uint32(len(c.Players())) },
		},
	})
}

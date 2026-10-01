package mpris

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
	"github.com/stubbedev/wayle/internal/rusterr"
)

// Wayle's own interface over the media service, which `wayle media`
// drives (wayle-media/src/dbus).
const (
	DaemonName = "com.wayle.Media1"
	DaemonPath = dbus.ObjectPath("/com/wayle/Media")
)

const daemonTimeout = 25 * time.Second

// Daemon is the com.wayle.Media1 object (dbus/server.rs MediaDaemon)
// over the shell's media service.
type Daemon struct {
	s *Service
}

func daemonCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), daemonTimeout)
}

// resolve is resolve_player: "" is the active player.
func (d *Daemon) resolve(player string) (string, *dbus.Error) {
	if player != "" {
		return player, nil
	}
	if active, ok := d.s.Active(); ok {
		return active.BusName, nil
	}
	return "", dbusx.Failed("No active player")
}

// failed is the daemon's fdo::Error::Failed(e.to_string()) over the
// MediaError spellings: an untracked player is PlayerNotFound, any
// other failure the Control error naming its action.
func failed(action, name string, err error) *dbus.Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoPlayer):
		return dbusx.Failed("player " + name + " not found")
	}
	return dbusx.Failed("cannot control player: " + action + ": " + rusterr.Zbus(err))
}

func (d *Daemon) control(player, action string, run func(context.Context, string) error) *dbus.Error {
	name, derr := d.resolve(player)
	if derr != nil {
		return derr
	}
	ctx, cancel := daemonCtx()
	defer cancel()
	return failed(action, name, run(ctx, name))
}

// PlayPause toggles playback.
func (d *Daemon) PlayPause(player string) *dbus.Error {
	return d.control(player, "play/pause", d.s.PlayPause)
}

// Next skips forward.
func (d *Daemon) Next(player string) *dbus.Error { return d.control(player, "next", d.s.Next) }

// Previous skips back.
func (d *Daemon) Previous(player string) *dbus.Error {
	return d.control(player, "previous", d.s.Previous)
}

// Seek moves to positionUS within the track; negative clamps to 0.
func (d *Daemon) Seek(player string, positionUS int64) *dbus.Error {
	return d.control(player, "set position", func(ctx context.Context, name string) error {
		return d.s.SetPosition(ctx, name, time.Duration(max(positionUS, 0))*time.Microsecond)
	})
}

// SetShuffle takes on, off, or toggle (any case).
func (d *Daemon) SetShuffle(player, state string) *dbus.Error {
	var run func(context.Context, string) error
	switch strings.ToLower(state) {
	case "on":
		run = func(ctx context.Context, name string) error { return d.s.SetShuffle(ctx, name, true) }
	case "off":
		run = func(ctx context.Context, name string) error { return d.s.SetShuffle(ctx, name, false) }
	case "toggle":
		// Rust reads the mode and writes its opposite, so a player
		// without Shuffle answers with its own error.
		run = func(ctx context.Context, name string) error {
			p, err := d.s.lookup(name)
			if err != nil {
				return err
			}
			return d.s.SetShuffle(ctx, name, !p.Shuffle)
		}
	default:
		return dbusx.InvalidArgs("Invalid shuffle state: " + state + ". Expected: on, off, toggle")
	}
	return d.control(player, "set shuffle", run)
}

// SetLoopStatus takes none, track, or playlist (any case).
func (d *Daemon) SetLoopStatus(player, mode string) *dbus.Error {
	var loop LoopMode
	switch strings.ToLower(mode) {
	case "none":
		loop = LoopNone
	case "track":
		loop = LoopTrack
	case "playlist":
		loop = LoopPlaylist
	default:
		return dbusx.InvalidArgs("Invalid loop mode: " + mode + ". Expected: none, track, playlist")
	}
	return d.control(player, "set loop mode", func(ctx context.Context, name string) error {
		return d.s.SetLoop(ctx, name, loop)
	})
}

// PlayerRow is one ListPlayers row: (id, identity, playback state).
type PlayerRow struct {
	ID       string
	Identity string
	State    string
}

// ListPlayers lists the players.
func (d *Daemon) ListPlayers() ([]PlayerRow, *dbus.Error) {
	players := d.s.Players()
	rows := make([]PlayerRow, len(players))
	for i, p := range players {
		rows[i] = PlayerRow{p.BusName, identity(p), stateName(p.State)}
	}
	return rows, nil
}

// GetActivePlayer names the active player, "" when none.
func (d *Daemon) GetActivePlayer() (string, *dbus.Error) { return d.activeName(), nil }

func (d *Daemon) activeName() string {
	if p, ok := d.s.Active(); ok {
		return p.BusName
	}
	return ""
}

// SetActivePlayer picks the active player; "" clears it.
func (d *Daemon) SetActivePlayer(player string) *dbus.Error {
	return failed("set active player", player, d.s.SetActive(player))
}

// GetPlayerInfo is the player's snapshot as a string map (Player::get
// with the Rust enums' Debug spellings).
func (d *Daemon) GetPlayerInfo(player string) (map[string]string, *dbus.Error) {
	name, derr := d.resolve(player)
	if derr != nil {
		return nil, derr
	}
	p, err := d.s.lookup(name)
	if err != nil {
		return nil, failed("get player", name, err)
	}
	shuffle := "Off"
	if p.Shuffle {
		shuffle = "On"
	}
	out := map[string]string{
		"id":              p.BusName,
		"identity":        identity(p),
		"playback_state":  stateName(p.State),
		"loop_mode":       loopName(p.Loop),
		"shuffle_mode":    shuffle,
		"volume":          strconv.FormatFloat(max(p.Volume, 0)*100, 'f', 0, 64),
		"can_go_next":     strconv.FormatBool(p.CanGoNext),
		"can_go_previous": strconv.FormatBool(p.CanGoPrevious),
		"can_seek":        strconv.FormatBool(p.CanSeek),
		"can_loop":        strconv.FormatBool(p.CanLoop),
		"can_shuffle":     strconv.FormatBool(p.CanShuffle),
		"title":           p.Title,
		"artist":          p.Artist,
		"album":           p.Album,
	}
	if p.ArtURL != "" {
		out["art_url"] = p.ArtURL
	}
	if p.Length > 0 {
		out["length_us"] = strconv.FormatInt(p.Length.Microseconds(), 10)
	}
	return out, nil
}

// identity is the player's Identity, its bus name when it has none.
func identity(p Player) string {
	if p.Identity != "" {
		return p.Identity
	}
	return p.BusName
}

func stateName(s PlaybackState) string {
	switch s {
	case StatePlaying:
		return "Playing"
	case StatePaused:
		return "Paused"
	}
	return "Stopped"
}

func loopName(m LoopMode) string {
	if wire, ok := m.wire(); ok {
		return wire
	}
	return "Unsupported"
}

// ServeDaemon exports the interface over the media service.
func ServeDaemon(conn *dbus.Conn, s *Service) (func(), error) {
	d := &Daemon{s: s}
	return dbusx.Serve(conn, dbusx.Service{
		Name:      DaemonName,
		Path:      DaemonPath,
		Interface: DaemonName,
		Methods:   d,
		Properties: dbusx.Getters{
			"ActivePlayer": func() any { return d.activeName() },
			"PlayerCount":  func() any { return uint32(len(s.Players())) },
		},
	})
}

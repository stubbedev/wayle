package mpris

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/rusterr"
)

// Controller is wayle-media's MediaService as its D-Bus daemon uses it:
// the discovered players in discovery order, the active one (chosen by
// selection.rs on every add, or set by hand), fresh per-player
// snapshots (Player::get), and the transport controls.
type Controller struct {
	conn     *dbus.Conn
	ignored  []string
	priority []string

	mu      sync.Mutex
	players []string // full bus names
	active  string   // "" when none
	stop    chan struct{}
}

// NewController discovers the running players and follows
// NameOwnerChanged on conn. ignored are bus-name substrings to skip
// (players-ignored); priority are the player-priority globs.
func NewController(conn *dbus.Conn, ignored, priority []string) (*Controller, error) {
	c := &Controller{conn: conn, ignored: ignored, priority: priority, stop: make(chan struct{})}
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err != nil {
		return nil, fmt.Errorf("mpris: match NameOwnerChanged: %w", err)
	}
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		conn.RemoveSignal(signals)
		return nil, fmt.Errorf("mpris: list names: %w", err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, busNamePrefix) && !c.shouldIgnore(name) {
			c.added(name)
		}
	}
	go c.follow(signals)
	return c, nil
}

// Close stops following the bus.
func (c *Controller) Close() { close(c.stop) }

func (c *Controller) shouldIgnore(name string) bool {
	for _, pattern := range c.ignored {
		if strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

func (c *Controller) follow(signals chan *dbus.Signal) {
	defer c.conn.RemoveSignal(signals)
	for {
		select {
		case <-c.stop:
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			if sig.Name != "org.freedesktop.DBus.NameOwnerChanged" || len(sig.Body) != 3 {
				continue
			}
			name, _ := sig.Body[0].(string)
			oldOwner, _ := sig.Body[1].(string)
			newOwner, _ := sig.Body[2].(string)
			if !strings.HasPrefix(name, busNamePrefix) {
				continue
			}
			switch {
			case oldOwner == "" && newOwner != "":
				if !c.shouldIgnore(name) {
					c.added(name)
				}
			case oldOwner != "" && newOwner == "":
				c.removed(name)
			case oldOwner != "" && newOwner != "":
				c.removed(name)
				if !c.shouldIgnore(name) {
					c.added(name)
				}
			}
		}
	}
}

// added is monitoring.rs's handle_player_added: the player goes to the
// end of the list and the active player is re-selected.
func (c *Controller) added(name string) {
	c.mu.Lock()
	c.players = slices.DeleteFunc(c.players, func(p string) bool { return p == name })
	c.players = append(c.players, name)
	list := slices.Clone(c.players)
	c.mu.Unlock()
	best := c.selectBest(list)
	c.mu.Lock()
	c.active = best
	c.mu.Unlock()
}

// removed is handle_player_removed: re-select only when the active
// player left.
func (c *Controller) removed(name string) {
	c.mu.Lock()
	c.players = slices.DeleteFunc(c.players, func(p string) bool { return p == name })
	list := slices.Clone(c.players)
	reselect := c.active == name
	c.mu.Unlock()
	if !reselect {
		return
	}
	best := c.selectBest(list)
	c.mu.Lock()
	c.active = best
	c.mu.Unlock()
}

// selectBest is selection.rs: the first priority pattern matching a
// player, else the first playing player, else the first player.
func (c *Controller) selectBest(players []string) string {
	for _, pattern := range c.priority {
		for _, p := range players {
			if Wildcard(pattern, p) {
				return p
			}
		}
	}
	for _, p := range players {
		if c.readPlayback(context.Background(), p) == "Playing" {
			return p
		}
	}
	if len(players) > 0 {
		return players[0]
	}
	return ""
}

// Players lists the players' bus names in list order.
func (c *Controller) Players() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.players)
}

// Active is the active player's bus name, "" when none.
func (c *Controller) Active() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active
}

// SetActive picks the active player by bus name; "" clears it.
func (c *Controller) SetActive(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name == "" {
		c.active = ""
		return nil
	}
	if !slices.Contains(c.players, name) {
		return errors.New("player " + name + " not found")
	}
	c.active = name
	return nil
}

// PlayerInfo is one Player::get snapshot, with the Rust enums' Debug
// spellings.
type PlayerInfo struct {
	ID            string
	Identity      string
	PlaybackState string // Playing, Paused, Stopped
	LoopMode      string // None, Track, Playlist, Unsupported
	ShuffleMode   string // On, Off
	Volume        float64
	CanGoNext     bool
	CanGoPrevious bool
	CanSeek       bool
	CanLoop       bool
	CanShuffle    bool
	Title         string
	Artist        string
	Album         string
	ArtURL        string
	HasArtURL     bool
	// LengthUS is the track length in microseconds, 0 when unknown.
	LengthUS uint64
	trackID  string
}

const mediaPlayer2 = "org.mpris.MediaPlayer2"

func (c *Controller) object(name string) dbus.BusObject {
	return c.conn.Object(name, dbus.ObjectPath(playerPath))
}

func (c *Controller) get(ctx context.Context, name, iface, prop string) (dbus.Variant, error) {
	var v dbus.Variant
	err := c.object(name).CallWithContext(ctx, properties+".Get", 0, iface, prop).Store(&v)
	return v, err
}

func (c *Controller) readPlayback(ctx context.Context, name string) string {
	v, err := c.get(ctx, name, playerIface, "PlaybackStatus")
	if err != nil {
		return "Stopped"
	}
	return playbackName(variantString(v))
}

func playbackName(status string) string {
	switch status {
	case "Playing", "Paused":
		return status
	default:
		return "Stopped"
	}
}

// Info is Player::get: every read that fails keeps the Rust default.
func (c *Controller) Info(ctx context.Context, name string) PlayerInfo {
	info := PlayerInfo{
		ID: name, Identity: name, PlaybackState: c.readPlayback(ctx, name),
		LoopMode: "None", ShuffleMode: "Off",
		Title: unknownMetadata, Artist: unknownMetadata, Album: unknownMetadata,
	}
	if v, err := c.get(ctx, name, mediaPlayer2, "Identity"); err == nil {
		info.Identity = variantString(v)
	}
	if v, err := c.get(ctx, name, playerIface, "Metadata"); err == nil {
		if md, ok := v.Value().(map[string]dbus.Variant); ok {
			info.applyMetadata(md)
		}
	}
	if v, err := c.get(ctx, name, playerIface, "LoopStatus"); err == nil {
		info.LoopMode = loopName(variantString(v))
		info.CanLoop = true
	}
	if v, err := c.get(ctx, name, playerIface, "Shuffle"); err == nil {
		if on, ok := v.Value().(bool); ok && on {
			info.ShuffleMode = "On"
		}
		info.CanShuffle = true
	}
	if v, err := c.get(ctx, name, playerIface, "Volume"); err == nil {
		if vol, ok := v.Value().(float64); ok {
			info.Volume = max(vol, 0) * 100
		}
	}
	boolean := func(prop string) bool {
		v, err := c.get(ctx, name, playerIface, prop)
		if err != nil {
			return false
		}
		b, _ := v.Value().(bool)
		return b
	}
	info.CanGoNext = boolean("CanGoNext")
	info.CanGoPrevious = boolean("CanGoPrevious")
	info.CanSeek = boolean("CanSeek")
	return info
}

// unknownMetadata is metadata's UNKNOWN_METADATA, used when the whole
// Metadata read fails.
const unknownMetadata = "Unknown"

func loopName(status string) string {
	switch status {
	case "None", "Track", "Playlist":
		return status
	default:
		return "Unsupported"
	}
}

// applyMetadata is TrackProperties::from_mpris.
func (info *PlayerInfo) applyMetadata(md map[string]dbus.Variant) {
	info.Title = variantString(md["xesam:title"])
	info.Artist = strings.Join(variantStringArray(md["xesam:artist"]), ", ")
	info.Album = variantString(md["xesam:album"])
	if v, ok := md["mpris:artUrl"]; ok {
		if s, ok := v.Value().(string); ok {
			info.ArtURL, info.HasArtURL = s, true
		}
	}
	switch length := md["mpris:length"].Value().(type) {
	case int64:
		if length > 0 {
			info.LengthUS = uint64(length)
		}
	case uint64:
		info.LengthUS = length
	}
	if v, ok := md["mpris:trackid"]; ok {
		switch id := v.Value().(type) {
		case dbus.ObjectPath:
			info.trackID = string(id)
		case string:
			info.trackID = id
		}
	}
}

// ControlError is the service's Control error ("cannot control
// player: ..."); OperationNotSupported reads "operation not
// supported: ...".
type ControlError struct{ msg string }

func (e *ControlError) Error() string { return e.msg }

func controlErr(action string, err error) error {
	return &ControlError{msg: "cannot control player: " + action + ": " + rusterr.Zbus(err)}
}

func (c *Controller) call(ctx context.Context, name, action, method string, args ...any) error {
	if err := c.object(name).CallWithContext(ctx, playerIface+"."+method, 0, args...).Err; err != nil {
		return controlErr(action, err)
	}
	return nil
}

func (c *Controller) set(ctx context.Context, name, action, prop string, value any) error {
	err := c.object(name).CallWithContext(ctx, properties+".Set", 0, playerIface, prop, dbus.MakeVariant(value)).Err
	if err != nil {
		return controlErr(action, err)
	}
	return nil
}

// PlayPause toggles playback.
func (c *Controller) PlayPause(ctx context.Context, name string) error {
	return c.call(ctx, name, "play/pause", "PlayPause")
}

// Next skips forward.
func (c *Controller) Next(ctx context.Context, name string) error {
	return c.call(ctx, name, "next", "Next")
}

// Previous skips back.
func (c *Controller) Previous(ctx context.Context, name string) error {
	return c.call(ctx, name, "previous", "Previous")
}

// Seek sets the position within the current track.
func (c *Controller) Seek(ctx context.Context, name string, positionUS int64) error {
	track := c.Info(ctx, name).trackID
	if track == "" {
		track = "/org/mpris/MediaPlayer2/TrackList/NoTrack"
	}
	if !dbus.ObjectPath(track).IsValid() {
		return &ControlError{msg: "cannot control player: invalid track id: " + track}
	}
	return c.call(ctx, name, "set position", "SetPosition", dbus.ObjectPath(track), max(positionUS, 0))
}

// SetShuffle sets shuffle on or off.
func (c *Controller) SetShuffle(ctx context.Context, name string, on bool) error {
	return c.set(ctx, name, "set shuffle", "Shuffle", on)
}

// ToggleShuffle flips shuffle from the snapshot's state.
func (c *Controller) ToggleShuffle(ctx context.Context, name string) error {
	return c.SetShuffle(ctx, name, c.Info(ctx, name).ShuffleMode != "On")
}

// SetLoop sets the loop status (None, Track, Playlist).
func (c *Controller) SetLoop(ctx context.Context, name, status string) error {
	return c.set(ctx, name, "set loop mode", "LoopStatus", status)
}

// Wildcard is media/src/glob.rs's matches: a case-sensitive glob (`*`,
// `?`, classes, `\` escapes); a malformed pattern matches nothing.
func Wildcard(pattern, text string) bool {
	ok, err := path.Match(pattern, text)
	return err == nil && ok
}

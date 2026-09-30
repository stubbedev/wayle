// Package mpris is the media player service over MPRIS2 (D-Bus), the
// Go counterpart of crates/wayle-media: it discovers players by bus
// name, keeps each one's snapshot current from PropertiesChanged,
// picks the active player the way selection.rs does, and drives the
// transport (play/pause, next, previous, seek, loop, shuffle).
package mpris

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/glob"
)

// D-Bus names of the MPRIS2 surface.
const (
	BusNamePrefix = "org.mpris.MediaPlayer2."
	ObjectPath    = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	RootIface     = "org.mpris.MediaPlayer2"
	PlayerIface   = "org.mpris.MediaPlayer2.Player"
	propsIface    = "org.freedesktop.DBus.Properties"
	// noTrack is the MPRIS "no track" object path SetPosition takes when
	// the player publishes no mpris:trackid (player/mod.rs NULL_PATH).
	noTrack = dbus.ObjectPath("/org/mpris/MediaPlayer2/TrackList/NoTrack")
)

// ErrNoPlayer reports a control aimed at a bus name the service does
// not track.
var ErrNoPlayer = errors.New("mpris: no such player")

// PlaybackState is the player's transport state.
type PlaybackState uint8

// Playback states.
const (
	StateStopped PlaybackState = iota
	StatePlaying
	StatePaused
)

// StateFromString maps MPRIS's PlaybackStatus.
func StateFromString(status string) PlaybackState {
	switch strings.TrimSpace(status) {
	case "Playing":
		return StatePlaying
	case "Paused":
		return StatePaused
	}
	return StateStopped
}

// LoopMode is the player's LoopStatus; LoopUnsupported marks a player
// without the property or with an unknown value (types.rs LoopMode).
type LoopMode uint8

// Loop modes.
const (
	LoopNone LoopMode = iota
	LoopTrack
	LoopPlaylist
	LoopUnsupported
)

// ParseLoopMode maps MPRIS's LoopStatus.
func ParseLoopMode(status string) LoopMode {
	switch status {
	case "None":
		return LoopNone
	case "Track":
		return LoopTrack
	case "Playlist":
		return LoopPlaylist
	}
	return LoopUnsupported
}

// wire is the LoopStatus spelling; unsupported has none.
func (m LoopMode) wire() (string, bool) {
	switch m {
	case LoopNone:
		return "None", true
	case LoopTrack:
		return "Track", true
	case LoopPlaylist:
		return "Playlist", true
	}
	return "", false
}

// Next is toggle_loop's cycle: None, Track, Playlist, None; false for
// an unsupported mode.
func (m LoopMode) Next() (LoopMode, bool) {
	switch m {
	case LoopNone:
		return LoopTrack, true
	case LoopTrack:
		return LoopPlaylist, true
	case LoopPlaylist:
		return LoopNone, true
	}
	return LoopUnsupported, false
}

// Player is one player's snapshot.
type Player struct {
	// BusName is the full well-known name (org.mpris.MediaPlayer2.x).
	BusName      string
	Identity     string
	DesktopEntry string

	Title   string
	Artist  string
	Album   string
	ArtURL  string
	TrackID string
	// Length is the track length; zero when the player reports none.
	Length time.Duration

	State   PlaybackState
	Loop    LoopMode
	Shuffle bool

	CanGoNext     bool
	CanGoPrevious bool
	CanSeek       bool
	// CanLoop and CanShuffle report whether the player publishes the
	// LoopStatus and Shuffle properties at all (player/mod.rs probes
	// the reads the same way).
	CanLoop    bool
	CanShuffle bool
}

// SelectBest is selection.rs select_best_player: the first priority
// pattern (in order) that matches a player's bus name, else the first
// playing player, else the first player. Patterns are wayle-media's
// case-sensitive wildcards.
func SelectBest(players []Player, priority []string) (Player, bool) {
	for _, pattern := range priority {
		for _, p := range players {
			if glob.Wildcard(pattern, p.BusName) {
				return p, true
			}
		}
	}
	for _, p := range players {
		if p.State == StatePlaying {
			return p, true
		}
	}
	if len(players) > 0 {
		return players[0], true
	}
	return Player{}, false
}

// Ignored is monitoring.rs should_ignore: a plain substring match of
// any pattern against the bus name.
func Ignored(busName string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(busName, pattern) {
			return true
		}
	}
	return false
}

// Source is what the media module and dropdown read and drive.
type Source interface {
	// Players lists the tracked players in discovery order.
	Players() []Player
	// Active returns the active player; false when there is none.
	Active() (Player, bool)
	// SetActive makes the tracked player with that bus name active (the
	// source picker); unknown names are ErrNoPlayer.
	SetActive(busName string) error
	// Subscribe returns a feed that ticks on every player-set, active,
	// or snapshot change. Feeds live as long as the source.
	Subscribe() <-chan struct{}
	// PlayPause, Next, and Previous drive the transport.
	PlayPause(ctx context.Context, busName string) error
	Next(ctx context.Context, busName string) error
	Previous(ctx context.Context, busName string) error
	// SetPosition seeks the current track to pos.
	SetPosition(ctx context.Context, busName string, pos time.Duration) error
	// ToggleLoop and ToggleShuffle step the loop cycle and flip shuffle.
	ToggleLoop(ctx context.Context, busName string) error
	ToggleShuffle(ctx context.Context, busName string) error
	// Position reads the live playback position (MPRIS emits no change
	// signal for it, so the dropdown polls).
	Position(ctx context.Context, busName string) (time.Duration, error)
}

// Service is the live Source on a bus.
type Service struct {
	conn     *dbus.Conn
	ignored  []string
	priority []string

	mu      sync.Mutex
	players []Player
	// owners maps each player's unique connection name to its bus
	// name, so a PropertiesChanged sender resolves to its player.
	owners map[string]string
	active string
	subs   []chan struct{}
	stop   chan struct{}
}

// NewSession connects to the session bus and starts the service.
func NewSession(ignored, priority []string) (*Service, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("mpris: session bus: %w", err)
	}
	s, err := New(conn, ignored, priority)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

// New starts the service on conn: it matches the player and bus-owner
// signals, discovers the players already on the bus, and follows them.
// ignored and priority are the players-ignored and player-priority
// config lists.
func New(conn *dbus.Conn, ignored, priority []string) (*Service, error) {
	s := &Service{
		conn:     conn,
		ignored:  ignored,
		priority: priority,
		owners:   make(map[string]string),
		stop:     make(chan struct{}),
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface(propsIface), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchObjectPath(ObjectPath)); err != nil {
		return nil, fmt.Errorf("mpris: match PropertiesChanged: %w", err)
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg0Namespace("org.mpris.MediaPlayer2")); err != nil {
		return nil, fmt.Errorf("mpris: match NameOwnerChanged: %w", err)
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return nil, fmt.Errorf("mpris: list names: %w", err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, BusNamePrefix) && !Ignored(name, ignored) {
			s.add(name)
		}
	}
	go s.run(signals)
	return s, nil
}

// Close stops following the bus and drops the connection.
func (s *Service) Close() error {
	close(s.stop)
	return s.conn.Close()
}

// run consumes the signals until Close.
func (s *Service) run(signals chan *dbus.Signal) {
	defer s.conn.RemoveSignal(signals)
	for {
		select {
		case <-s.stop:
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			s.handle(sig)
		}
	}
}

// handle routes one signal: bus-name ownership changes add, remove, or
// replace players (monitoring.rs spawn_name_monitoring); a player's
// PropertiesChanged re-reads its snapshot.
func (s *Service) handle(sig *dbus.Signal) {
	switch sig.Name {
	case "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) != 3 {
			return
		}
		name, _ := sig.Body[0].(string)
		oldOwner, _ := sig.Body[1].(string)
		newOwner, _ := sig.Body[2].(string)
		if !strings.HasPrefix(name, BusNamePrefix) {
			return
		}
		if oldOwner != "" {
			s.remove(name)
		}
		if newOwner != "" && !Ignored(name, s.ignored) {
			s.add(name)
		}
	case propsIface + ".PropertiesChanged":
		if len(sig.Body) < 1 {
			return
		}
		if iface, _ := sig.Body[0].(string); iface != PlayerIface && iface != RootIface {
			return
		}
		s.mu.Lock()
		name, ok := s.owners[sig.Sender]
		s.mu.Unlock()
		if ok {
			s.refresh(name)
		}
	}
}

// add reads a new player and re-selects the active one, as
// handle_player_added does on every arrival.
func (s *Service) add(name string) {
	p, err := s.read(context.Background(), name)
	if err != nil {
		return
	}
	var owner string
	_ = s.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner)
	s.mu.Lock()
	s.dropLocked(name)
	s.players = append(s.players, p)
	if owner != "" {
		s.owners[owner] = name
	}
	if best, ok := SelectBest(s.players, s.priority); ok {
		s.active = best.BusName
	}
	s.mu.Unlock()
	s.notify()
}

// remove drops a player; the active one leaving re-selects
// (handle_player_removed).
func (s *Service) remove(name string) {
	s.mu.Lock()
	dropped := s.dropLocked(name)
	if dropped && s.active == name {
		s.active = ""
		if best, ok := SelectBest(s.players, s.priority); ok {
			s.active = best.BusName
		}
	}
	s.mu.Unlock()
	if dropped {
		s.notify()
	}
}

// dropLocked removes a player and its owner mapping. The caller holds
// mu.
func (s *Service) dropLocked(name string) bool {
	for owner, n := range s.owners {
		if n == name {
			delete(s.owners, owner)
		}
	}
	for i, p := range s.players {
		if p.BusName == name {
			s.players = append(s.players[:i], s.players[i+1:]...)
			return true
		}
	}
	return false
}

// refresh re-reads one player's snapshot in place.
func (s *Service) refresh(name string) {
	p, err := s.read(context.Background(), name)
	if err != nil {
		return
	}
	s.mu.Lock()
	found := false
	for i := range s.players {
		if s.players[i].BusName == name {
			s.players[i] = p
			found = true
		}
	}
	s.mu.Unlock()
	if found {
		s.notify()
	}
}

// read pulls one player's root and player properties.
func (s *Service) read(ctx context.Context, name string) (Player, error) {
	obj := s.conn.Object(name, ObjectPath)
	var root, player map[string]dbus.Variant
	if err := obj.CallWithContext(ctx, propsIface+".GetAll", 0, RootIface).Store(&root); err != nil {
		return Player{}, fmt.Errorf("mpris: %s root: %w", name, err)
	}
	if err := obj.CallWithContext(ctx, propsIface+".GetAll", 0, PlayerIface).Store(&player); err != nil {
		return Player{}, fmt.Errorf("mpris: %s player: %w", name, err)
	}
	return parsePlayer(name, root, player), nil
}

// parsePlayer builds a snapshot from the two GetAll maps.
func parsePlayer(name string, root, player map[string]dbus.Variant) Player {
	p := Player{
		BusName:       name,
		Identity:      variantString(root["Identity"]),
		DesktopEntry:  variantString(root["DesktopEntry"]),
		State:         StateFromString(variantString(player["PlaybackStatus"])),
		CanGoNext:     variantBool(player["CanGoNext"]),
		CanGoPrevious: variantBool(player["CanGoPrevious"]),
		CanSeek:       variantBool(player["CanSeek"]),
		Loop:          LoopUnsupported,
	}
	if loop, ok := player["LoopStatus"]; ok {
		p.CanLoop = true
		p.Loop = ParseLoopMode(variantString(loop))
	}
	if shuffle, ok := player["Shuffle"]; ok {
		p.CanShuffle = true
		p.Shuffle = variantBool(shuffle)
	}
	var meta map[string]dbus.Variant
	if v, ok := player["Metadata"]; ok {
		meta, _ = v.Value().(map[string]dbus.Variant)
	}
	p.Title = variantString(meta["xesam:title"])
	p.Artist = strings.Join(variantStringArray(meta["xesam:artist"]), ", ")
	p.Album = variantString(meta["xesam:album"])
	p.ArtURL = variantString(meta["mpris:artUrl"])
	p.TrackID = variantString(meta["mpris:trackid"])
	p.Length = variantMicros(meta["mpris:length"])
	return p
}

// notify ticks every subscriber, coalescing per feed.
func (s *Service) notify() {
	s.mu.Lock()
	subs := s.subs
	s.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Subscribe implements Source.
func (s *Service) Subscribe() <-chan struct{} {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	return ch
}

// Players implements Source.
func (s *Service) Players() []Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Player, len(s.players))
	copy(out, s.players)
	return out
}

// Active implements Source.
func (s *Service) Active() (Player, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.players {
		if p.BusName == s.active {
			return p, true
		}
	}
	return Player{}, false
}

// SetActive implements Source (service.rs set_active_player).
func (s *Service) SetActive(busName string) error {
	s.mu.Lock()
	found := false
	for _, p := range s.players {
		if p.BusName == busName {
			found = true
		}
	}
	if found {
		s.active = busName
	}
	s.mu.Unlock()
	if !found {
		return fmt.Errorf("%w: %s", ErrNoPlayer, busName)
	}
	s.notify()
	return nil
}

// lookup returns the tracked snapshot for a control.
func (s *Service) lookup(busName string) (Player, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.players {
		if p.BusName == busName {
			return p, nil
		}
	}
	return Player{}, fmt.Errorf("%w: %s", ErrNoPlayer, busName)
}

// call runs one Player-interface method on a tracked player.
func (s *Service) call(ctx context.Context, busName, method string, args ...any) error {
	if _, err := s.lookup(busName); err != nil {
		return err
	}
	return s.conn.Object(busName, ObjectPath).CallWithContext(ctx, PlayerIface+"."+method, 0, args...).Err
}

// setProp writes one Player-interface property on a tracked player.
func (s *Service) setProp(ctx context.Context, busName, prop string, value any) error {
	return s.conn.Object(busName, ObjectPath).CallWithContext(ctx, propsIface+".Set", 0, PlayerIface, prop, dbus.MakeVariant(value)).Err
}

// PlayPause implements Source.
func (s *Service) PlayPause(ctx context.Context, busName string) error {
	return s.call(ctx, busName, "PlayPause")
}

// Next implements Source.
func (s *Service) Next(ctx context.Context, busName string) error {
	return s.call(ctx, busName, "Next")
}

// Previous implements Source.
func (s *Service) Previous(ctx context.Context, busName string) error {
	return s.call(ctx, busName, "Previous")
}

// SetPosition implements Source: SetPosition on the current track id,
// the NoTrack path when the player publishes none.
func (s *Service) SetPosition(ctx context.Context, busName string, pos time.Duration) error {
	p, err := s.lookup(busName)
	if err != nil {
		return err
	}
	track := noTrack
	if p.TrackID != "" && dbus.ObjectPath(p.TrackID).IsValid() {
		track = dbus.ObjectPath(p.TrackID)
	}
	return s.call(ctx, busName, "SetPosition", track, pos.Microseconds())
}

// ToggleLoop implements Source: the None, Track, Playlist cycle.
func (s *Service) ToggleLoop(ctx context.Context, busName string) error {
	p, err := s.lookup(busName)
	if err != nil {
		return err
	}
	next, ok := p.Loop.Next()
	if !ok || !p.CanLoop {
		return fmt.Errorf("mpris: %s: loop unsupported", busName)
	}
	wire, _ := next.wire()
	return s.setProp(ctx, busName, "LoopStatus", wire)
}

// ToggleShuffle implements Source.
func (s *Service) ToggleShuffle(ctx context.Context, busName string) error {
	p, err := s.lookup(busName)
	if err != nil {
		return err
	}
	if !p.CanShuffle {
		return fmt.Errorf("mpris: %s: shuffle unsupported", busName)
	}
	return s.setProp(ctx, busName, "Shuffle", !p.Shuffle)
}

// Position implements Source.
func (s *Service) Position(ctx context.Context, busName string) (time.Duration, error) {
	if _, err := s.lookup(busName); err != nil {
		return 0, err
	}
	var v dbus.Variant
	if err := s.conn.Object(busName, ObjectPath).CallWithContext(ctx, propsIface+".Get", 0, PlayerIface, "Position").Store(&v); err != nil {
		return 0, err
	}
	return variantMicros(v), nil
}

func variantString(v dbus.Variant) string {
	switch value := v.Value().(type) {
	case string:
		return value
	case dbus.ObjectPath:
		return string(value)
	}
	return ""
}

func variantStringArray(v dbus.Variant) []string {
	switch value := v.Value().(type) {
	case []string:
		return value
	case string:
		return []string{value}
	}
	return nil
}

func variantBool(v dbus.Variant) bool {
	b, _ := v.Value().(bool)
	return b
}

// variantMicros reads an MPRIS microsecond count (x or t); zero for
// anything non-positive or absent (metadata types.rs duration).
func variantMicros(v dbus.Variant) time.Duration {
	switch value := v.Value().(type) {
	case int64:
		if value > 0 {
			return time.Duration(value) * time.Microsecond
		}
	case uint64:
		if value > 0 {
			return time.Duration(value) * time.Microsecond
		}
	case int32:
		if value > 0 {
			return time.Duration(value) * time.Microsecond
		}
	}
	return 0
}

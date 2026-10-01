package network

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"slices"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/feed"
)

// Profile is one saved NetworkManager connection, the fields of
// core/settings_connection's ConnectionSettings the service reads.
type Profile struct {
	// Path is the Settings.Connection object, which activation takes.
	Path dbus.ObjectPath
	// UUID is stable across renames.
	UUID string
	// ID is the user-facing name.
	ID string
	// Type is connection.type: "vpn", "wireguard", "802-11-wireless", ...
	Type string
	// SSID is 802-11-wireless.ssid for a wifi profile, nil otherwise.
	SSID []byte
}

// Settings is the live list of saved profiles (core/settings), kept
// current by NM's NewConnection / ConnectionRemoved signals and each
// profile's Updated signal (a rename lands without a rebuild of
// anything else).
type Settings struct {
	nm nm

	mu       sync.Mutex
	profiles []Profile
	changes  feed.Tick
}

// newSettings reads every profile and starts following NM's
// announcements until ctx ends.
func newSettings(ctx context.Context, n nm) (*Settings, error) {
	s := &Settings{nm: n}
	signals := make(chan *dbus.Signal, 64)
	n.conn.Signal(signals)
	for _, opt := range [][]dbus.MatchOption{
		{dbus.WithMatchObjectPath(settingsPath), dbus.WithMatchInterface(settingsIface)},
		{dbus.WithMatchInterface(connectionIface), dbus.WithMatchMember("Updated")},
	} {
		if err := n.conn.AddMatchSignalContext(ctx, opt...); err != nil {
			n.conn.RemoveSignal(signals)
			return nil, fmt.Errorf("network: watch settings: %w", err)
		}
	}
	if err := s.reload(ctx); err != nil {
		n.conn.RemoveSignal(signals)
		return nil, err
	}
	go s.follow(ctx, signals)
	return s, nil
}

// reload re-reads the whole list (startup, and NM coming back).
func (s *Settings) reload(ctx context.Context) error {
	var paths []dbus.ObjectPath
	if err := s.nm.object(settingsPath).CallWithContext(ctx, settingsIface+".ListConnections", 0).Store(&paths); err != nil {
		return fmt.Errorf("network: list connections: %w", err)
	}
	profiles := make([]Profile, 0, len(paths))
	for _, path := range paths {
		if p, err := s.read(ctx, path); err == nil {
			profiles = append(profiles, p)
		}
	}
	s.mu.Lock()
	s.profiles = profiles
	s.mu.Unlock()
	feed.Notify(&s.changes)
	return nil
}

func (s *Settings) read(ctx context.Context, path dbus.ObjectPath) (Profile, error) {
	dict, err := s.nm.getSettings(ctx, path)
	if err != nil {
		return Profile{}, err
	}
	r := dictReader(dict)
	p := Profile{
		Path: path,
		UUID: r.str("connection", "uuid"),
		ID:   r.str("connection", "id"),
		Type: r.str("connection", "type"),
	}
	if ssid, ok := dict["802-11-wireless"]["ssid"].Value().([]byte); ok {
		p.SSID = ssid
	}
	return p, nil
}

func (s *Settings) follow(ctx context.Context, signals chan *dbus.Signal) {
	defer s.nm.conn.RemoveSignal(signals)
	for {
		select {
		case <-ctx.Done():
			return
		case sig, ok := <-signals:
			if !ok {
				return
			}
			s.handle(ctx, sig)
		}
	}
}

func (s *Settings) handle(ctx context.Context, sig *dbus.Signal) {
	switch sig.Name {
	case settingsIface + ".NewConnection":
		path, ok := firstPath(sig)
		if !ok {
			return
		}
		p, err := s.read(ctx, path)
		if err != nil {
			log.Printf("network: new connection %s: %v", path, err)
			return
		}
		s.mu.Lock()
		s.profiles = slices.DeleteFunc(s.profiles, func(q Profile) bool { return q.Path == path })
		s.profiles = append(s.profiles, p)
		s.mu.Unlock()
	case settingsIface + ".ConnectionRemoved":
		path, ok := firstPath(sig)
		if !ok {
			return
		}
		s.mu.Lock()
		s.profiles = slices.DeleteFunc(s.profiles, func(q Profile) bool { return q.Path == path })
		s.mu.Unlock()
	case connectionIface + ".Updated":
		p, err := s.read(ctx, sig.Path)
		if err != nil {
			return
		}
		s.mu.Lock()
		for i := range s.profiles {
			if s.profiles[i].Path == sig.Path {
				s.profiles[i] = p
			}
		}
		s.mu.Unlock()
	default:
		return
	}
	feed.Notify(&s.changes)
}

func firstPath(sig *dbus.Signal) (dbus.ObjectPath, bool) {
	if len(sig.Body) == 0 {
		return "", false
	}
	path, ok := sig.Body[0].(dbus.ObjectPath)
	return path, ok
}

// Profiles snapshots the saved profiles in NM's order.
func (s *Settings) Profiles() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.profiles)
}

// Changes ticks whenever a profile is added, removed, or rewritten.
func (s *Settings) Changes(ctx context.Context) <-chan struct{} {
	return s.changes.SubscribeContext(ctx)
}

// byUUID finds the profile with this UUID.
func (s *Settings) byUUID(uuid string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.UUID == uuid {
			return p, true
		}
	}
	return Profile{}, false
}

// ForSSID lists the saved profiles for a wifi network
// (connections_for_ssid).
func (s *Settings) ForSSID(ssid []byte) []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Profile
	for _, p := range s.profiles {
		if p.SSID != nil && bytes.Equal(p.SSID, ssid) {
			out = append(out, p)
		}
	}
	return out
}

// DeleteForSSID deletes every saved profile of a wifi network
// (delete_connections_for_ssid): one refusal is logged and does not
// stop the rest.
func (s *Settings) DeleteForSSID(ctx context.Context, ssid []byte) {
	for _, p := range s.ForSSID(ssid) {
		if err := s.Delete(ctx, p.Path); err != nil {
			log.Printf("network: failed to delete saved wifi profile: %v", err)
		}
	}
}

// Add saves a new profile to disk (AddConnection).
func (s *Settings) Add(ctx context.Context, dict ConnectionDict) (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	if err := s.nm.object(settingsPath).CallWithContext(ctx, settingsIface+".AddConnection", 0, dict).Store(&path); err != nil {
		return "", fmt.Errorf("dbus operation failed: %w", err)
	}
	return path, nil
}

// Update rewrites a saved profile in place.
func (s *Settings) Update(ctx context.Context, path dbus.ObjectPath, dict ConnectionDict) error {
	if err := s.nm.object(path).CallWithContext(ctx, connectionIface+".Update", 0, dict).Err; err != nil {
		return fmt.Errorf("cannot update connection: %w", err)
	}
	return nil
}

// Delete removes a saved profile.
func (s *Settings) Delete(ctx context.Context, path dbus.ObjectPath) error {
	if err := s.nm.object(path).CallWithContext(ctx, connectionIface+".Delete", 0).Err; err != nil {
		return fmt.Errorf("cannot delete connection: %w", err)
	}
	return nil
}

// Get reads a saved profile's settings.
func (s *Settings) Get(ctx context.Context, path dbus.ObjectPath) (ConnectionDict, error) {
	dict, err := s.nm.getSettings(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("cannot get settings: %w", err)
	}
	return dict, nil
}

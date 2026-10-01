package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/service/mpris"
)

// fakePlayer is one MPRIS player on the private bus.
type fakePlayer struct {
	mu    sync.Mutex
	calls []string
	props *prop.Properties
}

func (f *fakePlayer) record(call string) *dbus.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	return nil
}

func (f *fakePlayer) PlayPause() *dbus.Error { return f.record("PlayPause") }
func (f *fakePlayer) Next() *dbus.Error      { return f.record("Next") }
func (f *fakePlayer) Previous() *dbus.Error  { return f.record("Previous") }

func (f *fakePlayer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// startPlayer claims org.mpris.MediaPlayer2.<suffix> with the given
// Player properties; withShuffle adds a writable Shuffle.
func startPlayer(t *testing.T, suffix, identity, status string, withShuffle bool) *fakePlayer {
	t.Helper()
	conn := dbustest.SessionConn(t)
	f := &fakePlayer{}
	path := dbus.ObjectPath("/org/mpris/MediaPlayer2")
	if err := conn.Export(f, path, "org.mpris.MediaPlayer2.Player"); err != nil {
		t.Fatal(err)
	}
	player := map[string]*prop.Prop{
		"PlaybackStatus": {Value: status, Emit: prop.EmitFalse},
		"LoopStatus":     {Value: "None", Writable: true, Emit: prop.EmitTrue},
		"Volume":         {Value: 0.5, Emit: prop.EmitFalse},
		"CanGoNext":      {Value: true, Emit: prop.EmitFalse},
		"CanGoPrevious":  {Value: false, Emit: prop.EmitFalse},
		"CanSeek":        {Value: true, Emit: prop.EmitFalse},
		"Metadata": {Value: map[string]dbus.Variant{
			"xesam:title":  dbus.MakeVariant("Song"),
			"xesam:artist": dbus.MakeVariant([]string{"A", "B"}),
			"xesam:album":  dbus.MakeVariant("Record"),
			"mpris:length": dbus.MakeVariant(int64(185_000_000)),
		}, Emit: prop.EmitFalse},
	}
	if withShuffle {
		player["Shuffle"] = &prop.Prop{Value: false, Writable: true, Emit: prop.EmitTrue}
	}
	props, err := prop.Export(conn, path, prop.Map{
		"org.mpris.MediaPlayer2":        {"Identity": {Value: identity, Emit: prop.EmitFalse}},
		"org.mpris.MediaPlayer2.Player": player,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.props = props
	if reply, err := conn.RequestName("org.mpris.MediaPlayer2."+suffix, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("claim player: %v %v", reply, err)
	}
	return f
}

func serveMedia(t *testing.T, priority []string) *mpris.Service {
	t.Helper()
	conn := dbustest.SessionConn(t)
	c, err := mpris.New(conn, []string{"ignored"}, priority)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	release, err := mpris.ServeDaemon(conn, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	return c
}

func TestMediaCommands(t *testing.T) {
	dbustest.Session(t)
	// vlc is discovered at start; spotify (and the ignored player)
	// appear later, so the list order is deterministic.
	vlc := startPlayer(t, "vlc", "VLC media player", "Paused", false)
	c := serveMedia(t, nil)
	spotify := startPlayer(t, "spotify", "Spotify", "Playing", true)
	startPlayer(t, "ignored.instance1", "Hidden", "Playing", false)
	waitFor(t, func() bool { return len(c.Players()) == 2 && activeName(c) == "org.mpris.MediaPlayer2.spotify" })

	steps := []struct {
		args []string
		want string
	}{
		{[]string{"media", "list"}, "Available media players:\n  1. VLC media player (org.mpris.MediaPlayer2.vlc) [Paused]\n  2. Spotify (org.mpris.MediaPlayer2.spotify) [Playing]\n"},
		// The first playing player is active (no priority configured).
		{[]string{"media", "active"}, "Active player: Spotify - Song [Playing]\n"},
		{[]string{"media", "active", "1"}, "Set active player to: org.mpris.MediaPlayer2.vlc\n"},
		{[]string{"media", "active"}, "Active player: VLC media player - Song [Paused]\n"},
		{[]string{"media", "next"}, ""},
		{[]string{"media", "play-pause", "spot"}, ""},
		{[]string{"media", "loop", "track", "+2"}, "Loop mode set to track\n"},
		{[]string{"media", "shuffle", "on", "spotify"}, ""},
		{[]string{"media", "info", "spotify"}, "Player: Spotify\nStatus: Playing\nTitle: Song\nArtist: A, B\nAlbum: Record\nLength: 03:05\nVolume: 50%\nShuffle: On\nLoop: Track\nCapabilities: Seek, Next\n"},
	}
	for _, s := range steps {
		run := runCaptured
		if s.args[1] == "info" {
			// The snapshot follows the player's PropertiesChanged.
			run = func(t *testing.T, _ bool, args ...string) (string, string, int) { return runUntil(t, s.want, args...) }
		}
		stdout, stderr, code := run(t, false, s.args...)
		if code != 0 || stdout != s.want {
			t.Errorf("%v: code %d stdout %q stderr %q\nwant %q", s.args, code, stdout, stderr, s.want)
		}
	}
	if got := strings.Join(vlc.Calls(), ","); got != "Next" {
		t.Errorf("vlc calls = %s", got)
	}
	if got := strings.Join(spotify.Calls(), ","); got != "PlayPause" {
		t.Errorf("spotify calls = %s", got)
	}
}

func TestMediaPriorityAndHotplug(t *testing.T) {
	dbustest.Session(t)
	startPlayer(t, "vlc", "VLC", "Playing", false)
	c := serveMedia(t, []string{"*firefox*"})
	if got := activeName(c); got != "org.mpris.MediaPlayer2.vlc" {
		t.Fatalf("active = %q", got)
	}
	// A player matching the priority glob takes over when it appears.
	startPlayer(t, "firefox.instance_1_2", "Firefox", "Paused", false)
	waitFor(t, func() bool { return activeName(c) == "org.mpris.MediaPlayer2.firefox.instance_1_2" })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMediaErrors(t *testing.T) {
	dbustest.Session(t)
	if _, stderr, code := runCaptured(t, false, "media", "list"); code != 1 || stderr != "Error: Media service not running. Start wayle shell first.\n" {
		t.Errorf("not running: code %d %q", code, stderr)
	}
	c := serveMedia(t, nil)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"media", "next"}, "Error: Failed to skip to next track: No active player\n"},
		{[]string{"media", "next", "1"}, "Error: No media players available\n"},
		{[]string{"media", "list"}, ""},
	}
	for _, c := range cases[:2] {
		if _, stderr, code := runCaptured(t, false, c.args...); code != 1 || stderr != c.want {
			t.Errorf("%v: code %d %q, want %q", c.args, code, stderr, c.want)
		}
	}
	if stdout, _, _ := runCaptured(t, false, "media", "list"); stdout != "No media players found\n" {
		t.Errorf("empty list: %q", stdout)
	}
	if stdout, _, _ := runCaptured(t, false, "media", "active"); stdout != "No active player set\n" {
		t.Errorf("no active: %q", stdout)
	}

	startPlayer(t, "vlc", "VLC", "Paused", false)
	waitFor(t, func() bool { return len(c.Players()) == 1 })
	cases = []struct {
		args []string
		want string
	}{
		{[]string{"media", "next", "0"}, "Error: Player numbers start at 1\n"},
		{[]string{"media", "next", "5"}, "Error: Player 5 not found (available: 1-1)\n"},
		{[]string{"media", "next", "mpv"}, "Error: No player matching 'mpv'\nAvailable players:\n  1. VLC\n"},
		// Without a Shuffle property the toggle's Set fails in the player.
		{[]string{"media", "shuffle"}, "Error: Failed to set shuffle: cannot control player: set shuffle: org.freedesktop.DBus.Properties.Error.PropertyNotFound"},
	}
	for _, c := range cases {
		_, stderr, code := runCaptured(t, false, c.args...)
		if code != 1 || !strings.HasPrefix(stderr, c.want) {
			t.Errorf("%v: code %d %q, want %q", c.args, code, stderr, c.want)
		}
	}
}

// activeName is the media service's active bus name, "" when none.
func activeName(s *mpris.Service) string {
	p, _ := s.Active()
	return p.BusName
}

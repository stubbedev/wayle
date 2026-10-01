package mpris

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/stubbedev/wayle/internal/dbustest"
)

func TestStateFromString(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want PlaybackState
	}{
		{"Playing", StatePlaying},
		{"Paused", StatePaused},
		{"Stopped", StateStopped},
		{"\nPlaying\n", StatePlaying},
		{"garbage", StateStopped},
	} {
		if got := StateFromString(tc.raw); got != tc.want {
			t.Errorf("StateFromString(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestLoopModeParseAndCycle(t *testing.T) {
	for raw, want := range map[string]LoopMode{"None": LoopNone, "Track": LoopTrack, "Playlist": LoopPlaylist, "Sometimes": LoopUnsupported} {
		if got := ParseLoopMode(raw); got != want {
			t.Errorf("ParseLoopMode(%q) = %v, want %v", raw, got, want)
		}
	}
	// toggle_loop: None -> Track -> Playlist -> None.
	mode := LoopNone
	for _, want := range []LoopMode{LoopTrack, LoopPlaylist, LoopNone} {
		next, ok := mode.Next()
		if !ok || next != want {
			t.Fatalf("%v.Next() = %v, %v; want %v", mode, next, ok, want)
		}
		mode = next
	}
	if _, ok := LoopUnsupported.Next(); ok {
		t.Error("an unsupported loop mode must not cycle")
	}
}

func TestSelectBest(t *testing.T) {
	vlc := Player{BusName: BusNamePrefix + "vlc"}
	spotify := Player{BusName: BusNamePrefix + "spotify", State: StatePlaying}
	mpv := Player{BusName: BusNamePrefix + "mpv"}
	players := []Player{vlc, spotify, mpv}

	if got, _ := SelectBest(players, []string{"*mpv*", "*vlc*"}); got.BusName != mpv.BusName {
		t.Errorf("priority = %q, want the first matching pattern's player (mpv)", got.BusName)
	}
	if got, _ := SelectBest(players, []string{"*nope*"}); got.BusName != spotify.BusName {
		t.Errorf("no priority match = %q, want the playing player", got.BusName)
	}
	if got, _ := SelectBest([]Player{vlc, mpv}, nil); got.BusName != vlc.BusName {
		t.Errorf("nothing playing = %q, want the first player", got.BusName)
	}
	if _, ok := SelectBest(nil, nil); ok {
		t.Error("no players must select none")
	}
	// Priority patterns are case-sensitive wildcards.
	if got, _ := SelectBest(players, []string{"*MPV*"}); got.BusName == mpv.BusName {
		t.Error("a case-mismatched pattern matched")
	}
}

func TestIgnoredIsASubstringMatch(t *testing.T) {
	if !Ignored("org.mpris.MediaPlayer2.chromium.instance123", []string{"chromium"}) {
		t.Error("substring pattern did not ignore")
	}
	if Ignored("org.mpris.MediaPlayer2.vlc", []string{"spotify", "chrome"}) {
		t.Error("unrelated patterns ignored vlc")
	}
	if Ignored("org.mpris.MediaPlayer2.vlc", nil) {
		t.Error("no patterns ignored vlc")
	}
}

func TestParsePlayer(t *testing.T) {
	root := map[string]dbus.Variant{"Identity": dbus.MakeVariant("VLC"), "DesktopEntry": dbus.MakeVariant("vlc")}
	meta := map[string]dbus.Variant{
		"xesam:title":   dbus.MakeVariant("Song"),
		"xesam:artist":  dbus.MakeVariant([]string{"One", "Two"}),
		"xesam:album":   dbus.MakeVariant("Album"),
		"mpris:artUrl":  dbus.MakeVariant("file:///tmp/art.png"),
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/track/1")),
		"mpris:length":  dbus.MakeVariant(int64(90_000_000)),
	}
	player := map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant("Paused"),
		"Metadata":       dbus.MakeVariant(meta),
		"LoopStatus":     dbus.MakeVariant("Track"),
		"CanGoNext":      dbus.MakeVariant(true),
	}
	p := parsePlayer(BusNamePrefix+"vlc", root, player)
	if p.Identity != "VLC" || p.DesktopEntry != "vlc" || p.Title != "Song" || p.Artist != "One, Two" || p.Album != "Album" {
		t.Errorf("text fields = %+v", p)
	}
	if p.Length != 90*time.Second || p.TrackID != "/track/1" || p.ArtURL != "file:///tmp/art.png" {
		t.Errorf("length/track/art = %v %q %q", p.Length, p.TrackID, p.ArtURL)
	}
	if p.State != StatePaused || !p.CanGoNext || p.CanGoPrevious {
		t.Errorf("state/caps = %+v", p)
	}
	if !p.CanLoop || p.Loop != LoopTrack {
		t.Errorf("loop = %v (can %v), want Track", p.Loop, p.CanLoop)
	}
	// No Shuffle property: unsupported.
	if p.CanShuffle {
		t.Error("a player without Shuffle reported it")
	}
	// No LoopStatus: unsupported.
	if q := parsePlayer("x", nil, map[string]dbus.Variant{}); q.CanLoop || q.Loop != LoopUnsupported || q.Length != 0 {
		t.Errorf("bare player = %+v", q)
	}
}

// fakePlayer is an MPRIS player on the private bus, recording the
// transport calls it receives.
type fakePlayer struct {
	mu    sync.Mutex
	calls []string
	props *prop.Properties
	conn  *dbus.Conn
}

func (f *fakePlayer) record(call string) *dbus.Error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	return nil
}

func (f *fakePlayer) PlayPause() *dbus.Error { return f.record("PlayPause") }
func (f *fakePlayer) Next() *dbus.Error      { return f.record("Next") }
func (f *fakePlayer) Previous() *dbus.Error  { return f.record("Previous") }

func (f *fakePlayer) SetPosition(track dbus.ObjectPath, pos int64) *dbus.Error {
	return f.record("SetPosition " + string(track) + " " + time.Duration(pos*1000).String())
}

func (f *fakePlayer) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func startFakePlayer(t *testing.T, bus *dbustest.Bus, name, title string, status string) *fakePlayer {
	t.Helper()
	conn := bus.Conn(t)
	f := &fakePlayer{conn: conn}
	if err := conn.Export(f, ObjectPath, PlayerIface); err != nil {
		t.Fatal(err)
	}
	props, err := prop.Export(conn, ObjectPath, prop.Map{
		RootIface: {
			"Identity":     {Value: title + " player", Emit: prop.EmitConst},
			"DesktopEntry": {Value: "fake", Emit: prop.EmitConst},
		},
		PlayerIface: {
			"PlaybackStatus": {Value: status, Emit: prop.EmitTrue},
			"LoopStatus":     {Value: "None", Writable: true, Emit: prop.EmitTrue},
			"Shuffle":        {Value: false, Writable: true, Emit: prop.EmitTrue},
			"Position":       {Value: int64(42_000_000), Emit: prop.EmitFalse},
			"CanGoNext":      {Value: true, Emit: prop.EmitTrue},
			"CanGoPrevious":  {Value: true, Emit: prop.EmitTrue},
			"CanSeek":        {Value: true, Emit: prop.EmitTrue},
			"Metadata": {Value: map[string]dbus.Variant{
				"xesam:title":   dbus.MakeVariant(title),
				"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/t/1")),
				"mpris:length":  dbus.MakeVariant(int64(120_000_000)),
			}, Emit: prop.EmitTrue},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.props = props
	if reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request %s: %v %v", name, reply, err)
	}
	return f
}

// waitFor polls cond until it holds or a second passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServiceFollowsPlayersOnABus(t *testing.T) {
	bus := dbustest.Start(t)
	vlcName := BusNamePrefix + "vlc"
	vlc := startFakePlayer(t, bus, vlcName, "First", "Paused")
	startFakePlayer(t, bus, BusNamePrefix+"ignored.instance1", "Hidden", "Playing")

	svc, err := New(bus.Conn(t), []string{"ignored"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	feed, _ := svc.Subscribe()

	// Discovery skips the ignored player.
	if got := svc.Players(); len(got) != 1 || got[0].BusName != vlcName {
		t.Fatalf("players = %+v, want only vlc", got)
	}
	active, ok := svc.Active()
	if !ok || active.Title != "First" || active.Identity != "First player" || active.Length != 2*time.Minute {
		t.Fatalf("active = %+v", active)
	}

	// A PropertiesChanged re-reads the snapshot and ticks.
	vlc.props.SetMust(PlayerIface, "PlaybackStatus", "Playing")
	waitFor(t, "the playing state", func() bool {
		p, _ := svc.Active()
		return p.State == StatePlaying
	})
	select {
	case <-feed:
	case <-time.After(time.Second):
		t.Fatal("no tick for the property change")
	}

	// A second player arriving re-selects: the playing one wins.
	mpvName := BusNamePrefix + "mpv"
	mpv := startFakePlayer(t, bus, mpvName, "Second", "Paused")
	waitFor(t, "the second player", func() bool { return len(svc.Players()) == 2 })
	if p, _ := svc.Active(); p.BusName != vlcName {
		t.Errorf("active after mpv joined = %q, want the playing vlc", p.BusName)
	}
	// The picker overrides; unknown names are errors.
	if err := svc.SetActive(mpvName); err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.Active(); p.BusName != mpvName {
		t.Errorf("active after SetActive = %q", p.BusName)
	}
	if err := svc.SetActive(BusNamePrefix + "ghost"); err == nil {
		t.Error("SetActive on an unknown player succeeded")
	}

	// Controls reach the player.
	ctx := context.Background()
	for _, fn := range []func(context.Context, string) error{svc.PlayPause, svc.Next, svc.Previous} {
		if err := fn(ctx, mpvName); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SetPosition(ctx, mpvName, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	want := []string{"PlayPause", "Next", "Previous", "SetPosition /t/1 30s"}
	if got := mpv.Calls(); len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i, call := range mpv.Calls() {
		if call != want[i] {
			t.Errorf("call %d = %q, want %q", i, call, want[i])
		}
	}
	if err := svc.ToggleLoop(ctx, mpvName); err != nil {
		t.Fatal(err)
	}
	if err := svc.ToggleShuffle(ctx, mpvName); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "loop and shuffle", func() bool {
		for _, p := range svc.Players() {
			if p.BusName == mpvName {
				return p.Loop == LoopTrack && p.Shuffle
			}
		}
		return false
	})
	if pos, err := svc.Position(ctx, mpvName); err != nil || pos != 42*time.Second {
		t.Errorf("position = %v, %v; want 42s", pos, err)
	}
	if err := svc.PlayPause(ctx, BusNamePrefix+"ghost"); err == nil {
		t.Error("a control on an unknown player succeeded")
	}

	// The active player leaving re-selects among the rest.
	if _, err := mpv.conn.ReleaseName(mpvName); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "mpv to leave", func() bool { return len(svc.Players()) == 1 })
	if p, _ := svc.Active(); p.BusName != vlcName {
		t.Errorf("active after mpv left = %q, want vlc", p.BusName)
	}
}

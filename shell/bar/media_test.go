package bar

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/mpris"
)

// fakeMediaSource is a scripted mpris.Source recording the controls.
type fakeMediaSource struct {
	mu      sync.Mutex
	players []mpris.Player
	active  int // index into players; -1 for none
	calls   []string
	feed    chan struct{}
}

func newFakeMedia(players ...mpris.Player) *fakeMediaSource {
	active := -1
	if len(players) > 0 {
		active = 0
	}
	return &fakeMediaSource{players: players, active: active, feed: make(chan struct{}, 4)}
}

func (f *fakeMediaSource) Players() []mpris.Player { return f.players }

func (f *fakeMediaSource) Active() (mpris.Player, bool) {
	if f.active < 0 {
		return mpris.Player{}, false
	}
	return f.players[f.active], true
}

func (f *fakeMediaSource) SetActive(bus string) error {
	for i, p := range f.players {
		if p.BusName == bus {
			f.active = i
			return nil
		}
	}
	return mpris.ErrNoPlayer
}

func (f *fakeMediaSource) Subscribe() <-chan struct{} { return f.feed }

func (f *fakeMediaSource) record(call string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	return nil
}

func (f *fakeMediaSource) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeMediaSource) PlayPause(_ context.Context, bus string) error {
	return f.record("play-pause " + bus)
}

func (f *fakeMediaSource) Next(_ context.Context, bus string) error { return f.record("next " + bus) }

func (f *fakeMediaSource) Previous(_ context.Context, bus string) error {
	return f.record("previous " + bus)
}

func (f *fakeMediaSource) SetPosition(_ context.Context, bus string, pos time.Duration) error {
	return f.record("seek " + bus + " " + pos.String())
}

func (f *fakeMediaSource) ToggleLoop(_ context.Context, bus string) error {
	return f.record("loop " + bus)
}

func (f *fakeMediaSource) ToggleShuffle(_ context.Context, bus string) error {
	return f.record("shuffle " + bus)
}

func (f *fakeMediaSource) Position(context.Context, string) (time.Duration, error) {
	return 30 * time.Second, nil
}

// waitCalls polls until the fake saw n calls.
func waitCalls(t *testing.T, f *fakeMediaSource, n int) []string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(f.Calls()) < n && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	return f.Calls()
}

func TestMediaLabelPlaceholdersMatchRustAssertions(t *testing.T) {
	playing := mpris.Player{Title: "Song Name", Artist: "Artist Name", Album: "Album Name", State: mpris.StatePlaying}
	// helpers.rs format_label_basic_placeholders.
	if got := mediaLabel("{{ title }} - {{ artist }}", playing); got != "Song Name - Artist Name" {
		t.Errorf("= %q, want the title-artist join", got)
	}
	// format_label_all_placeholders, with helpers.rs's PLAY_ICON.
	if got := mediaLabel("{{ status_icon }} {{ title }} by {{ artist }} from {{ album }} ({{ status }})", playing); got != mediaPlayGlyph+" Song Name by Artist Name from Album Name (Playing)" {
		t.Errorf("= %q", got)
	}
	if got := mediaLabel("{{status_icon}} {{status}}", mpris.Player{State: mpris.StatePaused}); got != mediaPauseGlyph+" Paused" {
		t.Errorf("= %q, want the pause glyph and status", got)
	}
	if got := mediaLabel("{{ status_icon }} {{ status }}", mpris.Player{}); got != mediaStopGlyph+" Stopped" {
		t.Errorf("stopped = %q", got)
	}
}

func TestTruncateLabelEllipsizes(t *testing.T) {
	if got := truncateLabel("short", 35); got != "short" {
		t.Errorf("short label = %q", got)
	}
	got := truncateLabel(strings.Repeat("x", 40), 35)
	if len([]rune(got)) != 35 || !strings.HasSuffix(got, "…") {
		t.Errorf("long label = %d runes, want 35 with an ellipsis", len([]rune(got)))
	}
	if got := truncateLabel("unchanged", 0); got != "unchanged" {
		t.Errorf("max=0 disables truncation, got %q", got)
	}
}

func TestMediaIconResolution(t *testing.T) {
	spotify := mpris.Player{BusName: "org.mpris.MediaPlayer2.spotify", DesktopEntry: "spotify"}
	obscure := mpris.Player{BusName: "org.mpris.MediaPlayer2.obscure", DesktopEntry: "obscure"}
	bare := mpris.Player{BusName: "org.mpris.MediaPlayer2.bare"}
	env := func(have ...string) mediaIconEnv {
		return mediaIconEnv{
			exists: func(n string) bool { return slices.Contains(have, n) },
			desktopIcon: func(entry string) (string, bool) {
				if entry == "obscure" {
					return "obscure-app", true
				}
				return "", false
			},
		}
	}
	cfg := config.DefaultsMedia()
	for _, tc := range []struct {
		name string
		mode config.MediaIconType
		user map[string]string
		p    mpris.Player
		env  mediaIconEnv
		want string
	}{
		{"default mode", config.MediaIconTypeDefault, nil, spotify, env("ld-music-symbolic"), "ld-music-symbolic"},
		{"disc mode", config.MediaIconTypeSpinningDisc, nil, spotify, env("ld-disc-3-symbolic"), "ld-disc-3-symbolic"},
		{"mapped built-in", config.MediaIconTypeApplicationMapped, nil, spotify, env("si-spotify-symbolic"), "si-spotify-symbolic"},
		{"user mapping wins", config.MediaIconTypeApplicationMapped, map[string]string{"*spotify*": "my-spotify"}, spotify, env("my-spotify", "si-spotify-symbolic"), "my-spotify"},
		{"mapped falls to the entry's -symbolic", config.MediaIconTypeApplicationMapped, nil, obscure, env("obscure-symbolic"), "obscure-symbolic"},
		{"mapped missing tries the desktop icon", config.MediaIconTypeApplicationMapped, nil, obscure, env(), "obscure-app"},
		{"mapped with nothing is icon-name", config.MediaIconTypeApplicationMapped, nil, bare, env(), "ld-music-symbolic"},
		{"application reads the desktop entry", config.MediaIconTypeApplication, nil, obscure, env(), "obscure-app"},
		{"application without one is icon-name", config.MediaIconTypeApplication, nil, spotify, env("spotify-symbolic"), "ld-music-symbolic"},
		{"a missing resolved icon is icon-name", config.MediaIconTypeSpinningDisc, nil, spotify, env(), "ld-music-symbolic"},
	} {
		c := cfg
		c.IconType, c.PlayerIcons = tc.mode, tc.user
		if got := mediaIconName(c, tc.p, tc.env); got != tc.want {
			t.Errorf("%s: icon = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestMediaModuleRenders(t *testing.T) {
	cfg := config.Defaults()
	cfg.Media.IconType = config.MediaIconTypeSpinningDisc
	source := newFakeMedia(mpris.Player{BusName: "org.mpris.MediaPlayer2.x", Title: strings.Repeat("t", 50), Artist: "Band", State: mpris.StatePlaying})
	ctx := newTestContext(t, cfg)
	ctx.Media = source
	module, err := Create("media", ctx)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m := module.(*mediaModule)
	got := m.label.Text()
	if len([]rune(got)) != 35 || !strings.HasSuffix(got, "…") {
		t.Errorf("label = %q (%d runes), want the default format truncated to 35", got, len([]rune(got)))
	}
	root := m.root.(*barButton)
	if !root.HasClass("media-disc") || !root.HasClass("media-spinning") {
		t.Error("a playing player in disc mode misses media-disc/media-spinning")
	}

	// No player: the "--" label, icon-name, and no disc classes.
	source.active = -1
	m.refresh()
	if m.label.Text() != mediaNoPlayerLabel {
		t.Errorf("no-player label = %q, want --", m.label.Text())
	}
	if m.icon.Name() != cfg.Media.IconName {
		t.Errorf("no-player icon = %q, want icon-name", m.icon.Name())
	}
	if root.HasClass("media-disc") || root.HasClass("media-spinning") {
		t.Error("disc classes stayed without a player")
	}
}

func TestNewMediaRequiresSource(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Media = nil
	if _, err := Create("media", ctx); err == nil {
		t.Fatal("nil mpris source: want an error, got a module")
	}
}

func TestLoadFileAppliesMedia(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[modules.media]\nformat = \"{{ artist }}: {{ title }}\"\nlabel-max-length = 20\nlabel-show = false\n" +
		"icon-type = \"spinning-disc\"\nplayers-ignored = [\"chromium\"]\nplayer-priority = [\"*spotify*\"]\n" +
		"[modules.media.player-icons]\n\"*zzz*\" = \"z\"\n\"*aaa*\" = \"a\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	m := c.Media
	if m.Format != "{{ artist }}: {{ title }}" || m.LabelMaxLength != 20 || m.LabelShow || m.IconType != config.MediaIconTypeSpinningDisc {
		t.Errorf("config = %+v", m)
	}
	if len(m.PlayersIgnored) != 1 || len(m.PlayerPriority) != 1 {
		t.Errorf("ignored/priority = %v %v", m.PlayersIgnored, m.PlayerPriority)
	}
	// player-icons walk in pattern order (the BTreeMap).
	if mappings := m.PlayerIconMappings(); len(mappings) != 2 || mappings[0].Pattern != "*aaa*" {
		t.Errorf("player-icons = %v, want sorted", mappings)
	}
	if d := config.DefaultsMedia(); d.IconType != config.MediaIconTypeApplicationMapped || d.SpinningDiscIcon != "ld-disc-3-symbolic" || d.IconName != "ld-music-symbolic" {
		t.Errorf("defaults = %+v", d)
	}
}

func TestLoadFileRejectsBadMedia(t *testing.T) {
	for _, content := range []string{
		"[modules.media]\nlabel-max-length = -1\n",
		"[modules.media]\nicon-type = \"spinning\"\n",
		"[modules.media]\nplayers-ignored = \"chromium\"\n",
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error, got nil", content)
		}
	}
}

func TestMediaDurationAndProgress(t *testing.T) {
	for d, want := range map[time.Duration]string{0: "0:00", 65 * time.Second: "1:05", 354 * time.Second: "5:54", 3723 * time.Second: "1:02:03"} {
		if got := formatMediaDuration(d); got != want {
			t.Errorf("format(%v) = %q, want %q", d, got, want)
		}
	}
	if got := mediaProgress(30*time.Second, time.Minute); got != 50 {
		t.Errorf("progress = %v, want 50", got)
	}
	if got := mediaProgress(2*time.Minute, time.Minute); got != 100 {
		t.Errorf("past the end = %v, want clamped 100", got)
	}
	if got := mediaProgress(time.Second, 0); got != 0 {
		t.Errorf("unknown length = %v, want 0", got)
	}
}

func TestMediaDropdownTransport(t *testing.T) {
	cfg := config.Defaults()
	bus := "org.mpris.MediaPlayer2.vlc"
	player := mpris.Player{
		BusName: bus, Identity: "VLC", Title: "Song", State: mpris.StatePlaying, Length: time.Minute,
		CanGoNext: true, CanGoPrevious: false, CanSeek: true, CanLoop: true, Loop: mpris.LoopTrack,
	}
	source := newFakeMedia(player, mpris.Player{BusName: "org.mpris.MediaPlayer2.mpv", Identity: "mpv"})
	ctx := newTestContext(t, cfg)
	ctx.Media = source
	v := mediaDropdown(ctx).(*mediaView)
	defer v.dropdownClosed()

	if v.mode != "player" || v.title.Text() != "Song" || v.artist.Text() != mediaUnknownArtist {
		t.Fatalf("player view = mode %q, title %q, artist %q", v.mode, v.title.Text(), v.artist.Text())
	}
	if v.playIcon.Name() != "ld-pause-symbolic" || v.loopIcon.Name() != "ld-repeat-1-symbolic" {
		t.Errorf("icons = %q/%q, want pause and repeat-1", v.playIcon.Name(), v.loopIcon.Name())
	}
	if !v.loop.HasClass("active") || v.shuffle.HasClass("active") {
		t.Error("loop active / shuffle inactive classes wrong")
	}
	if !widget.IsEnabled(v.next) || widget.IsEnabled(v.previous) || widget.IsEnabled(v.shuffle) {
		t.Error("capabilities did not gate the buttons")
	}
	if v.length.Text() != "1:00" {
		t.Errorf("length = %q", v.length.Text())
	}

	// The buttons drive the active player.
	v.playPause.OnClick()
	v.next.OnClick()
	v.loop.OnClick()
	// A seek commits on release, at the percent of the length.
	v.seek.SetPressed(true)
	v.seek.SetValue(25)
	v.seek.SetPressed(false)
	calls := waitCalls(t, source, 4)
	want := map[string]bool{"play-pause " + bus: true, "next " + bus: true, "loop " + bus: true, "seek " + bus + " 15s": true}
	for _, c := range calls {
		if !want[c] {
			t.Errorf("unexpected call %q", c)
		}
		delete(want, c)
	}
	if len(want) != 0 {
		t.Errorf("missing calls %v (got %v)", want, calls)
	}

	// A position read moves the knob unless it is held.
	v.setPosition(45 * time.Second)
	if v.seek.Value() != 75 || v.position.Text() != "0:45" {
		t.Errorf("position = %v / %q, want 75 / 0:45", v.seek.Value(), v.position.Text())
	}
	v.seek.SetPressed(true)
	v.setPosition(0)
	if v.seek.Value() != 75 {
		t.Error("a position read moved the held knob")
	}
	v.seek.SetPressed(false)

	// The picker switches the active player and returns to the view.
	v.showPicker()
	if v.mode != "picker" {
		t.Fatal("source button did not open the picker")
	}
	source.SetActive("org.mpris.MediaPlayer2.mpv")
	v.mode = ""
	v.refresh()
	if v.title.Text() != mediaUnknownTitle {
		t.Errorf("after switching = %q, want mpv's placeholder title", v.title.Text())
	}

	// No player: the empty state.
	source.active = -1
	v.refresh()
	if v.mode != "empty" {
		t.Errorf("mode without a player = %q, want empty", v.mode)
	}
}

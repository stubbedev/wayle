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
	"github.com/stubbedev/wayle/i18n"
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

func (f *fakeMediaSource) Subscribe() (<-chan struct{}, func()) { return f.feed, func() {} }

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
	// appendModule hands the module its wrapper; the disc classes are
	// ancestor selectors on it.
	m.setChrome(widget.NewBox(widget.Row, 0, 0))
	// The text is whole; label-max-length caps the button's label width
	// (max-width-chars) once appendModule configures it.
	if got := m.label.Text(); got != strings.Repeat("t", 50)+" - Band" {
		t.Errorf("label = %q, want the whole default format", got)
	}
	root := m.root.(*barButton)
	root.configure(moduleButton("media", cfg), config.ClickConfig{}, nil)
	if root.label.MaxWidthChars() != int(cfg.Media.LabelMaxLength) || root.label.Ellipsize() != widget.EllipsizeEnd {
		t.Errorf("label cap = %d chars, ellipsize %v; want %d, end", root.label.MaxWidthChars(), root.label.Ellipsize(), cfg.Media.LabelMaxLength)
	}
	if m.chrome == nil || !widget.HasClass(m.chrome.(widget.Widget), "media-disc") || !widget.HasClass(m.chrome.(widget.Widget), "media-spinning") {
		t.Error("a playing player in disc mode misses media-disc/media-spinning on the module wrapper")
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
	if widget.HasClass(m.chrome.(widget.Widget), "media-disc") || widget.HasClass(m.chrome.(widget.Widget), "media-spinning") {
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

	if v.mode != "player" || v.title.Text() != "Song" || v.artist.Text() != i18n.T("dropdown-media-unknown-artist") {
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
	// A drag seeks at the percent of the length: the first move commits,
	// and the release commits again (DebouncedSlider).
	v.seek.Knob.SetPressed(true)
	v.seek.Knob.SetValue(25)
	v.seek.Knob.SetPressed(false)
	calls := waitCalls(t, source, 5)
	want := map[string]bool{"play-pause " + bus: true, "next " + bus: true, "loop " + bus: true, "seek " + bus + " 15s": true}
	seeks := 0
	for _, c := range calls {
		if c == "seek "+bus+" 15s" {
			seeks++
		}
		if !want[c] && c != "seek "+bus+" 15s" {
			t.Errorf("unexpected call %q", c)
		}
		delete(want, c)
	}
	if seeks != 2 {
		t.Errorf("seeks = %d, want the move and the release", seeks)
	}
	if len(want) != 0 {
		t.Errorf("missing calls %v (got %v)", want, calls)
	}

	// A position read moves the knob unless it is held (or just let go).
	v.seek.SetClock(func() time.Time { return time.Now().Add(time.Second) })
	v.setPosition(45 * time.Second)
	if v.seek.Value() != 75 || v.position.Text() != "0:45" {
		t.Errorf("position = %v / %q, want 75 / 0:45", v.seek.Value(), v.position.Text())
	}
	v.seek.Knob.SetPressed(true)
	v.setPosition(0)
	if v.seek.Value() != 75 {
		t.Error("a position read moved the held knob")
	}
	v.seek.Knob.SetPressed(false)

	// The picker switches the active player and returns to the view.
	v.showPicker()
	if v.mode != "picker" || v.pages.Visible() != "sources" || !v.pages.Switching() {
		t.Fatalf("the picker: mode %q, page %q, sliding %v; want the sources page sliding in", v.mode, v.pages.Visible(), v.pages.Switching())
	}
	source.SetActive("org.mpris.MediaPlayer2.mpv")
	v.mode = ""
	v.refresh()
	if v.title.Text() != i18n.T("dropdown-media-unknown-title") {
		t.Errorf("after switching = %q, want mpv's placeholder title", v.title.Text())
	}
	if v.pages.Visible() != "main" {
		t.Errorf("after picking, page %q, want back on main", v.pages.Visible())
	}

	// No player: the empty state.
	source.active = -1
	v.refresh()
	if v.mode != "empty" {
		t.Errorf("mode without a player = %q, want empty", v.mode)
	}
}

// The media panel paints from the stylesheet: the transport buttons
// carry no Go hover fills (button.media-control paints them), the
// labels take the cascade's ink through their classes, unknown
// metadata is the .placeholder state, and the seek class sits on the
// scale the slider rules target.
func TestMediaDropdownPaintsFromTheStylesheet(t *testing.T) {
	cfg := config.Defaults()
	ctx := styledContext(t, cfg)
	ctx.Media = newFakeMedia(mpris.Player{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC", Title: "Song", State: mpris.StatePlaying})
	v := mediaDropdown(ctx).(*mediaView)
	defer v.dropdownClosed()

	for _, b := range []*widget.Button{v.shuffle, v.previous, v.playPause, v.next, v.loop} {
		if b.BgHover != 0 || b.BgPressed != 0 {
			t.Error("a transport button carries Go hover fills")
		}
	}
	if got := v.title.Color(); got != 0 {
		t.Errorf("the title carries a programmatic color %#08x", uint32(got))
	}
	if got := v.position.Color(); got != 0 {
		t.Errorf("the position label carries a programmatic color %#08x", uint32(got))
	}
	if !v.seek.Knob.HasClass("media-seek-slider") {
		t.Error("the seek class is not on the scale")
	}
	// Unknown metadata is the placeholder state; known metadata is not.
	if v.title.HasClass("placeholder") || !v.artist.HasClass("placeholder") || !v.album.HasClass("placeholder") {
		t.Error("the placeholder state does not follow the raw fields")
	}

	// The picker is a classed list of option buttons with no Go fills;
	// the active player's option is the selected state.
	v.showPicker()
	var option *widget.Button
	list := false
	walkTree(v.sources, func(w widget.Widget) bool {
		switch w := w.(type) {
		case *widget.Button:
			if w.HasClass("media-source-option") {
				option = w
			}
		case *widget.Box:
			if w.HasClass("media-source-list") {
				list = true
			}
		}
		return true
	})
	if option == nil || !list {
		t.Fatalf("the picker = option %v, list %v; want a classed option in a media-source-list", option, list)
	}
	if option.BgHover != 0 || option.BgPressed != 0 {
		t.Error("the source option carries Go hover fills")
	}
	if !option.HasClass("selected") {
		t.Error("the active player's option is not selected")
	}
}

// The picker's chrome is the Rust tree: the header is the ghost-icon
// picker-back chain over the title, the rows scroll in a .picker-body,
// and the active row checks with tb-check-symbolic while the others
// carry none (source_item.rs:75, source_picker/mod.rs:36-65).
func TestMediaPickerChrome(t *testing.T) {
	source := newFakeMedia(
		mpris.Player{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC"},
		mpris.Player{BusName: "org.mpris.MediaPlayer2.mpv", Identity: "mpv"},
	)
	ctx := newTestContext(t, config.Defaults())
	ctx.Media = source
	v := mediaDropdown(ctx).(*mediaView)
	defer v.dropdownClosed()
	v.showPicker()

	var back *widget.Button
	var body *widget.Scroll
	walkTree(v.sources, func(w widget.Widget) bool {
		switch w := w.(type) {
		case *widget.Button:
			if w.HasClass("picker-back") {
				back = w
			}
		case *widget.Scroll:
			if w.HasClass("picker-body") {
				body = w
			}
		}
		return true
	})
	if back == nil {
		t.Fatal("the picker has no back button")
	}
	if !back.HasClass("ghost-icon") || !back.HasClass("picker-back") {
		t.Errorf("back button classes = %v, want ghost-icon + picker-back", back.Classes())
	}
	if body == nil {
		t.Error("the rows do not scroll in a .picker-body")
	}
	// The active row checks with tb-check-symbolic (source_item.rs:75);
	// buttons hide their subtrees from the walks, so the view carries
	// the icon.
	if v.check == nil || v.check.Name() != "tb-check-symbolic" || !v.check.HasClass("media-source-option-check") {
		t.Errorf("check icon = %v, want the tb-check-symbolic media-source-option-check", v.check)
	}

	// Negative: only the back button wears the ghost chrome — the
	// option rows are plain classed buttons.
	var ghost *widget.Button
	walkTree(v.sources, func(w widget.Widget) bool {
		if b, ok := w.(*widget.Button); ok && b != back && b.HasClass("ghost-icon") {
			ghost = b
		}
		return true
	})
	if ghost != nil {
		t.Error("another picker button wears the ghost-icon chrome")
	}

	// Back is NavigateBack: the player (or empty) page returns.
	back.OnClick()
	if v.mode == "picker" || v.pages.Visible() != "main" {
		t.Errorf("after back = mode %q page %q, want the main page", v.mode, v.pages.Visible())
	}
}

// The transport buttons carry no programmatic padding (button.media-control
// paints the box, static.css:6057-6071) and the pointer cursor every
// button gets.
func TestMediaTransportButtonsAreUnpaddedPointers(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	ctx.Media = newFakeMedia(mpris.Player{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC", State: mpris.StatePlaying})
	v := mediaDropdown(ctx).(*mediaView)
	defer v.dropdownClosed()
	_, px := dropdownFont(ctx)
	for _, tc := range []struct {
		b     *widget.Button
		glyph string
		px    float64
	}{
		{v.shuffle, "ld-shuffle-symbolic", px},
		{v.previous, "ld-skip-back-symbolic", px},
		{v.playPause, "ld-play-symbolic", px * 1.4},
		{v.next, "ld-skip-forward-symbolic", px},
		{v.loop, "ld-repeat-symbolic", px},
	} {
		want := widget.NewThemeIcon(tc.glyph, int(tc.px)).Measure(widgetConstraintsMax(200, 200))
		if got := tc.b.Measure(widgetConstraintsMax(200, 200)); got != want {
			t.Errorf("%s measures %v, want the bare icon's %v (programmatic padding)", tc.glyph, got, want)
		}
		if got := widget.CursorNameOf(tc.b); got != "pointer" {
			t.Errorf("%s cursor = %q, want pointer", tc.glyph, got)
		}
	}
	// Negative: the constructor padding the Rust tree leaves out would
	// show up as a padded measure.
	padded := widget.NewButton(widget.NewThemeIcon("ld-play-symbolic", int(px)), 6, 6)
	if padded.Measure(widgetConstraintsMax(200, 200)) == widget.NewThemeIcon("ld-play-symbolic", int(px)).Measure(widgetConstraintsMax(200, 200)) {
		t.Error("a padded button measures like its icon; the test cannot see padding")
	}
}

// A player switch retires the old knob position (methods.rs:164,200):
// the slider restarts from zero for the new player, a clear zeroes it,
// and a same-player refresh keeps the polled position.
func TestMediaSeekResetsOnPlayerSwitch(t *testing.T) {
	source := newFakeMedia(
		mpris.Player{BusName: "org.mpris.MediaPlayer2.vlc", Identity: "VLC", Length: time.Minute, CanSeek: true},
		mpris.Player{BusName: "org.mpris.MediaPlayer2.mpv", Identity: "mpv", Length: time.Minute, CanSeek: true},
	)
	ctx := newTestContext(t, config.Defaults())
	ctx.Media = source
	v := mediaDropdown(ctx).(*mediaView)
	defer v.dropdownClosed()
	v.setPosition(45 * time.Second)
	if v.seek.Value() != 75 {
		t.Fatalf("knob = %v, want 75 before the switch", v.seek.Value())
	}
	v.refresh()
	if v.seek.Value() != 75 {
		t.Errorf("a same-player refresh reset the knob to %v", v.seek.Value())
	}
	source.SetActive("org.mpris.MediaPlayer2.mpv")
	v.refresh()
	if v.seek.Value() != 0 {
		t.Errorf("after the switch the knob = %v, want the new player's 0", v.seek.Value())
	}
	v.setPosition(45 * time.Second)
	if v.seek.Value() != 75 {
		t.Fatalf("the new player's knob = %v, want 75", v.seek.Value())
	}
	source.active = -1
	v.refresh()
	if v.seek.Value() != 0 {
		t.Errorf("without a player the knob = %v, want 0", v.seek.Value())
	}
}

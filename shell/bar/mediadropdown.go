package bar

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/shell/widgets"
)

// Media dropdown sizing the code owns; the strings come from locales
// dropdowns/_media.ftl (dropdown-media-*).
const (
	mediaPositionPollEvery = time.Second
)

// formatMediaDuration is helpers.rs format_duration: m:ss, or h:mm:ss
// from an hour up.
func formatMediaDuration(d time.Duration) string {
	total := int(d / time.Second)
	h, m, s := total/3600, total%3600/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// mediaProgress is helpers.rs progress_fraction as a 0..100 percent.
func mediaProgress(pos, length time.Duration) float64 {
	if length <= 0 {
		return 0
	}
	return min(max(float64(pos)/float64(length), 0), 1) * 100
}

// mediaSourceIcon is helpers.rs resolve_source_icon: the app table on
// the identity, then the bus name, then the desktop entry; else the
// entry's -symbolic name, or the music glyph without one.
func mediaSourceIcon(p mpris.Player) string {
	for _, name := range []string{p.Identity, p.BusName, p.DesktopEntry} {
		if name == "" {
			continue
		}
		if icon, ok := appicons.Lookup(name); ok {
			return icon
		}
	}
	if p.DesktopEntry != "" {
		return p.DesktopEntry + "-symbolic"
	}
	return "ld-music-symbolic"
}

// mediaPlayPauseIcon and mediaLoopIcon are player_view methods.rs's
// glyph picks.
func mediaPlayPauseIcon(state mpris.PlaybackState) string {
	if state == mpris.StatePlaying {
		return "ld-pause-symbolic"
	}
	return "ld-play-symbolic"
}

func mediaLoopIcon(mode mpris.LoopMode) string {
	if mode == mpris.LoopTrack {
		return "ld-repeat-1-symbolic"
	}
	return "ld-repeat-symbolic"
}

// orUnknown substitutes the placeholder for an empty field.
func orUnknown(value, placeholder string) string {
	if value == "" {
		return placeholder
	}
	return value
}

// mediaView is the media dropdown (dropdowns/media): the player view
// with transport controls, or the source picker, or the no-player
// empty state. It follows the service while open and stops on Close.
type mediaView struct {
	ctx ModuleContext
	src mpris.Source

	*widget.Box
	// pages slides between the main page (the player or the empty
	// state, in main) and the source picker (sources).
	pages   *widget.Stack
	main    *widget.Box
	sources *widget.Box
	player  *widget.Box
	empty   *widget.Box
	mode    string

	identity   *widget.Label
	sourceIcon *widget.Icon
	art        *widget.Box
	artURL     string
	title      *widget.Label
	artist     *widget.Label
	album      *widget.Label
	seek       *widgets.DebouncedSlider
	position   *widget.Label
	length     *widget.Label
	shuffle    *widget.Button
	previous   *widget.Button
	playPause  *widget.Button
	playIcon   *widget.Icon
	next       *widget.Button
	loop       *widget.Button
	loopIcon   *widget.Icon
	// check is the active row's tb-check glyph in the source picker
	// (buttons hide their subtrees from the tree walks, so the view
	// carries it, like every other stateful widget here).
	check *widget.Icon

	current mpris.Player
	has     bool
	// seekBus is the player the seek knob last followed: a switch or a
	// clear resets it, the way methods.rs refresh_from_player and
	// clear_fields re-set the slider from the (new) player's progress.
	seekBus string

	once sync.Once
	stop chan struct{}
}

// mediaDropdown builds the live media card.
func mediaDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	if ctx.Media == nil {
		return mediaEmptyState(ctx)
	}
	v := &mediaView{ctx: ctx, src: ctx.Media, stop: make(chan struct{})}
	v.Box = widget.NewBox(widget.Column, 0, 14)
	v.AddClass("dropdown", "media-dropdown")
	v.main = widget.NewBox(widget.Column, 0, 0)
	v.sources = widget.NewBox(widget.Column, 0, 0)
	v.pages = widget.NewStack()
	pageSlide(v.pages, ctx.Config)
	v.pages.Add("main", v.main)
	v.pages.Add("sources", v.sources)
	v.Append(v.pages, true)
	v.empty = mediaEmptyState(ctx)
	v.buildPlayer(font, px)
	v.refresh()
	v.follow()
	return v
}

// mediaEmptyState is the EmptyState template with the media strings.
func mediaEmptyState(ctx ModuleContext) *widget.Box {
	font, px := dropdownFont(ctx)
	return emptyState(font, px, "ld-play-symbolic",
		i18n.T("dropdown-media-no-player-title"), i18n.T("dropdown-media-no-player-description"))
}

// controlButton builds one transport button: button.media-control
// paints it (the ink, the hover fill, the sizes, and the icon margins
// static.css:6057-6071), so nothing is set here — no padding, the
// pointer cursor every button carries.
func (v *mediaView) controlButton(icon *widget.Icon, class string, onClick func()) *widget.Button {
	b := widget.NewButton(icon, 0, 0)
	b.AddClass("media-control")
	if class != "" {
		b.AddClass(class)
	}
	b.OnClick = onClick
	return b
}

// buildPlayer assembles the player view once; refresh fills it in.
func (v *mediaView) buildPlayer(font render.Font, px float64) {
	v.player = widget.NewBox(widget.Column, 0, 0)

	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("media-header")
	title := widget.NewLabel(font, px, i18n.T("dropdown-media-title"), 0)
	title.AddClass("media-header-title")
	header.Append(title, true)
	v.sourceIcon = widget.NewThemeIcon("ld-music-symbolic", int(px))
	v.sourceIcon.AddClass("media-source-icon")
	v.identity = widget.NewLabel(font, px*0.9, "", 0)
	v.identity.AddClass("media-source-name")
	v.identity.SetEllipsize(widget.EllipsizeEnd)
	chevron := widget.NewThemeIcon("ld-chevron-right-symbolic", int(px))
	chevron.AddClass("media-source-chevron")
	source := widget.NewBox(widget.Row, 0, 0)
	source.Append(v.sourceIcon, false)
	source.Append(v.identity, false)
	source.Append(chevron, false)
	sourceButton := widget.NewButton(source, 0, 0)
	sourceButton.AddClass("media-source-button")
	sourceButton.OnClick = v.showPicker
	header.Append(sourceButton, false)
	v.player.Append(header, false)

	// The artwork vexpands into the panel's leftover height (player_view
	// mod.rs:123-127); its floor is whatever the classes give it.
	v.art = widget.NewBox(widget.Row, 0, 0)
	v.art.AddClass("media-artwork")
	v.player.Append(v.art, true)

	info := widget.NewBox(widget.Column, 0, 0)
	info.AddClass("media-info")
	v.title = widget.NewLabel(font, px*1.2, "", 0)
	v.title.AddClass("media-title")
	v.artist = widget.NewLabel(font, px, "", 0)
	v.artist.AddClass("media-artist")
	v.album = widget.NewLabel(font, px*0.9, "", 0)
	v.album.AddClass("media-album")
	// The info rows fill the panel and ellipsize at its edge, centered
	// text (xalign 0.5) like player_view mod.rs:158-184.
	for _, l := range []*widget.Label{v.title, v.artist, v.album} {
		l.SetEllipsize(widget.EllipsizeEnd)
		l.SetAlignment(render.AlignCenter)
		info.Append(l, false)
	}
	v.player.Append(info, false)

	// The seek bar is a DebouncedSlider without its value label; the
	// scale carries the class the slider rules target.
	v.seek = widgets.NewDebouncedSlider(0, nil, 0, 0, v.ctx.Invoke)
	v.seek.Knob.AddClass("media-seek-slider")
	v.seek.OnCommit = v.seekTo
	times := widget.NewBox(widget.Row, 0, 0)
	times.AddClass("media-progress-times")
	v.position = widget.NewLabel(font, px*0.85, "0:00", 0)
	v.position.AddClass("media-time")
	v.length = widget.NewLabel(font, px*0.85, "0:00", 0)
	v.length.AddClass("media-time")
	times.Append(v.position, true)
	times.Append(v.length, false)
	progress := widget.NewBox(widget.Column, 0, 0)
	progress.AddClass("media-progress")
	progress.Append(v.seek, false)
	progress.Append(times, false)
	v.player.Append(progress, false)

	controls := widget.NewBox(widget.Row, 0, 0)
	controls.AddClass("media-controls")
	v.shuffle = v.controlButton(widget.NewThemeIcon("ld-shuffle-symbolic", int(px)), "secondary", func() { v.fire("toggle shuffle", v.src.ToggleShuffle) })
	v.previous = v.controlButton(widget.NewThemeIcon("ld-skip-back-symbolic", int(px)), "", func() { v.fire("previous track", v.src.Previous) })
	v.playIcon = widget.NewThemeIcon("ld-play-symbolic", int(px*1.4))
	v.playPause = v.controlButton(v.playIcon, "main", func() { v.fire("play/pause", v.src.PlayPause) })
	v.next = v.controlButton(widget.NewThemeIcon("ld-skip-forward-symbolic", int(px)), "", func() { v.fire("next track", v.src.Next) })
	v.loopIcon = widget.NewThemeIcon("ld-repeat-symbolic", int(px))
	v.loop = v.controlButton(v.loopIcon, "secondary", func() { v.fire("toggle loop", v.src.ToggleLoop) })
	for _, b := range []*widget.Button{v.shuffle, v.previous, v.playPause, v.next, v.loop} {
		controls.AppendAligned(b, false, widget.AlignCenter)
	}
	v.player.AppendAligned(controls, false, widget.AlignCenter)
}

// fire runs one transport command off the loop; failures log, as the
// Rust fire_player_command does.
func (v *mediaView) fire(what string, cmd func(context.Context, string) error) {
	if !v.has {
		return
	}
	bus := v.current.BusName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cmd(ctx, bus); err != nil {
			log.Printf("media: %s failed: %v", what, err)
		}
	}()
}

// seekTo is SeekCommitted: the percent of the track length; nothing
// without a known length.
func (v *mediaView) seekTo(percent float64) {
	if !v.has || v.current.Length <= 0 {
		return
	}
	target := time.Duration(float64(v.current.Length) * percent / 100)
	v.fire("seek", func(ctx context.Context, bus string) error {
		return v.src.SetPosition(ctx, bus, target)
	})
}

// show swaps the visible view; the page expands, so the empty state
// centers in it (empty_state's set_expand) and the player takes it.
func (v *mediaView) show(mode string, w widget.Widget) {
	if v.mode == mode {
		return
	}
	v.mode = mode
	v.main.Clear()
	v.main.Append(w, true)
	v.pages.Show("main")
}

// refresh repaints from the active player (PlayerChanged plus the
// metadata, capability, state, loop, and shuffle watchers at once).
func (v *mediaView) refresh() {
	p, ok := v.src.Active()
	v.current, v.has = p, ok
	// A player switch (or clearing) retires the old knob position: the
	// slider follows the new player's progress from zero until the next
	// position read (methods.rs:164,200).
	bus := ""
	if ok {
		bus = p.BusName
	}
	if bus != v.seekBus {
		v.seekBus = bus
		v.seek.Set(0)
	}
	if v.mode == "picker" {
		v.buildPicker()
		return
	}
	if !ok {
		v.show("empty", v.empty)
		return
	}
	v.show("player", v.player)
	v.identity.SetText(p.Identity)
	v.sourceIcon.SetThemeName(mediaSourceIcon(p))
	v.title.SetText(orUnknown(p.Title, i18n.T("dropdown-media-unknown-title")))
	setClass(v.title, "placeholder", p.Title == "")
	v.artist.SetText(orUnknown(p.Artist, i18n.T("dropdown-media-unknown-artist")))
	setClass(v.artist, "placeholder", p.Artist == "")
	v.album.SetText(orUnknown(p.Album, i18n.T("dropdown-media-unknown-album")))
	setClass(v.album, "placeholder", p.Album == "")
	v.setArt(p.ArtURL)
	v.length.SetText(lengthText(p.Length))
	v.seek.SetEnabled(p.CanSeek)
	v.shuffle.SetEnabled(p.CanShuffle)
	setClass(v.shuffle, "active", p.Shuffle)
	v.previous.SetEnabled(p.CanGoPrevious)
	v.next.SetEnabled(p.CanGoNext)
	v.loop.SetEnabled(p.CanLoop)
	setClass(v.loop, "active", p.Loop != mpris.LoopNone && p.Loop != mpris.LoopUnsupported)
	v.loopIcon.SetThemeName(mediaLoopIcon(p.Loop))
	v.playIcon.SetThemeName(mediaPlayPauseIcon(p.State))
}

// lengthText is the length label: 0:00 without a known length.
func lengthText(length time.Duration) string {
	if length <= 0 {
		return "0:00"
	}
	return formatMediaDuration(length)
}

// setArt swaps the cover when the art URL changes: a file:// path or
// an http(s) URL fills the panel, anything else is the disc
// placeholder centered in it (the art resolver's
// Ready/NeedsDownload/Unresolvable). The icon's constructor px is only
// the fallback: the .media-artwork-placeholder-icon rules size it.
func (v *mediaView) setArt(url string) {
	if url == v.artURL && len(v.art.Children()) > 0 {
		return
	}
	v.artURL = url
	_, px := dropdownFont(v.ctx)
	v.art.Clear()
	art := mediaArt(url, "ld-disc-3-symbolic", "media-artwork-placeholder", "media-artwork-placeholder-icon", int(px))
	if _, isImage := art.(*widget.Image); isImage {
		v.art.Append(art, true)
		return
	}
	// The placeholder tile centers in the panel (Rust halign/valign
	// center on the placeholder box).
	row := widget.NewBox(widget.Row, 0, 0)
	row.Append(widget.NewSpacer(0, 0), true)
	row.Append(art, false)
	row.Append(widget.NewSpacer(0, 0), true)
	v.art.AppendAligned(row, true, widget.AlignCenter)
}

// mediaArt is a player's cover: the art at a file or http(s) URL scaled
// to cover, else the placeholder glyph in its tile box (boxClass, whose
// rules paint the tile, with imgClass on the glyph).
func mediaArt(url, placeholder, boxClass, imgClass string, px int) widget.Widget {
	var img *widget.Image
	switch {
	case strings.HasPrefix(url, "file://"):
		img = widget.NewFileImage(strings.TrimPrefix(url, "file://"))
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		img = widget.NewURLImage(url)
	}
	if img == nil {
		return iconTile(widget.NewThemeIcon(placeholder, px), boxClass, imgClass)
	}
	img.SetScale(widget.ImageCover)
	return img
}

// setPosition applies a position read.
func (v *mediaView) setPosition(pos time.Duration) {
	if !v.has {
		return
	}
	v.position.SetText(formatMediaDuration(pos))
	v.seek.Set(mediaProgress(pos, v.current.Length))
}

// showPicker is ShowSourcePicker: the list of players.
func (v *mediaView) showPicker() {
	v.mode = "picker"
	v.buildPicker()
}

// buildPicker lists every player; picking one makes it active and
// returns to the player view (source_picker/mod.rs: the .picker-header
// row with the ghost-icon back button over the .picker-body scroll).
func (v *mediaView) buildPicker() {
	font, px := dropdownFont(v.ctx)
	col := widget.NewBox(widget.Column, 0, 0)
	col.AddClass("media-source-picker")
	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("picker-header")
	back := dropdownButton(widget.NewThemeIcon("ld-arrow-left-symbolic", int(px)), "picker-back", func() { v.mode = ""; v.refresh() })
	back.AddClass("ghost-icon")
	header.Append(back, false)
	headerLabel := widget.NewLabel(font, px, i18n.T("dropdown-media-sources"), 0)
	headerLabel.AddClass("picker-title")
	header.Append(headerLabel, true)
	col.Append(header, false)
	list := widget.NewBox(widget.Column, 0, 0)
	list.AddClass("media-source-list")
	col.Append(dropdownScroll(list, "picker-body"), true)
	active, _ := v.src.Active()
	v.check = nil
	for _, p := range v.src.Players() {
		// The row shows the identity as-is (methods.rs:39); CSS paints
		// the option (radius, padding, border-spacing).
		row := widget.NewBox(widget.Row, 0, 0)
		row.AddClass("media-source-option-content")
		icon := widget.NewThemeIcon(mediaSourceIcon(p), int(px))
		tile := widget.NewCenterBox(nil, icon, nil)
		tile.AddClass("media-source-option-icon")
		row.AppendAligned(tile, false, widget.AlignCenter)
		name := widget.NewLabel(font, px, p.Identity, 0)
		name.AddClass("media-source-option-name")
		name.SetEllipsize(widget.EllipsizeEnd)
		row.AppendAligned(name, true, widget.AlignCenter)
		if p.BusName == active.BusName {
			check := widget.NewThemeIcon("tb-check-symbolic", int(px))
			check.AddClass("media-source-option-check")
			row.Append(check, false)
			v.check = check
		}
		b := widget.NewButton(row, 0, 0)
		b.AddClass("media-source-option")
		setClass(b, "selected", p.BusName == active.BusName)
		bus := p.BusName
		b.OnClick = func() {
			if err := v.src.SetActive(bus); err != nil {
				log.Printf("media: select %s: %v", bus, err)
			}
			v.mode = ""
			v.refresh()
		}
		list.Append(b, false)
	}
	v.sources.Clear()
	v.sources.Append(col, true)
	v.pages.Show("sources")
}

// follow keeps the view current while open: service ticks refresh,
// and the position polls once a second (MPRIS signals no position
// changes; the Rust player polls at position_poll_interval).
func (v *mediaView) follow() {
	ticks, unsubscribe := v.src.Subscribe()
	v.pollPosition()
	go func() {
		defer unsubscribe()
		ticker := time.NewTicker(mediaPositionPollEvery)
		defer ticker.Stop()
		for {
			select {
			case <-v.stop:
				return
			case <-ticks:
				v.ctx.Invoke(v.refresh)
			case <-ticker.C:
				v.pollPosition()
			}
		}
	}()
}

// pollPosition reads the position off the loop and applies it on it.
func (v *mediaView) pollPosition() {
	if v.ctx.App == nil || !v.has {
		return
	}
	bus := v.current.BusName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		pos, err := v.src.Position(ctx, bus)
		if err != nil {
			return
		}
		v.ctx.Invoke(func() { v.setPosition(pos) })
	}()
}

// dropdownClosed implements dropdownCloser: stop following the
// service once the popover went away.
func (v *mediaView) dropdownClosed() { v.once.Do(func() { close(v.stop) }) }

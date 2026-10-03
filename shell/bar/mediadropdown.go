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

	"github.com/stubbedev/wayle/internal/appicons"
	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/shell/widgets"
)

// Media dropdown strings (locales dropdowns/_media.ftl).
const (
	mediaTitleText         = "Now Playing"
	mediaNoPlayerTitle     = "No Media Playing"
	mediaNoPlayerText      = "Start playing media in any app to control it here"
	mediaSourcesText       = "Media Sources"
	mediaUnknownTitle      = "Unknown Title"
	mediaUnknownArtist     = "Unknown Artist"
	mediaUnknownAlbum      = "Unknown Album"
	mediaArtPx             = 180
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
	art        *fixedBox
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

	current mpris.Player
	has     bool

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
	return emptyState(font, px, "ld-play-symbolic", mediaNoPlayerTitle, mediaNoPlayerText)
}

// controlButton builds one transport button: button.media-control
// paints it (the ink, the hover fill, the sizes), so nothing is set
// here.
func (v *mediaView) controlButton(icon *widget.Icon, class string, onClick func()) *widget.Button {
	b := widget.NewButton(icon, 6, 6)
	b.AddClass("media-control")
	if class != "" {
		b.AddClass(class)
	}
	b.OnClick = onClick
	return b
}

// buildPlayer assembles the player view once; refresh fills it in.
func (v *mediaView) buildPlayer(font render.Font, px float64) {
	v.player = widget.NewBox(widget.Column, 8, 0)

	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("media-header")
	title := widget.NewLabel(font, px, mediaTitleText, 0)
	title.AddClass("media-header-title")
	header.Append(title, true)
	v.sourceIcon = widget.NewThemeIcon("ld-music-symbolic", int(px))
	v.sourceIcon.AddClass("media-source-icon")
	v.identity = widget.NewLabel(font, px*0.9, "", 0)
	v.identity.AddClass("media-source-name")
	chevron := widget.NewThemeIcon("ld-chevron-right-symbolic", int(px))
	chevron.AddClass("media-source-chevron")
	source := widget.NewBox(widget.Row, 6, 0)
	source.Append(v.sourceIcon, false)
	source.Append(v.identity, false)
	source.Append(chevron, false)
	sourceButton := widget.NewButton(source, 4, 6)
	sourceButton.AddClass("media-source-button")
	sourceButton.OnClick = v.showPicker
	header.Append(sourceButton, false)
	v.player.Append(header, false)

	v.art = newFixedBox(mediaArtPx, mediaArtPx, nil)
	v.art.AddClass("media-artwork")
	v.player.Append(v.art, false)

	info := widget.NewBox(widget.Column, 0, 0)
	info.AddClass("media-info")
	v.title = widget.NewLabel(font, px*1.2, "", 0)
	v.title.AddClass("media-title")
	v.artist = widget.NewLabel(font, px, "", 0)
	v.artist.AddClass("media-artist")
	v.album = widget.NewLabel(font, px*0.9, "", 0)
	v.album.AddClass("media-album")
	// The info rows fill the panel and ellipsize at its edge (the Rust
	// labels' max-width-chars 1 with hexpand).
	for _, l := range []*widget.Label{v.title, v.artist, v.album} {
		l.SetEllipsize(widget.EllipsizeEnd)
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
	progress := widget.NewBox(widget.Column, 2, 0)
	progress.AddClass("media-progress")
	progress.Append(v.seek, false)
	progress.Append(times, false)
	v.player.Append(progress, false)

	controls := widget.NewBox(widget.Row, 6, 0)
	controls.AddClass("media-controls")
	v.shuffle = v.controlButton(widget.NewThemeIcon("ld-shuffle-symbolic", int(px)), "secondary", func() { v.fire("toggle shuffle", v.src.ToggleShuffle) })
	v.previous = v.controlButton(widget.NewThemeIcon("ld-skip-back-symbolic", int(px)), "", func() { v.fire("previous track", v.src.Previous) })
	v.playIcon = widget.NewThemeIcon("ld-play-symbolic", int(px*1.4))
	v.playPause = v.controlButton(v.playIcon, "main", func() { v.fire("play/pause", v.src.PlayPause) })
	v.next = v.controlButton(widget.NewThemeIcon("ld-skip-forward-symbolic", int(px)), "", func() { v.fire("next track", v.src.Next) })
	v.loopIcon = widget.NewThemeIcon("ld-repeat-symbolic", int(px))
	v.loop = v.controlButton(v.loopIcon, "secondary", func() { v.fire("toggle loop", v.src.ToggleLoop) })
	for _, b := range []*widget.Button{v.shuffle, v.previous, v.playPause, v.next, v.loop} {
		controls.Append(b, false)
	}
	v.player.Append(controls, false)
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

// show swaps the visible view.
func (v *mediaView) show(mode string, w widget.Widget) {
	if v.mode == mode {
		return
	}
	v.mode = mode
	v.main.Clear()
	v.main.Append(w, false)
	v.pages.Show("main")
}

// refresh repaints from the active player (PlayerChanged plus the
// metadata, capability, state, loop, and shuffle watchers at once).
func (v *mediaView) refresh() {
	p, ok := v.src.Active()
	v.current, v.has = p, ok
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
	v.title.SetText(orUnknown(p.Title, mediaUnknownTitle))
	setClass(v.title, "placeholder", p.Title == "")
	v.artist.SetText(orUnknown(p.Artist, mediaUnknownArtist))
	setClass(v.artist, "placeholder", p.Artist == "")
	v.album.SetText(orUnknown(p.Album, mediaUnknownAlbum))
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
// an http(s) URL loads into the box, anything else is the disc
// placeholder (the art resolver's Ready/NeedsDownload/Unresolvable).
func (v *mediaView) setArt(url string) {
	if url == v.artURL && v.art.Child() != nil {
		return
	}
	v.artURL = url
	_, px := dropdownFont(v.ctx)
	v.art.SetChild(mediaArt(url, "ld-disc-3-symbolic", "media-artwork-placeholder", "media-artwork-placeholder-icon", int(px*3)))
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
// returns to the player view.
func (v *mediaView) buildPicker() {
	font, px := dropdownFont(v.ctx)
	col := widget.NewBox(widget.Column, 4, 0)
	col.AddClass("media-source-picker")
	header := widget.NewLabel(font, px, mediaSourcesText, 0)
	header.AddClass("picker-title")
	col.Append(header, false)
	list := widget.NewBox(widget.Column, 0, 0)
	list.AddClass("media-source-list")
	active, _ := v.src.Active()
	for _, p := range v.src.Players() {
		row := widget.NewBox(widget.Row, 8, 0)
		row.AddClass("media-source-option-content")
		icon := widget.NewThemeIcon(mediaSourceIcon(p), int(px))
		row.Append(iconTile(icon, "media-source-option-icon", ""), false)
		name := widget.NewLabel(font, px, orUnknown(p.Identity, p.BusName), 0)
		name.AddClass("media-source-option-name")
		row.Append(name, true)
		if p.BusName == active.BusName {
			check := widget.NewThemeIcon("ld-check-symbolic", int(px))
			check.AddClass("media-source-option-check")
			row.Append(check, false)
		}
		b := widget.NewButton(row, 4, 6)
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
	col.Append(list, false)
	v.sources.Clear()
	v.sources.Append(col, false)
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

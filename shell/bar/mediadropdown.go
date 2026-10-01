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
	mediaInfoMaxChars      = 36
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

// seekSlider is the progress slider: it reports a seek once, when the
// drag releases (the Rust DebouncedSlider's committed signal), and
// ignores programmatic position updates while held.
type seekSlider struct {
	*widget.Slider
	onCommit func(percent float64)
}

// HitTest makes the wrapper the input leaf, so the press protocol
// reaches its SetPressed.
func (s *seekSlider) HitTest(p widget.Point) widget.Widget { return s.HitLeaf(s, p) }

// SetPressed commits the value on release.
func (s *seekSlider) SetPressed(on bool) {
	was := s.Pressed
	s.Slider.SetPressed(on)
	if was && !on && s.onCommit != nil {
		s.onCommit(s.Value())
	}
}

// setPosition moves the knob unless the user holds it.
func (s *seekSlider) setPosition(percent float64) {
	if !s.Pressed {
		s.SetValue(percent)
	}
}

// mediaView is the media dropdown (dropdowns/media): the player view
// with transport controls, or the source picker, or the no-player
// empty state. It follows the service while open and stops on Close.
type mediaView struct {
	ctx ModuleContext
	src mpris.Source

	*widget.Box
	player *widget.Box
	empty  *widget.Box
	mode   string

	identity   *widget.Label
	sourceIcon *widget.Icon
	art        *fixedBox
	artURL     string
	title      *widget.Label
	artist     *widget.Label
	album      *widget.Label
	seek       *seekSlider
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
	v.AddClass("media-dropdown")
	v.empty = mediaEmptyState(ctx)
	v.buildPlayer(font, px)
	v.refresh()
	v.follow()
	return v
}

// mediaEmptyState is the EmptyState template with the media strings.
func mediaEmptyState(ctx ModuleContext) *widget.Box {
	font, px := dropdownFont(ctx)
	col := widget.NewBox(widget.Column, 6, 14)
	col.AddClass("empty-state")
	icon := widget.NewThemeIcon("ld-play-symbolic", int(px*2))
	icon.SetTint(mutedFg(ctx.Style.palette))
	col.Append(icon, false)
	col.Append(widget.NewLabel(font, px*1.1, mediaNoPlayerTitle, ctx.Style.fg), false)
	col.Append(widget.NewLabel(font, px*0.9, mediaNoPlayerText, mutedFg(ctx.Style.palette)), false)
	return col
}

// controlButton builds one transport button around a tinted glyph.
func (v *mediaView) controlButton(icon *widget.Icon, class string, onClick func()) *widget.Button {
	icon.SetTint(v.ctx.Style.fg)
	b := widget.NewButton(icon, 6, 6)
	b.AddClass("media-control")
	if class != "" {
		b.AddClass(class)
	}
	b.BgHover = v.ctx.Style.buttonBgHover
	b.BgPressed = v.ctx.Style.buttonBgActive
	b.OnClick = onClick
	return b
}

// buildPlayer assembles the player view once; refresh fills it in.
func (v *mediaView) buildPlayer(font render.Font, px float64) {
	muted := mutedFg(v.ctx.Style.palette)
	v.player = widget.NewBox(widget.Column, 8, 0)

	header := widget.NewBox(widget.Row, 8, 0)
	header.AddClass("media-header")
	header.Append(widget.NewLabel(font, px, mediaTitleText, v.ctx.Style.fg), true)
	v.sourceIcon = widget.NewThemeIcon("ld-music-symbolic", int(px))
	v.sourceIcon.SetTint(v.ctx.Style.fg)
	v.identity = widget.NewLabel(font, px*0.9, "", muted)
	chevron := widget.NewThemeIcon("ld-chevron-right-symbolic", int(px))
	chevron.SetTint(muted)
	source := widget.NewBox(widget.Row, 6, 0)
	source.Append(v.sourceIcon, false)
	source.Append(v.identity, false)
	source.Append(chevron, false)
	sourceButton := widget.NewButton(source, 4, 6)
	sourceButton.AddClass("media-source-button")
	sourceButton.BgHover = v.ctx.Style.buttonBgHover
	sourceButton.BgPressed = v.ctx.Style.buttonBgActive
	sourceButton.OnClick = v.showPicker
	header.Append(sourceButton, false)
	v.player.Append(header, false)

	v.art = newFixedBox(mediaArtPx, mediaArtPx, nil)
	v.player.Append(v.art, false)

	v.title = widget.NewLabel(font, px*1.2, "", v.ctx.Style.fg)
	v.artist = widget.NewLabel(font, px, "", muted)
	v.album = widget.NewLabel(font, px*0.9, "", muted)
	for _, l := range []*widget.Label{v.title, v.artist, v.album} {
		v.player.Append(l, false)
	}

	v.seek = &seekSlider{Slider: widget.NewSlider(0, 100, 0, 0)}
	v.seek.AddClass("media-seek-slider")
	v.seek.onCommit = v.seekTo
	v.player.Append(v.seek, false)
	times := widget.NewBox(widget.Row, 0, 0)
	v.position = widget.NewLabel(font, px*0.85, "0:00", muted)
	v.length = widget.NewLabel(font, px*0.85, "0:00", muted)
	times.Append(v.position, true)
	times.Append(v.length, false)
	v.player.Append(times, false)

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
	v.Clear()
	v.Append(w, false)
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
	v.title.SetText(truncateLabel(orUnknown(p.Title, mediaUnknownTitle), mediaInfoMaxChars))
	v.artist.SetText(truncateLabel(orUnknown(p.Artist, mediaUnknownArtist), mediaInfoMaxChars))
	v.album.SetText(truncateLabel(orUnknown(p.Album, mediaUnknownAlbum), mediaInfoMaxChars))
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
	var img *widget.Image
	switch {
	case strings.HasPrefix(url, "file://"):
		img = widget.NewFileImage(strings.TrimPrefix(url, "file://"))
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		img = widget.NewURLImage(url)
	}
	if img == nil {
		_, px := dropdownFont(v.ctx)
		placeholder := widget.NewThemeIcon("ld-disc-3-symbolic", int(px*3))
		placeholder.SetTint(mutedFg(v.ctx.Style.palette))
		v.art.SetChild(placeholder)
		return
	}
	img.SetScale(widget.ImageCover)
	v.art.SetChild(img)
}

// setPosition applies a position read.
func (v *mediaView) setPosition(pos time.Duration) {
	if !v.has {
		return
	}
	v.position.SetText(formatMediaDuration(pos))
	v.seek.setPosition(mediaProgress(pos, v.current.Length))
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
	col.Append(widget.NewLabel(font, px, mediaSourcesText, v.ctx.Style.fg), false)
	active, _ := v.src.Active()
	for _, p := range v.src.Players() {
		row := widget.NewBox(widget.Row, 8, 0)
		icon := widget.NewThemeIcon(mediaSourceIcon(p), int(px))
		icon.SetTint(v.ctx.Style.fg)
		row.Append(icon, false)
		row.Append(widget.NewLabel(font, px, orUnknown(p.Identity, p.BusName), v.ctx.Style.fg), true)
		if p.BusName == active.BusName {
			check := widget.NewThemeIcon("ld-check-symbolic", int(px))
			check.SetTint(v.ctx.Style.fg)
			row.Append(check, false)
		}
		b := widget.NewButton(row, 4, 6)
		b.AddClass("media-source-option")
		setClass(b, "selected", p.BusName == active.BusName)
		b.BgHover = v.ctx.Style.buttonBgHover
		b.BgPressed = v.ctx.Style.buttonBgActive
		bus := p.BusName
		b.OnClick = func() {
			if err := v.src.SetActive(bus); err != nil {
				log.Printf("media: select %s: %v", bus, err)
			}
			v.mode = ""
			v.refresh()
		}
		col.Append(b, false)
	}
	v.Clear()
	v.Append(col, false)
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

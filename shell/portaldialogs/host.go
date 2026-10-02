package portaldialogs

import (
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/internal/fileuri"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/shell/reveal"
)

// The surface's sizes (shell/portal_dialogs: set_width_request,
// pixel sizes, the app list's min-content-height).
const (
	surfaceWidth  = 420
	headerPx      = 64
	wallpaperPx   = 180
	appIconPx     = 32
	appListHeight = 360
	escapeKeycode = 1
)

// Window is the overlay surface the dialog maps (app.LayerWindow).
type Window interface{ Close() }

// Deps are the dialogs' collaborators.
type Deps struct {
	// Config is the live config (the dialog animation).
	Config func() *config.Config
	// Open maps the overlay; Invoke runs on the UI loop.
	Open   func(cfg app.LayerConfig) (Window, error)
	Invoke func(func())
	// Font labels the dialog in Ink until Sheet (the portal-dialog-*
	// classes) restyles it.
	Font  render.Font
	Ink   render.Color
	Sheet *widget.Stylesheet
	// Candidates are the apps offered for a type (desktopentry's
	// RecommendedFor, else every app); SetDefault records the
	// "always use" choice. Avatar is the ~/.face path, "" for none.
	Candidates func(choices []string, contentType string) []desktopentry.App
	SetDefault func(id, contentType string) error
	Avatar     func() string
}

// Dialogs is the com.wayle.PortalDialogs1 host: one overlay showing
// the latest request; a request arriving while one is open answers the
// older one no (or "" for an app chooser). Its Host methods may be
// called from any goroutine; the rest runs on the loop.
type Dialogs struct {
	d       Deps
	win     Window
	rev     *widget.Revealer
	answer  func(confirmed bool, choice string)
	closing bool
}

// New builds the host; nothing maps until a request.
func New(d Deps) *Dialogs { return &Dialogs{d: d} }

// ask shows a dialog on the loop and waits for its answer.
func (h *Dialogs) ask(build func(answer func(bool, string)) widget.Widget) (bool, string) {
	type result struct {
		ok     bool
		choice string
	}
	ch := make(chan result, 1)
	h.d.Invoke(func() {
		h.settle(false, "")
		h.answer = func(ok bool, choice string) { ch <- result{ok, choice} }
		h.show(build(h.finish))
	})
	r := <-ch
	return r.ok, r.choice
}

// settle answers the open request, if any.
func (h *Dialogs) settle(ok bool, choice string) {
	if h.answer != nil {
		answer := h.answer
		h.answer = nil
		answer(ok, choice)
	}
}

// finish answers and plays the exit.
func (h *Dialogs) finish(ok bool, choice string) {
	h.settle(ok, choice)
	h.hide()
}

// Access is the grant/deny prompt: the permission's icon above the
// title, the subtitle and body below it.
func (h *Dialogs) Access(r AccessRequest) bool {
	ok, _ := h.ask(func(answer func(bool, string)) widget.Widget {
		detail := strings.Join(nonEmpty(r.Subtitle, r.Body), "\n")
		return h.confirm(answer, h.icon(r.Icon, headerPx), r.Title, detail, r.DenyLabel, r.GrantLabel)
	})
	return ok
}

// Account asks to share the user's name and avatar, previewing it.
func (h *Dialogs) Account(reason string) bool {
	ok, _ := h.ask(func(answer func(bool, string)) widget.Widget {
		if reason == "" {
			reason = "An application is requesting your name and avatar."
		}
		return h.confirm(answer, h.file(h.d.Avatar(), headerPx), "Share account information?", reason, "Cancel", "Share")
	})
	return ok
}

// ConfirmWallpaper previews a file:// image before it is set.
func (h *Dialogs) ConfirmWallpaper(uri string) bool {
	ok, _ := h.ask(func(answer func(bool, string)) widget.Widget {
		path, _ := fileuri.Path(uri)
		return h.confirm(answer, h.file(path, wallpaperPx), "Set as wallpaper?", "", "Cancel", "Set wallpaper")
	})
	return ok
}

// ConfirmInstall confirms a dynamic launcher.
func (h *Dialogs) ConfirmInstall(name, iconName string) bool {
	ok, _ := h.ask(func(answer func(bool, string)) widget.Widget {
		return h.confirm(answer, h.icon(iconName, headerPx), "Install “"+name+"”?", "", "Cancel", "Install")
	})
	return ok
}

// ChooseApplication is the app chooser: icon + name rows with a live
// search filter, and "always use" when the type is known.
func (h *Dialogs) ChooseApplication(choices []string, contentType, _ string) string {
	_, choice := h.ask(func(answer func(bool, string)) widget.Widget {
		return h.chooser(answer, h.d.Candidates(choices, contentType), contentType)
	})
	return choice
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (h *Dialogs) label(text, class string) *widget.Label {
	l := widget.NewLabel(h.d.Font, 13, text, h.d.Ink)
	l.AddClass(class)
	l.SetWrap(true)
	return l
}

func (h *Dialogs) button(text, class string, onClick func()) *widget.Button {
	l := h.label(text, class+"-label")
	l.SetWrap(false)
	l.SetAlignment(render.AlignCenter)
	b := widget.NewButton(l, 0, 0)
	b.AddClass(class)
	b.OnClick = onClick
	return b
}

// icon is a themed icon (or an icon file), nil for none.
func (h *Dialogs) icon(name string, px int) widget.Widget {
	if name == "" {
		return nil
	}
	if filepath.IsAbs(name) {
		return widget.NewFileIcon(name, px)
	}
	return widget.NewThemeIcon(name, px)
}

// file is an image file fitted into a px square, nil for none.
func (h *Dialogs) file(path string, px int) widget.Widget {
	if path == "" {
		return nil
	}
	img := widget.NewFileImage(path)
	img.AddClass("portal-dialog-image")
	return credential.NewFixed(img, px, px)
}

// surface wraps the dialog's rows in the styled panel.
func (h *Dialogs) surface(rows ...widget.Widget) widget.Widget {
	box := widget.NewBox(widget.Column, 12, 0)
	box.AddClass("portal-dialog-surface")
	for _, r := range rows {
		if r != nil {
			box.Append(r, false)
		}
	}
	return credential.NewFixed(box, surfaceWidth, 0)
}

// actions is the right-aligned button row.
func (h *Dialogs) actions(buttons ...*widget.Button) widget.Widget {
	row := widget.NewBox(widget.Row, 8, 0)
	row.Append(widget.NewSpacer(0, 0), true)
	for _, b := range buttons {
		row.Append(b, false)
	}
	return row
}

// confirm is a yes/no dialog.
func (h *Dialogs) confirm(answer func(bool, string), header widget.Widget, title, body, cancel, confirm string) widget.Widget {
	rows := []widget.Widget{}
	if header != nil {
		rows = append(rows, credential.Center(header))
	}
	rows = append(rows, h.label(title, "portal-dialog-title"))
	if body != "" {
		rows = append(rows, h.label(body, "portal-dialog-body"))
	}
	rows = append(rows, h.actions(
		h.button(cancel, "portal-dialog-cancel", func() { answer(false, "") }),
		h.button(confirm, "portal-dialog-confirm", func() { answer(true, "") }),
	))
	return h.surface(rows...)
}

// chooser lists the apps; picking one answers its id.
func (h *Dialogs) chooser(answer func(bool, string), apps []desktopentry.App, contentType string) widget.Widget {
	list := widget.NewBox(widget.Column, 2, 0)
	list.AddClass("portal-dialog-app-list")
	type row struct {
		name   string
		button *widget.Button
	}
	var rows []row
	remember := widget.NewCheckButton(false)
	for _, a := range apps {
		id, name := a.ID, a.DisplayName()
		content := widget.NewBox(widget.Row, 10, 0)
		icon := h.icon(a.Icon(), appIconPx)
		if icon == nil {
			icon = widget.NewThemeIcon("application-x-executable-symbolic", appIconPx)
		}
		content.Append(icon, false)
		text := widget.NewBox(widget.Column, 0, 0)
		text.Append(h.label(name, "portal-dialog-app-name"), false)
		if desc := a.Comment(); desc != "" {
			d := h.label(desc, "portal-dialog-app-desc")
			d.SetWrap(false)
			d.SetEllipsize(widget.EllipsizeEnd)
			text.Append(d, false)
		}
		content.Append(text, true)
		b := widget.NewButton(content, 6, 0)
		b.AddClass("portal-dialog-app-row")
		b.OnClick = func() {
			if remember.Checked() && contentType != "" {
				if err := h.d.SetDefault(id, contentType); err != nil {
					log.Printf("appchooser: set-default %s for %s: %v", id, contentType, err)
				}
			}
			answer(true, id)
		}
		list.Append(b, false)
		rows = append(rows, row{strings.ToLower(name), b})
	}
	// The rows pack at the top; the spacer takes the viewport's slack.
	list.Append(widget.NewSpacer(0, 0), true)
	search := widget.NewEntry(h.d.Font, 13, h.d.Ink)
	search.AddClass("portal-dialog-search")
	search.SetPlaceholder("Search applications…")
	search.OnChanged = func(q string) {
		q = strings.ToLower(q)
		for _, r := range rows {
			r.button.SetVisible(q == "" || strings.Contains(r.name, q))
		}
	}
	// Filled, so a short list packs at the top instead of floating mid-view.
	view := widget.NewScroll(list)
	view.FillX, view.FillY = true, true
	scroll := credential.NewFixed(view, 0, appListHeight)
	var rememberRow widget.Widget
	if contentType != "" {
		r := widget.NewBox(widget.Row, 8, 0)
		r.AddClass("portal-dialog-remember")
		r.Append(remember, false)
		r.Append(h.label("Always use for this file type", "portal-dialog-remember-label"), true)
		rememberRow = r
	}
	return h.surface(
		h.label("Open With", "portal-dialog-title"),
		search,
		scroll,
		rememberRow,
		h.actions(h.button("Cancel", "portal-dialog-cancel", func() { answer(false, "") })),
	)
}

// show maps the overlay with tree and plays the entry.
func (h *Dialogs) show(tree widget.Widget) {
	if h.win != nil {
		h.win.Close()
		h.win, h.closing = nil, false
	}
	cfg := h.d.Config()
	h.rev = widget.NewRevealer(credential.Center(tree))
	root := widget.NewBox(widget.Column, 0, 0)
	root.AddClass("portal-dialog-window")
	if h.d.Sheet != nil {
		root.AttachStylesheet(h.d.Sheet)
	}
	root.Append(h.rev, true)
	win, err := h.d.Open(app.LayerConfig{
		Layer:         app.LayerOverlay,
		Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardOnDemand,
		Namespace:     "wayle-portal-dialog",
		Root:          root,
		OnKey: func(_ *widget.Router, keycode uint32, _ app.Mods) {
			if keycode == escapeKeycode {
				h.finish(false, "")
			}
		},
	})
	if err != nil {
		log.Printf("portal dialogs: cannot map the overlay: %v", err)
		h.settle(false, "")
		return
	}
	h.win = win
	reveal.Show(h.rev, cfg.Animations, config.AnimDialog)
}

// hide plays the exit and closes the surface.
func (h *Dialogs) hide() {
	if h.win == nil || h.closing {
		return
	}
	h.closing = true
	win := h.win
	reveal.Hide(h.rev, h.d.Config().Animations, config.AnimDialog, func() {
		win.Close()
		if h.win == win {
			h.win, h.rev, h.closing = nil, nil, false
		}
	})
}

// CandidateApps is the chooser's list (candidate_apps): the given
// desktop ids, else the apps recommended for the type, else every app.
func CandidateApps(choices []string, contentType string) []desktopentry.App {
	dirs := desktopentry.ApplicationDirs()
	all := desktopentry.AllApps(dirs)
	if len(choices) > 0 {
		var out []desktopentry.App
		for _, a := range all {
			if slices.Contains(choices, a.ID) {
				out = append(out, a)
			}
		}
		return out
	}
	if contentType != "" {
		if rec := desktopentry.RecommendedFor(dirs, desktopentry.MimeAppsLists(), contentType); len(rec) > 0 {
			return rec
		}
	}
	return all
}

// LocalAvatar is ~/.face when it exists.
func LocalAvatar() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	face := filepath.Join(home, ".face")
	if _, err := os.Stat(face); err != nil {
		return ""
	}
	return face
}

package filechooser

import (
	"bufio"
	"context"
	"image"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/fileuri"
	"github.com/stubbedev/wayle/internal/mime"
	"github.com/stubbedev/wayle/shell/credential"
	"github.com/stubbedev/wayle/shell/reveal"
)

// The sheet's geometry (shell/file_chooser: the surface's size
// request, column widths, icon sizes).
const (
	sheetW, sheetH   = 760, 520
	sidebarW         = 180
	searchW          = 150
	previewW         = 220
	listRowH         = 34
	gridCellW        = 112
	gridCellH        = 104
	rowIconPx        = 24
	gridIconPx       = 48
	previewIconPx    = 96
	navIconPx        = 16
	minColumnW       = 50
	maxColumnW       = 400
	crumbMaxChars    = 16
	filterPopupLeft  = 16
	filterPopupAbove = 56
	gripPx           = 18
	colGripPx        = 7
	thumbWorkers     = 4
)

// The keys and buttons the chooser binds: evdev keycodes and the
// mouse's thumb buttons.
const (
	keyEscape  = 1
	keySpace   = 57
	btnBack    = 0x113 // BTN_SIDE
	btnForward = 0x114 // BTN_EXTRA
)

// uriListMime is what a file drag offers.
const uriListMime = "text/uri-list"

// Window is the overlay surface the chooser maps (app.LayerWindow).
type Window interface {
	Close()
	SetFocus(widget.Widget)
}

// Deps are the chooser's collaborators.
type Deps struct {
	// Config is the live config (the chooser animation).
	Config func() *config.Config
	// Open maps the overlay; Invoke runs on the UI loop.
	Open   func(cfg app.LayerConfig) (Window, error)
	Invoke func(func())
	// Font labels the sheet in Ink until Sheet (the file-chooser-*
	// classes) restyles it.
	Font  render.Font
	Ink   render.Color
	Sheet *widget.Stylesheet
	// MIME guesses types for filters and icons.
	MIME *mime.Database
	// Home is where a request without a folder starts and what the
	// breadcrumbs shorten to Home.
	Home string
	// Places and Locations fill the sidebar for each request.
	Places, Locations func() []Place
	// ViewFile persists the sort and layout; "" keeps them in memory.
	ViewFile string
}

// Chooser is the com.wayle.FileChooser1 host: one overlay showing the
// latest request; a request arriving while one is open answers the
// older one with nothing. Open and Save may be called from any
// goroutine; the rest runs on the loop.
type Chooser struct {
	d       Deps
	win     Window
	rev     *widget.Revealer
	closing bool
	reply   func([]string)
	s       *session
	ui      *ui

	// view, hidden and recursive stick across requests.
	view              view
	hidden, recursive bool
	// columns are the size, modified and kind widths.
	columns [3]int

	stopWalk context.CancelFunc
	// navigating is set while one gesture's activations are being
	// applied: after the first descended, the rest index a listing
	// that is gone.
	navigating bool
	// drag is the sheet geometry a move or resize started from.
	drag struct{ x, y, w, h int }

	thumbs  map[thumbKey]image.Image
	waiting map[thumbKey][]*widget.Image
	workers chan struct{}
}

type thumbKey struct {
	path string
	px   int
}

// New builds the host; nothing maps until a request.
func New(d Deps) *Chooser {
	c := &Chooser{d: d, view: defaultView, columns: [3]int{90, 150, 90}, workers: make(chan struct{}, thumbWorkers)}
	if d.ViewFile != "" {
		if b, err := os.ReadFile(d.ViewFile); err == nil {
			c.view = parseView(string(b))
		}
	}
	return c
}

// Open shows the open dialog.
func (c *Chooser) Open(r OpenRequest) []string {
	return c.ask(r.Title, openMode(r), r.Filters, r.CurrentFolder, "")
}

// Save shows the save dialog seeded with the suggested name.
func (c *Chooser) Save(r SaveRequest) []string {
	return c.ask(r.Title, ModeSave, r.Filters, r.CurrentFolder, r.CurrentName)
}

// ask shows a request on the loop and waits for its answer.
func (c *Chooser) ask(title string, mode Mode, filters []Filter, folder, name string) []string {
	ch := make(chan []string, 1)
	c.d.Invoke(func() {
		c.settle(nil)
		c.reply = func(uris []string) { ch <- uris }
		c.begin(title, mode, filters, folder, name)
	})
	return <-ch
}

// settle answers the open request, if any, and stops its search.
func (c *Chooser) settle(uris []string) {
	c.cancelWalk()
	if c.reply != nil {
		reply := c.reply
		c.reply = nil
		reply(uris)
	}
}

// finish answers and plays the exit.
func (c *Chooser) finish(uris []string) {
	c.settle(uris)
	c.hide()
}

func (c *Chooser) begin(title string, mode Mode, filters []Filter, folder, name string) {
	c.thumbs, c.waiting = map[thumbKey]image.Image{}, map[thumbKey][]*widget.Image{}
	c.s = newSession(mode, folder, filters, c.d.MIME, c.d.Home, c.view)
	c.s.hidden, c.s.recursive = c.hidden, c.recursive
	c.s.refresh()
	c.build(title, name)
	c.show()
}

// --- the tree -------------------------------------------------------

// ui holds the widgets a request updates.
type ui struct {
	root                    *widget.Box
	sheet                   *sheet
	crumbs                  *widget.Box
	search                  *widget.SearchEntry
	name                    *widget.Entry
	list, grid              *widget.List
	colHeader               *widget.Box
	sortButtons             [4]*widget.Button
	sortLabels              [4]*widget.Label
	columns                 [3]*credential.Fixed
	empty                   *widget.Label
	quicklook               *widget.Box
	qlIcon                  *widget.Box
	qlName, qlInfo          *widget.Label
	preview                 *credential.Fixed
	pvIcon                  *widget.Box
	pvName, pvInfo          *widget.Label
	filterButton            *widget.ToggleButton
	filterLabel             *widget.Label
	filterPopup             *widget.Box
	viewToggle              *widget.Button
	viewIcon                *widget.Box
	previewToggle, hiddenTg *widget.ToggleButton
	recursiveToggle         *widget.ToggleButton
}

func (c *Chooser) label(text, class string) *widget.Label {
	l := widget.NewLabel(c.d.Font, 13, text, c.d.Ink)
	if class != "" {
		l.AddClass(class)
	}
	return l
}

func (c *Chooser) textButton(text, class string, onClick func()) (*widget.Button, *widget.Label) {
	l := c.label(text, class+"-label")
	l.SetAlignment(render.AlignCenter)
	b := widget.NewButton(l, 0, 0)
	b.AddClass(class)
	b.OnClick = onClick
	return b, l
}

// iconToggle is a gtk::ToggleButton with an icon: on starts it
// active, and onToggled hears every later change.
func (c *Chooser) iconToggle(icon, tooltip string, on bool, onToggled func(bool)) *widget.ToggleButton {
	holder := widget.NewBox(widget.Row, 0, 0)
	setIcon(holder, icon)
	b := widget.NewToggleButton(credential.Center(holder), 0, 0)
	b.AddClass("toggle", "file-chooser-nav", "flat")
	b.SetTooltip(tooltip)
	b.SetActive(on)
	b.OnToggled = onToggled
	return b
}

func (c *Chooser) iconButton(icon, tooltip string, onClick func()) *widget.Button {
	b, _ := c.swappableIconButton(icon, tooltip, onClick)
	return b
}

// swappableIconButton is an icon button whose icon setIcon can swap.
func (c *Chooser) swappableIconButton(icon, tooltip string, onClick func()) (*widget.Button, *widget.Box) {
	holder := widget.NewBox(widget.Row, 0, 0)
	setIcon(holder, icon)
	b := widget.NewButton(credential.Center(holder), 0, 0)
	b.AddClass("file-chooser-nav", "flat")
	b.SetTooltip(tooltip)
	b.OnClick = onClick
	return b, holder
}

func setIcon(holder *widget.Box, icon string) {
	holder.Clear()
	holder.Append(widget.NewThemeIcon(icon, navIconPx), false)
}

func (c *Chooser) build(title, name string) {
	u := &ui{}
	s := c.s

	// Header: up, the centred title, the view toggles. The bar is the
	// sheet's drag handle.
	headerRow := widget.NewBox(widget.Row, 2, 0)
	headerRow.Append(c.iconButton("go-up-symbolic", "Up", c.goUp), false)
	headerRow.Append(widget.NewSpacer(0, 0), true)
	u.viewToggle, u.viewIcon = c.swappableIconButton(viewIcon(c.view.grid), "Toggle grid view", c.toggleView)
	u.previewToggle = c.iconToggle("view-paged-symbolic", "Preview pane", false, c.togglePreview)
	u.hiddenTg = c.iconToggle("view-reveal-symbolic", "Show hidden files", c.hidden, c.toggleHidden)
	u.recursiveToggle = c.iconToggle("folder-saved-search-symbolic", "Search subfolders", c.recursive, c.toggleRecursive)
	headerRow.Append(u.viewToggle, false)
	for _, b := range []*widget.ToggleButton{u.previewToggle, u.hiddenTg, u.recursiveToggle} {
		headerRow.Append(b, false)
	}
	headerStack := widget.NewOverlay()
	headerStack.Append(headerRow)
	headerStack.Append(newPlace(c.label(title, "file-chooser-title"), alignCenter, alignCenter))
	header := widget.NewBox(widget.Column, 0, 0)
	header.AddClass("file-chooser-header")
	header.Append(&dragArea{child: headerStack, cursor: "grab", onStart: c.startMove, onDrag: c.move}, false)

	// Path bar: breadcrumbs and the search box.
	crumbBar := widget.NewBox(widget.Row, 8, 0)
	crumbBar.AddClass("file-chooser-crumbbar")
	u.crumbs = widget.NewBox(widget.Row, 2, 0)
	u.crumbs.AddClass("file-chooser-crumbs")
	crumbBar.Append(u.crumbs, true)
	// gtk::SearchEntry with no debounce: filtering the cached listing
	// is an in-memory scan, and a recursive walk is cancelled by its
	// generation instead.
	u.search = widget.NewSearchEntry(c.d.Font, 13, "Filter this folder")
	u.search.SetColor(c.d.Ink)
	u.search.AddClass("file-chooser-search")
	u.search.SetDelay(0)
	u.search.OnSearchChanged = c.setSearch
	crumbBar.Append(credential.NewFixed(u.search, searchW, 0), false)

	// Body: sidebar, the file views, the preview pane.
	body := widget.NewBox(widget.Row, 0, 0)
	body.AddClass("file-chooser-body")
	body.Append(c.sidebar(), false)
	body.Append(c.views(u), true)
	var card *widget.Box
	card, u.pvIcon, u.pvName, u.pvInfo = c.previewCard("file-chooser-preview", 22)
	// The pane, not just its card, hides: a hidden card would still
	// hold its width in the row.
	u.preview = credential.NewFixed(card, previewW, 0)
	u.preview.SetVisible(false)
	body.Append(u.preview, false)

	u.name = widget.NewEntry(c.d.Font, 13, c.d.Ink)
	u.name.AddClass("file-chooser-name")
	u.name.SetPlaceholder("File name")
	u.name.SetText(name)
	u.name.OnActivate = func(string) { c.confirm() }
	u.name.SetVisible(s.mode == ModeSave)

	// Footer: the type filter, cancel, confirm.
	footer := widget.NewBox(widget.Row, 8, 0)
	footer.AddClass("file-chooser-footer")
	u.filterLabel = c.label("", "file-chooser-filter-label")
	u.filterLabel.SetAlignment(render.AlignCenter)
	u.filterButton = widget.NewToggleButton(u.filterLabel, 0, 0)
	u.filterButton.AddClass("toggle", "file-chooser-filter")
	u.filterButton.OnToggled = c.setFilterPopup
	u.filterLabel.SetAlignment(render.AlignStart)
	u.filterLabel.SetEllipsize(widget.EllipsizeEnd)
	u.filterLabel.SetMaxWidthChars(28)
	u.filterButton.SetVisible(len(s.filters) > 0)
	if len(s.filters) > 0 {
		u.filterLabel.SetText(FilterLabel(s.filters[0]))
	}
	footer.Append(u.filterButton, false)
	footer.Append(widget.NewSpacer(0, 0), true)
	cancel, _ := c.textButton("Cancel", "file-chooser-cancel", func() { c.finish(nil) })
	confirm, _ := c.textButton(s.mode.confirmLabel(), "file-chooser-confirm", c.confirm)
	confirm.AddClass("suggested-action")
	footer.Append(cancel, false)
	footer.Append(confirm, false)

	surface := widget.NewBox(widget.Column, 0, 0)
	surface.AddClass("file-chooser-surface")
	surface.Append(header, false)
	surface.Append(crumbBar, false)
	surface.Append(body, true)
	surface.Append(u.name, false)
	surface.Append(footer, false)

	// The filter popup sits in the sheet itself rather than an
	// xdg_popup, which an exclusive overlay may not route input to.
	u.filterPopup = widget.NewBox(widget.Column, 0, 0)
	u.filterPopup.AddClass("file-chooser-filter-popup")
	filterList := widget.NewBox(widget.Column, 0, 0)
	filterList.AddClass("file-chooser-filter-list")
	for i, f := range s.filters {
		row, l := c.textButton(FilterLabel(f), "file-chooser-filter-row", func() { c.selectFilter(i) })
		l.SetAlignment(render.AlignStart)
		l.SetEllipsize(widget.EllipsizeEnd)
		l.SetMaxWidthChars(40)
		row.SetElement("row")
		filterList.Append(row, false)
	}
	u.filterPopup.Append(widget.NewScroll(filterList), true)
	u.filterPopup.SetVisible(false)
	popup := newPlace(u.filterPopup, alignStart, alignEnd)
	popup.left, popup.bottom = filterPopupLeft, filterPopupAbove

	grip := &dragArea{w: gripPx, h: gripPx, cursor: "se-resize", onStart: c.startResize, onDrag: c.resize}
	grip.AddClass("file-chooser-resize-grip")

	stack := widget.NewOverlay()
	stack.Append(surface)
	stack.Append(popup)
	stack.Append(newPlace(grip, alignEnd, alignEnd))
	u.sheet = newSheet(stack, sheetW, sheetH)
	u.sheet.onDrop = c.dropped

	c.ui = u
	c.updateSortLabels()
	c.relist()
}

func viewIcon(grid bool) string {
	if grid {
		return "view-list-symbolic"
	}
	return "view-grid-symbolic"
}

// sidebar is Favorites (the user's places) and Locations (/ and the
// mounts).
func (c *Chooser) sidebar() widget.Widget {
	box := widget.NewBox(widget.Column, 2, 0)
	box.AddClass("file-chooser-sidebar")
	section := func(title string, places []Place) {
		box.Append(c.label(title, "file-chooser-sidebar-header"), false)
		list := widget.NewBox(widget.Column, 0, 0)
		list.AddClass("file-chooser-places")
		for _, p := range places {
			row := widget.NewBox(widget.Row, 8, 0)
			icon := widget.NewThemeIcon(p.Icon, navIconPx)
			icon.AddClass("file-chooser-place-icon")
			row.Append(icon, false)
			l := c.label(p.Label, "")
			l.SetEllipsize(widget.EllipsizeEnd)
			row.Append(l, true)
			b := widget.NewButton(row, 0, 0)
			b.SetElement("row")
			path := p.Path
			b.OnClick = func() { c.goTo(path) }
			list.Append(b, false)
		}
		box.Append(list, false)
	}
	section("Favorites", c.d.Places())
	section("Locations", c.d.Locations())
	box.Append(widget.NewSpacer(0, 0), true)
	scroll := widget.NewScroll(box)
	// Filled, so the sections pack at the top instead of floating mid-view.
	scroll.FillX, scroll.FillY = true, true
	scroll.AddClass("file-chooser-sidebar-scroll")
	return credential.NewFixed(scroll, sidebarW, 0)
}

// views is the column header over the list and grid, with the empty
// label and the quick look card over them.
func (c *Chooser) views(u *ui) widget.Widget {
	u.colHeader = widget.NewBox(widget.Row, 0, 0)
	u.colHeader.AddClass("file-chooser-colheader")
	for col := range u.sortButtons {
		b, l := c.textButton("", "file-chooser-col", func() { c.sortBy(SortColumn(col)) })
		b.AddClass("flat")
		l.SetAlignment(render.AlignStart)
		u.sortButtons[col], u.sortLabels[col] = b, l
		if col == int(SortName) {
			u.colHeader.Append(b, true)
			continue
		}
		u.colHeader.Append(c.columnGrip(col-1), false)
		u.columns[col-1] = credential.NewFixed(b, c.columns[col-1], 0)
		u.colHeader.Append(u.columns[col-1], false)
	}

	u.list = widget.NewList[widget.Widget](rows{c, false}, listRowH)
	u.list.AddClass("file-chooser-list")
	u.grid = widget.NewList[widget.Widget](rows{c, true}, gridCellH)
	u.grid.AddClass("file-chooser-grid")
	u.grid.SetCellWidth(gridCellW)
	mode := widget.SelectionSingle
	if c.s.mode == ModeOpenMultiple {
		mode = widget.SelectionMultiple
	}
	for _, l := range []*widget.List{u.list, u.grid} {
		l.SetSelectionMode(mode)
		l.OnActivate = c.activate
		l.OnSelectionChanged = func([]int) { c.refreshPreview() }
	}

	u.empty = c.label("No items", "file-chooser-empty")
	u.quicklook, u.qlIcon, u.qlName, u.qlInfo = c.previewCard("file-chooser-quicklook", 32)
	u.quicklook.SetVisible(false)

	stack := widget.NewOverlay()
	stack.Append(u.list)
	stack.Append(u.grid)
	stack.Append(newPlace(u.empty, alignCenter, alignCenter))
	stack.Append(newPlace(u.quicklook, alignCenter, alignCenter))

	col := widget.NewBox(widget.Column, 0, 0)
	col.Append(u.colHeader, false)
	col.Append(stack, true)
	return col
}

// columnGrip resizes column i (size, modified, kind) by dragging its
// left edge: left grows it.
func (c *Chooser) columnGrip(i int) widget.Widget {
	var base int
	g := &dragArea{w: colGripPx, cursor: "col-resize"}
	g.AddClass("file-chooser-col-grip")
	g.onStart = func() { base = c.columns[i] }
	g.onDrag = func(dx, _ int) {
		c.columns[i] = min(max(base-dx, minColumnW), maxColumnW)
		c.ui.columns[i].SetSize(c.columns[i], 0)
	}
	g.onEnd = func() { c.ui.list.Refresh() }
	return g
}

// previewCard is an icon, a name and a detail line: the quick look
// card and the preview pane.
func (c *Chooser) previewCard(class string, nameChars int) (card, icon *widget.Box, name, info *widget.Label) {
	card = widget.NewBox(widget.Column, 12, 0)
	card.AddClass(class)
	icon = widget.NewBox(widget.Row, 0, 0)
	icon.AddClass(class + "-icon")
	name = c.label("", class+"-name")
	name.SetAlignment(render.AlignCenter)
	name.SetWrap(true)
	name.SetMaxWidthChars(nameChars)
	info = c.label("", class+"-info")
	info.SetAlignment(render.AlignCenter)
	info.SetWrap(true)
	card.Append(credential.Center(icon), false)
	card.Append(name, false)
	card.Append(info, false)
	return card, icon, name, info
}

// rows is a List model over the session's entries: list rows, or grid
// cells.
type rows struct {
	c    *Chooser
	grid bool
}

func (m rows) Len() int { return len(m.c.s.entries) }

func (m rows) Row(i int) widget.Widget {
	e := m.c.s.entries[i]
	if m.grid {
		return m.c.gridCell(e)
	}
	return m.c.fileRow(e)
}

// fileRow is the name (icon and label) with the size, modified and
// kind cells under their column headers.
func (c *Chooser) fileRow(e Entry) widget.Widget {
	row := widget.NewBox(widget.Row, 8, 0)
	icon := c.entryIcon(e, rowIconPx)
	row.Append(icon, false)
	name := c.label(c.s.displayName(e), "")
	// A recursive match is a relative path: keep both ends in view.
	if c.s.searching() {
		name.SetEllipsize(widget.EllipsizeMiddle)
	} else {
		name.SetEllipsize(widget.EllipsizeEnd)
	}
	row.Append(name, true)
	size := "--"
	if !e.IsDir {
		size = HumanSize(e.Size)
	}
	for i, text := range []string{size, FormatTime(e.Modified), e.Kind()} {
		cell := c.label(text, "file-chooser-cell")
		cell.SetEllipsize(widget.EllipsizeEnd)
		if i < 2 {
			cell.SetAlignment(render.AlignEnd)
		}
		row.Append(credential.NewFixed(cell, c.columns[i], 0), false)
	}
	return row
}

// gridCell is a large icon over a centred name.
func (c *Chooser) gridCell(e Entry) widget.Widget {
	cell := widget.NewBox(widget.Column, 6, 0)
	cell.AddClass("file-chooser-grid-cell")
	cell.Append(credential.Center(c.entryIcon(e, gridIconPx)), false)
	name := c.label(c.s.displayName(e), "")
	name.SetAlignment(render.AlignCenter)
	name.SetWrap(true)
	name.SetMaxLines(2)
	name.SetEllipsize(widget.EllipsizeEnd)
	cell.Append(name, false)
	return cell
}

// entryIcon is a thumbnail for a raster image, the folder glyph, or
// the type's themed icon.
func (c *Chooser) entryIcon(e Entry, px int) widget.Widget {
	switch {
	case e.IsDir:
		return widget.NewThemeIcon("folder-symbolic", px)
	case isRaster(e.Path):
		return c.thumb(e.Path, px)
	}
	return widget.NewThemeIcon(c.d.MIME.GenericIcon(c.d.MIME.TypeByName(e.Name())), px)
}

// thumb is a px square that fills with the image's thumbnail once one
// of the bounded workers decoded it (at twice the size, for HiDPI);
// each image is decoded once per request however often its row is
// rebuilt.
func (c *Chooser) thumb(path string, px int) widget.Widget {
	img := widget.NewImage(nil)
	img.SetScale(widget.ImageCover)
	img.AddClass("file-chooser-thumb")
	key := thumbKey{path, px}
	if done, ok := c.thumbs[key]; ok {
		img.SetImage(done)
	} else {
		if _, pending := c.waiting[key]; !pending {
			thumbs, waiting := c.thumbs, c.waiting
			go func() {
				c.workers <- struct{}{}
				done, err := thumbnail(path, 2*px)
				<-c.workers
				c.d.Invoke(func() {
					imgs := waiting[key]
					delete(waiting, key)
					if err != nil {
						return
					}
					thumbs[key] = done
					for _, w := range imgs {
						w.SetImage(done)
					}
				})
			}()
		}
		c.waiting[key] = append(c.waiting[key], img)
	}
	return credential.NewFixed(img, px, px)
}

// --- surface --------------------------------------------------------

func (c *Chooser) show() {
	if c.win != nil {
		c.win.Close()
		c.win, c.closing = nil, false
	}
	u := c.ui
	c.rev = widget.NewRevealer(u.sheet)
	u.root = widget.NewBox(widget.Column, 0, 0)
	u.root.AddClass("file-chooser-window")
	if c.d.Sheet != nil {
		u.root.AttachStylesheet(c.d.Sheet)
	}
	u.root.Append(c.rev, true)
	win, err := c.d.Open(app.LayerConfig{
		Layer:         app.LayerOverlay,
		Anchor:        app.AnchorTop | app.AnchorBottom | app.AnchorLeft | app.AnchorRight,
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardExclusive,
		Namespace:     "wayle-file-chooser",
		Root:          u.root,
		OnKey:         func(r *widget.Router, keycode uint32, _ app.Mods) { c.key(r, keycode) },
		OnPress:       func(button, _ uint32, _ widget.Widget) { c.press(button) },
	})
	if err != nil {
		log.Printf("file chooser: cannot map the overlay: %v", err)
		c.settle(nil)
		return
	}
	c.win = win
	if c.s.mode == ModeSave {
		win.SetFocus(u.name)
	} else {
		win.SetFocus(c.activeView())
	}
	reveal.Show(c.rev, c.d.Config().Animations, config.AnimFileChooser)
}

func (c *Chooser) hide() {
	if c.win == nil || c.closing {
		return
	}
	c.closing = true
	win := c.win
	reveal.Hide(c.rev, c.d.Config().Animations, config.AnimFileChooser, func() {
		win.Close()
		if c.win == win {
			c.win, c.rev, c.closing = nil, nil, false
		}
	})
}

// key: Escape closes the filter popup, then quick look, then cancels;
// space toggles quick look unless a text field has it.
func (c *Chooser) key(r *widget.Router, keycode uint32) {
	if c.reply == nil {
		return
	}
	switch keycode {
	case keyEscape:
		c.escape()
	case keySpace:
		switch r.Focused().(type) {
		case *widget.Entry, *widget.SearchEntry:
			// Space types into a text field.
		default:
			c.toggleQuicklook()
		}
	}
}

func (c *Chooser) escape() {
	switch u := c.ui; {
	case u.filterPopup.Visible():
		c.setFilterPopup(false)
	case u.quicklook.Visible():
		u.quicklook.SetVisible(false)
	default:
		c.finish(nil)
	}
}

// press walks the history on the mouse's thumb buttons.
func (c *Chooser) press(button uint32) {
	if c.reply == nil {
		return
	}
	switch button {
	case btnBack:
		c.step(true)
	case btnForward:
		c.step(false)
	}
}

func (c *Chooser) startMove() {
	c.drag.x, c.drag.y = c.ui.sheet.x, c.ui.sheet.y
}

func (c *Chooser) move(dx, dy int) { c.ui.sheet.moveTo(c.drag.x+dx, c.drag.y+dy) }

func (c *Chooser) startResize() {
	c.drag.w, c.drag.h = c.ui.sheet.w, c.ui.sheet.h
}

func (c *Chooser) resize(dx, dy int) { c.ui.sheet.resize(c.drag.w+dx, c.drag.h+dy) }

// --- state changes --------------------------------------------------

func (c *Chooser) activeView() *widget.List {
	if c.s.view.grid {
		return c.ui.grid
	}
	return c.ui.list
}

// relist repaints after the entries changed: the breadcrumbs, both
// views from the top, the empty label, the preview.
func (c *Chooser) relist() {
	u := c.ui
	u.crumbs.Clear()
	for i, cr := range Crumbs(c.s.dir, c.d.Home) {
		if i > 0 {
			u.crumbs.Append(c.label("›", "file-chooser-crumb-sep"), false)
		}
		b, _ := c.textButton(truncate(cr.Label, crumbMaxChars), "file-chooser-crumb", nil)
		b.AddClass("flat")
		if cr.Current {
			b.AddClass("current")
		}
		path := cr.Path
		b.OnClick = func() { c.goTo(path) }
		u.crumbs.Append(b, false)
	}
	grid := c.s.view.grid
	u.colHeader.SetVisible(!grid)
	u.list.SetVisible(!grid)
	u.grid.SetVisible(grid)
	u.list.Reset()
	u.grid.Reset()
	u.empty.SetVisible(len(c.s.entries) == 0)
	u.quicklook.SetVisible(false)
	c.refreshPreview()
}

// truncate shortens s to n runes with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (c *Chooser) goTo(dir string) {
	c.s.goTo(dir)
	c.afterNavigate()
}

func (c *Chooser) goUp() {
	if c.s.up() {
		c.afterNavigate()
	}
}

func (c *Chooser) step(back bool) {
	if c.s.step(back) {
		c.afterNavigate()
	}
}

func (c *Chooser) dropped(path string) {
	c.s.dropped(path)
	c.afterNavigate()
}

// afterNavigate empties the search box (the session already dropped
// the query) and repaints the new folder.
func (c *Chooser) afterNavigate() {
	c.cancelWalk()
	c.ui.search.SetText("")
	c.relist()
}

func (c *Chooser) cancelWalk() {
	if c.stopWalk != nil {
		c.stopWalk()
		c.stopWalk = nil
	}
}

// setSearch filters the cached listing in memory, or, searching
// subfolders, starts a walk that streams its matches in; a newer query
// cancels it.
func (c *Chooser) setSearch(query string) {
	c.cancelWalk()
	c.s.setSearch(query)
	c.relist()
	if !c.s.searching() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.stopWalk = cancel
	s, root, hidden := c.s, c.s.dir, c.s.hidden
	go WalkSearch(ctx, root, query, hidden, func(batch []string) {
		found := searchEntries(batch, s.mode)
		c.d.Invoke(func() {
			if ctx.Err() == nil && c.s == s && s.addFound(query, found) {
				c.relist()
			}
		})
	})
}

// restartSearch re-runs the query: a walk depends on the hidden and
// subfolder toggles.
func (c *Chooser) restartSearch() { c.setSearch(c.s.search) }

func (c *Chooser) toggleHidden(on bool) {
	c.hidden = on
	c.s.hidden = on
	if c.s.searching() {
		c.restartSearch()
		return
	}
	c.s.refresh()
	c.relist()
}

func (c *Chooser) toggleRecursive(on bool) {
	c.recursive = on
	c.s.recursive = on
	c.restartSearch()
}

func (c *Chooser) toggleView() {
	c.s.view.grid = !c.s.view.grid
	c.saveView()
	setIcon(c.ui.viewIcon, viewIcon(c.s.view.grid))
	c.relist()
}

func (c *Chooser) togglePreview(on bool) {
	c.ui.preview.SetVisible(on)
	c.refreshPreview()
}

func (c *Chooser) sortBy(col SortColumn) {
	c.s.sortBy(col)
	c.saveView()
	c.updateSortLabels()
	c.relist()
}

// saveView keeps the sort and layout for the next request and, with a
// file, across restarts (best effort).
func (c *Chooser) saveView() {
	c.view = c.s.view
	if c.d.ViewFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.d.ViewFile), 0o700); err == nil {
		_ = os.WriteFile(c.d.ViewFile, []byte(c.view.String()), 0o600)
	}
}

// updateSortLabels marks the sort column with its direction.
func (c *Chooser) updateSortLabels() {
	for col, l := range c.ui.sortLabels {
		text := [...]string{"Name", "Size", "Modified", "Kind"}[col]
		if SortColumn(col) == c.s.view.sort {
			if c.s.view.asc {
				text += " ↑"
			} else {
				text += " ↓"
			}
		}
		l.SetText(text)
	}
}

// setFilterPopup shows or hides the filter list with the filter
// toggle's state (set_active), from the toggle or a choice.
func (c *Chooser) setFilterPopup(on bool) {
	c.ui.filterPopup.SetVisible(on)
	c.ui.filterButton.SetActive(on)
}

func (c *Chooser) selectFilter(i int) {
	c.s.selectFilter(i)
	c.ui.filterLabel.SetText(FilterLabel(c.s.filters[c.s.active]))
	c.setFilterPopup(false)
	c.relist()
}

// activate is a double-click or Enter on row i.
func (c *Chooser) activate(i int) {
	if c.reply == nil || c.navigating || i < 0 || i >= len(c.s.entries) {
		return
	}
	name := c.s.entries[i].Name()
	switch c.s.activate(i) {
	case activateDescend:
		c.afterNavigate()
		c.navigating = true
		c.d.Invoke(func() { c.navigating = false })
	case activateName:
		c.ui.name.SetText(name)
	case activateConfirm:
		c.confirm()
	}
}

func (c *Chooser) confirm() {
	if c.reply == nil {
		return
	}
	if uris, ok := c.s.result(c.activeView().Selection(), c.ui.name.Text()); ok {
		c.finish(uris)
	}
}

// selected is the first selected entry of the active view.
func (c *Chooser) selected() (Entry, bool) {
	sel := c.activeView().Selection()
	if len(sel) == 0 || sel[0] >= len(c.s.entries) {
		return Entry{}, false
	}
	return c.s.entries[sel[0]], true
}

func (c *Chooser) toggleQuicklook() {
	u := c.ui
	if u.quicklook.Visible() {
		u.quicklook.SetVisible(false)
		return
	}
	if e, ok := c.selected(); ok {
		c.fillPreview(u.qlIcon, u.qlName, u.qlInfo, e)
		u.quicklook.SetVisible(true)
	}
}

func (c *Chooser) refreshPreview() {
	u := c.ui
	if u == nil || !u.preview.Visible() {
		return
	}
	if e, ok := c.selected(); ok {
		c.fillPreview(u.pvIcon, u.pvName, u.pvInfo, e)
		return
	}
	u.pvIcon.Clear()
	u.pvName.SetText("No selection")
	u.pvInfo.SetText("")
}

func (c *Chooser) fillPreview(icon *widget.Box, name, info *widget.Label, e Entry) {
	icon.Clear()
	icon.Append(c.entryIcon(e, previewIconPx), false)
	name.SetText(e.Name())
	info.SetText(previewInfo(e))
}

// --- system defaults ------------------------------------------------

// SystemLocations reads the mount table for OtherLocations.
func SystemLocations(home string) func() []Place {
	return func() []Place {
		f, err := os.Open("/proc/self/mounts")
		if err != nil {
			return OtherLocations(nil, home, os.Getenv("USER"))
		}
		defer func() { _ = f.Close() }()
		return OtherLocations(bufio.NewReader(f), home, os.Getenv("USER"))
	}
}

// droppedPath is the first local path in a text/uri-list payload.
func droppedPath(data []byte) (string, bool) {
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if p, ok := fileuri.Path(line); ok {
			return p, true
		}
	}
	return "", false
}

// DragEnter accepts a file drag.
func (s *sheet) DragEnter(mimes []string, _ widget.Point) string {
	if s.onDrop != nil && slices.Contains(mimes, uriListMime) {
		return uriListMime
	}
	return ""
}

// Drop navigates to the first dropped path.
func (s *sheet) Drop(_ string, data []byte, _ widget.Point) {
	if p, ok := droppedPath(data); ok {
		s.onDrop(p)
	}
}

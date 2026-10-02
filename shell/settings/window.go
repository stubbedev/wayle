package settings

import (
	"strconv"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// Window geometry and the page switch (app/mod.rs, app/window.rs).
const (
	defaultSidebarWidth = 220
	windowWidth         = 900
	windowHeight        = 650
	pageTransition      = 100 * time.Millisecond
	// pageCleanup is how long after a switch the old page is dropped:
	// the transition plus a buffer (TRANSITION_CLEANUP_BUFFER_MS).
	pageCleanup = pageTransition + 50*time.Millisecond
	// maxSidebarRem caps the sidebar drag (MAX_SIDEBAR_REM).
	maxSidebarRem = 25.0
	basePxPerRem  = 16.0
)

// window is SettingsApp's widget tree: the sidebar beside the page
// stack, the close button over the pages.
type window struct {
	k        *kit
	root     *widget.Box
	paned    *widget.Paned
	sidebar  *sidebar
	stack    *widget.Stack
	sections []navSection
	page     *settingsPage
	pageID   string
	serial   int
	// names are the pages in the stack, the current one last.
	names []string
	// after runs fn on the loop once d passed (the old page's
	// removal).
	after func(d time.Duration, fn func())
	// onClose closes the window; onResetAll asks before every runtime
	// override is dropped.
	onClose, onResetAll func()
}

func newWindow(k *kit, cfg *config.Config, after func(time.Duration, func())) *window {
	w := &window{k: k, sections: layout(cfg), after: after}
	w.stack = widget.NewStack()
	w.stack.SetTransition(widget.StackCrossfade, pageTransition)
	first := firstPage(w.sections)
	w.sidebar = newSidebar(k, w.sections, first)
	w.sidebar.onNavigate = w.show
	w.sidebar.onResetAll = func() {
		if w.onResetAll != nil {
			w.onResetAll()
		}
	}

	t := i18n.Settings()
	closeBtn := k.button(k.icon("ld-x-symbolic"), func() {
		if w.onClose != nil {
			w.onClose()
		}
	}, "settings-close")
	closeBtn.SetTooltip(t.Get("settings-close"))
	content := widget.NewOverlay().Append(w.stack).AppendAligned(closeBtn, widget.AlignEnd, widget.AlignStart)

	w.paned = widget.NewPaned(widget.Row, w.sidebar, content)
	w.paned.SetPosition(defaultSidebarWidth)
	w.paned.SetMaxPosition(w.sidebarLimit())
	w.root = widget.NewBox(widget.Column, 0, 0)
	w.root.SetElement("window")
	w.root.AddClass("settings-window")
	w.root.Append(w.paned, true)
	if first != "" {
		w.show(first)
	}
	return w
}

// sidebarLimit is setup_paned_clamp's bound: 25rem at the current
// styling scale.
func (w *window) sidebarLimit() int {
	return int(maxSidebarRem*basePxPerRem*float64(w.k.store.svc.Config().Styling.Scale) + 0.5)
}

// show builds the page fresh and cross-fades to it (show_page); the
// previous page is dropped once the transition ended.
func (w *window) show(id string) {
	if id == w.pageID {
		return
	}
	spec, ok := pageByID(w.sections, id)
	if !ok {
		return
	}
	w.serial++
	name := id + "#" + strconv.Itoa(w.serial)
	w.page, w.pageID = buildPage(w.k, spec), id
	w.stack.Add(name, w.page)
	w.stack.Show(name)
	w.names = append(w.names, name)
	w.after(pageCleanup, func() { w.dropStalePages(name) })
}

// dropStalePages removes every page but the one named keep, unless a
// later switch made another current.
func (w *window) dropStalePages(keep string) {
	if w.stack.Visible() != keep {
		return
	}
	for _, name := range w.names {
		if name != keep {
			w.stack.Remove(name)
		}
	}
	w.names = []string{keep}
}

// refresh re-reads the open page and the sidebar cap after a config
// change.
func (w *window) refresh() {
	w.paned.SetMaxPosition(w.sidebarLimit())
	if w.page != nil {
		w.page.refresh()
	}
}

package apptheme

import (
	"log"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/icons"
)

// iconSearch is the gelm icon search path the registry drives (a seam
// for tests; the shell's is gelm's process-wide theme).
type iconSearch struct {
	paths   func() []string
	set     func([]string)
	refresh func()
}

var gelmIcons = iconSearch{paths: widget.IconSearchPaths, set: widget.SetIconSearchPaths, refresh: widget.RefreshIcons}

// initIcons is bootstrap.rs's init_icons (IconRegistry::init): the
// icons directory and index.theme set up, stale icons migrated, the
// registry's roots put first on the icon search path, and the directory
// watched so icons `wayle icons install` writes while the shell runs
// resolve without a restart. A failure is logged and the shell runs on
// without wayle's icons. The returned function stops the watch.
func initIcons(r *icons.Registry, search iconSearch) (stop func()) {
	if err := r.EnsureSetup(); err != nil {
		log.Printf("wayle: icon registry init failed: %v", err)
		return func() {}
	}
	icons.Migrate(r.IconsDir())
	refresh := func() {
		search.set(r.ThemeSearchPaths(search.paths()))
		search.refresh()
	}
	refresh()
	stop, err := r.Watch(refresh)
	if err != nil {
		log.Printf("wayle: cannot watch icons directory: %v", err)
		return func() {}
	}
	return stop
}

// Setup is the process-wide half of a surface's start: the stylesheet
// fonts resolve through the system store, and wayle's icons join the
// icon search path. The returned function stops the icons watch.
func Setup() (stop func()) {
	widget.SetFaceResolver(FontResolver)
	registry, err := icons.NewRegistry()
	if err != nil {
		log.Printf("wayle: icon registry init failed: %v", err)
		return func() {}
	}
	return initIcons(registry, gelmIcons)
}

package settings

import (
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
)

// sidebar is Sidebar: the title, the collapsible nav sections, and the
// reset-all button.
type sidebar struct {
	*widget.Box
	active   string
	items    map[string]*widget.Button
	sections map[string]*navSectionView
	// onNavigate hears an item picked; onResetAll the footer button.
	onNavigate func(id string)
	onResetAll func()
}

// navSectionView is one section's header and its item list.
type navSectionView struct {
	header    *widget.Button
	items     *widget.Box
	collapsed bool
}

func newSidebar(k *kit, sections []navSection, active string) *sidebar {
	t := i18n.Settings()
	s := &sidebar{
		Box: widget.NewBox(widget.Column, 0, 0), active: active,
		items: map[string]*widget.Button{}, sections: map[string]*navSectionView{},
	}
	s.AddClass("sidebar")

	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("sidebar-header")
	header.Append(k.icon("ld-settings-symbolic", "sidebar-icon"), false)
	header.Append(k.label(t.Get("settings-title"), "sidebar-title"), false)
	s.Append(header, false)

	nav := widget.NewBox(widget.Column, 0, 0)
	nav.AddClass("sidebar-nav")
	for _, sec := range sections {
		box := widget.NewBox(widget.Column, 0, 0)
		box.AddClass("sidebar-section")
		view := &navSectionView{}
		title := widget.NewBox(widget.Row, 0, 0)
		title.Append(k.label(t.Get(sec.key)), true)
		title.Append(k.icon("ld-chevron-down-symbolic", "sidebar-section-chevron"), false)
		key := sec.key
		view.header = k.button(title, func() { s.toggleSection(key) }, "sidebar-section-title")
		box.Append(view.header, false)
		view.items = widget.NewBox(widget.Column, 0, 0)
		view.items.AddClass("sidebar-section-items")
		for _, p := range sec.pages {
			content := widget.NewBox(widget.Row, 0, 0)
			content.Append(k.icon(p.icon, "sidebar-item-icon"), false)
			content.Append(k.label(t.Get(p.navKey)), true)
			id := p.id
			b := k.button(content, func() { s.navigate(id) }, "sidebar-item")
			s.items[p.id] = b
			view.items.Append(b, false)
		}
		box.Append(view.items, false)
		s.sections[sec.key] = view
		nav.Append(box, false)
	}
	scroll := widget.NewScroll(nav)
	scroll.VerticalOnly, scroll.ShowBars, scroll.FillY = true, true, true
	s.Append(scroll, true)

	footer := widget.NewBox(widget.Row, 0, 0)
	footer.AddClass("sidebar-footer")
	reset := widget.NewBox(widget.Row, 0, 0)
	reset.Append(k.icon("ld-rotate-ccw-symbolic", "sidebar-reset-icon"), false)
	reset.Append(k.label(t.Get("settings-reset-all"), "sidebar-reset-label"), false)
	footer.Append(k.button(reset, func() {
		if s.onResetAll != nil {
			s.onResetAll()
		}
	}, "sidebar-reset-all"), true)
	s.Append(footer, false)

	if b := s.items[active]; b != nil {
		b.AddClass("active")
	}
	return s
}

// navigate marks id active and reports it (on_navigate).
func (s *sidebar) navigate(id string) {
	if b := s.items[s.active]; b != nil {
		b.RemoveClass("active")
	}
	s.active = id
	if b := s.items[id]; b != nil {
		b.AddClass("active")
	}
	if s.onNavigate != nil {
		s.onNavigate(id)
	}
}

// toggleSection folds or unfolds a section (on_toggle_section).
func (s *sidebar) toggleSection(key string) {
	v := s.sections[key]
	if v == nil {
		return
	}
	v.collapsed = !v.collapsed
	v.items.SetVisible(!v.collapsed)
	if v.collapsed {
		v.header.AddClass("collapsed")
	} else {
		v.header.RemoveClass("collapsed")
	}
}

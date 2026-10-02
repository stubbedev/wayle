package settings

import "github.com/stubbedev/wayle/config"

// initialPage is the page the window opens on (INITIAL_PAGE_ID), or
// the first page while that one is not ported.
const initialPage = "bar-general"

// navSection is NavSectionLayout: a titled sidebar group of pages.
type navSection struct {
	key   string
	pages []pageSpec
}

// layout is pages/nav.rs layout(): the sidebar's sections in order,
// each page built from the config (a page may list what the config
// holds).
func layout(cfg *config.Config) []navSection {
	sections := []navSection{
		{key: "settings-nav-bar-section", pages: []pageSpec{barDropdownPage(cfg)}},
	}
	out := sections[:0]
	for _, s := range sections {
		if len(s.pages) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// pageByID finds a page in the layout.
func pageByID(sections []navSection, id string) (pageSpec, bool) {
	for _, s := range sections {
		for _, p := range s.pages {
			if p.id == id {
				return p, true
			}
		}
	}
	return pageSpec{}, false
}

// firstPage is the page to open: initialPage, else the first listed.
func firstPage(sections []navSection) string {
	if _, ok := pageByID(sections, initialPage); ok {
		return initialPage
	}
	if len(sections) > 0 && len(sections[0].pages) > 0 {
		return sections[0].pages[0].id
	}
	return ""
}

// barDropdownPage is pages/bar/dropdown.rs.
func barDropdownPage(*config.Config) pageSpec {
	return pageSpec{
		id: "bar-dropdown", navKey: "settings-nav-bar-dropdown", icon: "ld-panel-bottom-symbolic",
		header: "settings-page-bar-dropdown",
		sections: []sectionSpec{
			{title: "settings-section-behavior", rows: fields("bar.dropdown-shadow", "bar.dropdown-autohide", "bar.dropdown-freeze-label")},
			{title: "settings-section-appearance", rows: []rowSpec{field("bar.dropdown-opacity", percentage)}},
		},
	}
}

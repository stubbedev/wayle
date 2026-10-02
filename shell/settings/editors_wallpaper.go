package settings

import (
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// The wallpaper page's own editors (editors/wallpaper_cycling,
// editors/monitor_wallpaper).

// revealDuration is a GtkRevealer's default transition-duration.
const revealDuration = 250 * time.Millisecond

// cyclingReveal is cycling_reveal: a full-width action row labelled
// Cycling Options whose nested rows slide in while the directory at
// path is set.
func cyclingReveal(path string, rows ...rowSpec) rowSpec {
	return rowSpec{
		path: path, key: "settings-wallpaper-cycling-options", fullWidth: true, action: true,
		editor: func(k *kit, s slot, _ config.FieldMeta) control { return newRevealGroup(k, s, rows) },
	}
}

// revealGroup holds the nested rows behind a slide-down revealer,
// revealed while the slot's text is not empty.
type revealGroup struct {
	*widget.Revealer
	slot slot
	rows []*settingRow
}

func newRevealGroup(k *kit, s slot, specs []rowSpec) *revealGroup {
	group := widget.NewBox(widget.Column, 0, 0)
	g := &revealGroup{Revealer: widget.NewRevealer(group), slot: s}
	for _, rs := range specs {
		row := newSettingRow(k, rs, fieldControl(k, rs))
		group.Append(row, false)
		g.rows = append(g.rows, row)
	}
	g.SetTransition(widget.RevealSlideDown)
	g.SetCollapse(true)
	// The first state shows at once; changes after it slide.
	g.SetDuration(0)
	g.refresh()
	g.SetDuration(revealDuration)
	return g
}

func (g *revealGroup) refresh() {
	dir, _ := g.slot.get().(string)
	g.SetRevealed(dir != "")
	for _, r := range g.rows {
		r.refresh()
	}
}

// monitorWallpaperList is monitor_wallpaper: a card per monitor, its
// connector name in the header and its wallpaper path, browse button
// and fit mode in the body.
var monitorWallpaperList = cardRow(cardSpec{
	identity: &cardField{key: "name", label: "settings-wallpaper-monitor-label", editor: func(k *kit, s slot) control {
		c := newLiveText(k, s, "DP-1")
		c.AddClass("monitor-name-entry")
		return c
	}},
	fields: []cardField{{label: "settings-wallpaper-path-label", fill: true, item: newMonitorBody}},
	blank: func() map[string]any {
		return map[string]any{"name": "", "fit-mode": string(config.FitFill), "wallpaper": ""}
	},
	addKey:   "settings-monitor-add",
	chrome:   &cardChrome{card: "monitor-card", header: "monitor-card-header", body: "monitor-card-body", remove: []string{"ghost-icon"}},
	classes:  []string{"monitor-wallpaper-control"},
	listCls:  "monitor-wallpaper-list",
	labelCls: "monitor-card-label",
})

// monitorBody is the card body's controls: the live path entry, the
// browse button and the fit mode dropdown.
type monitorBody struct {
	*widget.Box
	path   *liveText
	browse *widget.Button
	fit    *enumSelect
}

func newMonitorBody(k *kit, field func(key string) slot) control {
	wallpaper := field("wallpaper")
	c := &monitorBody{
		Box:  widget.NewBox(widget.Row, 0, 0),
		path: newLiveText(k, wallpaper, i18n.Settings().Get("settings-wallpaper-path-placeholder")),
		fit:  newEnumSelect(k, field("fit-mode"), config.MetaOf[config.FitMode]()),
	}
	c.path.AddClass("monitor-wallpaper-entry")
	c.fit.AddClass("monitor-fit-dropdown")
	c.browse = k.button(k.icon("ld-folder-open-symbolic"), func() {
		if k.pickers != nil {
			k.pickers.openFile(func(file string) {
				_ = wallpaper.set(file)
				c.path.refresh()
			})
		}
	}, "icon")
	c.AppendAligned(c.path, true, widget.AlignCenter)
	c.AppendAligned(c.browse, false, widget.AlignCenter)
	c.AppendAligned(c.fit, false, widget.AlignCenter)
	return c
}

func (c *monitorBody) refresh() {
	c.path.refresh()
	c.fit.refresh()
}

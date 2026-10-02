package settings

import (
	"slices"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// colorValueEditor is ColorValueControl: the swatch (custom colors
// only) and the dropdown of auto, transparent, custom and the palette
// tokens grouped under headers.
type colorValueEditor struct {
	*widget.Box
	k       *kit
	slot    slot
	drop    *widget.Dropdown
	swatch  *colorEditor
	ids     []string // each row's config value; "" for a header
	syncing bool
}

// Row ids that are not tokens (tokens.rs).
const (
	colorAuto        = "auto"
	colorTransparent = "transparent"
	colorCustom      = "#custom"
)

func newColorValueEditor(k *kit, s slot) *colorValueEditor {
	c := &colorValueEditor{Box: widget.NewBox(widget.Row, 0, 0), k: k, slot: s}
	c.AddClass("color-value-control")
	rows, ids := colorValueRows()
	c.ids = ids
	c.drop = widget.NewDropdownRows(k.face, 14, rows, 0)
	c.drop.AddClass("color-value-dropdown")
	c.drop.OnSelect = c.picked
	c.swatch = newColorEditor(k, s)
	c.swatch.AddClass("color-value-swatch")
	c.AppendAligned(c.swatch, false, widget.AlignCenter)
	c.AppendAligned(c.drop, false, widget.AlignCenter)
	c.refresh()
	return c
}

// colorValueRows are build_items: auto, transparent, custom, then each
// token group under its header, a dot styled as the token.
func colorValueRows() ([]widget.DropdownRow, []string) {
	t := i18n.Settings()
	dot := func(classes ...string) func() *widget.Icon {
		return func() *widget.Icon {
			ic := widget.NewThemeIcon("", 10)
			ic.AddClass(append([]string{"color-value-dot"}, classes...)...)
			return ic
		}
	}
	rows := []widget.DropdownRow{
		{Label: t.Get("settings-color-auto")},
		{Label: t.Get("settings-color-transparent"), Icon: dot("transparent")},
		{Label: t.Get("settings-color-custom"), Icon: dot("custom")},
	}
	ids := []string{colorAuto, colorTransparent, colorCustom}
	group := ""
	for _, tok := range config.CssTokens() {
		meta := cssTokenMeta[tok]
		if meta.group != group {
			group = meta.group
			rows = append(rows, widget.DropdownRow{Label: group, Header: true})
			ids = append(ids, "")
		}
		rows = append(rows, widget.DropdownRow{Label: meta.label, Icon: dot("token-" + string(tok))})
		ids = append(ids, string(tok))
	}
	return rows, ids
}

// picked writes the chosen row: a token or keyword as it is, custom
// as white, with the swatch shown for it.
func (c *colorValueEditor) picked(i int) {
	if c.syncing || i < 0 || i >= len(c.ids) || c.ids[i] == "" {
		return
	}
	v := c.ids[i]
	if v == colorCustom {
		// A hex value already shows as custom, so choosing it starts a
		// new one at white (select_custom).
		v = "#ffffff"
	}
	c.swatch.SetVisible(v[0] == '#')
	_ = c.slot.set(v)
}

func (c *colorValueEditor) refresh() {
	v, _ := c.slot.get().(string)
	id := v
	if strings.HasPrefix(v, "#") {
		id = colorCustom
	}
	c.syncing = true
	c.drop.SetSelected(max(slices.Index(c.ids, id), 0))
	c.syncing = false
	c.swatch.SetVisible(id == colorCustom)
	c.swatch.refresh()
}

// cssTokenMeta is token_meta: each token's label and group in the
// dropdown (English in Rust too).
var cssTokenMeta = map[config.CssToken]struct{ label, group string }{
	config.TokenBgBase: {"Base", "Backgrounds"}, config.TokenBgSurface: {"Surface", "Backgrounds"},
	config.TokenBgSurfaceElevated: {"Surface Elevated", "Backgrounds"}, config.TokenBgElevated: {"Elevated", "Backgrounds"},
	config.TokenBgOverlay: {"Overlay", "Backgrounds"}, config.TokenBgHover: {"Hover", "Backgrounds"},
	config.TokenBgActive: {"Active", "Backgrounds"}, config.TokenBgSelected: {"Selected", "Backgrounds"},
	config.TokenFgDefault: {"Default", "Foregrounds"}, config.TokenFgMuted: {"Muted", "Foregrounds"},
	config.TokenFgSubtle: {"Subtle", "Foregrounds"}, config.TokenFgOnAccent: {"On Accent", "Foregrounds"},
	config.TokenAccent: {"Accent", "Accent"}, config.TokenAccentSubtle: {"Accent Subtle", "Accent"},
	config.TokenAccentHover: {"Accent Hover", "Accent"},
	config.TokenStatusError: {"Error", "Status"}, config.TokenStatusWarning: {"Warning", "Status"},
	config.TokenStatusSuccess: {"Success", "Status"}, config.TokenStatusInfo: {"Info", "Status"},
	config.TokenStatusErrorSubtle: {"Error Subtle", "Status"}, config.TokenStatusWarningSubtle: {"Warning Subtle", "Status"},
	config.TokenStatusSuccessSubtle: {"Success Subtle", "Status"}, config.TokenStatusInfoSubtle: {"Info Subtle", "Status"},
	config.TokenStatusErrorHover: {"Error Hover", "Status"},
	config.TokenRed:              {"Red", "Semantic"}, config.TokenYellow: {"Yellow", "Semantic"},
	config.TokenGreen: {"Green", "Semantic"}, config.TokenBlue: {"Blue", "Semantic"},
	config.TokenBorderSubtle: {"Subtle", "Borders"}, config.TokenBorderDefault: {"Default", "Borders"},
	config.TokenBorderStrong: {"Strong", "Borders"}, config.TokenBorderAccent: {"Accent", "Borders"},
	config.TokenBorderError: {"Error", "Borders"},
}

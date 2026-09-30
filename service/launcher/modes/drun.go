package modes

import (
	"context"
	"html"
	"log"
	"slices"
	"strings"

	"github.com/stubbedev/wayle/internal/desktopentry"
	"github.com/stubbedev/wayle/internal/shlex"
	"github.com/stubbedev/wayle/service/launcher"
)

// DrunField is a desktop-entry field fed to the matcher
// (-drun-match-fields).
type DrunField uint8

// drun match fields.
const (
	DrunFieldName DrunField = iota
	DrunFieldGeneric
	DrunFieldExec
	DrunFieldCategories
	DrunFieldComment
	DrunFieldKeywords
)

// AllDrunFields is -drun-match-fields all.
var AllDrunFields = []DrunField{
	DrunFieldName, DrunFieldGeneric, DrunFieldExec, DrunFieldCategories, DrunFieldComment, DrunFieldKeywords,
}

// DrunConfig is drun's knobs (rofi's -drun-* family).
type DrunConfig struct {
	// Categories shows only apps in these categories (empty = all).
	Categories []string
	// ExcludeCategories hides apps in these.
	ExcludeCategories []string
	MatchFields       []DrunField
	// DisplayFormat is the row template over {name} {generic} {exec}
	// {categories} {comment}, [..] optional.
	DisplayFormat string
	// ShowActions adds desktop actions as rows.
	ShowActions bool
	// URLLauncher opens Type=Link entries.
	URLLauncher string
	// Terminal runs Terminal=true apps; empty autodetects.
	Terminal   string
	MaxHistory uint32
	// FallbackIcon is the icon for entries naming none
	// (-application-fallback-icon); empty is no icon.
	FallbackIcon    string
	IgnoredPrefixes []string
}

// DefaultDrunConfig is DrunConfig::default.
func DefaultDrunConfig() DrunConfig {
	return DrunConfig{
		MatchFields:   []DrunField{DrunFieldName, DrunFieldGeneric, DrunFieldExec, DrunFieldCategories, DrunFieldKeywords},
		DisplayFormat: "{name} [<span weight='light' size='small'><i>({generic})</i></span>]",
		URLLauncher:   "xdg-open",
		MaxHistory:    25,
	}
}

// drunEntry is what a row launches: an app (or one of its actions), or
// a link.
type drunEntry struct {
	desktopID string
	action    string
	app       *desktopentry.App
	url       string
	isLink    bool
}

type drunRow struct {
	item  launcher.Item
	entry drunEntry
}

// Drun launches applications from desktop entries (drun.rs).
type Drun struct {
	cfg     DrunConfig
	history *launcher.History
	entries []drunEntry
	// dirs is the application search path (tests point it elsewhere).
	dirs []string
}

// NewDrun builds the mode; a nil history disables frecency and
// recording.
func NewDrun(cfg DrunConfig, history *launcher.History) *Drun {
	return &Drun{cfg: cfg, history: history, dirs: desktopentry.ApplicationDirs()}
}

// Name is "drun".
func (*Drun) Name() string { return "drun" }

func (d *Drun) record(id string) {
	if IsIgnored(id, d.cfg.IgnoredPrefixes) || d.history == nil {
		return
	}
	if err := d.history.Record("drun", id, d.cfg.MaxHistory); err != nil {
		log.Printf("launcher: drun history record failed: %v", err)
	}
}

func (d *Drun) launch(e drunEntry) {
	if e.isLink {
		launcher.RunShell(d.cfg.URLLauncher + " " + shlex.MustQuote(e.url))
		return
	}
	var err error
	switch {
	case e.action != "":
		err = e.app.LaunchAction(e.action)
	case e.app.Terminal():
		d.launchInTerminal(*e.app)
	default:
		err = e.app.Launch()
	}
	if err != nil {
		log.Printf("launcher: launch %s failed: %v", e.desktopID, err)
	}
}

// launchInTerminal runs the Exec line, field codes stripped, inside a
// terminal: gio's own terminal lookup rarely fits Wayland setups.
func (d *Drun) launchInTerminal(app desktopentry.App) {
	argv, ok := shlex.Split(app.Exec())
	if !ok {
		log.Printf("launcher: unparseable Exec line %q", app.Exec())
		return
	}
	argv = slices.DeleteFunc(argv, isFieldCode)
	launcher.RunArgv(append([]string{launcher.DetectTerminal(d.cfg.Terminal), "-e"}, argv...))
}

func isFieldCode(token string) bool {
	switch token {
	case "%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%i", "%c", "%k", "%v", "%m":
		return true
	}
	return false
}

// Load lists the apps (and links), frecency first, then by name.
func (d *Drun) Load(context.Context) launcher.ModeState {
	var frecency map[string]float64
	if d.history != nil {
		frecency, _ = d.history.Frecency("drun")
	}
	rows := d.collectApps()
	rows = append(rows, d.collectLinks()...)
	slices.SortStableFunc(rows, func(a, b drunRow) int {
		wa, wb := frecency[a.entry.desktopID], frecency[b.entry.desktopID]
		if wa != wb {
			if wb > wa {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(a.item.Display), strings.ToLower(b.item.Display))
	})
	items := make([]launcher.Item, len(rows))
	d.entries = make([]drunEntry, len(rows))
	for i, r := range rows {
		items[i], d.entries[i] = r.item, r.entry
	}
	return launcher.ModeState{Items: items, Prompt: "drun", MarkupRows: true}
}

// Activate launches the row (recording it); custom input runs as a
// command, as rofi's drun does.
func (d *Drun) Activate(_ context.Context, target launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	if i, ok := target.Row(); ok {
		if int(i) >= len(d.entries) {
			return launcher.ActionNothing{}
		}
		e := d.entries[i]
		d.record(e.desktopID)
		d.launch(e)
		return launcher.ActionClose{}
	}
	if custom, ok := kind.(launcher.ActivateCustom); ok {
		launcher.RunShell(custom.Text)
		return launcher.ActionClose{}
	}
	return launcher.ActionNothing{}
}

// Delete forgets the row's history entry and reloads.
func (d *Drun) Delete(ctx context.Context, index uint32) launcher.Action {
	if d.history == nil || int(index) >= len(d.entries) {
		return launcher.ActionNothing{}
	}
	if err := d.history.Remove("drun", d.entries[index].desktopID); err != nil {
		log.Printf("launcher: drun history delete failed: %v", err)
	}
	return launcher.ActionReload{State: d.Load(ctx)}
}

// categoryAllowed is -drun-categories / -drun-exclude-categories.
func categoryAllowed(categories string, cfg DrunConfig) bool {
	var list []string
	for c := range strings.SplitSeq(categories, ";") {
		if c != "" {
			list = append(list, c)
		}
	}
	for _, c := range list {
		if slices.Contains(cfg.ExcludeCategories, c) {
			return false
		}
	}
	if len(cfg.Categories) == 0 {
		return true
	}
	for _, c := range list {
		if slices.Contains(cfg.Categories, c) {
			return true
		}
	}
	return false
}

func (d *Drun) collectApps() []drunRow {
	var rows []drunRow
	for _, app := range desktopentry.AllApps(d.dirs) {
		if !app.ShouldShow() || !categoryAllowed(app.Categories(), d.cfg) {
			continue
		}
		a := app
		rows = append(rows, drunRow{item: appItem(a, "", d.cfg), entry: drunEntry{desktopID: a.ID, app: &a}})
		if d.cfg.ShowActions {
			for _, action := range a.Actions() {
				rows = append(rows, drunRow{item: appItem(a, action, d.cfg), entry: drunEntry{desktopID: a.ID, action: action, app: &a}})
			}
		}
	}
	return rows
}

// markupEscape is glib::markup_escape_text.
func markupEscape(s string) string {
	return strings.ReplaceAll(html.EscapeString(s), "&#34;", "&quot;")
}

func appItem(app desktopentry.App, action string, cfg DrunConfig) launcher.Item {
	name := app.DisplayName()
	if action != "" {
		name += " (" + app.ActionName(action) + ")"
	}
	generic, exec, categories, comment := app.GenericName(), app.Exec(), app.Categories(), app.Comment()
	keywords := strings.Join(app.Keywords(), " ")
	display := launcher.Render(cfg.DisplayFormat, launcher.Values(map[string]string{
		"name": markupEscape(name), "generic": markupEscape(generic), "exec": markupEscape(exec),
		"categories": markupEscape(categories), "comment": markupEscape(comment),
	}))
	item := launcher.Item{
		Display: display,
		MatchText: buildMatchText(cfg.MatchFields, []fieldValue{
			{DrunFieldName, name},
			{DrunFieldGeneric, generic},
			{DrunFieldExec, exec},
			{DrunFieldCategories, categories},
			{DrunFieldComment, comment},
			{DrunFieldKeywords, keywords},
		}),
		Flags: launcher.FlagMarkup,
	}
	if raw, ok := app.String("Icon"); ok {
		item.Icon = iconSource(raw)
	} else if cfg.FallbackIcon != "" {
		item.Icon = iconSource(cfg.FallbackIcon)
	}
	return item
}

type fieldValue struct {
	field DrunField
	value string
}

func buildMatchText(fields []DrunField, values []fieldValue) string {
	var parts []string
	for _, v := range values {
		if slices.Contains(fields, v.field) && v.value != "" {
			parts = append(parts, v.value)
		}
	}
	return strings.Join(parts, " ")
}

func iconSource(raw string) launcher.Icon {
	if strings.HasPrefix(raw, "/") {
		return launcher.IconFile(raw)
	}
	return launcher.IconName(raw)
}

// collectLinks lists Type=Link entries, which rofi shows and opens with
// -drun-url-launcher.
func (d *Drun) collectLinks() []drunRow {
	var rows []drunRow
	for _, l := range desktopentry.Links(d.dirs) {
		item := launcher.Item{
			Display:   launcher.Render(d.cfg.DisplayFormat, launcher.Values(map[string]string{"name": markupEscape(l.Name)})),
			MatchText: l.Name + " " + l.URL,
			Flags:     launcher.FlagMarkup,
		}
		if l.Icon != "" {
			item.Icon = iconSource(l.Icon)
		}
		rows = append(rows, drunRow{item: item, entry: drunEntry{desktopID: l.ID, url: l.URL, isLink: true}})
	}
	return rows
}

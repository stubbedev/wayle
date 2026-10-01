// Package launcher is the launcher surface (crates/wayle-shell/src/
// shell/launcher): a layer-shell overlay per `wayle launcher` session,
// hosting the service/launcher engine. Sessions arrive over the
// launcher socket; the engine runs on its own goroutine and this
// package only renders and forwards input.
package launcher

import (
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/launcheripc"
	engine "github.com/stubbedev/wayle/service/launcher"
	"github.com/stubbedev/wayle/service/launcher/modes"
)

// uiSettings are one session's resolved UI knobs (setup.rs UiSettings).
type uiSettings struct {
	// width is [launcher] width in pixels; widthOverride is -width,
	// resolved late because percent and chars need the monitor and the
	// font.
	width         int
	widthOverride *launcheripc.Width
	offsetX       int
	offsetY       int
	lines         int
	fixedNumLines bool
	location      config.LauncherLocation
	password      bool
	mesg          *string
	// errorMessage is -e: a message-only dialog.
	errorMessage *string
	filter       *string
	// prompt is -p; modes supply their own otherwise.
	prompt        *string
	showIcons     bool
	sidebar       bool
	displayNames  map[string]string
	keybindings   []engine.Binding
	mouseBindings []engine.Binding
	hooks         engine.Hooks
	// css is the -font/-style CSS this session adds, "" for none.
	css             string
	previewCmd      string
	completer       string
	cycle           bool
	autoSelect      bool
	selectRow       *string
	selectedRow     *uint32
	displayColumns  []uint32
	columnSeparator string
	// ellipsize is start, middle, or end.
	ellipsize        string
	ballotSelected   string
	ballotUnselected string
	dump             bool
}

// sessionSetup is everything the surface needs to run one session.
type sessionSetup struct {
	modes       []engine.Mode
	initialMode int
	matcher     engine.MatcherOptions
	ui          uiSettings
	history     *engine.History
}

// setupDeps are the shell services a session's modes draw on.
type setupDeps struct {
	clipboard modes.ClipboardHistory
	// openHistory opens the history database (a test seam).
	openHistory func() (*engine.History, error)
}

// deref returns *p or fallback.
func deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}

// buildSession resolves a session from the CLI's options merged over
// the live config (setup.rs build). rows is the dmenu row stream.
func buildSession(opts launcheripc.SessionOptions, cfg *config.Config, rows <-chan []string, deps setupDeps) sessionSetup {
	l := cfg.Launcher
	var history *engine.History
	if l.History.Enable && deps.openHistory != nil {
		h, err := deps.openHistory()
		if err != nil {
			log.Printf("launcher: history unavailable: %v", err)
		} else {
			history = h
		}
	}
	bindings := engine.EffectiveKeybindings(engine.MergeOverrides(l.Keybindings, opts.KbOverrides))

	var ms []engine.Mode
	if opts.Dmenu {
		ms = append(ms, modes.NewDmenu(dmenuConfig(opts), rows))
	} else {
		for _, name := range requestedModes(opts, l) {
			if m := buildMode(name, opts, cfg, history, bindings, deps); m != nil {
				ms = append(ms, m)
			} else {
				log.Printf("launcher: mode %q not available; skipped", name)
			}
		}
	}
	initial := 0
	if opts.Mode != nil {
		for i, m := range ms {
			if m.Name() == *opts.Mode {
				initial = i
				break
			}
		}
	}
	width := int(l.Width.ResolvePx(config.LauncherWidthBaseRem*16, float64(cfg.Styling.Scale)))
	location := l.Location
	if opts.Location != nil {
		if loc, ok := config.LauncherLocationFromRofi(int(*opts.Location)); ok {
			location = loc
		}
	}
	return sessionSetup{
		modes:       ms,
		initialMode: initial,
		matcher:     matcherOptions(opts, l),
		history:     history,
		ui: uiSettings{
			width:            width,
			widthOverride:    opts.Width,
			offsetX:          int(deref(opts.XOffset, 0)),
			offsetY:          int(deref(opts.YOffset, 0)),
			lines:            int(deref(opts.Lines, l.Lines)),
			fixedNumLines:    !opts.NoFixedNumLines && l.FixedNumLines,
			location:         location,
			password:         opts.Password,
			mesg:             opts.Mesg,
			errorMessage:     opts.ErrorMessage,
			filter:           opts.Filter,
			prompt:           opts.Prompt,
			showIcons:        deref(opts.ShowIcons, l.ShowIcons),
			sidebar:          deref(opts.SidebarMode, l.SidebarMode),
			displayNames:     engine.MergeOverrides(l.DisplayNames, opts.DisplayNames),
			keybindings:      bindings,
			mouseBindings:    engine.EffectiveMouseBindings(engine.MergeOverrides(l.MouseBindings, opts.MouseOverrides)),
			hooks:            hooksOf(opts),
			css:              sessionCSS(opts, l),
			previewCmd:       previewCmd(opts, l),
			completer:        deref(opts.CompleterMode, ""),
			cycle:            deref(opts.Cycle, l.Cycle),
			autoSelect:       deref(opts.AutoSelect, l.AutoSelect),
			selectRow:        opts.Select,
			selectedRow:      opts.SelectedRow,
			displayColumns:   opts.DisplayColumns,
			columnSeparator:  deref(opts.DisplayColumnSeparator, "\t"),
			ellipsize:        ellipsize(opts),
			ballotSelected:   deref(opts.BallotSelected, "☑ "),
			ballotUnselected: deref(opts.BallotUnselected, "☐ "),
			dump:             opts.Dump,
		},
	}
}

// ellipsize: -keep-right is -ellipsize-mode start by another name.
func ellipsize(opts launcheripc.SessionOptions) string {
	if opts.KeepRight {
		return "start"
	}
	return deref(opts.EllipsizeMode, "end")
}

// requestedModes is -modes, else [launcher] modes, with -show's mode
// guaranteed present (first) and the -completer-mode appended, so the
// key can reach it without it being the mode the session opens on.
func requestedModes(opts launcheripc.SessionOptions, l config.LauncherConfig) []string {
	names := append([]string(nil), l.Modes...)
	if opts.Modes != nil {
		names = append([]string(nil), opts.Modes...)
	}
	if opts.Mode != nil && !slices.Contains(names, *opts.Mode) {
		names = append([]string{*opts.Mode}, names...)
	}
	if c := deref(opts.CompleterMode, ""); c != "" && !slices.Contains(names, c) {
		names = append(names, c)
	}
	return names
}

func buildMode(name string, opts launcheripc.SessionOptions, cfg *config.Config, history *engine.History, bindings []engine.Binding, deps setupDeps) engine.Mode {
	l := cfg.Launcher
	switch name {
	case "drun":
		return modes.NewDrun(drunConfig(opts, l), history)
	case "run":
		return modes.NewRun(runConfig(opts, l), history)
	case "window":
		return modes.NewWindow(windowConfig(opts, l, false))
	case "windowcd":
		return modes.NewWindow(windowConfig(opts, l, true))
	case "ssh":
		return modes.NewSSH(sshConfig(opts, l), history)
	case "filebrowser":
		return modes.NewFileBrowser(fileBrowserConfig(l, false))
	case "recursivebrowser":
		return modes.NewFileBrowser(fileBrowserConfig(l, true))
	case "keys":
		return modes.NewKeys(bindings)
	case "calc":
		return modes.NewCalc()
	case "clipboard":
		return modes.NewClipboard(deps.clipboard)
	case "emoji":
		return modes.NewEmoji()
	case "combi":
		var children []engine.Mode
		for _, child := range l.Combi.Modes {
			if child == "combi" {
				continue // no recursion
			}
			if m := buildMode(child, opts, cfg, history, bindings, deps); m != nil {
				children = append(children, m)
			}
		}
		if len(children) == 0 {
			return nil
		}
		return modes.NewCombi(children, deref(opts.CombiDisplayFormat, l.Combi.DisplayFormat))
	}
	// Custom script modes: name:script inline, or a [launcher.scripts] key.
	if n, script, ok := strings.Cut(name, ":"); ok {
		return modes.NewScript(n, expandHome(script))
	}
	if script, ok := l.Scripts[name]; ok {
		return modes.NewScript(name, expandHome(script))
	}
	return nil
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		return os.Getenv("HOME") + rest
	}
	return path
}

func dmenuConfig(opts launcheripc.SessionOptions) modes.DmenuConfig {
	return modes.DmenuConfig{
		Prompt:      opts.Prompt,
		Message:     opts.Mesg,
		MarkupRows:  opts.MarkupRows,
		MultiSelect: opts.MultiSelect,
		NoCustom:    opts.NoCustom || opts.OnlyMatch,
		Urgent:      opts.Urgent,
		Active:      opts.Active,
	}
}

var windowFieldNames = map[string][]modes.WindowField{
	"title":   {modes.WindowFieldTitle},
	"class":   {modes.WindowFieldClass},
	"name":    {modes.WindowFieldName},
	"role":    {modes.WindowFieldRole},
	"desktop": {modes.WindowFieldDesktop},
	"all":     {modes.WindowFieldTitle, modes.WindowFieldClass, modes.WindowFieldName, modes.WindowFieldRole, modes.WindowFieldDesktop},
}

func windowConfig(opts launcheripc.SessionOptions, l config.LauncherConfig, currentOnly bool) modes.WindowConfig {
	var fields []modes.WindowField
	if opts.WindowMatchFields != nil {
		for _, raw := range opts.WindowMatchFields {
			fields = append(fields, windowFieldNames[raw]...)
		}
	} else {
		for _, f := range l.Window.MatchFields {
			fields = append(fields, windowFieldNames[string(f)]...)
		}
	}
	return modes.WindowConfig{
		Format:             deref(opts.WindowFormat, l.Window.Format),
		MatchFields:        fields,
		HideActive:         deref(opts.HideActiveWindow, l.Window.HideActive),
		CloseOnDelete:      l.Window.CloseOnDelete,
		WindowCommand:      deref(opts.WindowCommand, ""),
		CurrentDesktopOnly: currentOnly,
	}
}

func sshConfig(opts launcheripc.SessionOptions, l config.LauncherConfig) modes.SSHConfig {
	return modes.SSHConfig{
		Client:          deref(opts.SSHClient, l.SSH.Client),
		Command:         deref(opts.SSHCommand, l.SSH.Command),
		ParseHosts:      deref(opts.ParseHosts, l.SSH.ParseHosts),
		ParseKnownHosts: deref(opts.ParseKnownHosts, l.SSH.ParseKnownHosts),
		Terminal:        deref(opts.Terminal, l.Terminal),
		MaxHistory:      l.History.MaxSize,
	}
}

var fileSorts = map[config.LauncherFileSort]modes.FileSort{
	config.FileSortName:  modes.FileSortName,
	config.FileSortMtime: modes.FileSortMtime,
	config.FileSortAtime: modes.FileSortAtime,
	config.FileSortCtime: modes.FileSortCtime,
}

func fileBrowserConfig(l config.LauncherConfig, recursive bool) modes.FileBrowserConfig {
	return modes.FileBrowserConfig{
		Directory:        l.Filebrowser.Directory,
		Sorting:          fileSorts[l.Filebrowser.SortingMethod],
		DirectoriesFirst: l.Filebrowser.DirectoriesFirst,
		ShowHidden:       l.Filebrowser.ShowHidden,
		Command:          l.Filebrowser.Command,
		Recursive:        recursive,
	}
}

var drunFieldNames = map[string]modes.DrunField{
	"name":       modes.DrunFieldName,
	"generic":    modes.DrunFieldGeneric,
	"exec":       modes.DrunFieldExec,
	"categories": modes.DrunFieldCategories,
	"comment":    modes.DrunFieldComment,
	"keywords":   modes.DrunFieldKeywords,
}

func drunConfig(opts launcheripc.SessionOptions, l config.LauncherConfig) modes.DrunConfig {
	var fields []modes.DrunField
	switch {
	case opts.DrunMatchFields != nil && slices.Contains(opts.DrunMatchFields, "all"):
		fields = modes.AllDrunFields
	case opts.DrunMatchFields != nil:
		for _, raw := range opts.DrunMatchFields {
			if f, ok := drunFieldNames[raw]; ok {
				fields = append(fields, f)
			}
		}
	default:
		for _, f := range l.Drun.MatchFields {
			fields = append(fields, drunFieldNames[string(f)])
		}
	}
	categories := l.Drun.Categories
	if opts.DrunCategories != nil {
		categories = opts.DrunCategories
	}
	exclude := l.Drun.ExcludeCategories
	if opts.DrunExcludeCategories != nil {
		exclude = opts.DrunExcludeCategories
	}
	return modes.DrunConfig{
		Categories:        categories,
		ExcludeCategories: exclude,
		MatchFields:       fields,
		DisplayFormat:     deref(opts.DrunDisplayFormat, l.Drun.DisplayFormat),
		ShowActions:       deref(opts.DrunShowActions, l.Drun.ShowActions),
		URLLauncher:       deref(opts.DrunURLLauncher, l.Drun.URLLauncher),
		Terminal:          deref(opts.Terminal, l.Terminal),
		MaxHistory:        l.History.MaxSize,
		FallbackIcon:      deref(opts.ApplicationFallbackIcon, ""),
		IgnoredPrefixes:   opts.IgnoredPrefixes,
	}
}

func runConfig(opts launcheripc.SessionOptions, l config.LauncherConfig) modes.RunConfig {
	return modes.RunConfig{
		RunCommand:      deref(opts.RunCommand, l.Run.RunCommand),
		ShellCommand:    deref(opts.RunShellCommand, l.Run.ShellCommand),
		ListCommand:     deref(opts.RunListCommand, l.Run.ListCommand),
		Terminal:        deref(opts.Terminal, l.Terminal),
		MaxHistory:      l.History.MaxSize,
		IgnoredPrefixes: opts.IgnoredPrefixes,
	}
}

// matcherOptions merges the matching flags over [launcher]. The CLI's
// strings keep the Rust leniency: an unknown -matching is normal, an
// unknown -sorting-method levenshtein.
func matcherOptions(opts launcheripc.SessionOptions, l config.LauncherConfig) engine.MatcherOptions {
	methods := map[string]engine.MatchMethod{
		"normal": engine.MatchNormal, "regex": engine.MatchRegex, "glob": engine.MatchGlob,
		"fuzzy": engine.MatchFuzzy, "prefix": engine.MatchPrefix,
	}
	method := methods[string(l.Matching)]
	if opts.Matching != nil {
		method = methods[*opts.Matching] // unknown -> MatchNormal, the zero
	}
	var caseMode engine.CaseMode
	switch {
	case deref(opts.CaseInsensitive, false):
		caseMode = engine.CaseModeInsensitive
	case deref(opts.CaseSmart, false):
		caseMode = engine.CaseModeSmart
	case deref(opts.CaseSensitive, false):
		caseMode = engine.CaseModeSensitive
	default:
		caseMode = map[config.LauncherCase]engine.CaseMode{
			config.CaseInsensitive: engine.CaseModeInsensitive,
			config.CaseSmart:       engine.CaseModeSmart,
			config.CaseSensitive:   engine.CaseModeSensitive,
		}[l.Case]
	}
	sortMethod := engine.SortLevenshtein
	if l.SortingMethod == config.SortingFzf {
		sortMethod = engine.SortFzf
	}
	if opts.SortingMethod != nil {
		sortMethod = engine.SortLevenshtein
		if *opts.SortingMethod == "fzf" || *opts.SortingMethod == "fzf-v2" {
			sortMethod = engine.SortFzf
		}
	}
	negate := '-'
	for _, r := range l.NegateChar {
		negate = r
		break
	}
	if opts.NegateChar != nil {
		for _, r := range *opts.NegateChar {
			negate = r
			break
		}
	}
	return engine.MatcherOptions{
		Method:       method,
		Case:         caseMode,
		Tokenize:     deref(opts.Tokenize, l.Tokenize),
		Normalize:    deref(opts.NormalizeMatch, l.NormalizeMatch),
		NegationChar: negate,
		Sort:         deref(opts.Sort, l.Sort),
		SortMethod:   sortMethod,
	}
}

func hooksOf(opts launcheripc.SessionOptions) engine.Hooks {
	return engine.Hooks{
		SelectionChanged: deref(opts.OnSelectionChanged, ""),
		EntryAccepted:    deref(opts.OnEntryAccepted, ""),
		ModeChanged:      deref(opts.OnModeChanged, ""),
		MenuCanceled:     deref(opts.OnMenuCanceled, ""),
		MenuError:        deref(opts.OnMenuError, ""),
	}
}

// previewCmd is -preview-cmd, else the configured one; blank is none.
func previewCmd(opts launcheripc.SessionOptions, l config.LauncherConfig) string {
	cmd := deref(opts.PreviewCmd, l.PreviewCmd)
	if strings.TrimSpace(cmd) == "" {
		return ""
	}
	return cmd
}

// fontDescription is a parsed Pango font description.
type fontDescription struct {
	family string
	// size is in points, or pixels when absolute.
	size     float64
	absolute bool
	italic   bool
	// weight is the CSS numeric weight; 0 leaves it unset.
	weight int
}

// pangoWeights are the weight words pango_font_description_from_string
// takes, to their numeric weights.
var pangoWeights = map[string]int{
	"thin": 100, "ultra-light": 200, "extra-light": 200, "light": 300, "semi-light": 350,
	"demi-light": 350, "book": 380, "regular": 400, "medium": 500, "semi-bold": 600,
	"demi-bold": 600, "bold": 700, "ultra-bold": 800, "extra-bold": 800, "heavy": 900,
	"black": 900, "ultra-heavy": 1000, "extra-heavy": 1000,
}

// parseFontDescription reads "[FAMILY-LIST] [STYLE-OPTIONS] [SIZE]":
// the size last (a trailing "px" makes it absolute), style and weight
// words before it, the rest the family.
func parseFontDescription(desc string) fontDescription {
	words := strings.Fields(strings.ReplaceAll(desc, ",", " "))
	var fd fontDescription
	if n := len(words); n > 0 {
		last := words[n-1]
		absolute := strings.HasSuffix(last, "px")
		if v, err := strconv.ParseFloat(strings.TrimSuffix(last, "px"), 64); err == nil && v > 0 {
			fd.size, fd.absolute = v, absolute
			words = words[:n-1]
		}
	}
	for len(words) > 0 {
		w := strings.ToLower(words[len(words)-1])
		switch {
		case w == "italic" || w == "oblique":
			fd.italic = true
		case w == "normal" || w == "roman":
		case pangoWeights[w] != 0:
			fd.weight = pangoWeights[w]
		default:
			fd.family = strings.Join(words, " ")
			return fd
		}
		words = words[:len(words)-1]
	}
	return fd
}

// fontProperties turns a Pango description into CSS properties. The
// syntaxes are not interchangeable: Pango puts the size last
// ("Monospace 20"), CSS's font shorthand first, so handing CSS the
// Pango string silently drops the declaration.
func fontProperties(desc string) string {
	fd := parseFontDescription(desc)
	var props []string
	if fd.family != "" {
		props = append(props, fmt.Sprintf("font-family: %q;", fd.family))
	}
	if fd.size > 0 {
		unit := "pt"
		if fd.absolute {
			unit = "px"
		}
		props = append(props, "font-size: "+strconv.FormatFloat(fd.size, 'f', -1, 64)+unit+";")
	}
	if fd.italic {
		props = append(props, "font-style: italic;")
	}
	if fd.weight > 0 && fd.weight != 400 {
		props = append(props, fmt.Sprintf("font-weight: %d;", fd.weight))
	}
	return strings.Join(props, " ")
}

// sessionCSS is the CSS one session adds: the -font (or [launcher]
// font) scoped to .launcher-surface, and the -style preset. A preset
// name that does not exist is a typo in a bind, logged rather than
// silently rendering the default look.
func sessionCSS(opts launcheripc.SessionOptions, l config.LauncherConfig) string {
	var css strings.Builder
	if font := strings.TrimSpace(deref(opts.Font, l.Font)); font != "" {
		fmt.Fprintf(&css, ".launcher-surface { %s }\n", fontProperties(font))
	}
	if opts.Style != nil {
		if preset, ok := l.Styles[*opts.Style]; ok {
			css.WriteString(preset)
		} else {
			log.Printf("launcher: no such [launcher.styles] preset %q", *opts.Style)
		}
	}
	if strings.TrimSpace(css.String()) == "" {
		return ""
	}
	return css.String()
}

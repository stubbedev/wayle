package config

// LauncherWidthBaseRem is the rem the width multiplier scales (600px).
const LauncherWidthBaseRem = 37.5

// LauncherConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Application launcher / dmenu (rofi replacement).
type LauncherConfig struct {
	// Surface position on screen.
	Location LauncherLocation `cfg:"location"`
	// Surface width: a multiplier of the default 600px (`1.0` = default)
	// or absolute pixels (e.g. `"800px"`).
	Width Size `cfg:"width"`
	// Visible result lines.
	Lines uint32 `cfg:"lines"`
	// Output connector to show on ("" = focused output).
	Monitor string `cfg:"monitor"`
	// Modes enabled by default (order = tab/kb-mode-next order). Script
	// modes are referenced by their `[launcher.scripts]` key.
	Modes []string `cfg:"modes"`
	// Wrap selection at list edges.
	Cycle bool `cfg:"cycle"`
	// Matching method.
	Matching LauncherMatching `cfg:"matching"`
	// Split the query into independently matched words.
	Tokenize bool `cfg:"tokenize"`
	// Token prefix that negates a token.
	NegateChar string `cfg:"negate-char"`
	// Strip accents/normalize Unicode while matching.
	NormalizeMatch bool `cfg:"normalize-match"`
	// Rank results by match quality (off = keep list order, rofi default).
	Sort bool `cfg:"sort"`
	// Ranking method when `sort` is on.
	SortingMethod LauncherSorting `cfg:"sorting-method"`
	// Case handling.
	Case LauncherCase `cfg:"case"`
	// Terminal emulator for terminal apps ("" = autodetect).
	Terminal string `cfg:"terminal"`
	// Show row icons.
	ShowIcons bool `cfg:"show-icons"`
	// Icon theme override ("" = system theme).
	IconTheme string `cfg:"icon-theme"`
	// Show mode tabs at the bottom of the surface.
	SidebarMode bool `cfg:"sidebar-mode"`
	// Accept automatically when exactly one result remains.
	AutoSelect bool `cfg:"auto-select"`
	// Select the row under the mouse cursor.
	HoverSelect bool `cfg:"hover-select"`
	// Keep the list height fixed at `lines` rows.
	FixedNumLines bool `cfg:"fixed-num-lines"`
	// Per-mode display-name overrides (rofi `display-{mode}`), e.g.
	// `drun = "apps"`.
	DisplayNames map[string]string `cfg:"display-names"`
	// Custom script modes: name → executable (rofi `name:script`).
	Scripts map[string]string `cfg:"scripts"`
	// Keybinding overrides: action (rofi `kb-` name without the prefix,
	// e.g. `accept`, `cancel`, `mode-next`, `custom-1`) → comma-separated
	// key list (e.g. `"Control+Tab,F3"`). Unset actions keep rofi's
	// defaults.
	Keybindings map[string]string `cfg:"keybindings"`
	// Mouse binding overrides: action (rofi's `me-`/`ml-` name, prefix
	// included, e.g. `me-accept-entry`, `ml-row-down`) → comma-separated
	// button list (e.g. `"MouseDPrimary"`, `"ScrollDown"`). Unset actions
	// keep their defaults.
	MouseBindings map[string]string `cfg:"mouse-bindings"`
	// Launcher font, as a Pango description (`"Inter 12"`). Empty keeps the
	// shell's font. rofi's `-font` overrides it per invocation.
	Font string `cfg:"font"`
	// Named looks selectable per invocation with `-style <name>`: name →
	// the CSS applied for that session, on top of `[styling]`.
	//
	// CSS rather than a second copy of the display keys: a preset exists to
	// say "this invocation looks different", and there is no list of the
	// ways someone might want that. `-style compact` then costs one config
	// entry instead of a schema change.
	Styles map[string]string `cfg:"styles"`
	// Command that turns a `thumbnail://` row icon into an image file:
	// `{input}` the icon's path, `{output}` where to write the thumbnail,
	// `{size}` the requested pixel size. Empty uses the system's XDG
	// thumbnailers. rofi's `-preview-cmd`.
	PreviewCmd string `cfg:"preview-cmd"`
	// Launch history / frecency.
	History LauncherHistoryConfig `cfg:"history"`
	// drun (application) mode.
	Drun LauncherDrunConfig `cfg:"drun"`
	// run (command) mode.
	Run LauncherRunConfig `cfg:"run"`
	// window switcher mode.
	Window LauncherWindowConfig `cfg:"window"`
	// ssh mode.
	SSH LauncherSSHConfig `cfg:"ssh"`
	// file browser mode.
	Filebrowser LauncherFilebrowserConfig `cfg:"filebrowser"`
	// combi (combined modes) mode.
	Combi LauncherCombiConfig `cfg:"combi"`
}

// LauncherHistoryConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Launch history / frecency settings.
type LauncherHistoryConfig struct {
	// Record launches and rank frequently used entries first.
	Enable bool `cfg:"enable"`
	// Maximum remembered entries per mode.
	MaxSize uint32 `cfg:"max-size"`
}

// LauncherDrunConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// drun (application) mode settings.
type LauncherDrunConfig struct {
	// Only show apps within these categories (empty = all).
	Categories []string `cfg:"categories"`
	// Hide apps within these categories.
	ExcludeCategories []string `cfg:"exclude-categories"`
	// Desktop-entry fields fed to the matcher.
	MatchFields []LauncherDrunField `cfg:"match-fields"`
	// Row template: `{name}`, `{generic}`, `{exec}`, `{categories}`,
	// `{comment}`; `[..]` renders only when its placeholders are non-empty.
	DisplayFormat string `cfg:"display-format"`
	// Expose desktop-file actions as extra rows.
	ShowActions bool `cfg:"show-actions"`
	// Command opening `Type=Link` entries.
	URLLauncher string `cfg:"url-launcher"`
}

// LauncherRunConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// run (command) mode settings.
type LauncherRunConfig struct {
	// Plain accept template (`{cmd}`).
	RunCommand string `cfg:"run-command"`
	// Run-in-terminal template (`{terminal}`, `{cmd}`).
	ShellCommand string `cfg:"shell-command"`
	// Extra command whose stdout lines add entries.
	ListCommand string `cfg:"list-command"`
}

// LauncherWindowConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// window switcher mode settings.
type LauncherWindowConfig struct {
	// Row template: `{w}` workspace, `{c}` class, `{t}` title, `{n}` name,
	// `{r}` role.
	Format string `cfg:"format"`
	// Window fields fed to the matcher.
	MatchFields []LauncherWindowField `cfg:"match-fields"`
	// Hide the currently focused window from the list.
	HideActive bool `cfg:"hide-active"`
	// Shift-delete closes the selected window.
	CloseOnDelete bool `cfg:"close-on-delete"`
}

// LauncherSSHConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// ssh mode settings.
type LauncherSSHConfig struct {
	// SSH client binary.
	Client string `cfg:"client"`
	// Connect template (`{terminal}`, `{ssh-client}`, `{host}`).
	Command string `cfg:"command"`
	// Include hosts from `/etc/hosts`.
	ParseHosts bool `cfg:"parse-hosts"`
	// Include hosts from `~/.ssh/known_hosts`.
	ParseKnownHosts bool `cfg:"parse-known-hosts"`
}

// LauncherFilebrowserConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// file browser mode settings.
type LauncherFilebrowserConfig struct {
	// Start directory ("" = home).
	Directory string `cfg:"directory"`
	// File ordering.
	SortingMethod LauncherFileSort `cfg:"sorting-method"`
	// List directories before files.
	DirectoriesFirst bool `cfg:"directories-first"`
	// Show hidden files.
	ShowHidden bool `cfg:"show-hidden"`
	// Command opening the picked file ("" = xdg-open).
	Command string `cfg:"command"`
}

// LauncherCombiConfig is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// combi mode settings.
type LauncherCombiConfig struct {
	// Modes merged into the combined list.
	Modes []string `cfg:"modes"`
	// Row template (`{mode}`, `{text}`).
	DisplayFormat string `cfg:"display-format"`
}

// LauncherLocation is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Launcher surface position (rofi `-location` 0-8 grid).
type LauncherLocation string

// LauncherLocation values.
const (
	// Centered (rofi location 0).
	LauncherCenter LauncherLocation = "center"
	// Top-left (1).
	LauncherNorthWest LauncherLocation = "north-west"
	// Top edge (2).
	LauncherNorth LauncherLocation = "north"
	// Top-right (3).
	LauncherNorthEast LauncherLocation = "north-east"
	// Right edge (4).
	LauncherEast LauncherLocation = "east"
	// Bottom-right (5).
	LauncherSouthEast LauncherLocation = "south-east"
	// Bottom edge (6).
	LauncherSouth LauncherLocation = "south"
	// Bottom-left (7).
	LauncherSouthWest LauncherLocation = "south-west"
	// Left edge (8).
	LauncherWest LauncherLocation = "west"
)

// LauncherMatching is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Matching method (rofi `-matching`).
type LauncherMatching string

// LauncherMatching values.
const (
	// Tokenized substring matching.
	MatchingNormal LauncherMatching = "normal"
	// Regular expression.
	MatchingRegex LauncherMatching = "regex"
	// Glob patterns per token.
	MatchingGlob LauncherMatching = "glob"
	// fzf-style fuzzy matching.
	MatchingFuzzy LauncherMatching = "fuzzy"
	// Tokenized prefix matching.
	MatchingPrefix LauncherMatching = "prefix"
)

// LauncherSorting is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Result sorting method (rofi `-sorting-method`).
type LauncherSorting string

// LauncherSorting values.
const (
	// Levenshtein distance to the query (rofi "normal").
	SortingLevenshtein LauncherSorting = "levenshtein"
	// fzf match-quality score.
	SortingFzf LauncherSorting = "fzf"
)

// LauncherCase is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Case handling (collapses rofi `-case-sensitive`/`-case-smart`).
type LauncherCase string

// LauncherCase values.
const (
	// Always case-insensitive.
	CaseInsensitive LauncherCase = "insensitive"
	// Sensitive only when the query contains an uppercase char.
	CaseSmart LauncherCase = "smart"
	// Always case-sensitive.
	CaseSensitive LauncherCase = "sensitive"
)

// LauncherDrunField is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Desktop-entry fields searched by drun (rofi `-drun-match-fields`).
type LauncherDrunField string

// LauncherDrunField values.
const (
	// Localized Name.
	DrunFieldName LauncherDrunField = "name"
	// GenericName.
	DrunFieldGeneric LauncherDrunField = "generic"
	// Exec command line.
	DrunFieldExec LauncherDrunField = "exec"
	// Categories list.
	DrunFieldCategories LauncherDrunField = "categories"
	// Comment.
	DrunFieldComment LauncherDrunField = "comment"
	// Keywords list.
	DrunFieldKeywords LauncherDrunField = "keywords"
)

// LauncherWindowField is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// Window fields searched by window mode (rofi `-window-match-fields`).
type LauncherWindowField string

// LauncherWindowField values.
const (
	// Window title.
	WindowFieldTitle LauncherWindowField = "title"
	// Application class/app-id.
	WindowFieldClass LauncherWindowField = "class"
	// Window name.
	WindowFieldName LauncherWindowField = "name"
	// Window role.
	WindowFieldRole LauncherWindowField = "role"
	// Workspace/desktop name.
	WindowFieldDesktop LauncherWindowField = "desktop"
)

// LauncherFileSort is a [launcher] schema type (launcher/mod.rs, launcher/types.rs).
//
// File sorting in the file browser (rofi filebrowser `sorting-method`).
type LauncherFileSort string

// LauncherFileSort values.
const (
	// By file name.
	FileSortName LauncherFileSort = "name"
	// By modification time.
	FileSortMtime LauncherFileSort = "mtime"
	// By access time.
	FileSortAtime LauncherFileSort = "atime"
	// By creation time.
	FileSortCtime LauncherFileSort = "ctime"
)

var (
	_ = registerEnum(LauncherCenter, LauncherNorthWest, LauncherNorth, LauncherNorthEast,
		LauncherEast, LauncherSouthEast, LauncherSouth, LauncherSouthWest, LauncherWest)
	_ = registerEnum(MatchingNormal, MatchingRegex, MatchingGlob, MatchingFuzzy, MatchingPrefix)
	_ = registerEnum(SortingLevenshtein, SortingFzf)
	_ = registerEnum(CaseInsensitive, CaseSmart, CaseSensitive)
	_ = registerEnum(DrunFieldName, DrunFieldGeneric, DrunFieldExec, DrunFieldCategories, DrunFieldComment, DrunFieldKeywords)
	_ = registerEnum(WindowFieldTitle, WindowFieldClass, WindowFieldName, WindowFieldRole, WindowFieldDesktop)
	_ = registerEnum(FileSortName, FileSortMtime, FileSortAtime, FileSortCtime)
)

// launcherLocations is rofi's -location numbering: 0 center, then
// clockwise from the top-left corner.
var launcherLocations = []LauncherLocation{
	LauncherCenter, LauncherNorthWest, LauncherNorth, LauncherNorthEast,
	LauncherEast, LauncherSouthEast, LauncherSouth, LauncherSouthWest, LauncherWest,
}

// LauncherLocationFromRofi maps rofi's numeric -location onto the
// schema enum; false outside 0..8.
func LauncherLocationFromRofi(n int) (LauncherLocation, bool) {
	if n < 0 || n >= len(launcherLocations) {
		return "", false
	}
	return launcherLocations[n], true
}

// DefaultsLauncher returns the schema defaults.
func DefaultsLauncher() LauncherConfig {
	return LauncherConfig{
		Location:       LauncherCenter,
		Width:          Scale(1),
		Lines:          10,
		Modes:          []string{"drun", "run", "window"},
		Cycle:          true,
		Matching:       MatchingNormal,
		Tokenize:       true,
		NegateChar:     "-",
		NormalizeMatch: true,
		SortingMethod:  SortingLevenshtein,
		Case:           CaseInsensitive,
		ShowIcons:      true,
		FixedNumLines:  true,
		DisplayNames:   map[string]string{},
		Scripts:        map[string]string{},
		Keybindings:    map[string]string{},
		MouseBindings:  map[string]string{},
		Styles:         map[string]string{},
		History:        LauncherHistoryConfig{Enable: true, MaxSize: 25},
		Drun: LauncherDrunConfig{
			MatchFields: []LauncherDrunField{
				DrunFieldName, DrunFieldGeneric, DrunFieldExec, DrunFieldCategories, DrunFieldKeywords,
			},
			DisplayFormat: "{name} [<span weight='light' size='small'><i>({generic})</i></span>]",
			URLLauncher:   "xdg-open",
		},
		Run: LauncherRunConfig{
			RunCommand:   "{cmd}",
			ShellCommand: "{terminal} -e {cmd}",
		},
		Window: LauncherWindowConfig{
			Format:        "{w}   {c}   {t}",
			MatchFields:   []LauncherWindowField{WindowFieldTitle, WindowFieldClass},
			CloseOnDelete: true,
		},
		SSH: LauncherSSHConfig{
			Client:          "ssh",
			Command:         "{terminal} -e {ssh-client} {host}",
			ParseKnownHosts: true,
		},
		Filebrowser: LauncherFilebrowserConfig{
			SortingMethod:    FileSortName,
			DirectoriesFirst: true,
		},
		Combi: LauncherCombiConfig{
			Modes:         []string{"window", "drun", "run"},
			DisplayFormat: "{text}",
		},
	}
}

// configSchemaName keeps the Rust def name.
func (LauncherSSHConfig) configSchemaName() string { return "LauncherSshConfig" }

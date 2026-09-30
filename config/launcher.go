package config

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

// LauncherWidthBaseRem is the base width (rem, 1rem = 16px) a Size
// multiplier in [launcher] width resolves against: 37.5rem = 600px
// (schemas/launcher WIDTH_BASE_REM).
const LauncherWidthBaseRem = 37.5

// LauncherLocation is the launcher's screen position, rofi's -location
// 0-8 grid by name.
type LauncherLocation string

// Launcher locations, in rofi's numeric order (0 center, 1 north-west,
// then clockwise).
const (
	LauncherCenter    LauncherLocation = "center"
	LauncherNorthWest LauncherLocation = "north-west"
	LauncherNorth     LauncherLocation = "north"
	LauncherNorthEast LauncherLocation = "north-east"
	LauncherEast      LauncherLocation = "east"
	LauncherSouthEast LauncherLocation = "south-east"
	LauncherSouth     LauncherLocation = "south"
	LauncherSouthWest LauncherLocation = "south-west"
	LauncherWest      LauncherLocation = "west"
)

// launcherLocations is the rofi numbering: index = -location value.
var launcherLocations = []LauncherLocation{
	LauncherCenter, LauncherNorthWest, LauncherNorth, LauncherNorthEast,
	LauncherEast, LauncherSouthEast, LauncherSouth, LauncherSouthWest, LauncherWest,
}

// LauncherLocationFromRofi maps rofi's numeric -location (0-8) onto the
// named location; anything else is not a location.
func LauncherLocationFromRofi(n int) (LauncherLocation, bool) {
	if n < 0 || n >= len(launcherLocations) {
		return "", false
	}
	return launcherLocations[n], true
}

// LauncherMatching is the matching method (rofi -matching).
type LauncherMatching string

// Matching methods.
const (
	MatchingNormal LauncherMatching = "normal"
	MatchingRegex  LauncherMatching = "regex"
	MatchingGlob   LauncherMatching = "glob"
	MatchingFuzzy  LauncherMatching = "fuzzy"
	MatchingPrefix LauncherMatching = "prefix"
)

// LauncherSorting is the ranking method when sort is on (rofi
// -sorting-method).
type LauncherSorting string

// Sorting methods.
const (
	SortingLevenshtein LauncherSorting = "levenshtein"
	SortingFzf         LauncherSorting = "fzf"
)

// LauncherCase is case handling while matching (rofi -case-sensitive
// and -case-smart collapsed).
type LauncherCase string

// Case modes.
const (
	CaseInsensitive LauncherCase = "insensitive"
	CaseSmart       LauncherCase = "smart"
	CaseSensitive   LauncherCase = "sensitive"
)

// LauncherDrunField is a desktop-entry field drun matches against
// (rofi -drun-match-fields).
type LauncherDrunField string

// drun match fields.
const (
	DrunFieldName       LauncherDrunField = "name"
	DrunFieldGeneric    LauncherDrunField = "generic"
	DrunFieldExec       LauncherDrunField = "exec"
	DrunFieldCategories LauncherDrunField = "categories"
	DrunFieldComment    LauncherDrunField = "comment"
	DrunFieldKeywords   LauncherDrunField = "keywords"
)

// LauncherWindowField is a window field the window mode matches against
// (rofi -window-match-fields).
type LauncherWindowField string

// window match fields.
const (
	WindowFieldTitle   LauncherWindowField = "title"
	WindowFieldClass   LauncherWindowField = "class"
	WindowFieldName    LauncherWindowField = "name"
	WindowFieldRole    LauncherWindowField = "role"
	WindowFieldDesktop LauncherWindowField = "desktop"
)

// LauncherFileSort is the file browser's ordering.
type LauncherFileSort string

// File orderings.
const (
	FileSortName  LauncherFileSort = "name"
	FileSortMtime LauncherFileSort = "mtime"
	FileSortAtime LauncherFileSort = "atime"
	FileSortCtime LauncherFileSort = "ctime"
)

// The schema's value sets, for load-time validation.
var (
	validMatching     = setOf(MatchingNormal, MatchingRegex, MatchingGlob, MatchingFuzzy, MatchingPrefix)
	validSorting      = setOf(SortingLevenshtein, SortingFzf)
	validCase         = setOf(CaseInsensitive, CaseSmart, CaseSensitive)
	validDrunFields   = setOf(DrunFieldName, DrunFieldGeneric, DrunFieldExec, DrunFieldCategories, DrunFieldComment, DrunFieldKeywords)
	validWindowFields = setOf(WindowFieldTitle, WindowFieldClass, WindowFieldName, WindowFieldRole, WindowFieldDesktop)
	validFileSorts    = setOf(FileSortName, FileSortMtime, FileSortAtime, FileSortCtime)
	validLauncherLocs = setOf(launcherLocations...)
)

func setOf[T comparable](values ...T) map[T]bool {
	set := make(map[T]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set
}

// LauncherConfig is the [launcher] section: the application launcher
// and dmenu (schemas/launcher/mod.rs).
type LauncherConfig struct {
	Location       LauncherLocation
	Width          Size
	Lines          uint32
	Monitor        string
	Modes          []string
	Cycle          bool
	Matching       LauncherMatching
	Tokenize       bool
	NegateChar     string
	NormalizeMatch bool
	Sort           bool
	SortingMethod  LauncherSorting
	Case           LauncherCase
	Terminal       string
	ShowIcons      bool
	IconTheme      string
	SidebarMode    bool
	AutoSelect     bool
	HoverSelect    bool
	FixedNumLines  bool
	DisplayNames   map[string]string
	Scripts        map[string]string
	Keybindings    map[string]string
	MouseBindings  map[string]string
	Font           string
	Styles         map[string]string
	PreviewCmd     string
	History        LauncherHistoryConfig
	Drun           LauncherDrunConfig
	Run            LauncherRunConfig
	Window         LauncherWindowConfig
	SSH            LauncherSSHConfig
	Filebrowser    LauncherFilebrowserConfig
	Combi          LauncherCombiConfig
}

// LauncherHistoryConfig is [launcher.history]: launch history and
// frecency.
type LauncherHistoryConfig struct {
	Enable  bool
	MaxSize uint32
}

// LauncherDrunConfig is [launcher.drun].
type LauncherDrunConfig struct {
	Categories        []string
	ExcludeCategories []string
	MatchFields       []LauncherDrunField
	DisplayFormat     string
	ShowActions       bool
	URLLauncher       string
}

// LauncherRunConfig is [launcher.run].
type LauncherRunConfig struct {
	RunCommand   string
	ShellCommand string
	ListCommand  string
}

// LauncherWindowConfig is [launcher.window].
type LauncherWindowConfig struct {
	Format        string
	MatchFields   []LauncherWindowField
	HideActive    bool
	CloseOnDelete bool
}

// LauncherSSHConfig is [launcher.ssh].
type LauncherSSHConfig struct {
	Client          string
	Command         string
	ParseHosts      bool
	ParseKnownHosts bool
}

// LauncherFilebrowserConfig is [launcher.filebrowser].
type LauncherFilebrowserConfig struct {
	Directory        string
	SortingMethod    LauncherFileSort
	DirectoriesFirst bool
	ShowHidden       bool
	Command          string
}

// LauncherCombiConfig is [launcher.combi].
type LauncherCombiConfig struct {
	Modes         []string
	DisplayFormat string
}

// DefaultsLauncher returns the schema defaults.
func DefaultsLauncher() LauncherConfig {
	return LauncherConfig{
		Location:       LauncherCenter,
		Width:          Size{Value: 1.0, Unit: SizeMultiplier},
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

// launcherDoc mirrors the [launcher] table. Pointers distinguish an
// absent key (keep the default) from a present one.
type launcherDoc struct {
	Location       *string            `toml:"location"`
	Width          tomlValue          `toml:"width"`
	Lines          *uint32            `toml:"lines"`
	Monitor        *string            `toml:"monitor"`
	Modes          *[]string          `toml:"modes"`
	Cycle          *bool              `toml:"cycle"`
	Matching       *string            `toml:"matching"`
	Tokenize       *bool              `toml:"tokenize"`
	NegateChar     *string            `toml:"negate-char"`
	NormalizeMatch *bool              `toml:"normalize-match"`
	Sort           *bool              `toml:"sort"`
	SortingMethod  *string            `toml:"sorting-method"`
	Case           *string            `toml:"case"`
	Terminal       *string            `toml:"terminal"`
	ShowIcons      *bool              `toml:"show-icons"`
	IconTheme      *string            `toml:"icon-theme"`
	SidebarMode    *bool              `toml:"sidebar-mode"`
	AutoSelect     *bool              `toml:"auto-select"`
	HoverSelect    *bool              `toml:"hover-select"`
	FixedNumLines  *bool              `toml:"fixed-num-lines"`
	DisplayNames   *map[string]string `toml:"display-names"`
	Scripts        *map[string]string `toml:"scripts"`
	Keybindings    *map[string]string `toml:"keybindings"`
	MouseBindings  *map[string]string `toml:"mouse-bindings"`
	Font           *string            `toml:"font"`
	Styles         *map[string]string `toml:"styles"`
	PreviewCmd     *string            `toml:"preview-cmd"`
	History        *struct {
		Enable  *bool   `toml:"enable"`
		MaxSize *uint32 `toml:"max-size"`
	} `toml:"history"`
	Drun *struct {
		Categories        *[]string `toml:"categories"`
		ExcludeCategories *[]string `toml:"exclude-categories"`
		MatchFields       *[]string `toml:"match-fields"`
		DisplayFormat     *string   `toml:"display-format"`
		ShowActions       *bool     `toml:"show-actions"`
		URLLauncher       *string   `toml:"url-launcher"`
	} `toml:"drun"`
	Run *struct {
		RunCommand   *string `toml:"run-command"`
		ShellCommand *string `toml:"shell-command"`
		ListCommand  *string `toml:"list-command"`
	} `toml:"run"`
	Window *struct {
		Format        *string   `toml:"format"`
		MatchFields   *[]string `toml:"match-fields"`
		HideActive    *bool     `toml:"hide-active"`
		CloseOnDelete *bool     `toml:"close-on-delete"`
	} `toml:"window"`
	SSH *struct {
		Client          *string `toml:"client"`
		Command         *string `toml:"command"`
		ParseHosts      *bool   `toml:"parse-hosts"`
		ParseKnownHosts *bool   `toml:"parse-known-hosts"`
	} `toml:"ssh"`
	Filebrowser *struct {
		Directory        *string `toml:"directory"`
		SortingMethod    *string `toml:"sorting-method"`
		DirectoriesFirst *bool   `toml:"directories-first"`
		ShowHidden       *bool   `toml:"show-hidden"`
		Command          *string `toml:"command"`
	} `toml:"filebrowser"`
	Combi *struct {
		Modes         *[]string `toml:"modes"`
		DisplayFormat *string   `toml:"display-format"`
	} `toml:"combi"`
}

// applyLauncher overlays [launcher]. Every enum value is checked against
// the schema's set: a typo is a load error, not a silent default.
func applyLauncher(md toml.MetaData, prim toml.Primitive) (LauncherConfig, error) {
	cfg := DefaultsLauncher()
	var doc launcherDoc
	if err := md.PrimitiveDecode(prim, &doc); err != nil {
		return cfg, err
	}
	var err error
	if cfg.Location, err = pickEnum("launcher location", doc.Location, cfg.Location, validLauncherLocs); err != nil {
		return cfg, err
	}
	if doc.Width.value != nil {
		if err := cfg.Width.unmarshal(doc.Width.value, "launcher width"); err != nil {
			return cfg, err
		}
	}
	if cfg.Matching, err = pickEnum("launcher matching", doc.Matching, cfg.Matching, validMatching); err != nil {
		return cfg, err
	}
	if cfg.SortingMethod, err = pickEnum("launcher sorting-method", doc.SortingMethod, cfg.SortingMethod, validSorting); err != nil {
		return cfg, err
	}
	if cfg.Case, err = pickEnum("launcher case", doc.Case, cfg.Case, validCase); err != nil {
		return cfg, err
	}
	setIf(&cfg.Lines, doc.Lines)
	setIf(&cfg.Monitor, doc.Monitor)
	setIf(&cfg.Modes, doc.Modes)
	setIf(&cfg.Cycle, doc.Cycle)
	setIf(&cfg.Tokenize, doc.Tokenize)
	setIf(&cfg.NegateChar, doc.NegateChar)
	setIf(&cfg.NormalizeMatch, doc.NormalizeMatch)
	setIf(&cfg.Sort, doc.Sort)
	setIf(&cfg.Terminal, doc.Terminal)
	setIf(&cfg.ShowIcons, doc.ShowIcons)
	setIf(&cfg.IconTheme, doc.IconTheme)
	setIf(&cfg.SidebarMode, doc.SidebarMode)
	setIf(&cfg.AutoSelect, doc.AutoSelect)
	setIf(&cfg.HoverSelect, doc.HoverSelect)
	setIf(&cfg.FixedNumLines, doc.FixedNumLines)
	setIf(&cfg.DisplayNames, doc.DisplayNames)
	setIf(&cfg.Scripts, doc.Scripts)
	setIf(&cfg.Keybindings, doc.Keybindings)
	setIf(&cfg.MouseBindings, doc.MouseBindings)
	setIf(&cfg.Font, doc.Font)
	setIf(&cfg.Styles, doc.Styles)
	setIf(&cfg.PreviewCmd, doc.PreviewCmd)
	if h := doc.History; h != nil {
		setIf(&cfg.History.Enable, h.Enable)
		setIf(&cfg.History.MaxSize, h.MaxSize)
	}
	if d := doc.Drun; d != nil {
		setIf(&cfg.Drun.Categories, d.Categories)
		setIf(&cfg.Drun.ExcludeCategories, d.ExcludeCategories)
		if d.MatchFields != nil {
			fields, err := enumList("launcher drun match-fields", *d.MatchFields, validDrunFields)
			if err != nil {
				return cfg, err
			}
			cfg.Drun.MatchFields = fields
		}
		setIf(&cfg.Drun.DisplayFormat, d.DisplayFormat)
		setIf(&cfg.Drun.ShowActions, d.ShowActions)
		setIf(&cfg.Drun.URLLauncher, d.URLLauncher)
	}
	if r := doc.Run; r != nil {
		setIf(&cfg.Run.RunCommand, r.RunCommand)
		setIf(&cfg.Run.ShellCommand, r.ShellCommand)
		setIf(&cfg.Run.ListCommand, r.ListCommand)
	}
	if w := doc.Window; w != nil {
		setIf(&cfg.Window.Format, w.Format)
		if w.MatchFields != nil {
			fields, err := enumList("launcher window match-fields", *w.MatchFields, validWindowFields)
			if err != nil {
				return cfg, err
			}
			cfg.Window.MatchFields = fields
		}
		setIf(&cfg.Window.HideActive, w.HideActive)
		setIf(&cfg.Window.CloseOnDelete, w.CloseOnDelete)
	}
	if s := doc.SSH; s != nil {
		setIf(&cfg.SSH.Client, s.Client)
		setIf(&cfg.SSH.Command, s.Command)
		setIf(&cfg.SSH.ParseHosts, s.ParseHosts)
		setIf(&cfg.SSH.ParseKnownHosts, s.ParseKnownHosts)
	}
	if f := doc.Filebrowser; f != nil {
		setIf(&cfg.Filebrowser.Directory, f.Directory)
		if cfg.Filebrowser.SortingMethod, err = pickEnum("launcher filebrowser sorting-method", f.SortingMethod, cfg.Filebrowser.SortingMethod, validFileSorts); err != nil {
			return cfg, err
		}
		setIf(&cfg.Filebrowser.DirectoriesFirst, f.DirectoriesFirst)
		setIf(&cfg.Filebrowser.ShowHidden, f.ShowHidden)
		setIf(&cfg.Filebrowser.Command, f.Command)
	}
	if c := doc.Combi; c != nil {
		setIf(&cfg.Combi.Modes, c.Modes)
		setIf(&cfg.Combi.DisplayFormat, c.DisplayFormat)
	}
	return cfg, nil
}

// setIf overwrites *dst with *src when the key was present.
func setIf[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// pickEnum validates an optional enum key against its schema set.
func pickEnum[T ~string](key string, raw *string, fallback T, valid map[T]bool) (T, error) {
	if raw == nil {
		return fallback, nil
	}
	v := T(*raw)
	if !valid[v] {
		return fallback, fmt.Errorf("%s: unknown value %q", key, *raw)
	}
	return v, nil
}

// enumList validates every element of an enum list.
func enumList[T ~string](key string, raw []string, valid map[T]bool) ([]T, error) {
	out := make([]T, 0, len(raw))
	for _, r := range raw {
		v := T(r)
		if !valid[v] {
			return nil, fmt.Errorf("%s: unknown value %q", key, r)
		}
		out = append(out, v)
	}
	return out, nil
}

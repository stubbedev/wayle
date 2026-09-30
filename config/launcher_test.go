package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLauncherDefaultsMatchTheSchema(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Launcher
	if l.Location != LauncherCenter || l.Lines != 10 || !l.Cycle || !l.Tokenize ||
		l.NegateChar != "-" || !l.NormalizeMatch || l.Sort || l.SortingMethod != SortingLevenshtein ||
		l.Case != CaseInsensitive || !l.ShowIcons || !l.FixedNumLines || l.Matching != MatchingNormal {
		t.Errorf("launcher defaults drifted: %+v", l)
	}
	if !reflect.DeepEqual(l.Modes, []string{"drun", "run", "window"}) {
		t.Errorf("modes = %v", l.Modes)
	}
	if l.Width != (Size{Value: 1.0, Unit: SizeMultiplier}) {
		t.Errorf("width = %+v", l.Width)
	}
	if !l.History.Enable || l.History.MaxSize != 25 {
		t.Errorf("history = %+v", l.History)
	}
	if l.Drun.DisplayFormat != "{name} [<span weight='light' size='small'><i>({generic})</i></span>]" ||
		l.Drun.URLLauncher != "xdg-open" || len(l.Drun.MatchFields) != 5 {
		t.Errorf("drun = %+v", l.Drun)
	}
	if l.Run.RunCommand != "{cmd}" || l.Run.ShellCommand != "{terminal} -e {cmd}" {
		t.Errorf("run = %+v", l.Run)
	}
	if l.Window.Format != "{w}   {c}   {t}" || !l.Window.CloseOnDelete ||
		!reflect.DeepEqual(l.Window.MatchFields, []LauncherWindowField{WindowFieldTitle, WindowFieldClass}) {
		t.Errorf("window = %+v", l.Window)
	}
	if l.SSH.Client != "ssh" || l.SSH.Command != "{terminal} -e {ssh-client} {host}" || l.SSH.ParseHosts || !l.SSH.ParseKnownHosts {
		t.Errorf("ssh = %+v", l.SSH)
	}
	if l.Filebrowser.SortingMethod != FileSortName || !l.Filebrowser.DirectoriesFirst || l.Filebrowser.ShowHidden {
		t.Errorf("filebrowser = %+v", l.Filebrowser)
	}
	if !reflect.DeepEqual(l.Combi.Modes, []string{"window", "drun", "run"}) || l.Combi.DisplayFormat != "{text}" {
		t.Errorf("combi = %+v", l.Combi)
	}
}

func TestLauncherSectionApplies(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, `
[launcher]
location = "north-east"
width = "800px"
lines = 6
modes = ["drun", "clip"]
matching = "fuzzy"
sort = true
sorting-method = "fzf"
case = "smart"
fixed-num-lines = false

[launcher.scripts]
clip = "~/bin/clip.sh"

[launcher.keybindings]
cancel = "Control+q"

[launcher.mouse-bindings]
me-accept-entry = "MouseDPrimary"

[launcher.styles]
compact = ".launcher-row { padding: 0; }"

[launcher.history]
max-size = 5

[launcher.drun]
match-fields = ["name", "comment"]

[launcher.window]
match-fields = ["desktop"]

[launcher.filebrowser]
sorting-method = "mtime"
`))
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Launcher
	if l.Location != LauncherNorthEast || l.Width != (Size{Value: 800, Unit: SizePixels}) || l.Lines != 6 {
		t.Errorf("geometry = %v %+v %d", l.Location, l.Width, l.Lines)
	}
	if l.Matching != MatchingFuzzy || !l.Sort || l.SortingMethod != SortingFzf || l.Case != CaseSmart || l.FixedNumLines {
		t.Errorf("matching = %+v", l)
	}
	if l.Scripts["clip"] != "~/bin/clip.sh" || l.Keybindings["cancel"] != "Control+q" ||
		l.MouseBindings["me-accept-entry"] != "MouseDPrimary" || l.Styles["compact"] == "" {
		t.Errorf("maps = %+v %+v %+v %+v", l.Scripts, l.Keybindings, l.MouseBindings, l.Styles)
	}
	if l.History.MaxSize != 5 || !l.History.Enable {
		t.Errorf("history = %+v (an unset enable keeps its default)", l.History)
	}
	if !reflect.DeepEqual(l.Drun.MatchFields, []LauncherDrunField{DrunFieldName, DrunFieldComment}) {
		t.Errorf("drun match-fields = %v", l.Drun.MatchFields)
	}
	if !reflect.DeepEqual(l.Window.MatchFields, []LauncherWindowField{WindowFieldDesktop}) {
		t.Errorf("window match-fields = %v", l.Window.MatchFields)
	}
	if l.Filebrowser.SortingMethod != FileSortMtime {
		t.Errorf("filebrowser sorting = %v", l.Filebrowser.SortingMethod)
	}
}

func TestLauncherBadValuesAreLoadErrors(t *testing.T) {
	for name, body := range map[string]string{
		"location":           `location = "middle"`,
		"matching":           `matching = "exact"`,
		"sorting-method":     `sorting-method = "normal"`,
		"case":               `case = "upper"`,
		"width":              `width = "wide"`,
		"negative lines":     `lines = -1`,
		"drun match field":   "[launcher.drun]\nmatch-fields = [\"name\", \"icon\"]",
		"window match field": "[launcher.window]\nmatch-fields = [\"all\"]",
		"file sort":          "[launcher.filebrowser]\nsorting-method = \"size\"",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadFile(writeConfig(t, "[launcher]\n"+body+"\n"))
			if err == nil {
				t.Fatalf("%s: a bad value must be a load error", name)
			}
			if !strings.Contains(err.Error(), "launcher") && name != "negative lines" {
				t.Errorf("error does not name the section: %v", err)
			}
			if cfg.Launcher.Location != LauncherCenter {
				t.Errorf("a failed load must leave the defaults, got %+v", cfg.Launcher)
			}
		})
	}
}

func TestLauncherLocationFromRofi(t *testing.T) {
	for n, want := range launcherLocations {
		got, ok := LauncherLocationFromRofi(n)
		if !ok || got != want {
			t.Errorf("rofi location %d = %v %v, want %v", n, got, ok, want)
		}
	}
	for _, n := range []int{-1, 9, 42} {
		if _, ok := LauncherLocationFromRofi(n); ok {
			t.Errorf("rofi location %d must not be a location", n)
		}
	}
	if got, _ := LauncherLocationFromRofi(3); got != LauncherNorthEast {
		t.Errorf("rofi 3 = %v, want north-east", got)
	}
}

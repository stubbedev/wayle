package config

import (
	"reflect"
	"strings"
	"testing"
)

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

// A bad launcher value is a diagnostic naming its field; that field
// keeps its default and the rest of the section still applies.
func TestLauncherBadValuesAreFieldDiagnostics(t *testing.T) {
	for name, tc := range map[string]struct{ body, path string }{
		"location":           {`location = "middle"`, "launcher.location"},
		"matching":           {`matching = "exact"`, "launcher.matching"},
		"sorting-method":     {`sorting-method = "normal"`, "launcher.sorting-method"},
		"case":               {`case = "upper"`, "launcher.case"},
		"negative lines":     {`lines = -1`, "launcher.lines"},
		"drun match field":   {"[launcher.drun]\nmatch-fields = [\"name\", \"icon\"]", "launcher.drun.match-fields"},
		"window match field": {"[launcher.window]\nmatch-fields = [\"all\"]", "launcher.window.match-fields"},
		"file sort":          {"[launcher.filebrowser]\nsorting-method = \"size\"", "launcher.filebrowser.sorting-method"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := LoadFile(writeConfig(t, "[launcher]\nterminal = \"foot\"\n"+tc.body+"\n"))
			if err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("diagnostics %v, want one for %s", err, tc.path)
			}
			want := DefaultsLauncher()
			want.Terminal = "foot"
			if !reflect.DeepEqual(cfg.Launcher, want) {
				t.Errorf("only the bad field may keep its default:\n%+v\nwant\n%+v", cfg.Launcher, want)
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

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	return writeFile(t, t.TempDir(), "config.toml", body)
}

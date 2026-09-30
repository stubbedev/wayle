package desktopentry

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, dir, rel, body string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKeyFileStringsListsAndLocales(t *testing.T) {
	kf, err := ParseKeyFile([]byte("# c\n[Desktop Entry]\nName=Files\nName[de]=Dateien\nComment=a\\sb\\nc\nKeywords=one;two\\;half;three;\nNoDisplay=true\n"))
	if err != nil {
		t.Fatal(err)
	}
	kf.languages = languagesFor("de_DE.UTF-8")
	if v, _ := kf.LocaleString(DesktopGroup, "Name"); v != "Dateien" {
		t.Errorf("localized name = %q", v)
	}
	kf.languages = languagesFor("fr_FR")
	if v, _ := kf.LocaleString(DesktopGroup, "Name"); v != "Files" {
		t.Errorf("fallback name = %q", v)
	}
	if v, _ := kf.String(DesktopGroup, "Comment"); v != "a b\nc" {
		t.Errorf("escapes = %q", v)
	}
	if got := kf.List(DesktopGroup, "Keywords"); !reflect.DeepEqual(got, []string{"one", "two;half", "three"}) {
		t.Errorf("list = %q", got)
	}
	if !kf.Bool(DesktopGroup, "NoDisplay") || kf.Bool(DesktopGroup, "Missing") {
		t.Error("booleans")
	}
}

func TestMalformedKeyFilesAreRefused(t *testing.T) {
	for _, body := range []string{"Name=x\n", "[Desktop Entry]\nnot a pair\n", "[Desktop Entry\n"} {
		if _, err := ParseKeyFile([]byte(body)); err == nil {
			t.Errorf("%q must not parse", body)
		}
	}
}

func TestLanguagesWiden(t *testing.T) {
	if got := languagesFor("sr_RS.UTF-8@latin"); !reflect.DeepEqual(got, []string{"sr_RS@latin", "sr_RS", "sr@latin", "sr"}) {
		t.Errorf("got %q", got)
	}
	for _, l := range []string{"", "C", "POSIX"} {
		if got := languagesFor(l); got != nil {
			t.Errorf("%q = %q, want none", l, got)
		}
	}
}

func appsFixture(t *testing.T) (string, string) {
	t.Helper()
	bin := t.TempDir()
	tool := filepath.Join(bin, "mytool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	dir := t.TempDir()
	write(t, dir, "mytool.desktop", "[Desktop Entry]\nType=Application\nName=My Tool\nExec=mytool %U\nIcon=mytool\nActions=new;ghost;\n\n[Desktop Action new]\nName=New Window\nExec=mytool --new\n")
	write(t, dir, "kde/sub.desktop", "[Desktop Entry]\nType=Application\nName=Sub\nExec=mytool\n")
	write(t, dir, "missing.desktop", "[Desktop Entry]\nType=Application\nName=Gone\nExec=not-installed-anywhere\n")
	write(t, dir, "tryexec.desktop", "[Desktop Entry]\nType=Application\nName=Try\nTryExec=not-installed-anywhere\nExec=mytool\n")
	write(t, dir, "hidden.desktop", "[Desktop Entry]\nType=Application\nName=Hidden\nHidden=true\nExec=mytool\n")
	write(t, dir, "link.desktop", "[Desktop Entry]\nType=Link\nName=Example\nURL=https://example.com\n")
	write(t, dir, "nodisplay.desktop", "[Desktop Entry]\nType=Application\nName=NoDisplay\nNoDisplay=true\nExec=mytool\n")
	return dir, tool
}

func TestAllAppsAppliesGioLoadRules(t *testing.T) {
	dir, _ := appsFixture(t)
	var ids []string
	for _, a := range AllApps([]string{dir}) {
		ids = append(ids, a.ID)
	}
	want := map[string]bool{"mytool.desktop": true, "kde-sub.desktop": true, "nodisplay.desktop": true}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v", ids)
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("unexpected app %s (hidden, missing Exec/TryExec, and links are not apps)", id)
		}
	}
}

func TestAnEarlierDirWinsADesktopID(t *testing.T) {
	dir, _ := appsFixture(t)
	user := t.TempDir()
	write(t, user, "mytool.desktop", "[Desktop Entry]\nType=Application\nName=User Tool\nExec=mytool\n")
	a, ok := FindApp([]string{user, dir}, "mytool.desktop")
	if !ok || a.DisplayName() != "User Tool" {
		t.Errorf("found %q", a.DisplayName())
	}
	// A Hidden entry in the earlier dir still claims the id.
	write(t, user, "mytool.desktop", "[Desktop Entry]\nType=Application\nName=U\nHidden=true\nExec=mytool\n")
	if _, ok := FindApp([]string{user, dir}, "mytool.desktop"); ok {
		t.Error("a Hidden override must mask the system entry")
	}
}

func TestShouldShowAndShownIn(t *testing.T) {
	dir, _ := appsFixture(t)
	nd, _ := FindApp([]string{dir}, "nodisplay.desktop")
	if nd.ShouldShow() {
		t.Error("NoDisplay must not show")
	}
	kf, _ := ParseKeyFile([]byte("[Desktop Entry]\nOnlyShowIn=GNOME;\nNotShowIn=KDE;\n"))
	a := App{file: kf}
	if !a.ShownIn([]string{"GNOME"}) || a.ShownIn([]string{"KDE"}) || a.ShownIn([]string{"sway"}) {
		t.Error("OnlyShowIn/NotShowIn")
	}
	kf, _ = ParseKeyFile([]byte("[Desktop Entry]\nNotShowIn=KDE;\n"))
	a = App{file: kf}
	if !a.ShownIn([]string{"sway"}) || a.ShownIn([]string{"KDE"}) {
		t.Error("NotShowIn only")
	}
}

func TestExpandExecFieldCodes(t *testing.T) {
	dir, _ := appsFixture(t)
	a, _ := FindApp([]string{dir}, "mytool.desktop")
	for execLine, want := range map[string][]string{
		"mytool %U":              {"mytool"},
		"mytool %i --name %c":    {"mytool", "--icon", "mytool", "--name", "My Tool"},
		"mytool 100%% --file=%f": {"mytool", "100%", "--file="},
		"mytool %k":              {"mytool", a.Path},
		"'my tool' \"a b\"":      {"my tool", "a b"},
	} {
		got, err := a.ExpandExec(execLine)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("ExpandExec(%q) = %q %v, want %q", execLine, got, err, want)
		}
	}
	if _, err := a.ExpandExec("mytool 'unbalanced"); err == nil {
		t.Error("an unparseable Exec must be an error")
	}
}

func TestActions(t *testing.T) {
	dir, _ := appsFixture(t)
	a, _ := FindApp([]string{dir}, "mytool.desktop")
	if got := a.Actions(); !reflect.DeepEqual(got, []string{"new"}) {
		t.Errorf("actions = %q (an action without a group is dropped)", got)
	}
	if a.ActionName("new") != "New Window" {
		t.Errorf("action name = %q", a.ActionName("new"))
	}
	if err := a.LaunchAction("ghost"); err == nil {
		t.Error("an unknown action must be an error")
	}
}

func TestLinks(t *testing.T) {
	dir, _ := appsFixture(t)
	links := Links([]string{dir})
	if len(links) != 1 || links[0].URL != "https://example.com" || links[0].ID != "link.desktop" {
		t.Errorf("links = %+v", links)
	}
}

func TestLaunchRunsTheExecDetached(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	script := filepath.Join(bin, "marktool")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$GIO_LAUNCHED_DESKTOP_FILE\" > \""+marker+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin:"+os.Getenv("PATH"))
	dir := t.TempDir()
	path := write(t, dir, "mark.desktop", "[Desktop Entry]\nType=Application\nName=Mark\nExec=marktool %u\n")
	a, ok := FindApp([]string{dir}, "mark.desktop")
	if !ok {
		t.Fatal("fixture app not found")
	}
	if err := a.Launch(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && string(data) == path+"\n"
	})
}

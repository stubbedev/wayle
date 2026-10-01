package icons

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/wayle/internal/icons/icontest"
)

const homeSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 12l9-9 9 9" stroke="#000" stroke-width="2" fill="none"/></svg>`

// newTestManager is a manager over a fresh registry and the fake CDN.
func newTestManager(t *testing.T, cdn *icontest.CDN) *Manager {
	t.Helper()
	t.Setenv("XDG_DATA_DIRS", "")
	return NewManagerWith(RegistryAt(t.TempDir()), cdn.Client())
}

func TestManagerInstall(t *testing.T) {
	cdn := icontest.NewCDN(map[string]string{
		Tabler.CDNURL("home"):  homeSVG,
		Tabler.CDNURL("junk"):  `<html xmlns="http://www.w3.org/1999/xhtml"><body/></html>`,
		Tabler.CDNURL("blank"): `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"/>`,
		Tabler.CDNURL("../x"):  homeSVG,
	})
	m := newTestManager(t, cdn)

	result, err := m.Install(context.Background(), Tabler, []string{"home", "missing", "junk", "blank", "../x"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Installed, []string{"tb-home-symbolic"}) {
		t.Errorf("installed = %v, want [tb-home-symbolic]", result.Installed)
	}
	want := []InstallFailure{
		{"missing", "cannot fetch icon 'missing' from cdn: HTTP 404 Not Found"},
		{"junk", "invalid SVG content for 'junk': SVG data parsing failed cause the document does not have a root node"},
		{"blank", "invalid SVG content for 'blank': no extractable paths"},
		{"../x", "icon name 'tb-../x' must not contain '/', '\\', or '..'"},
	}
	if !slices.Equal(result.Failed, want) {
		t.Errorf("failed =\n%q\nwant\n%q", result.Failed, want)
	}
	got := readFile(t, filepath.Join(m.Registry().IconsDir(), "tb-home-symbolic.svg"))
	if symbolic, _ := ToSymbolic(homeSVG); got != symbolic {
		t.Errorf("installed file is not the transformed SVG:\n%s", got)
	}
	if !m.IsInstalled("tb-home-symbolic") || m.IsInstalled("tb-missing-symbolic") {
		t.Error("IsInstalled disagrees with what installed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(m.Registry().IconsDir()), "x-symbolic.svg")); err == nil {
		t.Error("a slug escaped the icons directory")
	}
}

func TestManagerInstallUncreatableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewManagerWith(RegistryAt(file), icontest.NewCDN(nil).Client())
	_, err := m.Install(context.Background(), Tabler, []string{"home"})
	if !isDirectoryError(err) {
		t.Fatalf("err = %v, want a DirectoryError", err)
	}
}

func TestManagerRemove(t *testing.T) {
	m := newTestManager(t, icontest.NewCDN(nil))
	dir := m.Registry().IconsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeIcon(t, dir, "cm-a-symbolic.svg", homeSVG)
	if err := m.Remove("cm-a-symbolic"); err != nil {
		t.Fatal(err)
	}
	if m.IsInstalled("cm-a-symbolic") {
		t.Error("still installed after Remove")
	}
	err := m.Remove("cm-a-symbolic")
	if err == nil || err.Error() != "icon 'cm-a-symbolic' not found" {
		t.Errorf("removing it again: %v, want not found", err)
	}
}

func TestManagerListMergesSystemIcons(t *testing.T) {
	m := newTestManager(t, icontest.NewCDN(nil))
	user := m.Registry().IconsDir()
	data := t.TempDir()
	system := filepath.Join(data, "wayle", "icons", "hicolor", "scalable", "actions")
	for _, d := range []string{user, system} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_DATA_DIRS", "/nonexistent:"+data)
	writeIcon(t, user, "tb-b-symbolic.svg", "")
	writeIcon(t, user, "shared-symbolic.svg", "")
	writeIcon(t, user, "notes.txt", "")
	writeIcon(t, user, ".svg", "")
	writeIcon(t, system, "ld-a-symbolic.svg", "")
	writeIcon(t, system, "shared-symbolic.svg", "")
	if got, want := m.List(), []string{"ld-a-symbolic", "shared-symbolic", "tb-b-symbolic"}; !slices.Equal(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestManagerImportLocal(t *testing.T) {
	m := newTestManager(t, icontest.NewCDN(nil))
	src := writeIcon(t, t.TempDir(), "mine.svg", homeSVG)

	name, err := m.ImportLocal(src, "mine")
	if err != nil || name != "cm-mine-symbolic" {
		t.Fatalf("ImportLocal = %q %v, want cm-mine-symbolic", name, err)
	}
	if !m.IsInstalled("cm-mine-symbolic") {
		t.Error("not installed")
	}

	bad := writeIcon(t, t.TempDir(), "bad.svg", "\xff\xfe")
	for _, tc := range []struct{ path, name, want string }{
		{src, "../up", "icon name '../up' must not contain '/', '\\', or '..'"},
		{src, "a/b", "icon name 'a/b' must not contain '/', '\\', or '..'"},
		{src, "", "icon name '' must not contain '/', '\\', or '..'"},
		{"/nonexistent.svg", "x", "icon '/nonexistent.svg' not found"},
		{bad, "x", "cannot read file '" + bad + "'"},
	} {
		if _, err := m.ImportLocal(tc.path, tc.name); err == nil || err.Error() != tc.want {
			t.Errorf("ImportLocal(%s, %q) = %v, want %q", filepath.Base(tc.path), tc.name, err, tc.want)
		}
	}
}

func TestManagerImportDir(t *testing.T) {
	m := newTestManager(t, icontest.NewCDN(nil))
	dir := t.TempDir()
	writeIcon(t, dir, "tb-kept.svg", homeSVG)
	writeIcon(t, dir, "ld-also-symbolic.svg", homeSVG)
	writeIcon(t, dir, "plain.svg", homeSVG)
	writeIcon(t, dir, "broken.svg", "<svg")
	writeIcon(t, dir, "readme.md", "x")

	result, err := m.ImportDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(result.Installed)
	if want := []string{"cm-plain-symbolic", "ld-also-symbolic", "tb-kept-symbolic"}; !slices.Equal(result.Installed, want) {
		t.Errorf("installed = %v, want %v (known prefixes kept, cm- added to the rest)", result.Installed, want)
	}
	if len(result.Failed) != 1 || result.Failed[0].Slug != "broken.svg" {
		t.Errorf("failed = %+v, want broken.svg", result.Failed)
	}

	if _, err := m.ImportDir(filepath.Join(dir, "readme.md")); err == nil || err.Error() != "icon '"+filepath.Join(dir, "readme.md")+"' not found" {
		t.Errorf("ImportDir(a file) = %v, want not found", err)
	}
}

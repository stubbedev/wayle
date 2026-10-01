package icons

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// legacyLucideWifi is a format-1 icon: a Grappa stroke, which GTK
// 4.21-4.23 render wrong.
const legacyLucideWifi = "<svg width='16' height='16' " +
	"xmlns:gpa='https://www.gtk.org/grappa' gpa:version='1'>" +
	"<path d='M2 8L14 8' stroke-width='1.33' stroke-linecap='round' " +
	"stroke='rgb(0,0,0)' fill='none' gpa:stroke='foreground'/></svg>"

func writeIcon(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sentinel(t *testing.T, dir string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, sentinelFilename))
	return string(data), err == nil
}

func TestMigrateRewritesStaleIcons(t *testing.T) {
	dir := t.TempDir()
	writeIcon(t, dir, sentinelFilename, "1\n")
	legacy := writeIcon(t, dir, "ld-wifi-symbolic.svg", legacyLucideWifi)
	current, _ := ToSymbolic(legacyLucideWifi)
	fresh := writeIcon(t, dir, "ld-bell-symbolic.svg", current)

	report := Migrate(dir)

	if report != (MigrationReport{Migrated: 1, Skipped: 1}) {
		t.Errorf("report = %+v, want 1 migrated (the legacy icon), 1 skipped (the current one)", report)
	}
	if got := readFile(t, legacy); got != current {
		t.Errorf("migrated icon =\n%s\nwant the current transform's\n%s", got, current)
	}
	if got := readFile(t, fresh); got != current {
		t.Error("a current icon was rewritten")
	}
	if s, ok := sentinel(t, dir); !ok || strings.TrimSpace(s) != strconv.Itoa(FormatVersion) {
		t.Errorf("sentinel = %q %v, want %d", s, ok, FormatVersion)
	}
}

func TestMigrateCurrentSentinelShortCircuits(t *testing.T) {
	dir := t.TempDir()
	writeIcon(t, dir, sentinelFilename, strconv.Itoa(FormatVersion)+"\n")
	legacy := writeIcon(t, dir, "ld-wifi-symbolic.svg", legacyLucideWifi)

	if report := Migrate(dir); report != (MigrationReport{}) {
		t.Errorf("report = %+v, want no scan", report)
	}
	if got := readFile(t, legacy); got != legacyLucideWifi {
		t.Error("the scan ran past a current sentinel")
	}
}

func TestMigrateUnparseableKeepsOriginalAndSentinelUnwritten(t *testing.T) {
	dir := t.TempDir()
	broken := "<svg viewBox='0 0 16 16'><!-- gpa:stroke=foreground --></svg>"
	target := writeIcon(t, dir, "ld-broken-symbolic.svg", broken)

	if report := Migrate(dir); report.Failed != 1 {
		t.Errorf("report = %+v, want 1 failed", report)
	}
	if _, ok := sentinel(t, dir); ok {
		t.Error("sentinel written despite a failure: the icon would never be retried")
	}
	if got := readFile(t, target); got != broken {
		t.Errorf("original replaced: %q", got)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeIcon(t, dir, "ld-wifi-symbolic.svg", legacyLucideWifi)
	if first := Migrate(dir); first.Migrated != 1 {
		t.Fatalf("first run = %+v, want 1 migrated", first)
	}
	if second := Migrate(dir); second != (MigrationReport{}) {
		t.Errorf("second run = %+v, want the sentinel to skip the scan", second)
	}
}

func TestMigrateSweepsOrphansAndLeavesNone(t *testing.T) {
	dir := t.TempDir()
	orphan := writeIcon(t, dir, ".ld-x-symbolic.svg.tmp.1.0", "partial")
	probe := writeIcon(t, dir, ".write-probe.1.0", "")
	writeIcon(t, dir, "ld-wifi-symbolic.svg", legacyLucideWifi)

	Migrate(dir)

	for _, p := range []string{orphan, probe} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s survived the sweep", filepath.Base(p))
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), tempNameInfix) || strings.Contains(e.Name(), writeProbeInfix) {
			t.Errorf("left behind %s", e.Name())
		}
	}
}

func TestMigrateSkipsMissingAndUnwritableDirs(t *testing.T) {
	if report := Migrate(filepath.Join(t.TempDir(), "absent")); report != (MigrationReport{}) {
		t.Errorf("missing dir: %+v", report)
	}
	dir := t.TempDir()
	legacy := writeIcon(t, dir, "ld-wifi-symbolic.svg", legacyLucideWifi)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if report := Migrate(dir); report != (MigrationReport{}) {
		t.Errorf("read-only dir: %+v, want a no-op", report)
	}
	if got := readFile(t, legacy); got != legacyLucideWifi {
		t.Error("a read-only dir was migrated")
	}
}

func TestNeedsMigration(t *testing.T) {
	current, _ := ToSymbolic(legacyLucideWifi)
	for content, want := range map[string]bool{
		legacyLucideWifi: true,
		current:          false,
		"<svg xmlns:gpa='https://www.gtk.org/grappa' gpa:version='3'/>":  false,
		"<svg xmlns:gpa='https://www.gtk.org/grappa' gpa:version='+2'/>": false, // u32 parse takes a +
		"<svg xmlns:gpa='https://www.gtk.org/grappa' gpa:version='x'/>":  true,
		"<svg/>": true, // unversioned
	} {
		if got := needsMigration(content); got != want {
			t.Errorf("needsMigration(%q) = %v, want %v", content, got, want)
		}
	}
}

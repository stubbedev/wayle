package icons

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// This file ports migrate.rs: rewrite icons written in an older format
// (Grappa stroke attributes, or an older gpa:version) in place, then
// drop a sentinel so later runs skip the scan.

const (
	sentinelFilename   = ".format-version"
	legacyStrokeMarker = "gpa:stroke="
	versionAttrPrefix  = "gpa:version='"
	tempNameInfix      = ".svg.tmp."
	writeProbeInfix    = ".write-probe."
)

// MigrationReport counts one pass.
type MigrationReport struct {
	Migrated, Skipped, Failed int
}

// Migrate rewrites every stale SVG in dir. It is a no-op when dir is
// missing or unwritable or the sentinel is current; per-file failures
// are logged, counted, and keep the sentinel from being written.
func Migrate(dir string) MigrationReport {
	if !shouldMigrate(dir) {
		return MigrationReport{}
	}
	sweepOrphanTempfiles(dir)
	report := scanAndMigrate(dir)
	if d, err := os.Open(dir); err == nil { //nolint:gosec // the icons directory
		_ = d.Sync()
		_ = d.Close()
	}
	if report.Failed == 0 {
		if err := writeAtomically(filepath.Join(dir, sentinelFilename), fmt.Appendf(nil, "%d\n", FormatVersion)); err != nil {
			log.Printf("icons: cannot write migration sentinel; will retry next launch: %v", err)
		}
	}
	return report
}

func shouldMigrate(dir string) bool {
	return exists(dir) && !upToDate(dir) && isWritable(dir)
}

func upToDate(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, sentinelFilename)) //nolint:gosec // inside the icons directory
	if err != nil {
		return false
	}
	v, err := parseRustU32(strings.TrimSpace(string(data)))
	return err == nil && v >= FormatVersion
}

var nonce atomic.Uint64

func isWritable(dir string) bool {
	probe := filepath.Join(dir, fmt.Sprintf("%s%d.%d", writeProbeInfix, os.Getpid(), nonce.Add(1)-1))
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm) //nolint:gosec // inside the icons directory
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}

func sweepOrphanTempfiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.Contains(name, tempNameInfix) && !strings.Contains(name, writeProbeInfix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			log.Printf("icons: cannot remove orphan tempfile %s: %v", name, err)
		}
	}
}

func scanAndMigrate(dir string) MigrationReport {
	var report MigrationReport
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("icons: cannot read icons directory: %v", err)
		report.Failed = 1
		return report
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".svg" || e.Name() == ".svg" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := readUTF8(path)
		if err != nil {
			log.Printf("icons: cannot read %s; skipping: %v", path, err)
			report.Failed++
			continue
		}
		if !needsMigration(content) {
			report.Skipped++
			continue
		}
		transformed, ok := ToSymbolic(content)
		if !ok {
			log.Printf("icons: %s produced no extractable paths; original kept untouched", path)
			report.Failed++
			continue
		}
		if err := writeAtomically(path, []byte(transformed)); err != nil {
			log.Printf("icons: cannot write migrated icon %s: %v", path, err)
			report.Failed++
			continue
		}
		report.Migrated++
	}
	return report
}

func needsMigration(content string) bool {
	if strings.Contains(content, legacyStrokeMarker) {
		return true
	}
	v, ok := fileFormatVersion(content)
	return !ok || v < FormatVersion
}

func fileFormatVersion(content string) (uint64, bool) {
	_, after, ok := strings.Cut(content, versionAttrPrefix)
	if !ok {
		return 0, false
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, "'")
	if !ok0 {
		return 0, false
	}
	v, err := parseRustU32(before0)
	return v, err == nil
}

// parseRustU32 is str::parse::<u32>: ASCII digits with an optional '+'.
func parseRustU32(s string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(s, "+"), 10, 32)
}

func writeAtomically(target string, data []byte) error {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s%s%d.%d", base, tempNameInfix, os.Getpid(), nonce.Add(1)-1))
	err := func() error {
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm) //nolint:gosec // inside the icons directory
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		return os.Rename(tmp, target)
	}()
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

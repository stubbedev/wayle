package icons

import (
	"context"
	"maps"
	"slices"
	"strings"
)

// This file ports sync.rs: find every icon name a config references and
// install the ones missing on disk.

// MissingIcon is a referenced icon not on disk.
type MissingIcon struct {
	Name string
	// Source is the CLI name of the source to fetch from; empty for a
	// user-imported cm- icon, which cannot be fetched.
	Source string
	Slug   string
}

// SyncFailure is one icon that errored, as "<source>/<slug>".
type SyncFailure struct {
	Name  string
	Error string
}

// SyncSummary is a sync outcome.
type SyncSummary struct {
	Installed []string
	Failed    []SyncFailure
	Skipped   []string
}

// ScanStrings adds every icon name found in text to into: text is split
// on characters that cannot be in an icon name and each chunk shaped
// like <known-prefix>-<slug>-symbolic is kept (scan_string).
func ScanStrings(text string, into map[string]bool) {
	chunks := strings.FieldsFunc(text, func(r rune) bool { return !isIconNameChar(r) })
	for _, chunk := range chunks {
		if _, ok := classify(chunk); ok {
			into[chunk] = true
		}
	}
}

// ScanValue walks a config value in the parsed shape (config.Service
// Value: map[string]any tables, []any arrays) and scans every string
// leaf (walk_value).
func ScanValue(v any, into map[string]bool) {
	switch t := v.(type) {
	case string:
		ScanStrings(t, into)
	case []any:
		for _, item := range t {
			ScanValue(item, into)
		}
	case map[string]any:
		for _, child := range t {
			ScanValue(child, into)
		}
	}
}

func isIconNameChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

// classify is MissingIcon::from_name: <prefix>-<slug>-symbolic with a
// non-empty slug and a known prefix; Source is empty for the custom
// prefix, whose icons only exist where they were imported.
func classify(name string) (MissingIcon, bool) {
	stripped, ok := strings.CutSuffix(name, "-symbolic")
	if !ok {
		return MissingIcon{}, false
	}
	prefix, slug, ok := strings.Cut(stripped, "-")
	if !ok || slug == "" {
		return MissingIcon{}, false
	}
	icon := MissingIcon{Name: name, Slug: slug}
	for _, s := range Sources {
		if s.Prefix == prefix {
			icon.Source = s.CLIName
			return icon, true
		}
	}
	return icon, prefix == CustomPrefix
}

// FindMissing is find_missing: the referenced names not installed that
// classify, in name order.
func FindMissing(referenced, installed map[string]bool) []MissingIcon {
	var out []MissingIcon
	for _, name := range slices.Sorted(maps.Keys(referenced)) {
		if installed[name] {
			continue
		}
		if icon, ok := classify(name); ok {
			out = append(out, icon)
		}
	}
	return out
}

// InstallMissing installs every fetchable icon, one batch per source in
// source-name order; cm- icons are skipped.
func InstallMissing(ctx context.Context, missing []MissingIcon, m *Manager) SyncSummary {
	var summary SyncSummary
	bySource := map[string][]string{}
	for _, icon := range missing {
		if icon.Source == "" {
			summary.Skipped = append(summary.Skipped, icon.Name)
			continue
		}
		bySource[icon.Source] = append(bySource[icon.Source], icon.Slug)
	}
	for _, name := range slices.Sorted(maps.Keys(bySource)) {
		slugs := bySource[name]
		fail := func(msg string) {
			for _, slug := range slugs {
				summary.Failed = append(summary.Failed, SyncFailure{Name: name + "/" + slug, Error: msg})
			}
		}
		source, err := SourceByCLIName(name)
		if err != nil {
			fail(err.Error())
			continue
		}
		result, err := m.Install(ctx, source, slugs)
		if err != nil {
			fail(err.Error())
			continue
		}
		summary.Installed = append(summary.Installed, result.Installed...)
		for _, f := range result.Failed {
			summary.Failed = append(summary.Failed, SyncFailure{Name: name + "/" + f.Slug, Error: f.Error})
		}
	}
	return summary
}

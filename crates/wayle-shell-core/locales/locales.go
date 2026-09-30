// Package locales embeds wayle-shell-core's Fluent partials for the Go
// shell, so the Rust and Go shells read one set of FTL files. The
// generated per-crate copies (wayle-shell-core.ftl and friends) are
// embedded too but ignored: the i18n package concatenates the `_*.ftl`
// partials itself, as build.rs does.
package locales

import "embed"

// FS holds every locale directory (en-US, fr, ...).
//
//go:embed all:*
var FS embed.FS

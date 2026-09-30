// Package locales embeds wayle-i18n's Fluent partials (the settings and
// config strings) for the Go port. The build.rs-generated wayle-i18n.ftl
// is ignored: the i18n package concatenates the `_*.ftl` partials
// itself.
package locales

import "embed"

// FS holds every locale directory (en-US, fr, ...).
//
//go:embed all:*
var FS embed.FS

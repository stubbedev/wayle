// Package locales embeds wayle-greeter's Fluent files for the Go
// greeter, so both read one set of FTL files.
package locales

import "embed"

// FS holds every locale directory (en-US, fr, ...).
//
//go:embed all:*
var FS embed.FS

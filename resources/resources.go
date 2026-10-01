// Package resources embeds the repository's bundled assets for the Go
// binary: the symbolic icons `wayle icons setup` installs (the Rust CLI
// reads the same directory through a compile-time path).
package resources

import "embed"

// Icons holds icons/hicolor/scalable/actions/*.svg.
//
//go:embed icons/hicolor/scalable/actions/*.svg
var Icons embed.FS

// IconsDir is the directory inside Icons.
const IconsDir = "icons/hicolor/scalable/actions"

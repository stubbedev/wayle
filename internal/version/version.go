// Package version carries the wayle release version the Go binary
// reports; it tracks the workspace version in Cargo.toml while both
// trees ship, and version_test.go holds the two together.
package version

// Version is the wayle release version (Cargo.toml workspace.package).
const Version = "0.8.53"

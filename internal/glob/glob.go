// Package glob is the blocklist matcher: case-insensitive `*`
// wildcards, shared by the notification blocklist and the systray
// blacklist (wayle-core's glob.rs).
package glob

import "strings"

// Match reports whether name matches the wildcard pattern.
func Match(pattern, name string) bool {
	pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		idx := strings.Index(name[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	// The last segment must reach the end unless the pattern ends in *.
	if last := parts[len(parts)-1]; last != "" && !strings.HasSuffix(pattern, "*") {
		return strings.HasSuffix(name, last)
	}
	return true
}

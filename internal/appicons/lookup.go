package appicons

import (
	"strings"

	"github.com/stubbedev/wayle/internal/glob"
)

// Lookup is icons.rs lookup_app_icon: the first Default pattern that
// matches the lowercased name (an exact match of the lowercased name
// counts too, as matches_glob's shortcut does). Callers pass app ids,
// window classes, or player identities alike.
func Lookup(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, entry := range Default {
		if lower == entry.Pattern || glob.Wildcard(entry.Pattern, lower) {
			return entry.Icon, true
		}
	}
	return "", false
}

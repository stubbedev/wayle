package appicons

import (
	"sort"
	"strings"

	"github.com/stubbedev/wayle/internal/glob"
)

// The user app-icon-map prefixes: title: matches the window title,
// app: (or no prefix) the app id.
const (
	titlePrefix = "title:"
	appPrefix   = "app:"
)

// Window is what the resolver reads off a compositor window. The Has
// flags distinguish an unset field from an empty one, as the Rust
// Option does.
type Window struct {
	AppID    string
	Title    string
	HasAppID bool
	HasTitle bool
}

// Resolve is the sway/niri/mango helpers.rs resolve_app_icon:
// title-prefixed user patterns against the title first, then app- or
// un-prefixed user patterns against the app id, then the Default
// table, then fallback. User patterns are tried in sorted key order
// (the BTreeMap order). Matching is glob.Wildcard: case-sensitive.
func Resolve(w Window, userMap map[string]string, fallback string) string {
	keys := make([]string, 0, len(userMap))
	for key := range userMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var titleKeys, appKeys []string
	for _, key := range keys {
		if strings.HasPrefix(key, titlePrefix) {
			titleKeys = append(titleKeys, key)
		} else {
			appKeys = append(appKeys, key)
		}
	}
	if w.HasTitle {
		if icon, ok := matchPrefixed(titleKeys, userMap, titlePrefix, w.Title); ok {
			return icon
		}
	}
	if !w.HasAppID {
		return fallback
	}
	if icon, ok := matchPrefixed(appKeys, userMap, appPrefix, w.AppID); ok {
		return icon
	}
	for _, entry := range Default {
		if glob.Wildcard(entry.Pattern, w.AppID) {
			return entry.Icon
		}
	}
	return fallback
}

// matchPrefixed strips prefix from each pattern (when present) and
// returns the first match's icon.
func matchPrefixed(keys []string, userMap map[string]string, prefix, query string) (string, bool) {
	for _, key := range keys {
		if glob.Wildcard(strings.TrimPrefix(key, prefix), query) {
			return userMap[key], true
		}
	}
	return "", false
}

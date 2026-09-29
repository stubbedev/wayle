// Package bar renders wayle's bar: the layer surfaces docked to the
// screen edges, one per output that the config gives a layout.
package bar

import (
	"github.com/stubbedev/wayle/config"
)

// FindLayout resolves the layout for one output's connector name: the
// exact monitor match wins, then "*" as the fallback, and extends
// chains inherit the unset sections of their parent layout. A monitor
// with neither an exact entry nor a "*" entry gets no bar.
func FindLayout(layouts []config.BarLayout, connector string) (config.BarLayout, bool) {
	if layout, ok := findByMonitor(layouts, connector); ok {
		return mergeParent(layout, layouts, map[string]bool{}), true
	}
	if layout, ok := findByMonitor(layouts, "*"); ok {
		return mergeParent(layout, layouts, map[string]bool{}), true
	}
	return config.BarLayout{}, false
}

func findByMonitor(layouts []config.BarLayout, monitor string) (config.BarLayout, bool) {
	for _, candidate := range layouts {
		if candidate.Monitor == monitor {
			return candidate, true
		}
	}
	return config.BarLayout{}, false
}

// mergeParent walks the extends chain, filling the sections the child
// left empty from the first ancestor that has them. A cycle or a
// missing parent stops the walk and keeps what was resolved so far —
// matching the Rust shell's warn-and-skip behavior.
func mergeParent(layout config.BarLayout, all []config.BarLayout, visited map[string]bool) config.BarLayout {
	resolved := layout
	if resolved.Extends == "" {
		return resolved
	}
	if visited[resolved.Extends] {
		return resolved
	}
	visited[resolved.Extends] = true
	parent, ok := findByMonitor(all, resolved.Extends)
	if !ok {
		return resolved
	}
	parent = mergeParent(parent, all, visited)
	if len(resolved.Left) == 0 {
		resolved.Left = parent.Left
	}
	if len(resolved.Center) == 0 {
		resolved.Center = parent.Center
	}
	if len(resolved.Right) == 0 {
		resolved.Right = parent.Right
	}
	return resolved
}

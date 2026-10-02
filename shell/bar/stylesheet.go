package bar

import "github.com/stubbedev/wayle/config"

// locationClass is Location::css_class: the root's location class.
func locationClass(location config.Location) string { return string(location) }

// rootClasses are apply_css_classes (bar/methods.rs): bar, the
// connector, the location, and floating when an inset pulls the bar
// off its edges.
func rootClasses(connector string, cfg *config.Config) []string {
	classes := []string{"bar", locationClass(cfg.Bar.Location)}
	if connector != "" {
		classes = append(classes, connector)
	}
	if !cfg.Bar.InsetEdge.IsZero() || !cfg.Bar.InsetEnds.IsZero() {
		classes = append(classes, "floating")
	}
	return classes
}

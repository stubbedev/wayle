package bar

import (
	"fmt"
	"strings"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/styling"
)

// barStylesheet renders the bar's rules: the port of the Rust bar's
// build_css, with token values resolved to concrete colors instead of
// CSS variables.
func barStylesheet(style barStyle) string {
	var b strings.Builder
	fmt.Fprintf(&b, ".bar {\n  background-color: %s;\n  border-radius: %dpx;\n}\n",
		styling.HexRGBA(style.bg), style.radius)
	fmt.Fprintf(&b, ".bar-group {\n  background-color: %s;\n  border-radius: %dpx;\n  padding: %dpx;\n}\n",
		styling.HexRGBA(style.groupBg), style.groupRadius, style.groupPadding)
	return b.String()
}

// loadBarStylesheet installs the generated stylesheet.
func loadBarStylesheet(style barStyle) {
	widget.LoadStylesheet(barStylesheet(style))
}

// locationClass is Location::css_class: the root's location class.
func locationClass(location config.Location) string { return string(location) }

// addRootClasses tags the bar root the way apply_css_classes does: the
// connector, the location, and floating when the insets pull the bar
// off its edges.
func addRootClasses(w interface{ AddClass(...string) }, connector string, cfg *config.Config) {
	classes := []string{"bar", locationClass(cfg.Bar.Location)}
	if connector != "" {
		classes = append(classes, connector)
	}
	if !cfg.Bar.InsetEdge.IsZero() || !cfg.Bar.InsetEnds.IsZero() {
		classes = append(classes, "floating")
	}
	w.AddClass(classes...)
}

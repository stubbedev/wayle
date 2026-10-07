//go:build atspi

package bar

import (
	"log"

	"github.com/stubbedev/gelm/app"
)

// a11yStarter is the application's bridge entry point; *app.Application
// satisfies it in tagged builds, and tests substitute a fake.
type a11yStarter interface {
	ServeAccessibility(app.A11YOptions) error
}

// serveA11y starts gelm's AT-SPI bridge on the application, so builds
// with the `atspi` tag expose the widget tree to screen readers the way
// the GTK shell's bridge does. A missing accessibility bus (no
// org.a11y.Bus on the session bus) is a notice, not a failure: the
// shell keeps running without the tree.
func serveA11y(application a11yStarter) {
	if err := application.ServeAccessibility(app.A11YOptions{}); err != nil {
		log.Printf("bar: a11y: %v", err)
	}
}

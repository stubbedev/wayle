//go:build !atspi

package bar

import "github.com/stubbedev/gelm/app"

// serveA11y is a no-op without the `atspi` tag: default builds carry
// none of the accessibility bridge, as gelm's default builds do.
func serveA11y(*app.Application) {}

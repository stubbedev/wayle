package bar

import "testing"

// TestServeA11yDefaultBuildIsNoOp pins the untagged half of the a11y
// seam: without the `atspi` build tag the call site in run() must
// compile against the same signature and do nothing, so default
// builds start no bridge and never touch the session bus for it.
func TestServeA11yDefaultBuildIsNoOp(t *testing.T) {
	serveA11y(nil)
}

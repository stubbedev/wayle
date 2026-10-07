//go:build atspi

package bar

import (
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/stubbedev/gelm/app"
)

// a11y_starter is defined in a11y.go (tagged builds only).

// TestServeA11yStartsTheBridgeAndSurvivesItsFailure pins the tagged
// half: the shell attempts the bridge with default options, a missing
// accessibility bus is a logged notice it survives — never a startup
// failure.
func TestServeA11yStartsTheBridgeAndSurvivesItsFailure(t *testing.T) {
	fake := &fakeStarter{}
	serveA11y(fake)
	if !fake.called {
		t.Fatal("the bridge start was not attempted")
	}
	if fake.opts != (app.A11YOptions{}) {
		t.Fatalf("bridge options = %+v, want the zero value", fake.opts)
	}

	var out strings.Builder
	prev := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(prev)
	serveA11y(&fakeStarter{err: errors.New("no org.a11y.Bus on this bus")})
	if !strings.Contains(out.String(), "bar: a11y:") {
		t.Fatalf("a failed bridge start must log, got %q", out.String())
	}
}

type fakeStarter struct {
	called bool
	opts   app.A11YOptions
	err    error
}

func (f *fakeStarter) ServeAccessibility(opts app.A11YOptions) error {
	f.called = true
	f.opts = opts
	return f.err
}

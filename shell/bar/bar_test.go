package bar

import (
	"testing"

	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
)

func TestAnchorsForEachLocation(t *testing.T) {
	for _, tc := range []struct {
		location config.Location
		want     app.Anchor
	}{
		{config.LocationTop, app.AnchorTop | app.AnchorLeft | app.AnchorRight},
		{config.LocationBottom, app.AnchorBottom | app.AnchorLeft | app.AnchorRight},
		{config.LocationLeft, app.AnchorLeft | app.AnchorTop | app.AnchorBottom},
		{config.LocationRight, app.AnchorRight | app.AnchorTop | app.AnchorBottom},
	} {
		if got := AnchorsFor(tc.location); got != tc.want {
			t.Errorf("AnchorsFor(%q) = %d, want %d", tc.location, got, tc.want)
		}
	}
}

func TestAnchorsForUnknownIsZero(t *testing.T) {
	if got := AnchorsFor("sideways"); got != 0 {
		t.Errorf("AnchorsFor(sideways) = %d, want 0 (validation rejects this at load)", got)
	}
}

func TestExclusiveZoneReservesBarHeightOnlyWhenSet(t *testing.T) {
	if got := exclusiveZone(true, 32); got != 32 {
		t.Errorf("exclusiveZone(true, 32) = %d, want 32", got)
	}
	if got := exclusiveZone(false, 32); got != 0 {
		t.Errorf("exclusiveZone(false, 32) = %d, want 0", got)
	}
}

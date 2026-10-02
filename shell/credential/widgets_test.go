package credential

import (
	"testing"

	"github.com/stubbedev/gelm/widget"
)

func TestFixedSetSizeRepinsAndInvalidates(t *testing.T) {
	f := NewFixed(NewSpacer(10, 10), 90, 0)
	con := widget.Constraints{Max: widget.Size{W: 500, H: 500}}
	if got := f.Measure(con); got.W != 90 || got.H != 10 {
		t.Fatalf("pinned width = %+v, want 90 x natural 10", got)
	}
	f.SetSize(120, 30)
	if w, h := f.Size(); w != 120 || h != 30 {
		t.Errorf("Size = %d x %d after SetSize", w, h)
	}
	if got := f.Measure(con); got.W != 120 || got.H != 30 {
		t.Errorf("re-pinned measure = %+v, want 120 x 30 (the cache must not answer)", got)
	}
	// Re-pinning to the same size is not a change.
	f.SetSize(120, 30)
	if got := f.Measure(con); got.W != 120 {
		t.Errorf("same-size SetSize measure = %+v", got)
	}
}

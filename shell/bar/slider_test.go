package bar

import (
	"slices"
	"testing"
	"time"
)

func newTestSlider(t *testing.T) (*debouncedSlider, *[]float64, *time.Time) {
	t.Helper()
	clock := time.Unix(1000, 0)
	commits := &[]float64{}
	// The headless Invoke stands in for the loop, as in a module.
	d := newDebouncedSlider(0, 100, 50, testFont(t), 12, 0, ModuleContext{}.Invoke)
	d.now = func() time.Time { return clock }
	d.onCommit = func(v float64) { *commits = append(*commits, v) }
	return d, commits, &clock
}

// The first move commits at once; moves inside 100ms leave one trailing
// commit of the last value; the release commits the final value.
func TestDebouncedSliderThrottlesAndCommitsOnRelease(t *testing.T) {
	d, commits, clock := newTestSlider(t)
	d.knob.SetPressed(true)
	d.knob.SetValue(60)
	d.knob.SetValue(61)
	d.knob.SetValue(62)
	if !slices.Equal(*commits, []float64{60}) {
		t.Fatalf("commits = %v, want only the first move", *commits)
	}
	if d.trailing == nil {
		t.Fatal("no trailing commit scheduled")
	}
	*clock = clock.Add(150 * time.Millisecond)
	d.knob.SetValue(70)
	if !slices.Equal(*commits, []float64{60, 70}) || d.trailing != nil {
		t.Fatalf("after the throttle window commits = %v (trailing %v)", *commits, d.trailing != nil)
	}
	d.knob.SetPressed(false)
	if !slices.Equal(*commits, []float64{60, 70, 70}) {
		t.Fatalf("release commits = %v", *commits)
	}
	if got := d.label.Text(); got != "70%" {
		t.Errorf("label = %q", got)
	}
}

func TestDebouncedSliderTrailingCommitLands(t *testing.T) {
	d, commits, _ := newTestSlider(t)
	d.knob.SetValue(10)
	d.knob.SetValue(20)
	waitHeadless(t, "the trailing commit", func() bool { return len(*commits) == 2 })
	if (*commits)[1] != 20 {
		t.Errorf("trailing = %v, want the last value", *commits)
	}
}

// External values are ignored while held and for 150ms after release,
// and never commit.
func TestDebouncedSliderIgnoresExternalValuesWhileInteracting(t *testing.T) {
	d, commits, clock := newTestSlider(t)
	d.set(30)
	if d.value() != 30 || len(*commits) != 0 {
		t.Fatalf("external set: value %v commits %v", d.value(), *commits)
	}
	d.knob.SetPressed(true)
	d.set(80)
	if d.value() != 30 {
		t.Error("an external value moved a held slider")
	}
	d.knob.SetPressed(false)
	*clock = clock.Add(100 * time.Millisecond)
	d.set(80)
	if d.value() != 30 {
		t.Error("an external value landed inside the release grace")
	}
	*clock = clock.Add(100 * time.Millisecond)
	d.set(80)
	if d.value() != 80 {
		t.Error("an external value after the grace did not land")
	}
}

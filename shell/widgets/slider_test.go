package widgets

import (
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/gelm/render"
	"golang.org/x/image/font/gofont/goregular"
)

// testLoop stands in for the application loop: Invoke queues, and the
// loop goroutine runs each function under the lock the test reads with.
type testLoop struct {
	mu sync.Mutex
}

func (l *testLoop) invoke(fn func()) {
	go func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		fn()
	}()
}

// waitLoop polls cond under the loop lock for a second.
func (l *testLoop) wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		l.mu.Lock()
		ok := cond()
		l.mu.Unlock()
		if ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func testFont(t *testing.T) render.Font {
	t.Helper()
	face, err := render.LoadFont(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func newTestSlider(t *testing.T) (*DebouncedSlider, *[]float64, *time.Time) {
	t.Helper()
	return newTestSliderOn(t, &testLoop{})
}

func newTestSliderOn(t *testing.T, loop *testLoop) (*DebouncedSlider, *[]float64, *time.Time) {
	t.Helper()
	clock := time.Unix(1000, 0)
	commits := &[]float64{}
	d := NewDebouncedSlider(50, testFont(t), 12, 0, loop.invoke)
	d.now = func() time.Time { return clock }
	d.OnCommit = func(v float64) { *commits = append(*commits, v) }
	return d, commits, &clock
}

// The first move commits at once; moves inside 100ms leave one trailing
// commit of the last value; the release commits the final value.
func TestDebouncedSliderThrottlesAndCommitsOnRelease(t *testing.T) {
	d, commits, clock := newTestSlider(t)
	d.Knob.SetPressed(true)
	d.Knob.SetValue(60)
	d.Knob.SetValue(61)
	d.Knob.SetValue(62)
	if !slices.Equal(*commits, []float64{60}) {
		t.Fatalf("commits = %v, want only the first move", *commits)
	}
	if d.trailing == nil {
		t.Fatal("no trailing commit scheduled")
	}
	*clock = clock.Add(150 * time.Millisecond)
	d.Knob.SetValue(70)
	if !slices.Equal(*commits, []float64{60, 70}) || d.trailing != nil {
		t.Fatalf("after the throttle window commits = %v (trailing %v)", *commits, d.trailing != nil)
	}
	d.Knob.SetPressed(false)
	if !slices.Equal(*commits, []float64{60, 70, 70}) {
		t.Fatalf("release commits = %v", *commits)
	}
	if got := d.label.Text(); got != "70%" {
		t.Errorf("label = %q", got)
	}
}

func TestDebouncedSliderTrailingCommitLands(t *testing.T) {
	loop := &testLoop{}
	d, commits, _ := newTestSliderOn(t, loop)
	loop.mu.Lock()
	d.Knob.SetValue(10)
	d.Knob.SetValue(20)
	loop.mu.Unlock()
	loop.wait(t, "the trailing commit", func() bool { return len(*commits) == 2 })
	if (*commits)[1] != 20 {
		t.Errorf("trailing = %v, want the last value", *commits)
	}
}

// External values are ignored while held and for 150ms after release,
// and never commit.
func TestDebouncedSliderIgnoresExternalValuesWhileInteracting(t *testing.T) {
	d, commits, clock := newTestSlider(t)
	d.Set(30)
	if d.Value() != 30 || len(*commits) != 0 {
		t.Fatalf("external set: value %v commits %v", d.Value(), *commits)
	}
	d.Knob.SetPressed(true)
	d.Set(80)
	if d.Value() != 30 {
		t.Error("an external value moved a held slider")
	}
	d.Knob.SetPressed(false)
	*clock = clock.Add(100 * time.Millisecond)
	d.Set(80)
	if d.Value() != 30 {
		t.Error("an external value landed inside the release grace")
	}
	*clock = clock.Add(100 * time.Millisecond)
	d.Set(80)
	if d.Value() != 80 {
		t.Error("an external value after the grace did not land")
	}
}

// A ranged slider spans its own bounds and labels with its format.
func TestRangedSliderBoundsAndFormat(t *testing.T) {
	d := NewRangedSlider(500, 5000, 900, testFont(t), 12, 0, (&testLoop{}).invoke)
	d.SetFormat(func(v float64) string { return strconv.FormatFloat(v/1000, 'f', 1, 64) + "s" })
	if got := d.Label().Text(); got != "0.9s" {
		t.Errorf("label = %q, want the new format", got)
	}
	d.Set(9000)
	if d.Value() != 5000 {
		t.Errorf("value = %v, want clamped to the max", d.Value())
	}
	if NewDebouncedSlider(0, nil, 0, 0, nil).Label() != nil {
		t.Error("a fontless slider has a label")
	}
}

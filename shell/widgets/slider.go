// Package widgets is wayle-widgets: the controls the shell surfaces and
// the settings app share.
package widgets

import (
	"strconv"
	"time"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// DebouncedSlider's cadences (wayle-widgets slider/debounced).
const (
	sliderThrottle = 100 * time.Millisecond
	sliderGrace    = 150 * time.Millisecond
)

// DebouncedSlider is wayle-widgets' DebouncedSlider: a slider with an optional value
// label that commits (backend writes) at most every 100ms while
// dragged, with a trailing commit of the last value and one on release,
// and ignores external values while held and for 150ms after.
type DebouncedSlider struct {
	*widget.Box
	// Knob is the slider itself (class it, read it).
	Knob  *pressSlider
	label *widget.Label
	// Format renders the value label (default "{:.0}%").
	Format func(float64) string
	invoke func(func())

	// OnCommit hears the throttled user commits.
	OnCommit func(float64)
	// OnValue sees every value change, the user's and set's alike
	// (the value-changed notify a #[watch] reads).
	OnValue      func(float64)
	programmatic bool
	dragging     bool
	dragEnded    time.Time
	lastCommit   time.Time
	trailing     *time.Timer
	now          func() time.Time
}

// pressSlider routes the press protocol to its owner.
type pressSlider struct {
	*widget.Slider
	owner *DebouncedSlider
}

// HitTest makes the wrapper the input leaf, so SetPressed reaches it.
func (s *pressSlider) HitTest(p widget.Point) widget.Widget { return s.HitLeaf(s, p) }

// SetPressed tracks the drag; the release commits the final value.
func (s *pressSlider) SetPressed(on bool) {
	was := s.Pressed
	s.Slider.SetPressed(on)
	d := s.owner
	switch {
	case on && !was:
		d.dragging, d.dragEnded = true, time.Time{}
	case !on && was:
		d.dragging, d.dragEnded = false, d.now()
		d.cancelTrailing()
		d.commit(s.Value())
	}
}

// NewDebouncedSlider builds a 0-100 slider (every wayle slider is a
// percentage); with a font the
// value label shows (format, default "{:.0}%"). invoke runs the
// trailing commit on the loop.
func NewDebouncedSlider(value float64, font render.Font, px float64, color render.Color, invoke func(func())) *DebouncedSlider {
	return NewRangedSlider(0, 100, value, font, px, color, invoke)
}

// NewRangedSlider is NewDebouncedSlider over [min, max].
func NewRangedSlider(min, max, value float64, font render.Font, px float64, color render.Color, invoke func(func())) *DebouncedSlider {
	d := &DebouncedSlider{Box: widget.NewBox(widget.Row, 8, 0), invoke: invoke, now: time.Now}
	d.Format = func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + "%" }
	d.Knob = &pressSlider{Slider: widget.NewSlider(min, max, 0, value), owner: d}
	d.Knob.OnChanged = d.changed
	d.Append(d.Knob, true)
	if font != nil {
		d.label = widget.NewLabel(font, px, d.Format(value), color)
		d.label.SetAlignment(render.AlignEnd)
		d.Append(d.label, false)
	}
	return d
}

// changed is the value_changed handler: user moves update the label
// and commit through the throttle; programmatic ones do not commit.
func (d *DebouncedSlider) changed(v float64) {
	if d.label != nil {
		d.label.SetText(d.Format(v))
	}
	if d.OnValue != nil {
		d.OnValue(v)
	}
	if d.programmatic {
		return
	}
	if d.lastCommit.IsZero() || d.now().Sub(d.lastCommit) >= sliderThrottle {
		d.cancelTrailing()
		d.commit(v)
		return
	}
	d.cancelTrailing()
	d.trailing = time.AfterFunc(sliderThrottle, func() {
		d.invoke(func() {
			d.trailing = nil
			d.commit(v)
		})
	})
}

func (d *DebouncedSlider) commit(v float64) {
	d.lastCommit = d.now()
	if d.OnCommit != nil {
		d.OnCommit(v)
	}
}

func (d *DebouncedSlider) cancelTrailing() {
	if d.trailing != nil {
		d.trailing.Stop()
		d.trailing = nil
	}
}

// interacting is user_is_interacting: held, or released under 150ms ago.
func (d *DebouncedSlider) interacting() bool {
	return d.dragging || !d.dragEnded.IsZero() && d.now().Sub(d.dragEnded) < sliderGrace
}

// Set is set_value_external: an outside value, ignored while the user
// interacts.
func (d *DebouncedSlider) Set(v float64) {
	if d.interacting() {
		return
	}
	d.programmatic = true
	d.Knob.SetValue(v)
	d.programmatic = false
}

// Value is the slider's current value.
func (d *DebouncedSlider) Value() float64 { return d.Knob.Value() }

// SetFormat replaces the value label's format and re-renders it.
func (d *DebouncedSlider) SetFormat(format func(float64) string) {
	d.Format = format
	if d.label != nil {
		d.label.SetText(format(d.Value()))
	}
}

// Label is the value label, nil without a font.
func (d *DebouncedSlider) Label() *widget.Label { return d.label }

// SetClock replaces the clock the throttle and the release grace read
// (a test seam).
func (d *DebouncedSlider) SetClock(now func() time.Time) { d.now = now }

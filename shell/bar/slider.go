package bar

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

// debouncedSlider is DebouncedSlider: a slider with an optional value
// label that commits (backend writes) at most every 100ms while
// dragged, with a trailing commit of the last value and one on release,
// and ignores external values while held and for 150ms after.
type debouncedSlider struct {
	*widget.Box
	knob   *pressSlider
	label  *widget.Label
	format func(float64) string
	invoke func(func())

	onCommit func(float64)
	// onValue sees every value change, the user's and set's alike
	// (the value-changed notify a #[watch] reads).
	onValue      func(float64)
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
	owner *debouncedSlider
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

// newDebouncedSlider builds a 0-100 slider (every wayle slider is a
// percentage); with a font the
// value label shows (format, default "{:.0}%"). invoke runs the
// trailing commit on the loop.
func newDebouncedSlider(value float64, font render.Font, px float64, color render.Color, invoke func(func())) *debouncedSlider {
	d := &debouncedSlider{Box: widget.NewBox(widget.Row, 8, 0), invoke: invoke, now: time.Now}
	d.format = func(v float64) string { return strconv.FormatFloat(v, 'f', 0, 64) + "%" }
	d.knob = &pressSlider{Slider: widget.NewSlider(0, 100, 0, value), owner: d}
	d.knob.OnChanged = d.changed
	d.Append(d.knob, true)
	if font != nil {
		d.label = widget.NewLabel(font, px, d.format(value), color)
		d.label.SetAlignment(render.AlignEnd)
		d.Append(d.label, false)
	}
	return d
}

// changed is the value_changed handler: user moves update the label
// and commit through the throttle; programmatic ones do not commit.
func (d *debouncedSlider) changed(v float64) {
	if d.label != nil {
		d.label.SetText(d.format(v))
	}
	if d.onValue != nil {
		d.onValue(v)
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

func (d *debouncedSlider) commit(v float64) {
	d.lastCommit = d.now()
	if d.onCommit != nil {
		d.onCommit(v)
	}
}

func (d *debouncedSlider) cancelTrailing() {
	if d.trailing != nil {
		d.trailing.Stop()
		d.trailing = nil
	}
}

// interacting is user_is_interacting: held, or released under 150ms ago.
func (d *debouncedSlider) interacting() bool {
	return d.dragging || !d.dragEnded.IsZero() && d.now().Sub(d.dragEnded) < sliderGrace
}

// set is set_value_external: an outside value, ignored while the user
// interacts.
func (d *debouncedSlider) set(v float64) {
	if d.interacting() {
		return
	}
	d.programmatic = true
	d.knob.SetValue(v)
	d.programmatic = false
}

// value is the slider's current value.
func (d *debouncedSlider) value() float64 { return d.knob.Value() }

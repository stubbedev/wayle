// Package reveal plays the [animations] config on gelm revealers: the
// transition style and duration each transient surface resolves for
// entering or exiting (the per-surface, global, base cascade), the job
// of the Rust shell's surface_anim helper.
package reveal

import (
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// Transition is the revealer transition for a config animation style.
func Transition(t config.AnimationType) widget.RevealTransition {
	switch t {
	case config.AnimationNone:
		return widget.RevealNone
	case config.AnimationSlideUp:
		return widget.RevealSlideUp
	case config.AnimationSlideDown:
		return widget.RevealSlideDown
	case config.AnimationSlideLeft:
		return widget.RevealSlideLeft
	case config.AnimationSlideRight:
		return widget.RevealSlideRight
	case config.AnimationSwingUp:
		return widget.RevealSwingUp
	case config.AnimationSwingDown:
		return widget.RevealSwingDown
	case config.AnimationSwingLeft:
		return widget.RevealSwingLeft
	case config.AnimationSwingRight:
		return widget.RevealSwingRight
	case config.AnimationBounce:
		return widget.RevealBounce
	case config.AnimationGenie:
		return widget.RevealGenie
	case config.AnimationZoom:
		return widget.RevealZoom
	case config.AnimationRotate:
		return widget.RevealRotate
	case config.AnimationFlip:
		return widget.RevealFlip
	}
	return widget.RevealFade
}

// Arm sets r up for surface s entering (exiting false) or exiting: the
// resolved transition and duration, disabled animations snapping.
func Arm(r *widget.Revealer, anims config.AnimationsConfig, s config.AnimSurface, exiting bool) {
	r.SetTransition(Transition(anims.TransitionFor(s, exiting)))
	r.SetDuration(time.Duration(anims.DurationFor(s, exiting)) * time.Millisecond)
}

// Show arms r for entering s and reveals it.
func Show(r *widget.Revealer, anims config.AnimationsConfig, s config.AnimSurface) {
	Arm(r, anims, s, false)
	r.SetRevealed(true)
}

// Hide arms r for exiting s and hides it; done runs once the exit has
// played (at once when animations are off or nothing shows), unless a
// Show interrupts it first.
func Hide(r *widget.Revealer, anims config.AnimationsConfig, s config.AnimSurface, done func()) {
	if !r.Revealed() && r.Progress() == 0 {
		r.SetOnTransitionDone(nil)
		if done != nil {
			done()
		}
		return
	}
	Arm(r, anims, s, true)
	r.SetOnTransitionDone(func(revealed bool) {
		r.SetOnTransitionDone(nil)
		if !revealed && done != nil {
			done()
		}
	})
	r.SetRevealed(false)
}

// GenieEdge is the edge a genie collapses toward for a surface anchored
// at the top (the popup position's rule: top positions collapse up,
// everything else down).
func GenieEdge(top bool) widget.Edge {
	if top {
		return widget.EdgeTop
	}
	return widget.EdgeBottom
}

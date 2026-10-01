package reveal

import (
	"testing"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

func TestTransitionCoversEveryStyle(t *testing.T) {
	for style, want := range map[config.AnimationType]widget.RevealTransition{
		config.AnimationNone: widget.RevealNone, config.AnimationFade: widget.RevealFade,
		config.AnimationSlideUp: widget.RevealSlideUp, config.AnimationSlideDown: widget.RevealSlideDown,
		config.AnimationSlideLeft: widget.RevealSlideLeft, config.AnimationSlideRight: widget.RevealSlideRight,
		config.AnimationSwingUp: widget.RevealSwingUp, config.AnimationSwingDown: widget.RevealSwingDown,
		config.AnimationSwingLeft: widget.RevealSwingLeft, config.AnimationSwingRight: widget.RevealSwingRight,
		config.AnimationBounce: widget.RevealBounce, config.AnimationGenie: widget.RevealGenie,
		config.AnimationZoom: widget.RevealZoom, config.AnimationRotate: widget.RevealRotate,
		config.AnimationFlip: widget.RevealFlip,
	} {
		if got := Transition(style); got != want {
			t.Errorf("%s: %v, want %v", style, got, want)
		}
	}
}

func TestArmResolvesTheCascade(t *testing.T) {
	anims := config.DefaultsAnimations()
	exit := config.AnimationZoom
	anims.Osd.Exit = &exit
	r := widget.NewRevealer(nil)
	Arm(r, anims, config.AnimOsd, true)
	if r.Transition() != widget.RevealZoom {
		t.Errorf("osd exit = %v, want its own zoom", r.Transition())
	}
	Arm(r, anims, config.AnimOsd, false)
	if r.Transition() != widget.RevealFade {
		t.Errorf("osd enter = %v, want the base fade", r.Transition())
	}
	anims.Enabled = false
	Arm(r, anims, config.AnimOsd, true)
	if r.Transition() != widget.RevealNone {
		t.Errorf("disabled = %v, want none", r.Transition())
	}
}

func TestHide(t *testing.T) {
	anims := config.DefaultsAnimations()

	// Nothing showing: done at once.
	done := 0
	Hide(widget.NewRevealer(nil), anims, config.AnimOsd, func() { done++ })
	if done != 1 {
		t.Errorf("hiding a hidden revealer: %d dones, want 1", done)
	}

	// Showing: done once the exit lands.
	r := widget.NewRevealer(nil)
	Show(r, anims, config.AnimOsd)
	r.Finish()
	done = 0
	Hide(r, anims, config.AnimOsd, func() { done++ })
	if done != 0 {
		t.Fatal("done before the exit played")
	}
	r.Finish()
	if done != 1 {
		t.Errorf("after the exit: %d dones, want 1", done)
	}

	// A Show interrupting the exit cancels done.
	done = 0
	Show(r, anims, config.AnimOsd)
	r.Finish()
	Hide(r, anims, config.AnimOsd, func() { done++ })
	Show(r, anims, config.AnimOsd)
	r.Finish()
	if done != 0 {
		t.Errorf("an interrupted exit ran done %d times", done)
	}
	Hide(r, anims, config.AnimOsd, nil) // a nil done is allowed
	r.Finish()
}

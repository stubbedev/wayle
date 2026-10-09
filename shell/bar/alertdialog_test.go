package bar

import (
	"testing"

	"github.com/stubbedev/wayle/config"
)

// fakeAlert records the alerts a view asks and holds their answers.
type fakeAlert struct {
	specs   []alertSpec
	answers []func(bool)
}

func (f *fakeAlert) ask(spec alertSpec, answer func(bool)) {
	f.specs = append(f.specs, spec)
	f.answers = append(f.answers, answer)
}

// The alert is gtk::AlertDialog's shape: modal, [cancel, accept], and
// cancel both the default and the cancel button - Enter and Esc never
// accept.
func TestAlertDialogIsModalAndDefaultsToCancel(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	var got []bool
	cfg := alertDialog(ctx, alertSpec{message: "Delete Corp?", detail: "gone for good", cancel: "Cancel", accept: "Delete"},
		func(ok bool) { got = append(got, ok) })
	if !cfg.Modal {
		t.Error("the alert is not modal")
	}
	if len(cfg.Buttons) != 2 || cfg.Buttons[0].Label != "Cancel" || cfg.Buttons[1].Label != "Delete" {
		t.Fatalf("buttons = %+v, want cancel then accept", cfg.Buttons)
	}
	if cfg.DefaultResponse != cfg.Buttons[0].Response || cfg.CancelResponse != cfg.Buttons[0].Response {
		t.Errorf("default %q cancel %q, want both the cancel button", cfg.DefaultResponse, cfg.CancelResponse)
	}
	cfg.OnResponse(cfg.Buttons[0].Response)
	cfg.OnResponse(cfg.Buttons[1].Response)
	cfg.OnResponse("closed")
	if len(got) != 3 || got[0] || !got[1] || got[2] {
		t.Errorf("answers = %v, want only accept to go ahead", got)
	}
}

// Without an application nothing can ask, so nothing is confirmed.
func TestShowAlertHeadlessDeclines(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	answered, accepted := false, true
	ctx.showAlert(alertSpec{message: "m"}, func(ok bool) { answered, accepted = true, ok })
	if !answered || accepted {
		t.Errorf("answered %v accepted %v, want a decline", answered, accepted)
	}
}

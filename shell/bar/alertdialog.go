package bar

import (
	"log"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"
)

// alertSpec is the gtk::AlertDialog the dropdowns build for a
// destructive action: the message, its detail, and the buttons
// [cancel, accept] with cancel as both the default and the cancel
// button (cancel_button(0), default_button(0)).
type alertSpec struct {
	message, detail string
	cancel, accept  string
}

// Alert responses.
const (
	alertCancel = "cancel"
	alertAccept = "accept"
)

// alertDialog declares spec as a modal dialog (modal(true)): Enter
// and Esc both answer cancel, so only a click on accept goes ahead.
// answer hears true for accept, false for anything else.
func alertDialog(ctx ModuleContext, spec alertSpec, answer func(accepted bool)) app.DialogConfig {
	px := 14.0
	if ctx.Style != nil && ctx.Style.labelPx > 0 {
		px = ctx.Style.labelPx
	}
	body := widget.NewBox(widget.Column, 0, 8)
	message := widget.NewLabel(headingFont(ctx, px*1.15), px*1.15, spec.message, 0)
	message.SetWrap(true)
	message.AddClass("heading")
	body.Append(message, false)
	if spec.detail != "" {
		detail := widget.NewLabel(ctx.Font, px, spec.detail, 0)
		detail.SetWrap(true)
		detail.AddClass("body")
		body.Append(detail, false)
	}
	return app.DialogConfig{
		Width:   400,
		Content: body,
		Buttons: []app.DialogButton{
			{Label: spec.cancel, Response: alertCancel},
			{Label: spec.accept, Response: alertAccept},
		},
		DefaultResponse: alertCancel,
		CancelResponse:  alertCancel,
		Modal:           true,
		OnResponse:      func(r string) { answer(r == alertAccept) },
	}
}

// headingFont is the message's bold face, the context's own when the
// sans family cannot be loaded (headless construction).
func headingFont(ctx ModuleContext, px float64) render.Font {
	if ctx.App == nil || ctx.Config == nil {
		return ctx.Font
	}
	face, err := app.FontWeighted(ctx.Config.General.FontSans, px, 700, false)
	if err != nil {
		return ctx.Font
	}
	return app.FontFallback(face)
}

// showAlert asks spec in a modal dialog and answers once. The open
// dropdowns close first: the dialog is its own window, and a popover
// holding the keyboard would keep Enter and Esc from it (GTK's popover
// loses its grab to the dialog the same way). Without an application
// (headless) nothing can ask, so nothing is confirmed.
func (c ModuleContext) showAlert(spec alertSpec, answer func(accepted bool)) {
	if c.Alert != nil {
		c.Alert(spec, answer)
		return
	}
	if c.App == nil {
		answer(false)
		return
	}
	if c.Dropdowns != nil {
		c.Dropdowns.closeAll()
	}
	if _, err := c.App.NewDialog(nil, alertDialog(c, spec, answer)); err != nil {
		log.Printf("bar: alert: %v", err)
		answer(false)
	}
}

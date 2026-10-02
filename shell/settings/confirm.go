package settings

import (
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
)

// Modal text widths (confirm_modal: max_width_chars).
const (
	modalTitleChars       = 30
	modalDescriptionChars = 35
)

// confirmSpec is ConfirmModalConfig with a danger confirm button and
// the warning icon, the one shape the settings app asks for.
type confirmSpec struct {
	title, description, confirm, cancel string
}

// resetAllConfirm is the reset-all question (SettingsAppMsg::ConfirmResetAll).
func resetAllConfirm() confirmSpec {
	t := i18n.Settings()
	return confirmSpec{
		title:       t.Get("settings-reset-all-title"),
		description: t.Get("settings-reset-all-description"),
		confirm:     t.Get("settings-reset-all-confirm"),
		cancel:      t.Get("settings-reset-all-cancel"),
	}
}

// Responses of the confirm modal.
const (
	responseConfirm = "confirm"
	responseCancel  = "cancel"
)

// confirmContent is ConfirmModal's tree: the warning icon and the
// text in the header, cancel and the danger confirm in the footer.
// respond answers the dialog.
func confirmContent(k *kit, spec confirmSpec, respond func(string)) *widget.Box {
	root := widget.NewBox(widget.Column, 0, 0)
	root.SetElement("window")
	root.AddClass("modal")

	header := widget.NewBox(widget.Row, 0, 0)
	header.AddClass("modal-header")
	icon := widget.NewBox(widget.Column, 0, 0)
	icon.AddClass("modal-icon", "warning")
	icon.Append(k.icon("tb-alert-triangle-symbolic"), true)
	header.Append(icon, false)
	text := widget.NewBox(widget.Column, 0, 0)
	text.AddClass("modal-header-content")
	title := k.label(spec.title, "modal-title")
	title.SetWrap(true)
	title.SetMaxWidthChars(modalTitleChars)
	text.Append(title, false)
	if spec.description != "" {
		desc := k.label(spec.description, "modal-description")
		desc.SetWrap(true)
		desc.SetMaxWidthChars(modalDescriptionChars)
		text.Append(desc, false)
	}
	header.Append(text, true)
	root.Append(header, false)

	footer := widget.NewBox(widget.Row, 0, 0)
	footer.AddClass("modal-footer")
	footer.Append(widget.NewSpacer(0, 0), true)
	footer.Append(k.button(k.label(spec.cancel), func() { respond(responseCancel) }, "secondary"), false)
	footer.Append(k.button(k.label(spec.confirm), func() { respond(responseConfirm) }, "danger"), false)
	root.Append(footer, false)
	return root
}

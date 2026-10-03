package bar

import (
	"strconv"
	"sync"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/mail"
)

// mailRowIconPx is ROW_ICON_SIZE.
const mailRowIconPx = 20

// mailView is the mail dropdown: one row per account (its icon, name,
// and unread count, dimmed at zero), or the empty text, rebuilt as the
// service changes while open.
type mailView struct {
	ctx  ModuleContext
	font render.Font
	px   float64

	*widget.Box
	list *widget.Box

	once sync.Once
	stop chan struct{}
}

func mailDropdown(ctx ModuleContext) widget.Widget {
	font, px := dropdownFont(ctx)
	v := &mailView{ctx: ctx, font: font, px: px, stop: make(chan struct{})}
	v.Box = widget.NewBox(widget.Column, 10, 14)
	v.AddClass("dropdown", "mail-dropdown")
	v.Append(dropdownHeader(font, px, "ld-mail-symbolic", i18n.T("dropdown-mail-title")), false)
	v.list = widget.NewBox(widget.Column, 4, 0)
	v.list.AddClass("mail-dropdown-list")
	// The DropdownContent template's box: the stylesheet's default ink
	// and padding for everything the list does not class itself.
	content := widget.NewBox(widget.Column, 0, 0)
	content.AddClass("dropdown-content")
	content.Append(dropdownScroll(v.list, ""), true)
	v.Append(content, true)
	v.rebuild()
	v.follow()
	return v
}

func (v *mailView) accounts() []mail.AccountUnread {
	if v.ctx.Mail == nil {
		return nil
	}
	return v.ctx.Mail.State().Accounts
}

// rebuild is rebuild_list.
func (v *mailView) rebuild() {
	v.list.Clear()
	accounts := v.accounts()
	if len(accounts) == 0 {
		// .mail-dropdown-empty inks it muted.
		empty := widget.NewLabel(v.font, v.px, i18n.T("dropdown-mail-empty"), 0)
		empty.AddClass("mail-dropdown-empty")
		empty.SetWrap(true)
		v.list.Append(empty, false)
		return
	}
	for _, a := range accounts {
		v.list.Append(v.row(a), false)
	}
}

// row is account_row.
func (v *mailView) row(a mail.AccountUnread) widget.Widget {
	row := widget.NewBox(widget.Row, 8, 0)
	row.AddClass("mail-dropdown-row")
	icon := widget.NewThemeIcon(a.Icon, mailRowIconPx)
	row.Append(icon, false)
	// .mail-account-name inks it fg-default.
	name := widget.NewLabel(v.font, v.px, a.Name, 0)
	name.AddClass("mail-account-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	row.Append(name, true)
	// .mail-account-count inks it fg-default, .dim muted.
	count := widget.NewLabel(v.font, v.px, strconv.FormatUint(uint64(a.Count), 10), 0)
	count.AddClass("mail-account-count")
	if a.Count == 0 {
		count.AddClass("dim")
	}
	row.Append(count, false)
	return row
}

func (v *mailView) follow() {
	if v.ctx.App == nil || v.ctx.Mail == nil {
		return
	}
	ticks, stop := v.ctx.Mail.Subscribe()
	go func() {
		defer stop()
		for {
			select {
			case <-v.stop:
				return
			case _, ok := <-ticks:
				if !ok {
					return
				}
				v.ctx.Invoke(v.rebuild)
			}
		}
	}()
}

// dropdownClosed implements dropdownCloser.
func (v *mailView) dropdownClosed() { v.once.Do(func() { close(v.stop) }) }

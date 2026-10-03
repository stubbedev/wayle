package bar

import (
	"strconv"
	"sync"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
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
	v.Append(dropdownScroll(v.list, ""), true)
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
		empty := widget.NewLabel(v.font, v.px, i18n.T("dropdown-mail-empty"), mutedFg(v.ctx.Style.palette))
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
	icon.SetTint(v.ctx.Style.fg)
	row.Append(icon, false)
	name := widget.NewLabel(v.font, v.px, a.Name, v.ctx.Style.fg)
	name.AddClass("mail-account-name")
	name.SetEllipsize(widget.EllipsizeEnd)
	row.Append(name, true)
	countColor := v.ctx.Style.fg
	count := widget.NewLabel(v.font, v.px, strconv.FormatUint(uint64(a.Count), 10), countColor)
	count.AddClass("mail-account-count")
	if a.Count == 0 {
		count.AddClass("dim")
		count.SetColor(tokenColor(v.ctx.Style.palette, config.TokenFgSubtle))
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

package bar

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/internal/desktopnotify"
	"github.com/stubbedev/wayle/internal/icons"
	"github.com/stubbedev/wayle/service/mail"
)

// startMail builds and runs the shared mail service, as the Rust
// bootstrap does whether or not a mail module is placed. Without a
// session bus it still counts; it only cannot notify. The configured
// providers' brand icons install alongside. The returned function stops
// it.
func startMail(ctx *ModuleContext) func() {
	var notifier mail.Notifier
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		notifier = mail.DesktopNotifier{Sender: desktopnotify.NewSender(conn)}
	}
	svc := mail.New(ctx.Config.Mail, mail.CLI{}, notifier)
	ctx.Mail = svc
	runCtx, cancel := context.WithCancel(context.Background())
	go svc.Run(runCtx)
	if manager, err := icons.NewManager(); err == nil {
		go mail.InstallProviderIcons(runCtx, ctx.Config.Mail.Accounts, manager)
	}
	return cancel
}

// mailLabel is format_label: {{ count }} replaced, not a template.
func mailLabel(format string, count uint32) string {
	return strings.ReplaceAll(format, "{{ count }}", strconv.FormatUint(uint64(count), 10))
}

// mailModule is the module: the shared service's unread total on a bar
// button, the module root hidden at zero under hide-when-zero.
type mailModule struct {
	ctx    ModuleContext
	label  *widget.Label
	icon   *widget.Icon
	root   widget.Widget
	chrome moduleChrome
	count  uint32
	stop   func()
}

var errMailNoService = errors.New("mail: requires the shared mail service")

func newMail(ctx ModuleContext) (Module, error) {
	if ctx.Mail == nil {
		return nil, errMailNoService
	}
	m := &mailModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.Mail.Icon())
	m.root = assembleModule(ctx, m.icon, m.label)
	m.apply(ctx.Mail.State().Total)
	ticks, unsubscribe := ctx.Mail.Subscribe()
	done := make(chan struct{})
	m.stop = func() {
		unsubscribe()
		close(done)
	}
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ticks:
			}
			total := ctx.Mail.State().Total
			m.ctx.Invoke(func() { m.apply(total) })
		}
	}()
	return m, nil
}

// apply is update_display for one count.
func (m *mailModule) apply(count uint32) {
	cfg := m.ctx.Config.Mail
	m.count = count
	text := ""
	if cfg.LabelShow {
		text = mailLabel(cfg.Format, count)
	}
	m.label.SetText(text)
	m.showChrome()
}

// showChrome hides the module root at zero under hide-when-zero.
func (m *mailModule) showChrome() {
	if m.chrome != nil {
		m.chrome.SetVisible(m.count > 0 || !m.ctx.Config.Mail.HideWhenZero)
	}
}

func (m *mailModule) setChrome(c moduleChrome) {
	m.chrome = c
	m.showChrome()
}

func (m *mailModule) Root() widget.Widget { return m.root }

// Stop unsubscribes from the service.
func (m *mailModule) Stop() {
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
}

package bar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/service/mail"
)

// staticNotmuch answers every query with one count and has no
// database, so Run publishes the seed and then idles.
type staticNotmuch uint32

func (n staticNotmuch) Count(context.Context, string) uint32             { return uint32(n) }
func (staticNotmuch) Newest(context.Context, string, int) []mail.Message { return nil }
func (staticNotmuch) DatabasePath(context.Context) (string, bool)        { return "", false }

func seededMail(t *testing.T, cfg config.MailConfig, count uint32) *mail.Service {
	t.Helper()
	svc := mail.New(cfg, staticNotmuch(count), nil)
	ticks, stop := svc.Subscribe()
	defer stop()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.Run(ctx)
	select {
	case <-ticks:
	case <-time.After(2 * time.Second):
		t.Fatal("mail service never published its seed")
	}
	return svc
}

func TestMailModuleShowsTheServiceTotal(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	ctx.Mail = seededMail(t, cfg.Mail, 7)
	m, err := newMail(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer m.(*mailModule).Stop()
	if got := m.(*mailModule).label.Text(); got != "7" {
		t.Errorf("label = %q, want the service's 7", got)
	}
}

func TestMailModuleWithoutTheServiceErrors(t *testing.T) {
	if _, err := newMail(newTestContext(t, config.Defaults())); !errors.Is(err, errMailNoService) {
		t.Errorf("err = %v, want errMailNoService", err)
	}
}

func TestMailLabel(t *testing.T) {
	cfg := config.DefaultsMail()
	if got := mailLabel(cfg.Format, 3, cfg.HideWhenZero); got != "3" {
		t.Errorf("= %q, want 3", got)
	}
	// hide-when-zero blanks the label at zero.
	if got := mailLabel(cfg.Format, 0, cfg.HideWhenZero); got != "" {
		t.Errorf("zero = %q, want empty", got)
	}
	if got := mailLabel(cfg.Format, 0, false); got != "0" {
		t.Errorf("zero shown = %q, want 0", got)
	}
	if got := mailLabel("Unread: {{ count }}", 12, false); got != "Unread: 12" {
		t.Errorf("format = %q", got)
	}
}

func TestLoadFileAppliesMailAccounts(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	good := "[[modules.mail.accounts]]\nname = \"Work\"\nquery = \"folder:work and tag:unread\"\nprovider = \"gmail\"\n\n[modules.mail]\nformat = \"{{ count }} new\"\nhide-when-zero = false\n"
	if err := osWrite(path, good); err != nil {
		t.Fatal(err)
	}
	c, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(c.Mail.Accounts) != 1 {
		t.Fatalf("accounts = %+v", c.Mail.Accounts)
	}
	account := c.Mail.Accounts[0]
	if account.Name != "Work" || account.Query != "folder:work and tag:unread" || account.Provider != "gmail" {
		t.Errorf("account = %+v", account)
	}
	if c.Mail.Format != "{{ count }} new" || c.Mail.HideWhenZero {
		t.Errorf("mail = %+v", c.Mail)
	}

	for _, bad := range []string{
		"[[modules.mail.accounts]]\nquery = \"tag:unread\"\n",
		"[[modules.mail.accounts]]\nname = \"Work\"\n",
	} {
		if err := osWrite(path, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadFile(path); err == nil {
			t.Errorf("%q: want a load error", bad)
		}
	}
}

// The mail rows paint from the stylesheet: .mail-account-name and
// .mail-account-count (dim at zero) take the cascade's ink, and only
// the provider icon, which no rule covers, keeps its tint.
func TestMailDropdownPaintsFromTheStylesheet(t *testing.T) {
	cfg := config.Defaults()
	ctx := styledContext(t, cfg)
	mc := cfg.Mail
	mc.Accounts = []config.MailAccount{{Name: "Work", Query: "tag:work"}}
	ctx.Mail = seededMail(t, mc, 0)
	v := mailDropdown(ctx).(*mailView)

	row := v.list.Children()[0].(*widget.Box)
	var icon *widget.Icon
	var labels []*widget.Label
	walkTree(row, func(w widget.Widget) bool {
		switch w := w.(type) {
		case *widget.Icon:
			icon = w
		case *widget.Label:
			labels = append(labels, w)
		}
		return true
	})
	if icon == nil || len(labels) != 2 {
		t.Fatalf("row = icon %v, %d labels; want an icon, a name, and a count", icon, len(labels))
	}
	if got := labels[0].Color(); got != 0 {
		t.Errorf("the account name carries a programmatic color %#08x", uint32(got))
	}
	count := labels[1]
	if got := count.Color(); got != 0 || !count.HasClass("dim") {
		t.Errorf("the zero count = color %#08x dim %v; want uncolored and dim", uint32(count.Color()), count.HasClass("dim"))
	}
	if icon.Tint() == 0 {
		t.Error("the provider icon lost its tint although no rule covers it")
	}

	// The empty text is uncolored; .mail-dropdown-empty inks it muted.
	empty := mailDropdown(newTestContext(t, config.Defaults())).(*mailView)
	if l, ok := empty.list.Children()[0].(*widget.Label); !ok || l.Color() != 0 || !l.HasClass("mail-dropdown-empty") {
		t.Errorf("the empty state = %#v, want an uncolored mail-dropdown-empty label", empty.list.Children()[0])
	}
}

func TestMailDropdownRowsAndEmptyState(t *testing.T) {
	cfg := config.Defaults()
	ctx := newTestContext(t, cfg)
	v := mailDropdown(ctx).(*mailView)
	if len(v.list.Children()) != 1 {
		t.Fatalf("no service: %d rows, want the empty text", len(v.list.Children()))
	}
	if l, ok := v.list.Children()[0].(*widget.Label); !ok || l.Text() != i18n.T("dropdown-mail-empty") {
		t.Errorf("empty = %#v", v.list.Children()[0])
	}
	mc := cfg.Mail
	mc.Accounts = []config.MailAccount{{Name: "Work", Query: "tag:work"}, {Name: "Home", Query: "tag:home"}}
	ctx.Mail = seededMail(t, mc, 0)
	v = mailDropdown(ctx).(*mailView)
	if got := len(v.list.Children()); got != 2 {
		t.Fatalf("rows = %d, want one per account", got)
	}
	row := v.list.Children()[0].(*widget.Box)
	count := row.Children()[2].(*widget.Label)
	if count.Text() != "0" || !count.HasClass("dim") {
		t.Errorf("zero count = %q dim %v", count.Text(), count.HasClass("dim"))
	}
}

package bar

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
)

// mailQueryCount runs `notmuch count <query>`, 0 on any failure — the
// Rust query_count's warn-and-zero behavior.
func mailQueryCount(ctx context.Context, query string) int {
	out, err := exec.CommandContext(ctx, "notmuch", "count", query).Output() //nolint:gosec // the query comes from the user's own config
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return count
}

// mailTotal sums the configured queries: the per-account list when
// present, else the single query.
func mailTotal(ctx context.Context, cfg config.MailConfig) int {
	if len(cfg.Accounts) == 0 {
		return mailQueryCount(ctx, cfg.Query)
	}
	total := 0
	for _, account := range cfg.Accounts {
		total += mailQueryCount(ctx, account.Query)
	}
	return total
}

// mailLabel renders {{ count }}; hide-when-zero blanks at zero.
func mailLabel(format string, count int, hideWhenZero bool) string {
	if hideWhenZero && count == 0 {
		return ""
	}
	return replaceTemplateVar(format, "count", strconv.Itoa(count))
}

// mail is the module: the notmuch unread total.
type mailModule struct {
	ctx   ModuleContext
	label *widget.Label
	stop  func()
}

// mailPollInterval is the re-query cadence. The Rust service is
// inotify-driven; the Go port polls until the runtime grows a file
// watcher.
const mailPollInterval = 15 * time.Second

func newMail(ctx ModuleContext) (Module, error) {
	if ctx.App == nil {
		return nil, errors.New("mail: requires the application loop")
	}
	m := &mailModule{ctx: ctx, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.refresh()
	runCtx, cancel := context.WithCancel(context.Background())
	m.stop = cancel
	go func() {
		ticker := time.NewTicker(mailPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
			count := mailTotal(runCtx, m.ctx.Config.Mail)
			m.ctx.Invoke(func() { m.apply(count) })
		}
	}()
	return m, nil
}

// refresh re-queries synchronously at construction time.
func (m *mailModule) refresh() {
	m.apply(mailTotal(context.Background(), m.ctx.Config.Mail))
}

// apply renders one count.
func (m *mailModule) apply(count int) {
	cfg := m.ctx.Config.Mail
	text := ""
	if cfg.LabelShow {
		text = mailLabel(cfg.Format, count, cfg.HideWhenZero)
	}
	m.label.SetText(text)
}

func (m *mailModule) Root() widget.Widget { return m.label }

// Stop ends the re-query loop.
func (m *mailModule) Stop() {
	if m.stop != nil {
		m.stop()
	}
}

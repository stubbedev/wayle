// Package mail is the unread-mail service, the Go counterpart of
// crates/wayle-shell-core's services/mail: notmuch queries (one per
// account, or the single query), re-run when the notmuch maildir
// changes, with optional desktop notifications for newly-arrived
// messages. The bar module and the dropdown both read one Service.
package mail

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/feed"
	"github.com/stubbedev/wayle/internal/fswatch"
)

// Debounce coalesces a maildir-sync burst into one re-query.
const Debounce = 500 * time.Millisecond

// NotifyMax caps how many per-message notifications one arrival burst
// fires, so a large sync cannot flood the notification daemon.
const NotifyMax = 5

// AccountUnread is one account's resolved icon and unread count.
type AccountUnread struct {
	Name  string
	Icon  string
	Count uint32
}

// State is one published read: the per-account counts in config order
// (empty without accounts) and the total.
type State struct {
	Accounts []AccountUnread
	Total    uint32
}

// Message is one newest-first search hit.
type Message struct {
	Sender  string
	Subject string
}

// Notmuch is the query seam.
type Notmuch interface {
	// Count is `notmuch count <query>`, 0 on any failure.
	Count(ctx context.Context, query string) uint32
	// Newest returns up to limit newest matches, empty on failure.
	Newest(ctx context.Context, query string, limit int) []Message
	// DatabasePath is `notmuch config get database.path`; ok=false
	// when there is no database to watch.
	DatabasePath(ctx context.Context) (string, bool)
}

// Notifier fires one desktop notification, fire-and-forget.
type Notifier interface {
	Notify(summary, body, icon string)
}

// Service holds the counts and runs the watch loop.
type Service struct {
	cfg      config.MailConfig
	notmuch  Notmuch
	notifier Notifier
	debounce time.Duration

	mu    sync.Mutex
	state State
	ticks *feed.Tick
}

// New builds a service; Run starts it. A nil notifier disables
// notifications regardless of the notify key.
func New(cfg config.MailConfig, notmuch Notmuch, notifier Notifier) *Service {
	return &Service{
		cfg:      cfg,
		notmuch:  notmuch,
		notifier: notifier,
		debounce: Debounce,
		ticks:    feed.NewTick(),
	}
}

// State returns the latest counts.
func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return State{Accounts: append([]AccountUnread(nil), s.state.Accounts...), Total: s.state.Total}
}

// Subscribe ticks after every publish; stop unregisters.
func (s *Service) Subscribe() (<-chan struct{}, func()) { return s.ticks.Subscribe() }

// Run seeds the counts without notifying (existing unread mail at
// startup is not "new"), then re-queries after each settled burst of
// maildir changes until ctx ends. Without a notmuch database the seed
// is all there is, as in the Rust service. The watch is armed before
// the seed query so a change during it is not lost.
func (s *Service) Run(ctx context.Context) {
	watcher := s.watchMaildir(ctx)
	s.recompute(ctx, false)
	if watcher == nil {
		<-ctx.Done()
		return
	}
	defer func() { _ = watcher.Close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case _, open := <-watcher.Changed():
			if !open {
				<-ctx.Done()
				return
			}
		}
		// Settle the burst, then drain what queued before re-querying.
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.debounce):
		}
		select {
		case <-watcher.Changed():
		default:
		}
		s.recompute(ctx, true)
	}
}

// watchMaildir arms a recursive watch on the notmuch database path;
// nil when there is no database or it cannot be watched.
func (s *Service) watchMaildir(ctx context.Context) *fswatch.Watcher {
	path, ok := s.notmuch.DatabasePath(ctx)
	if !ok {
		return nil
	}
	watcher, err := fswatch.New(fswatch.Changes, true)
	if err != nil {
		log.Printf("mail: cannot create maildir watcher: %v", err)
		return nil
	}
	if err := watcher.Add(path); err != nil {
		log.Printf("mail: cannot watch maildir %s: %v", path, err)
		_ = watcher.Close()
		return nil
	}
	return watcher
}

// recompute re-runs the queries and publishes; with notify (and the
// notify key) set, every query whose count rose fires its batch.
func (s *Service) recompute(ctx context.Context, notify bool) {
	notify = notify && s.cfg.Notify && s.notifier != nil
	previous := s.State()

	var next State
	var batch []newMail
	if len(s.cfg.Accounts) == 0 {
		next.Total = s.notmuch.Count(ctx, s.cfg.Query)
		if notify && next.Total > previous.Total {
			batch = s.buildBatch(ctx, s.cfg.Query, s.cfg.IconName, "New mail", previous.Total, next.Total)
		}
	} else {
		prevCounts := make(map[string]uint32, len(previous.Accounts))
		for _, account := range previous.Accounts {
			prevCounts[account.Name] = account.Count
		}
		for _, account := range s.cfg.Accounts {
			count := s.notmuch.Count(ctx, account.Query)
			next.Total += count
			icon := account.ResolvedIcon()
			if prev := prevCounts[account.Name]; notify && count > prev {
				batch = append(batch, s.buildBatch(ctx, account.Query, icon, account.Name, prev, count)...)
			}
			next.Accounts = append(next.Accounts, AccountUnread{Name: account.Name, Icon: icon, Count: count})
		}
	}

	s.mu.Lock()
	s.state = next
	s.mu.Unlock()

	for _, item := range batch {
		s.notifier.Notify(
			renderNotification(s.cfg.NotifySummary, item),
			renderNotification(s.cfg.NotifyBody, item),
			notifyIconArg(item.icon),
		)
	}

	// Subscribers hear about a pass once it is complete.
	feed.Notify(s.ticks)
}

// newMail is one arrived message rendered into a notification.
type newMail struct {
	icon    string
	sender  string
	subject string
	count   uint32 // total unread for the query after the arrival
	new     uint32 // how many arrived in this burst
}

// buildBatch fetches up to NotifyMax of the newest matches, falling
// back to one count-only entry when no details can be read.
func (s *Service) buildBatch(ctx context.Context, query, icon, fallbackSender string, previous, total uint32) []newMail {
	arrived := total - previous
	messages := s.notmuch.Newest(ctx, query, min(int(arrived), NotifyMax))
	if len(messages) == 0 {
		return []newMail{{
			icon:    icon,
			sender:  fallbackSender,
			subject: strconv.FormatUint(uint64(arrived), 10) + " new (" + strconv.FormatUint(uint64(total), 10) + " unread)",
			count:   total,
			new:     arrived,
		}}
	}
	batch := make([]newMail, 0, len(messages))
	for _, m := range messages {
		batch = append(batch, newMail{icon: icon, sender: m.Sender, subject: m.Subject, count: total, new: arrived})
	}
	return batch
}

// renderNotification substitutes {{ sender }}, {{ subject }},
// {{ count }} and {{ new }}.
func renderNotification(format string, m newMail) string {
	return strings.NewReplacer(
		"{{ sender }}", m.sender,
		"{{ subject }}", m.subject,
		"{{ count }}", strconv.FormatUint(uint64(m.count), 10),
		"{{ new }}", strconv.FormatUint(uint64(m.new), 10),
	).Replace(format)
}

// notifyIconArg resolves an icon name to its SVG in wayle's icon
// registry (notification daemons do not search wayle's private icon
// dir by name), else the bare name for system-themed icons.
func notifyIconArg(icon string) string {
	base, ok := iconRegistryPath()
	if !ok {
		return icon
	}
	path := filepath.Join(base, "hicolor", "scalable", "actions", icon+".svg")
	if _, err := os.Stat(path); err != nil {
		return icon
	}
	return path
}

// iconRegistryPath is wayle-icons' IconRegistry::default_path:
// $XDG_DATA_HOME/wayle/icons, or ~/.local/share/wayle/icons.
func iconRegistryPath() (string, bool) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return "", false
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "wayle", "icons"), true
}

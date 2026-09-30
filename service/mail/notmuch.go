package mail

import (
	"context"
	"encoding/json"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/wayle/internal/desktopnotify"
)

// CLI runs the notmuch binary, as the Rust service does.
type CLI struct{}

func notmuch(ctx context.Context, args ...string) ([]byte, bool) {
	out, err := exec.CommandContext(ctx, "notmuch", args...).Output() //nolint:gosec // the queries come from the user's own config
	return out, err == nil
}

// Count is `notmuch count <query>`, 0 on any failure (query_count).
func (CLI) Count(ctx context.Context, query string) uint32 {
	out, ok := notmuch(ctx, "count", query)
	if !ok {
		return 0
	}
	count, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(count)
}

// Newest is `notmuch search --format=json --sort=newest-first
// --limit=N <query>`, one (sender, subject) per thread, empty on any
// failure (query_new_messages).
func (CLI) Newest(ctx context.Context, query string, limit int) []Message {
	if limit <= 0 {
		return nil
	}
	out, ok := notmuch(ctx, "search", "--format=json", "--sort=newest-first", "--limit="+strconv.Itoa(limit), query)
	if !ok {
		return nil
	}
	return parseSearch(out)
}

// parseSearch reads notmuch's thread JSON. `authors` is
// "matched | non-matched"; the sender is the first matched author.
func parseSearch(out []byte) []Message {
	var threads []struct {
		Authors string `json:"authors"`
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal(out, &threads); err != nil {
		return nil
	}
	messages := make([]Message, 0, len(threads))
	for _, thread := range threads {
		matched, _, _ := strings.Cut(thread.Authors, "|")
		first, _, _ := strings.Cut(matched, ",")
		m := Message{Sender: strings.TrimSpace(first), Subject: strings.TrimSpace(thread.Subject)}
		if m.Sender == "" && m.Subject == "" {
			continue
		}
		messages = append(messages, m)
	}
	return messages
}

// DatabasePath is `notmuch config get database.path` (maildir_path).
func (CLI) DatabasePath(ctx context.Context) (string, bool) {
	out, ok := notmuch(ctx, "config", "get", "database.path")
	if !ok {
		return "", false
	}
	path := strings.TrimSpace(string(out))
	return path, path != ""
}

// DesktopNotifier fires mail notifications as "Wayle" through the
// session notification daemon.
type DesktopNotifier struct {
	Sender *desktopnotify.Sender
}

// notifyTimeout bounds one fire-and-forget send.
const notifyTimeout = 5 * time.Second

// Notify sends in the background; failures are logged, never returned,
// as a missing notification must not fail the re-query.
func (d DesktopNotifier) Notify(summary, body, icon string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		if _, err := d.Sender.Send(ctx, "Wayle", summary, body, icon); err != nil {
			log.Printf("mail: notify: %v", err)
		}
	}()
}

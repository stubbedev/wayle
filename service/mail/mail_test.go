package mail

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/wayle/config"
)

// fakeNotmuch serves counts by query and records calls.
type fakeNotmuch struct {
	mu      sync.Mutex
	counts  map[string]uint32
	newest  map[string][]Message
	dbPath  string
	counted int
}

func (f *fakeNotmuch) set(query string, n uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[query] = n
}

func (f *fakeNotmuch) setNewest(query string, messages []Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.newest[query] = messages
}

func (f *fakeNotmuch) Count(_ context.Context, query string) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counted++
	return f.counts[query]
}

func (f *fakeNotmuch) Newest(_ context.Context, query string, limit int) []Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.newest[query]
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}

func (f *fakeNotmuch) DatabasePath(context.Context) (string, bool) {
	return f.dbPath, f.dbPath != ""
}

type sent struct{ summary, body, icon string }

type fakeNotifier struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeNotifier) Notify(summary, body, icon string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{summary, body, icon})
}

func (f *fakeNotifier) all() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sent(nil), f.sent...)
}

func startService(t *testing.T, cfg config.MailConfig, nm *fakeNotmuch, notifier Notifier) (*Service, <-chan struct{}) {
	t.Helper()
	// Keep the host's installed icons out of the notification icons.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	svc := New(cfg, nm, notifier)
	svc.debounce = 20 * time.Millisecond
	ticks, stop := svc.Subscribe()
	t.Cleanup(stop)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitTick(t, ticks, "seed")
	return svc, ticks
}

func waitTick(t *testing.T, ticks <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ticks:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s: no publish", what)
	}
}

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func notifyConfig() config.MailConfig {
	cfg := config.DefaultsMail()
	cfg.Notify = true
	return cfg
}

func TestSeedPublishesWithoutNotifying(t *testing.T) {
	nm := &fakeNotmuch{counts: map[string]uint32{"tag:unread": 4}, newest: map[string][]Message{
		"tag:unread": {{Sender: "Alice", Subject: "Hi"}},
	}}
	notifier := &fakeNotifier{}
	svc, _ := startService(t, notifyConfig(), nm, notifier)
	if got := svc.State(); got.Total != 4 || len(got.Accounts) != 0 {
		t.Errorf("state = %+v, want total 4 and no accounts", got)
	}
	if n := notifier.all(); len(n) != 0 {
		t.Errorf("startup notified %v; unread mail at startup is not new", n)
	}
}

func TestMaildirChangeRequeriesAndNotifiesArrivals(t *testing.T) {
	maildir := t.TempDir()
	cur := filepath.Join(maildir, "INBOX", "cur")
	if err := os.MkdirAll(cur, 0o700); err != nil {
		t.Fatal(err)
	}
	nm := &fakeNotmuch{dbPath: maildir, counts: map[string]uint32{"tag:unread": 1}, newest: map[string][]Message{}}
	notifier := &fakeNotifier{}
	cfg := notifyConfig()
	cfg.NotifySummary = "{{ new }} from {{ sender }}"
	cfg.NotifyBody = "{{ subject }} ({{ count }})"
	svc, ticks := startService(t, cfg, nm, notifier)

	nm.set("tag:unread", 3)
	nm.setNewest("tag:unread", []Message{{Sender: "Alice", Subject: "Lunch"}, {Sender: "Bob", Subject: "Build"}})
	touch(t, cur, "2:2,")
	waitTick(t, ticks, "after a maildir write")

	if got := svc.State().Total; got != 3 {
		t.Errorf("total = %d, want 3", got)
	}
	want := []sent{
		{"2 from Alice", "Lunch (3)", "ld-mail-symbolic"},
		{"2 from Bob", "Build (3)", "ld-mail-symbolic"},
	}
	if got := notifier.all(); !reflect.DeepEqual(got, want) {
		t.Errorf("notifications = %+v, want %+v", got, want)
	}

	// A drop (mail read) re-queries but notifies nothing.
	nm.set("tag:unread", 0)
	touch(t, cur, "3:2,S")
	waitTick(t, ticks, "after a read")
	if got := svc.State().Total; got != 0 {
		t.Errorf("total = %d, want 0", got)
	}
	if got := notifier.all(); len(got) != 2 {
		t.Errorf("a falling count notified: %+v", got)
	}
}

func TestNotifyOffStaysQuiet(t *testing.T) {
	maildir := t.TempDir()
	nm := &fakeNotmuch{dbPath: maildir, counts: map[string]uint32{"tag:unread": 0}, newest: map[string][]Message{}}
	notifier := &fakeNotifier{}
	svc, ticks := startService(t, config.DefaultsMail(), nm, notifier)
	nm.set("tag:unread", 9)
	touch(t, maildir, "new")
	waitTick(t, ticks, "after a write")
	if svc.State().Total != 9 {
		t.Errorf("total = %d, want 9", svc.State().Total)
	}
	if got := notifier.all(); len(got) != 0 {
		t.Errorf("notify=false still notified: %+v", got)
	}
}

func TestAccountsCountSeparatelyAndFallBackToACountOnlyNotification(t *testing.T) {
	maildir := t.TempDir()
	cfg := notifyConfig()
	house := "house"
	cfg.Accounts = []config.MailAccount{
		{Name: "Work", Query: "folder:work", Provider: config.MailProviderGmail},
		{Name: "Home", Query: "folder:home", Provider: config.MailProviderGeneric, Icon: &house},
	}
	nm := &fakeNotmuch{dbPath: maildir, counts: map[string]uint32{"folder:work": 2, "folder:home": 1}, newest: map[string][]Message{}}
	notifier := &fakeNotifier{}
	svc, ticks := startService(t, cfg, nm, notifier)

	state := svc.State()
	wantAccounts := []AccountUnread{{Name: "Work", Icon: "si-gmail-symbolic", Count: 2}, {Name: "Home", Icon: "house", Count: 1}}
	if state.Total != 3 || !reflect.DeepEqual(state.Accounts, wantAccounts) {
		t.Fatalf("state = %+v", state)
	}

	// Work gains 8 with no readable details; Home is unchanged.
	nm.set("folder:work", 10)
	touch(t, maildir, "new")
	waitTick(t, ticks, "after a write")
	want := []sent{{"Work", "8 new (10 unread)", "si-gmail-symbolic"}}
	if got := notifier.all(); !reflect.DeepEqual(got, want) {
		t.Errorf("notifications = %+v, want %+v", got, want)
	}
}

func TestBurstIsCappedAtNotifyMax(t *testing.T) {
	maildir := t.TempDir()
	nm := &fakeNotmuch{dbPath: maildir, counts: map[string]uint32{"tag:unread": 0}, newest: map[string][]Message{}}
	notifier := &fakeNotifier{}
	_, ticks := startService(t, notifyConfig(), nm, notifier)
	var many []Message
	for range 20 {
		many = append(many, Message{Sender: "s", Subject: "x"})
	}
	nm.setNewest("tag:unread", many)
	nm.set("tag:unread", 20)
	touch(t, maildir, "burst")
	waitTick(t, ticks, "after a burst")
	if got := len(notifier.all()); got != NotifyMax {
		t.Errorf("notifications = %d, want the %d cap", got, NotifyMax)
	}
}

func TestNoDatabaseMeansNoRequery(t *testing.T) {
	nm := &fakeNotmuch{counts: map[string]uint32{"tag:unread": 1}, newest: map[string][]Message{}}
	_, ticks := startService(t, config.DefaultsMail(), nm, nil)
	select {
	case <-ticks:
		t.Fatal("re-queried without a database to watch")
	case <-time.After(100 * time.Millisecond):
	}
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if nm.counted != 1 {
		t.Errorf("count calls = %d, want the seed only", nm.counted)
	}
}

func TestParseSearchTakesTheFirstMatchedAuthor(t *testing.T) {
	out := []byte(`[
		{"authors": "Alice Smith, Bob| Carol", "subject": " Lunch "},
		{"authors": "", "subject": ""},
		{"authors": "Dave", "subject": "Build"}
	]`)
	want := []Message{{Sender: "Alice Smith", Subject: "Lunch"}, {Sender: "Dave", Subject: "Build"}}
	if got := parseSearch(out); !reflect.DeepEqual(got, want) {
		t.Errorf("parse = %+v, want %+v", got, want)
	}
	if got := parseSearch([]byte("not json")); got != nil {
		t.Errorf("bad json = %+v, want nil", got)
	}
}

func fakeNotmuchBinary(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notmuch"), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCLIParsesAndZeroesOnFailure(t *testing.T) {
	fakeNotmuchBinary(t, `case "$1" in
count) [ "$2" = "tag:unread" ] && echo 12 || exit 1 ;;
config) echo "/home/me/Mail" ;;
search) echo '[{"authors":"Eve","subject":"'"$4"'"}]' ;;
esac`)
	ctx := context.Background()
	if got := (CLI{}).Count(ctx, "tag:unread"); got != 12 {
		t.Errorf("count = %d, want 12", got)
	}
	if got := (CLI{}).Count(ctx, "bad query"); got != 0 {
		t.Errorf("failing count = %d, want 0", got)
	}
	if path, ok := (CLI{}).DatabasePath(ctx); !ok || path != "/home/me/Mail" {
		t.Errorf("database path = %q, %v", path, ok)
	}
	if got := (CLI{}).Newest(ctx, "tag:unread", 3); len(got) != 1 || got[0].Subject != "--limit=3" {
		t.Errorf("newest = %+v, want the limit passed through", got)
	}
	if got := (CLI{}).Newest(ctx, "tag:unread", 0); got != nil {
		t.Errorf("limit 0 = %+v, want no search", got)
	}
}

func TestCLIWithoutADatabase(t *testing.T) {
	fakeNotmuchBinary(t, "exit 1\n")
	if _, ok := (CLI{}).DatabasePath(context.Background()); ok {
		t.Error("failing config get: want no database")
	}
}

func TestNotifyIconArgPrefersTheRegistrySVG(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	dir := filepath.Join(data, "wayle", "icons", "hicolor", "scalable", "actions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	touch(t, dir, "si-gmail-symbolic.svg")
	if got := notifyIconArg("si-gmail-symbolic"); got != filepath.Join(dir, "si-gmail-symbolic.svg") {
		t.Errorf("installed icon = %q, want its path", got)
	}
	if got := notifyIconArg("mail-unread"); got != "mail-unread" {
		t.Errorf("themed icon = %q, want the bare name", got)
	}
}

package notifications

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayle", "notifications.db")
	st, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

func newStoredService(t *testing.T, st *Store) *Service {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	svc := NewService()
	if err := svc.AttachStore(st); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestStoreRoundTrip(t *testing.T) {
	st, _ := openTestStore(t)
	svc := newStoredService(t, st)
	id := svc.NotifyHints("mail", 0, "mail-icon", "Subject", "Body", []string{"default", "Open", "reply", "Reply"},
		map[string]dbus.Variant{
			"urgency":        dbus.MakeVariant(byte(2)),
			"desktop-entry":  dbus.MakeVariant("org.mail"),
			"sender-pid":     dbus.MakeVariant(int64(4242)),
			"suppress-sound": dbus.MakeVariant(false),
			"resident":       dbus.MakeVariant(true),
			"image-path":     dbus.MakeVariant("/tmp/pic.png"),
			"image-data":     dbus.MakeVariant([]any{int32(1), int32(1), int32(4), true, int32(8), int32(4), []byte{1, 2, 3, 4}}),
			"x-list":         dbus.MakeVariant([]string{"a", "b"}),
		}, 5000)

	got, err := st.Load(time.Now(), true)
	if err != nil || len(got) != 1 {
		t.Fatalf("load = %v, %v", got, err)
	}
	n := got[0]
	if n.ID != id || n.AppName != "mail" || n.AppIcon != "mail-icon" || n.Summary != "Subject" || n.Body != "Body" || n.ExpireMS != 5000 {
		t.Errorf("fields = %+v", n)
	}
	if len(n.Actions) != 2 || n.Actions[1] != (Action{ID: "reply", Label: "Reply"}) {
		t.Errorf("actions = %v", n.Actions)
	}
	// The inline pixels win over image-path, as cached by Notify; the
	// image_path column keeps that file.
	want := svc.Notifications()[0].ImagePath
	if n.Urgency != UrgencyCritical || n.DesktopEntry != "org.mail" || n.ImagePath != want || want == "" || want == "/tmp/pic.png" || !n.Resident {
		t.Errorf("decoded hints: urgency %v entry %q image %q resident %v", n.Urgency, n.DesktopEntry, n.ImagePath, n.Resident)
	}
	if pid, _ := n.Hints["sender-pid"].Value().(int64); pid != 4242 {
		t.Errorf("sender-pid = %v", n.Hints["sender-pid"])
	}
	if _, ok := n.Hints["image-data"]; ok {
		t.Error("the inline image pixels were stored")
	}
	if _, ok := n.Hints["x-list"]; ok {
		t.Error("a container hint was written in a form the Rust store cannot read")
	}
	if n.Added.UnixMilli() == 0 || n.Expires.IsZero() {
		t.Errorf("timestamps: added %v expires %v", n.Added, n.Expires)
	}
}

// TestStoreReadsTheRustRows pins compatibility with the file the Rust
// shell wrote: its zvariant hint JSON, NULL columns, and the image_path
// column restoring the image-path hint.
func TestStoreReadsTheRustRows(t *testing.T) {
	st, _ := openTestStore(t)
	_, err := st.db.Exec(`INSERT INTO notifications
		(id, app_name, replaces_id, app_icon, summary, body, actions, hints, expire_timeout, timestamp, image_path)
		VALUES (572, 'Gmail', NULL, NULL, 'New mail', NULL, '["default","Open"]',
		'{"urgency":{"signature":"y","value":2},"desktop-entry":{"signature":"s","value":"gmail"},"sender-pid":{"signature":"x","value":1234},"suppress-sound":{"signature":"b","value":false}}',
		NULL, 1790883830548, '/icons/gmail.svg')`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(time.Now(), true)
	if err != nil || len(got) != 1 {
		t.Fatalf("load = %v, %v", got, err)
	}
	n := got[0]
	if n.ID != 572 || n.AppName != "Gmail" || n.Body != "" || n.AppIcon != "" || n.ExpireMS != -1 || n.ReplacesID != 0 {
		t.Errorf("fields = %+v", n)
	}
	if n.Urgency != UrgencyCritical || n.DesktopEntry != "gmail" || n.ImagePath != "/icons/gmail.svg" {
		t.Errorf("hints: urgency %v entry %q image %q", n.Urgency, n.DesktopEntry, n.ImagePath)
	}
	if _, ok := n.DefaultAction(); !ok {
		t.Error("the stored default action was lost")
	}
	if n.Added.UnixMilli() != 1790883830548 {
		t.Errorf("timestamp = %d", n.Added.UnixMilli())
	}

	// A broken hints column keeps the row, without hints.
	if _, err := st.db.Exec(`UPDATE notifications SET hints = 'not json' WHERE id = 572`); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Load(time.Now(), true); err != nil || len(got) != 1 || got[0].Urgency != UrgencyNormal {
		t.Errorf("broken hints: %v, %v", got, err)
	}
}

func TestStoreDropsWhatExpiredWhileDown(t *testing.T) {
	st, _ := openTestStore(t)
	old := time.Now().Add(-time.Hour)
	for _, n := range []*Notification{
		{ID: 1, Summary: "timed out", ExpireMS: 1000, Added: old},
		{ID: 2, Summary: "still due", ExpireMS: int32((2 * time.Hour).Milliseconds()), Added: old},
		{ID: 3, Summary: "never expires", ExpireMS: 0, Added: old},
		{ID: 4, Summary: "server default", ExpireMS: -1, Added: old},
	} {
		if err := st.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.Load(time.Now(), true)
	ids := map[uint32]bool{}
	for _, n := range got {
		ids[n.ID] = true
	}
	if ids[1] || !ids[2] || !ids[3] || !ids[4] {
		t.Errorf("kept %v, want 2, 3 and 4", ids)
	}
	if all, _ := st.Load(time.Now(), false); len(all) != 4 {
		t.Errorf("remove-expired off kept %d, want all 4", len(all))
	}
}

func TestServiceKeepsTheStoreInStep(t *testing.T) {
	st, path := openTestStore(t)
	svc := newStoredService(t, st)
	a := svc.Notify("mail", 0, "", "A", "", nil, 0)
	b := svc.Notify("chat", 0, "", "B", "", nil, 0)
	svc.NotifyHints("tmp", 0, "", "Transient", "", nil, map[string]dbus.Variant{"transient": dbus.MakeVariant(true)}, 0)
	svc.Close(a, Dismissed)
	got, _ := st.Load(time.Now(), true)
	if len(got) != 1 || got[0].ID != b {
		t.Fatalf("store = %v, want only B (A closed, the transient never stored)", got)
	}

	// A transient replacement takes the stored one off the disk too.
	svc.NotifyHints("chat", b, "", "B again", "", nil, map[string]dbus.Variant{"transient": dbus.MakeVariant(true)}, 0)
	if got, _ := st.Load(time.Now(), true); len(got) != 0 {
		t.Errorf("store after a transient replacement = %v", got)
	}

	// A restart: ids continue past the stored ones and their apps own
	// them again.
	c := svc.Notify("mail", 0, "", "C", "", nil, 0)
	_ = st.Close()
	st2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	svc2 := newStoredService(t, st2)
	if svc2.Count() != 1 || svc2.Notifications()[0].ID != c {
		t.Fatalf("restored = %v, want C", svc2.Notifications())
	}
	if len(svc2.Popups()) != 0 {
		t.Error("a restored notification popped up again")
	}
	if id := svc2.Notify("mail", c, "", "C updated", "", nil, 0); id != c {
		t.Errorf("the owner's replacement got id %d, want %d", id, c)
	}
	if id := svc2.Notify("other", c, "", "hijack", "", nil, 0); id <= c {
		t.Errorf("another app's replaces_id got %d, want a fresh id past %d", id, c)
	}
	svc2.DismissAll()
	if got, _ := st2.Load(time.Now(), true); len(got) != 0 {
		t.Errorf("store after dismiss all = %v", got)
	}
}

func TestRestoredExpiryStillFires(t *testing.T) {
	st, _ := openTestStore(t)
	if err := st.Add(&Notification{ID: 7, AppName: "app", Summary: "soon", ExpireMS: 1500, Added: time.Now().Add(-100 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	svc := newStoredService(t, st)
	if svc.Count() != 1 {
		t.Fatal("the pending notification was not restored")
	}
	deadline := time.Now().Add(4 * time.Second)
	for svc.Count() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if svc.Count() != 0 {
		t.Error("a restored notification never expired")
	}
	if got, _ := st.Load(time.Now(), false); len(got) != 0 {
		t.Errorf("the expired one stayed on disk: %v", got)
	}
}

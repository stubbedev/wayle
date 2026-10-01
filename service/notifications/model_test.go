package notifications

import (
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
)

func TestParseActions(t *testing.T) {
	if got := ParseActions(nil); len(got) != 0 {
		t.Errorf("nil = %v", got)
	}
	got := ParseActions([]string{"reply", "Reply", "delete", "Delete", "odd"})
	want := []Action{{"reply", "Reply"}, {"delete", "Delete"}, {"odd", "odd"}}
	if !slices.Equal(got, want) {
		t.Errorf("= %v, want %v", got, want)
	}
}

func TestUrgencyFromByte(t *testing.T) {
	for in, want := range map[byte]Urgency{0: UrgencyLow, 1: UrgencyNormal, 2: UrgencyCritical, 9: UrgencyNormal} {
		if got := UrgencyFromByte(in); got != want {
			t.Errorf("UrgencyFromByte(%d) = %v, want %v", in, got, want)
		}
	}
}

// wireImage is the (iiibiiay) struct a client sends.
type wireImage struct {
	W, H, Stride int32
	Alpha        bool
	Bits, Chans  int32
	Data         []byte
}

// rgbImage is image data as godbus decodes a received struct.
func rgbImage(w, h, stride int32) dbus.Variant {
	data := make([]byte, stride*h)
	for i := range data {
		data[i] = byte(i)
	}
	return dbus.MakeVariant([]any{w, h, stride, false, int32(8), int32(3), data})
}

func TestDecodeHints(t *testing.T) {
	none := func(imageData) (string, bool) { t.Error("cached without image data"); return "", false }
	h := decodeHints(nil, none)
	if h != (hints{urgency: UrgencyNormal}) {
		t.Errorf("no hints = %+v, want normal urgency only", h)
	}
	h = decodeHints(map[string]dbus.Variant{
		"urgency":       dbus.MakeVariant(byte(2)),
		"image-path":    dbus.MakeVariant("/img.png"),
		"desktop-entry": dbus.MakeVariant("firefox"),
		"transient":     dbus.MakeVariant(true),
		"resident":      dbus.MakeVariant(true),
	}, none)
	want := hints{UrgencyCritical, "/img.png", "firefox", true, true}
	if h != want {
		t.Errorf("hints = %+v, want %+v", h, want)
	}
	// Mistyped hints keep their defaults.
	h = decodeHints(map[string]dbus.Variant{"urgency": dbus.MakeVariant(int32(2)), "transient": dbus.MakeVariant("yes")}, none)
	if h.urgency != UrgencyNormal || h.transient {
		t.Errorf("mistyped = %+v", h)
	}
}

func TestDecodeHintsCachesImageData(t *testing.T) {
	var cached []imageData
	cache := func(img imageData) (string, bool) {
		cached = append(cached, img)
		return "/cache/x.png", true
	}
	// image-data wins over image-path and the deprecated keys.
	h := decodeHints(map[string]dbus.Variant{
		"image-path": dbus.MakeVariant("/hint.png"),
		"image-data": rgbImage(2, 1, 6),
		"icon_data":  rgbImage(9, 9, 27),
	}, cache)
	if h.imagePath != "/cache/x.png" || len(cached) != 1 || cached[0].width != 2 {
		t.Errorf("image-data: path %q, cached %+v", h.imagePath, cached)
	}
	cached = nil
	h = decodeHints(map[string]dbus.Variant{"image_data": rgbImage(3, 1, 9)}, cache)
	if len(cached) != 1 || cached[0].width != 3 {
		t.Errorf("legacy key not read: %+v", cached)
	}
	// A malformed struct caches nothing and keeps image-path.
	cached = nil
	h = decodeHints(map[string]dbus.Variant{
		"image-path": dbus.MakeVariant("/hint.png"),
		"image-data": dbus.MakeVariant([]any{int32(1)}),
	}, cache)
	if len(cached) != 0 || h.imagePath != "/hint.png" {
		t.Errorf("malformed: cached %v path %q", cached, h.imagePath)
	}
}

func TestCacheImageWritesPNGOnce(t *testing.T) {
	dir := t.TempDir()
	// Two RGB pixels per row with two bytes of padding.
	img := imageData{
		width: 2, height: 2, rowstride: 8, bitsPerSample: 8, channels: 3,
		data: []byte{1, 2, 3, 4, 5, 6, 0, 0, 7, 8, 9, 10, 11, 12, 0, 0},
	}
	path, ok := cacheImageIn(dir, img)
	if !ok || filepath.Dir(path) != dir {
		t.Fatalf("cache = %q %v", path, ok)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := decoded.At(1, 1).RGBA(); r>>8 != 10 || g>>8 != 11 || b>>8 != 12 || a>>8 != 0xff {
		t.Errorf("pixel (1,1) = %d %d %d %d", r>>8, g>>8, b>>8, a>>8)
	}
	// The same pixels reuse the file.
	info, _ := os.Stat(path)
	again, ok := cacheImageIn(dir, img)
	info2, _ := os.Stat(again)
	if !ok || again != path || !info2.ModTime().Equal(info.ModTime()) {
		t.Error("identical pixels were written again")
	}
}

func TestCacheImageRejectsUnsupported(t *testing.T) {
	dir := t.TempDir()
	for name, img := range map[string]imageData{
		"16-bit":     {width: 1, height: 1, rowstride: 6, bitsPerSample: 16, channels: 3, data: make([]byte, 6)},
		"2 channels": {width: 1, height: 1, rowstride: 2, bitsPerSample: 8, channels: 2, data: make([]byte, 2)},
		"short":      {width: 4, height: 4, rowstride: 16, bitsPerSample: 8, channels: 4, data: make([]byte, 10)},
		"empty":      {width: 0, height: 0, rowstride: 0, bitsPerSample: 8, channels: 4},
	} {
		if path, ok := cacheImageIn(dir, img); ok {
			t.Errorf("%s cached to %q", name, path)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("rejected images left files: %v", entries)
	}
}

func TestReplacesIDOnlyForTheOwner(t *testing.T) {
	s := newTestService(t)
	id := s.Notify("mail", 0, "", "First", "", nil, 0)
	if again := s.Notify("mail", id, "", "Second", "", nil, 0); again != id {
		t.Fatalf("owner replace = %d, want %d", again, id)
	}
	other := s.Notify("chat", id, "", "Hijack", "", nil, 0)
	if other == id {
		t.Fatal("another app replaced an id it does not own")
	}
	if got := s.Count(); got != 2 {
		t.Errorf("count = %d, want the replaced mail plus the chat", got)
	}
	// An unknown replaces_id is a fresh id too.
	if fresh := s.Notify("mail", 999, "", "x", "", nil, 0); fresh == 999 {
		t.Error("an unowned replaces_id was honored")
	}
}

func TestHistoryNewestFirstAndTransientSkipsIt(t *testing.T) {
	s := newTestService(t)
	a := s.Notify("a", 0, "", "A", "", nil, 0)
	b := s.Notify("b", 0, "", "B", "", nil, 0)
	if got := s.Notifications(); len(got) != 2 || got[0].ID != b || got[1].ID != a {
		t.Fatalf("history = %v, want newest first", got)
	}
	tr := s.NotifyHints("c", 0, "", "T", "", nil, map[string]dbus.Variant{"transient": dbus.MakeVariant(true)}, 0)
	if s.Count() != 2 {
		t.Error("a transient notification reached the history")
	}
	if p := s.Popups(); len(p) == 0 || p[0].ID != tr || !p[0].Transient {
		t.Errorf("transient popup missing: %v", p)
	}
}

func TestInvokeActionResidentStays(t *testing.T) {
	s := newTestService(t)
	feed, _ := s.Subscribe()
	var reasons []ClosedReason
	s.SetEmitter(func(signal string, args ...any) {
		if signal == Interface+".NotificationClosed" {
			reasons = append(reasons, ClosedReason(args[1].(uint32)))
		}
	})
	plain := s.Notify("app", 0, "", "plain", "", []string{"default", "Open"}, 0)
	resident := s.NotifyHints("app", 0, "", "res", "", []string{"default", "Open"},
		map[string]dbus.Variant{"resident": dbus.MakeVariant(true)}, 0)
	collect(t, feed, 2)
	s.InvokeAction(resident, "default")
	if s.Count() != 2 || len(reasons) != 0 {
		t.Fatalf("resident invoke closed it: count %d reasons %v", s.Count(), reasons)
	}
	s.InvokeAction(plain, "default")
	if s.Count() != 1 || !slices.Equal(reasons, []ClosedReason{ClosedCall}) {
		t.Errorf("plain invoke: count %d reasons %v, want closed", s.Count(), reasons)
	}
}

func TestDefaultAction(t *testing.T) {
	n := &Notification{Actions: ParseActions([]string{"reply", "Reply", "default", "Open"})}
	if a, ok := n.DefaultAction(); !ok || a.Label != "Open" {
		t.Errorf("default = %v %v", a, ok)
	}
	n = &Notification{Actions: ParseActions([]string{"reply", "Reply"})}
	if _, ok := n.DefaultAction(); ok {
		t.Error("no default action reported one")
	}
}

// The D-Bus Notify call carries its hints into the stored notification.
func TestServerNotifyDecodesHints(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	bus := dbustest.Start(t)
	svc := NewService()
	server, err := Serve(bus.Conn(t), svc)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Release() }()
	obj := bus.Conn(t).Object(Interface, ObjPath)
	var id uint32
	err = obj.Call(Interface+".Notify", 0, "app", uint32(0), "", "sum", "body", []string{"default", "Open"},
		map[string]dbus.Variant{
			"urgency":    dbus.MakeVariant(byte(2)),
			"image-data": dbus.MakeVariant(wireImage{1, 1, 3, false, 8, 3, []byte{1, 2, 3}}),
		}, int32(0)).Store(&id)
	if err != nil {
		t.Fatal(err)
	}
	n := svc.Notifications()[0]
	if n.ID != id || n.Urgency != UrgencyCritical || n.ImagePath == "" {
		t.Errorf("stored = %+v", n)
	}
	if _, err := os.Stat(n.ImagePath); err != nil {
		t.Errorf("cached image: %v", err)
	}
	if _, ok := n.DefaultAction(); !ok {
		t.Error("default action lost")
	}
}

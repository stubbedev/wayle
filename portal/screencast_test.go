package portal

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/shell/sharepicker"
)

// fakeSharePicker is the shell's com.wayle.SharePicker1.
type fakeSharePicker struct {
	reply dbustest.Var[string]
	asked dbustest.Var[[]any]
	// block holds Pick until closed (a picker the user leaves open).
	block chan struct{}
}

func (f *fakeSharePicker) Pick(windowList string, allowToken, multiple bool) (string, *dbus.Error) {
	f.asked.Update(func(a []any) []any { return append(a, []any{windowList, allowToken, multiple}) })
	if f.block != nil {
		<-f.block
	}
	return f.reply.Load(), nil
}

func serveSharePicker(t *testing.T, r *rig, reply string) *fakeSharePicker {
	t.Helper()
	f := &fakeSharePicker{}
	f.reply.Store(reply)
	conn := r.bus.Conn(t)
	if err := conn.Export(f, sharepicker.ServicePath, sharepicker.ServiceName); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.RequestName(sharepicker.ServiceName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return f
}

// fakeStream is a started stream.
type fakeStream struct {
	node   uint32
	closed dbustest.Var[bool]
}

func (s *fakeStream) NodeID() uint32       { return s.node }
func (s *fakeStream) Size() (int32, int32) { return 1920, 1080 }
func (s *fakeStream) Close()               { s.closed.Store(true) }

// fakeStarts records stream starts; fail names payloads that will not
// start.
type fakeStarts struct {
	started dbustest.Var[[]string]
	streams dbustest.Var[[]*fakeStream]
	fail    map[string]bool
}

func (f *fakeStarts) start(t captureTarget, cursor bool, fps uint32) (screenStream, error) {
	f.started.Update(func(s []string) []string {
		return append(s, t.payload()+map[bool]string{true: "+cursor", false: ""}[cursor])
	})
	if f.fail[t.payload()] {
		return nil, errors.New("gone")
	}
	s := &fakeStream{node: uint32(40 + len(f.streams.Load()))}
	f.streams.Update(func(all []*fakeStream) []*fakeStream { return append(all, s) })
	_ = fps
	return s, nil
}

func castRig(t *testing.T, fail ...string) (*rig, *fakeStarts) {
	f := &fakeStarts{fail: map[string]bool{}}
	for _, p := range fail {
		f.fail[p] = true
	}
	r := newRig(t, func(b *Backend) {
		b.screenCast = newScreenCast(b.conn, b.sessions, b.sizes, f.start)
	})
	return r, f
}

func (r *rig) castSession(t *testing.T, options Vardict) {
	t.Helper()
	if code, _ := r.interactive(t, ScreenCastIface+".CreateSession", handle, sessA, "org.app", Vardict{}); code != ResponseSuccess {
		t.Fatalf("CreateSession = %d", code)
	}
	if code, _ := r.interactive(t, ScreenCastIface+".SelectSources", handle, sessA, "org.app", options); code != ResponseSuccess {
		t.Fatalf("SelectSources = %d", code)
	}
}

func (r *rig) castStart(t *testing.T) (uint32, map[string]dbus.Variant) {
	t.Helper()
	return r.interactive(t, ScreenCastIface+".Start", handle, sessA, "org.app", "", Vardict{})
}

func TestScreenCastProperties(t *testing.T) {
	r, _ := castRig(t)
	for name, want := range map[string]uint32{"AvailableSourceTypes": 7, "AvailableCursorModes": 3, "version": 4} {
		if v, err := r.obj.GetProperty(ScreenCastIface + "." + name); err != nil || v.Value() != want {
			t.Errorf("%s = %v, %v", name, v, err)
		}
	}
}

func TestScreenCastPicksAndStreams(t *testing.T) {
	r, f := castRig(t)
	p := serveSharePicker(t, r, "r/screen:DP-1")
	r.castSession(t, Vardict{"persist_mode": dbus.MakeVariant(uint32(2))})
	code, res := r.castStart(t)
	if code != ResponseSuccess {
		t.Fatalf("Start = %d", code)
	}
	// No cursor_mode is embedded; the picker gets no window list, the
	// token box, single-select.
	if got := f.started.Load(); !slices.Equal(got, []string{"screen:DP-1+cursor"}) {
		t.Errorf("started %v", got)
	}
	if asked := p.asked.Load(); len(asked) != 1 || !slices.Equal(asked[0].([]any), []any{"", true, false}) {
		t.Errorf("picker asked %v", asked)
	}
	var streams []castStream
	if err := dbus.Store([]any{res["streams"].Value()}, &streams); err != nil {
		t.Fatal(err)
	}
	if len(streams) != 1 || streams[0].Node != 40 || streams[0].Props["source_type"].Value() != uint32(1) {
		t.Fatalf("streams = %+v", streams)
	}
	if size := streams[0].Props["size"].Value().([]any); size[0] != int32(1920) || size[1] != int32(1080) {
		t.Errorf("size = %v, want an (ii)", size)
	}
	if res["persist_mode"].Value() != uint32(2) {
		t.Errorf("persist_mode = %v", res["persist_mode"])
	}
	token := res["restore_data"]
	if got, ok := decodeRestore(token); !ok || got.payload() != "screen:DP-1" {
		t.Errorf("restore_data = %v", token)
	}
	if size, ok := r.backend.sizes.get(40); !ok || size != [2]int32{1920, 1080} {
		t.Errorf("RemoteDesktop sees %v, %v", size, ok)
	}

	// Closing the session stops its streams and forgets their sizes.
	if err := r.client.Object(BusName, sessA).Call(SessionIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	if !f.streams.Load()[0].closed.Load() {
		t.Error("the stream outlived its session")
	}
	if _, ok := r.backend.sizes.get(40); ok {
		t.Error("a closed stream's size stayed registered")
	}
}

func TestScreenCastWithoutATokenGrant(t *testing.T) {
	r, f := castRig(t)
	serveSharePicker(t, r, "/window:kitty-1")
	r.castSession(t, Vardict{"persist_mode": dbus.MakeVariant(uint32(1)), "cursor_mode": dbus.MakeVariant(cursorHidden)})
	_, res := r.castStart(t)
	if _, ok := res["restore_data"]; ok {
		t.Error("a token the user did not grant")
	}
	if got := f.started.Load(); !slices.Equal(got, []string{"window:kitty-1"}) {
		t.Errorf("started %v: a hidden cursor", got)
	}
}

func TestScreenCastReplaysARestoreToken(t *testing.T) {
	r, f := castRig(t)
	p := serveSharePicker(t, r, "/screen:HDMI-A-1")
	token := dbus.MakeVariant(encodeRestore(captureTarget{kind: targetRegion, name: "DP-2", x: 5, y: 6, width: 640, height: 480}))
	r.castSession(t, Vardict{"restore_data": token, "persist_mode": dbus.MakeVariant(uint32(2))})
	code, res := r.castStart(t)
	if code != ResponseSuccess || len(p.asked.Load()) != 0 {
		t.Fatalf("Start = %d, picker asked %v: a token replays without one", code, p.asked.Load())
	}
	if got := f.started.Load(); !slices.Equal(got, []string{"region:DP-2@5,6,640,480+cursor"}) {
		t.Errorf("started %v", got)
	}
	if got, _ := decodeRestore(res["restore_data"]); got.payload() != "region:DP-2@5,6,640,480" {
		t.Errorf("reissued %v", res["restore_data"])
	}
}

func TestScreenCastRepromptsAGoneRestoreTarget(t *testing.T) {
	r, f := castRig(t, "screen:DP-9")
	p := serveSharePicker(t, r, "/screen:DP-1")
	r.castSession(t, Vardict{"restore_data": dbus.MakeVariant(encodeRestore(captureTarget{kind: targetOutput, name: "DP-9"}))})
	if code, _ := r.castStart(t); code != ResponseSuccess {
		t.Fatalf("Start = %d", code)
	}
	if got := f.started.Load(); !slices.Equal(got, []string{"screen:DP-9+cursor", "screen:DP-1+cursor"}) || len(p.asked.Load()) != 1 {
		t.Errorf("started %v, picker asked %d times", got, len(p.asked.Load()))
	}
}

func TestScreenCastCancels(t *testing.T) {
	r, f := castRig(t)
	r.castSession(t, Vardict{})
	if code, _ := r.castStart(t); code != ResponseCancelled {
		t.Errorf("no shell = %d, want cancelled", code)
	}
	serveSharePicker(t, r, "")
	if code, _ := r.castStart(t); code != ResponseCancelled || len(f.started.Load()) != 0 {
		t.Errorf("picker cancel = %d, started %v", code, f.started.Load())
	}
}

func TestScreenCastStartsAllOrNothing(t *testing.T) {
	r, f := castRig(t, "screen:HDMI-A-1")
	p := serveSharePicker(t, r, "/screen:DP-1;screen:HDMI-A-1")
	r.castSession(t, Vardict{"multiple": dbus.MakeVariant(true)})
	if code, _ := r.castStart(t); code != ResponseOther {
		t.Fatalf("Start = %d, want other", code)
	}
	if asked := p.asked.Load(); asked[0].([]any)[2] != true {
		t.Errorf("not multi-select: %v", asked)
	}
	if s := f.streams.Load(); len(s) != 1 || !s[0].closed.Load() {
		t.Error("the stream started before the failure kept running")
	}
	if _, ok := r.backend.sizes.get(40); ok {
		t.Error("a rolled-back stream registered its size")
	}
}

func TestScreenCastRequestCloseEndsThePicker(t *testing.T) {
	r, _ := castRig(t)
	p := serveSharePicker(t, r, "/screen:DP-1")
	p.block = make(chan struct{})
	defer close(p.block)
	r.castSession(t, Vardict{})
	done := make(chan uint32, 1)
	go func() {
		var code uint32
		var res map[string]dbus.Variant
		_ = r.obj.Call(ScreenCastIface+".Start", 0, handle, sessA, "org.app", "", Vardict{}).Store(&code, &res)
		done <- code
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(p.asked.Load()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := r.client.Object(BusName, handle).Call(RequestIface+".Close", 0).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != ResponseCancelled {
			t.Errorf("Start = %d after Close", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not end the picker")
	}
}

func TestParsePickerReply(t *testing.T) {
	token, targets, ok := parsePickerReply("r/screen:DP-1;window:firefox@instance-3;region:HDMI-A-1@10,-20,800,600")
	var got []string
	for _, t := range targets {
		got = append(got, t.payload())
	}
	if !ok || !token || !slices.Equal(got, []string{"screen:DP-1", "window:firefox@instance-3", "region:HDMI-A-1@10,-20,800,600"}) {
		t.Errorf("= %v %v %v", token, got, ok)
	}
	if targets[2].sourceType() != sourceVirtual || targets[1].sourceType() != sourceWindow || targets[0].sourceType() != sourceMonitor {
		t.Error("source types")
	}
	for _, bad := range []string{"", "garbage", "/screen:", "/window:", "/region:DP-1@1,2,3", "/region:DP-1@1,2,0,5", "/region:@1,2,3,4", "/bogus:x", "/screen:DP-1;bogus"} {
		if _, _, ok := parsePickerReply(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestRestoreTokens(t *testing.T) {
	foreign := dbus.MakeVariant(restoreToken{"other-portal", 1, dbus.MakeVariant("screen:DP-1")})
	if _, ok := decodeRestore(foreign); ok {
		t.Error("a foreign token decoded")
	}
	if _, ok := decodeRestore(dbus.MakeVariant(uint32(42))); ok {
		t.Error("a non-structure decoded")
	}
}

func TestEffectiveFPS(t *testing.T) {
	for _, tc := range [][3]int64{{60, 0, 60}, {60, 144000, 60}, {60, 30000, 30}, {60, 500, 1}, {0, 0, 1}} {
		if got := effectiveFPS(uint32(tc[0]), int32(tc[1])); int64(got) != tc[2] {
			t.Errorf("effectiveFPS(%d, %d) = %d", tc[0], tc[1], got)
		}
	}
	if !showCursor(cursorEmbedded) || !showCursor(cursorMetadata) || showCursor(cursorHidden) {
		t.Error("showCursor")
	}
}

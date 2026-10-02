package eis

import (
	"fmt"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/stubbedev/wayle/internal/clib"
)

// libei is the reference C client, the oracle the server is held to.
type libei struct {
	newSender      func(user unsafe.Pointer) uintptr
	configureName  func(ei uintptr, name string)
	setupFd        func(ei uintptr, fd int32) int32
	getFd          func(ei uintptr) int32
	dispatch       func(ei uintptr)
	getEvent       func(ei uintptr) uintptr
	eventType      func(ev uintptr) int32
	eventSeat      func(ev uintptr) uintptr
	eventDevice    func(ev uintptr) uintptr
	eventUnref     func(ev uintptr) uintptr
	unref          func(ei uintptr) uintptr
	now            func(ei uintptr) uint64
	hasCap         func(dev uintptr, c uint32) bool
	startEmulating func(dev uintptr, seq uint32)
	stopEmulating  func(dev uintptr)
	frame          func(dev uintptr, t uint64)
	motion         func(dev uintptr, x, y float64)
	button         func(dev uintptr, code uint32, press bool)
	scrollDelta    func(dev uintptr, x, y float64)
	scrollDiscrete func(dev uintptr, x, y int32)
	key            func(dev uintptr, code uint32, press bool)
	keysym         func(dev uintptr, sym uint32, press bool)
	bindCaps       uintptr // variadic, NULL-terminated
}

var (
	libeiOnce sync.Once
	libeiLib  *libei
	libeiErr  error
)

func loadLibei(t *testing.T) *libei {
	t.Helper()
	libeiOnce.Do(func() {
		h, err := clib.Open(clib.SystemCandidates("libei.so.1"))
		if err != nil {
			libeiErr = err
			return
		}
		l := &libei{}
		for _, f := range []struct {
			fn   any
			name string
		}{
			{&l.newSender, "ei_new_sender"},
			{&l.configureName, "ei_configure_name"},
			{&l.setupFd, "ei_setup_backend_fd"},
			{&l.getFd, "ei_get_fd"},
			{&l.dispatch, "ei_dispatch"},
			{&l.getEvent, "ei_get_event"},
			{&l.eventType, "ei_event_get_type"},
			{&l.eventSeat, "ei_event_get_seat"},
			{&l.eventDevice, "ei_event_get_device"},
			{&l.eventUnref, "ei_event_unref"},
			{&l.unref, "ei_unref"},
			{&l.now, "ei_now"},
			{&l.hasCap, "ei_device_has_capability"},
			{&l.startEmulating, "ei_device_start_emulating"},
			{&l.stopEmulating, "ei_device_stop_emulating"},
			{&l.frame, "ei_device_frame"},
			{&l.motion, "ei_device_pointer_motion"},
			{&l.button, "ei_device_button_button"},
			{&l.scrollDelta, "ei_device_scroll_delta"},
			{&l.scrollDiscrete, "ei_device_scroll_discrete"},
			{&l.key, "ei_device_keyboard_key"},
			{&l.keysym, "ei_device_text_keysym"},
		} {
			sym, err := purego.Dlsym(h, f.name)
			if err != nil {
				libeiErr = fmt.Errorf("%s: %w", f.name, err)
				return
			}
			purego.RegisterFunc(f.fn, sym)
		}
		if l.bindCaps, err = purego.Dlsym(h, "ei_seat_bind_capabilities"); err != nil {
			libeiErr = err
			return
		}
		libeiLib = l
	})
	if libeiErr != nil {
		t.Fatalf("libei: %v", libeiErr)
	}
	return libeiLib
}

// libei's event types and capabilities (libei.h).
const (
	evConnect       = 1
	evDisconnect    = 2
	evSeatAdded     = 3
	evDeviceResumed = 8

	eiCapPointer  = 1 << 0
	eiCapKeyboard = 1 << 2
	eiCapScroll   = 1 << 4
	eiCapButton   = 1 << 5
	eiCapText     = 1 << 6
)

// recorder is the Handler under test.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

func (r *recorder) Motion(dx, dy float64)       { r.add(fmt.Sprint("motion ", dx, " ", dy)) }
func (r *recorder) Button(c uint32, p bool)     { r.add(fmt.Sprint("button ", c, " ", p)) }
func (r *recorder) Scroll(dx, dy float64)       { r.add(fmt.Sprint("scroll ", dx, " ", dy)) }
func (r *recorder) ScrollDiscrete(dx, dy int32) { r.add(fmt.Sprint("discrete ", dx, " ", dy)) }
func (r *recorder) Key(c uint32, p bool)        { r.add(fmt.Sprint("key ", c, " ", p)) }
func (r *recorder) Keysym(sym uint32, p bool)   { r.add(fmt.Sprint("keysym ", sym, " ", p)) }

// drive connects a libei sender to fd and emulates input on every
// device the server resumes, until resumed devices reach want or the
// timeout passes.
func drive(t *testing.T, l *libei, fd int, caps []uintptr, want int, timeout time.Duration) (connected bool, resumed int) {
	t.Helper()
	ei := l.newSender(nil)
	defer l.unref(ei)
	l.configureName(ei, "wayle-test")
	if rc := l.setupFd(ei, int32(fd)); rc != 0 {
		t.Fatalf("ei_setup_backend_fd = %d", rc)
	}
	efd := int(l.getFd(ei))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) && resumed < want {
		pfd := []pollFd{{fd: int32(efd), events: 1}}
		pollWait(pfd, 100)
		l.dispatch(ei)
		for ev := l.getEvent(ei); ev != 0; ev = l.getEvent(ei) {
			switch l.eventType(ev) {
			case evConnect:
				connected = true
			case evDisconnect:
				t.Error("the server disconnected")
			case evSeatAdded:
				args := append([]uintptr{l.eventSeat(ev)}, caps...)
				purego.SyscallN(l.bindCaps, append(args, 0)...)
			case evDeviceResumed:
				resumed++
				emulate(l, ei, l.eventDevice(ev))
			}
			l.eventUnref(ev)
		}
	}
	// Let the last requests go out.
	for range 3 {
		l.dispatch(ei)
		time.Sleep(20 * time.Millisecond)
	}
	return connected, resumed
}

func emulate(l *libei, ei, dev uintptr) {
	l.startEmulating(dev, 1)
	if l.hasCap(dev, eiCapPointer) {
		l.motion(dev, 1.5, -2)
		l.button(dev, 0x110, true)
		l.scrollDelta(dev, 0, 3)
		l.scrollDiscrete(dev, 0, 240)
	}
	if l.hasCap(dev, eiCapKeyboard) {
		l.key(dev, 30, true)
		l.key(dev, 30, false)
	}
	if l.hasCap(dev, eiCapText) {
		l.keysym(dev, 0x41, true)
	}
	l.frame(dev, l.now(ei))
	l.stopEmulating(dev)
}

type pollFd struct {
	fd             int32
	events, revent int16
}

func pollWait(fds []pollFd, ms int) {
	_, _, _ = syscall.Syscall(syscall.SYS_POLL, uintptr(unsafe.Pointer(&fds[0])), uintptr(len(fds)), uintptr(ms))
}

func TestLibeiSenderEmulatesThroughTheServer(t *testing.T) {
	l := loadLibei(t)
	rec := &recorder{}
	client, err := Pair(rec)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(client.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	connected, resumed := drive(t, l, fd, []uintptr{eiCapPointer, eiCapKeyboard, eiCapButton, eiCapScroll, eiCapText}, 2, 5*time.Second)
	if !connected || resumed != 2 {
		t.Fatalf("connected %v, %d devices resumed; want a keyboard and a pointer", connected, resumed)
	}
	deadline := time.Now().Add(2 * time.Second)
	want := []string{"motion 1.5 -2", "button 272 true", "scroll 0 3", "discrete 0 240", "key 30 true", "key 30 false", "keysym 65 true"}
	for time.Now().Before(deadline) && len(rec.list()) < len(want) {
		time.Sleep(10 * time.Millisecond)
	}
	got := rec.list()
	slices.Sort(got)
	sorted := slices.Clone(want)
	slices.Sort(sorted)
	if !slices.Equal(got, sorted) {
		t.Errorf("handled %q, want %q", rec.list(), want)
	}
}

func TestLibeiPointerOnlyBindGetsNoKeyboard(t *testing.T) {
	l := loadLibei(t)
	rec := &recorder{}
	client, err := Pair(rec)
	if err != nil {
		t.Fatal(err)
	}
	fd, _ := syscall.Dup(int(client.Fd()))
	_ = client.Close()
	_, resumed := drive(t, l, fd, []uintptr{eiCapPointer, eiCapButton, eiCapScroll}, 2, 500*time.Millisecond)
	if resumed != 1 {
		t.Fatalf("%d devices resumed, want the pointer alone", resumed)
	}
	for _, e := range rec.list() {
		if e[:3] == "key" {
			t.Errorf("a keyboard event without a keyboard bind: %s", e)
		}
	}
}

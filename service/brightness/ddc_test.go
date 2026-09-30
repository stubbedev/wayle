package brightness

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakeMonitor speaks DDC/CI's luminance get/set on one fake bus.
type fakeMonitor struct {
	mu      sync.Mutex
	current uint16
	max     uint16
	// failWrites NAKs that many Set VCP writes before accepting.
	failWrites int
	// corrupt flips the reply checksum.
	corrupt bool
	reply   []byte
	writes  []uint16
	closes  int // how many opened connections were closed
}

var errNAK = errors.New("i2c: NAK")

func (m *fakeMonitor) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(p) < 3 || p[0] != ddcSubAddress || p[len(p)-1] != checksum(ddcAddress<<1, p[:len(p)-1]) {
		return 0, errNAK
	}
	payload := p[2 : len(p)-1]
	switch payload[0] {
	case 0x01: // Get VCP Feature
		body := []byte{0x02, 0x00, payload[1], 0x00, byte(m.max >> 8), byte(m.max), byte(m.current >> 8), byte(m.current)}
		if payload[1] != vcpLuminance {
			body[1] = 0x01
		}
		frame := append([]byte{ddcAddress << 1, 0x80 | byte(len(body))}, body...)
		sum := checksum((ddcAddress<<1|1)^ddcSubAddress, frame[1:])
		if m.corrupt {
			sum ^= 0xff
		}
		m.reply = append(frame, sum)
	case 0x03: // Set VCP Feature
		if m.failWrites > 0 {
			m.failWrites--
			return 0, errNAK
		}
		value := uint16(payload[2])<<8 | uint16(payload[3])
		m.current = value
		m.writes = append(m.writes, value)
	}
	return len(p), nil
}

func (m *fakeMonitor) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.reply == nil {
		return 0, errNAK
	}
	n := copy(p, m.reply)
	m.reply = nil
	return n, nil
}

func (m *fakeMonitor) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closes++
	return nil
}

func (m *fakeMonitor) written() []uint16 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uint16(nil), m.writes...)
}

// fakeConn is one open of a fake monitor's node, as each open(2) of
// /dev/i2c-N is its own fd.
type fakeConn struct {
	m      *fakeMonitor
	closed bool
}

func (c *fakeConn) Write(p []byte) (int, error) {
	if c.closed {
		return 0, os.ErrClosed
	}
	return c.m.Write(p)
}

func (c *fakeConn) Read(p []byte) (int, error) {
	if c.closed {
		return 0, os.ErrClosed
	}
	return c.m.Read(p)
}

func (c *fakeConn) Close() error {
	c.closed = true
	return c.m.Close()
}

// silentBus is a bus with no DDC/CI display (a sensor, HDMI audio).
type silentBus struct{}

func (silentBus) Write(p []byte) (int, error) { return len(p), nil }
func (silentBus) Read([]byte) (int, error)    { return 0, errNAK }
func (silentBus) Close() error                { return nil }

// fakeI2C repoints DevDir at a temp dir holding i2c-N nodes and routes
// openBus to the given buses; nodes without an entry fail to open.
type fakeI2C struct {
	mu    sync.Mutex
	dir   string
	buses map[uint32]i2cBus
}

func newFakeI2C(t *testing.T) *fakeI2C {
	t.Helper()
	f := &fakeI2C{dir: t.TempDir(), buses: map[uint32]i2cBus{}}
	origDir, origOpen, origSleep := DevDir, openBus, ddcSleep
	DevDir = f.dir
	openBus = func(path string) (i2cBus, error) {
		n, err := strconv.ParseUint(filepath.Base(path)[len(i2cNodePrefix):], 10, 32)
		if err != nil {
			return nil, err
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		bus, ok := f.buses[uint32(n)]
		if !ok {
			return nil, os.ErrPermission
		}
		if m, ok := bus.(*fakeMonitor); ok {
			return &fakeConn{m: m}, nil
		}
		return bus, nil
	}
	ddcSleep = func(time.Duration) {}
	t.Cleanup(func() { DevDir, openBus, ddcSleep = origDir, origOpen, origSleep })
	return f
}

func (f *fakeI2C) plug(t *testing.T, n uint32, bus i2cBus) {
	t.Helper()
	path := filepath.Join(f.dir, i2cNodePrefix+strconv.FormatUint(uint64(n), 10))
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if bus != nil {
		f.buses[n] = bus
	}
}

func (f *fakeI2C) unplug(t *testing.T, n uint32) {
	t.Helper()
	if err := os.Remove(filepath.Join(f.dir, i2cNodePrefix+strconv.FormatUint(uint64(n), 10))); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.buses, n)
}

func TestEncodeCommandMatchesTheWireFormat(t *testing.T) {
	// ddcutil's well-known Get VCP 0x10 request.
	want := []byte{0x51, 0x82, 0x01, 0x10, 0xac}
	got := encodeCommand([]byte{0x01, vcpLuminance})
	if string(got) != string(want) {
		t.Errorf("packet = % x, want % x", got, want)
	}
}

func TestDecodeReplyValidatesFraming(t *testing.T) {
	body := []byte{0x02, 0x00, 0x10, 0x00, 0x00, 0x64, 0x00, 0x32}
	frame := append([]byte{0x6e, 0x88}, body...)
	frame = append(frame, checksum(0x50, frame))
	payload, err := decodeReply(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	value, err := decodeVCP(payload)
	if err != nil || value.maximum != 100 || value.current != 50 {
		t.Errorf("vcp = %+v, %v; want 50 of 100", value, err)
	}

	bad := append([]byte(nil), frame...)
	bad[len(bad)-1] ^= 1
	if _, err := decodeReply(bad); !errors.Is(err, errDDCChecksum) {
		t.Errorf("bad checksum = %v", err)
	}
	if _, err := decodeReply(frame[:5]); !errors.Is(err, errDDCLength) {
		t.Errorf("short frame = %v", err)
	}
	noBit := append([]byte(nil), frame...)
	noBit[1] = 0x08
	if _, err := decodeReply(noBit); err == nil {
		t.Error("frame without the length bit decoded")
	}
}

func TestDecodeVCPRejectsErrorsAndBadShapes(t *testing.T) {
	for name, payload := range map[string][]byte{
		"unsupported": {0x02, 0x01, 0x10, 0, 0, 0, 0, 0},
		"unknown rc":  {0x02, 0x07, 0x10, 0, 0, 0, 0, 0},
		"opcode":      {0x03, 0x00, 0x10, 0, 0, 0, 0, 0},
		"length":      {0x02, 0x00, 0x10},
	} {
		if _, err := decodeVCP(payload); err == nil {
			t.Errorf("%s: decoded, want an error", name)
		}
	}
}

func noSleep(time.Duration) {}

func TestRetryReturnsFirstSuccessWithoutRetrying(t *testing.T) {
	calls := 0
	err := withRetry(noSleep, func() error { calls++; return nil })
	if err != nil || calls != 1 {
		t.Errorf("err = %v, calls = %d; want success after 1", err, calls)
	}
}

func TestRetryRecoversAfterTransientFailure(t *testing.T) {
	calls := 0
	err := withRetry(noSleep, func() error {
		calls++
		if calls < 2 {
			return errNAK
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Errorf("err = %v, calls = %d; want success after 2", err, calls)
	}
}

func TestRetryExhaustsAndReportsTheLastError(t *testing.T) {
	calls := 0
	err := withRetry(noSleep, func() error {
		calls++
		return errors.New("fail " + strconv.Itoa(calls))
	})
	if err == nil || err.Error() != "fail "+strconv.Itoa(ddcAttempts) || calls != ddcAttempts {
		t.Errorf("err = %v, calls = %d; want the attempt-%d error", err, calls, ddcAttempts)
	}
}

func TestHandleKeepsTheQuietTimeBetweenCommands(t *testing.T) {
	var slept []time.Duration
	h := &ddcHandle{bus: &fakeMonitor{max: 100}, sleep: func(d time.Duration) { slept = append(slept, d) }}
	if err := h.setVCP(vcpLuminance, 10); err != nil {
		t.Fatal(err)
	}
	if err := h.setVCP(vcpLuminance, 20); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 1 || slept[0] <= 0 || slept[0] > commandDelay {
		t.Errorf("slept %v, want one wait of up to %v before the second command", slept, commandDelay)
	}
}

func TestScanKeepsOnlyAnsweringMonitors(t *testing.T) {
	fake := newFakeI2C(t)
	fake.plug(t, 7, &fakeMonitor{current: 30, max: 100})
	fake.plug(t, 2, silentBus{})
	fake.plug(t, 4, nil)                                             // cannot be opened
	fake.plug(t, 9, &fakeMonitor{current: 1, max: 1, corrupt: true}) // garbled reply
	if err := os.WriteFile(filepath.Join(fake.dir, "i2c-bogus"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	m := newDDCManager()
	added, removed := m.refresh()
	if len(added) != 1 || added[0] != "ddci2c-7" || len(removed) != 0 {
		t.Fatalf("refresh = %v / %v, want ddci2c-7 added", added, removed)
	}
	devices := m.devices()
	if len(devices) != 1 {
		t.Fatalf("devices = %+v", devices)
	}
	if d := devices[0]; d.Type != TypeDDC || d.Brightness != 30 || d.Max != 100 || d.Percentage() != 30 {
		t.Errorf("device = %+v", d)
	}
}

func TestRefreshReconcilesUnplugs(t *testing.T) {
	fake := newFakeI2C(t)
	first := &fakeMonitor{current: 30, max: 100}
	fake.plug(t, 3, first)
	fake.plug(t, 5, &fakeMonitor{current: 60, max: 100})
	m := newDDCManager()
	if added, _ := m.refresh(); len(added) != 2 {
		t.Fatalf("added = %v", added)
	}
	fake.unplug(t, 5)
	added, removed := m.refresh()
	if len(added) != 0 || len(removed) != 1 || removed[0] != "ddci2c-5" {
		t.Errorf("refresh = %v / %v, want ddci2c-5 removed only", added, removed)
	}
	if first.closes == 0 {
		t.Error("the replaced handle was not closed")
	}
	if err := m.setRaw("ddci2c-5", 10); !errors.Is(err, ErrDDCUnavailable) {
		t.Errorf("write to an unplugged monitor = %v, want ErrDDCUnavailable", err)
	}
}

func TestSetRawClampsAndRetries(t *testing.T) {
	fake := newFakeI2C(t)
	monitor := &fakeMonitor{current: 10, max: 80, failWrites: 2}
	fake.plug(t, 1, monitor)
	m := newDDCManager()
	m.refresh()
	if err := m.setRaw("ddci2c-1", 500); err != nil {
		t.Fatalf("setRaw: %v", err)
	}
	if got := monitor.written(); len(got) != 1 || got[0] != 80 {
		t.Errorf("writes = %v, want one clamped to the max 80 after two NAKs", got)
	}
	if d := m.devices()[0]; d.Brightness != 80 {
		t.Errorf("cached level = %d, want 80", d.Brightness)
	}

	monitor.mu.Lock()
	monitor.failWrites = ddcAttempts
	monitor.mu.Unlock()
	if err := m.setRaw("ddci2c-1", 5); err == nil {
		t.Error("write NAK'd on every attempt: want an error")
	}
	if d := m.devices()[0]; d.Brightness != 80 {
		t.Errorf("a failed write changed the cached level to %d", d.Brightness)
	}
}

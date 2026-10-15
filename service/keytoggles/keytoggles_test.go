package keytoggles

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// Only a lock key's release is a toggle: its press, a repeat, other
// keys, and other event types are not.
func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		typ, code uint16
		value     int32
		want      Key
		ok        bool
	}{
		{evKey, keyCapsLock, 0, CapsLock, true},
		{evKey, keyNumLock, 0, NumLock, true},
		{evKey, keyScrollLock, 0, ScrollLock, true},
		{evKey, keyCapsLock, 1, 0, false},
		{evKey, keyCapsLock, 2, 0, false},
		{evKey, 30, 0, 0, false},
		{evLed, keyCapsLock, 0, 0, false},
	} {
		got, ok := detect(tc.typ, tc.code, tc.value)
		if ok != tc.ok || got != tc.want {
			t.Errorf("detect(%d, %d, %d) = %d, %v; want %d, %v", tc.typ, tc.code, tc.value, got, ok, tc.want, tc.ok)
		}
	}
}

// The ioctl requests are the kernel's EVIOCGBIT(0, 4) and EVIOCGLED(2).
func TestIoctlNumbers(t *testing.T) {
	if got := ioc(eviocgbitBase, evMaxBytes); got != 0x80044520 {
		t.Errorf("EVIOCGBIT(0, 4) = %#x", got)
	}
	if got := ioc(eviocgledIocNr, ledMaxBytes); got != 0x80024519 {
		t.Errorf("EVIOCGLED(2) = %#x", got)
	}
}

func TestLedBits(t *testing.T) {
	mask := []byte{1<<ledCapsL | 1<<ledScrollL}
	if !hasBit(mask, ledBit(CapsLock)) || !hasBit(mask, ledBit(ScrollLock)) || hasBit(mask, ledBit(NumLock)) {
		t.Errorf("mask %08b read wrong", mask[0])
	}
	if hasBit(mask, 9) {
		t.Error("a bit past the mask read as set")
	}
}

func event(typ, code uint16, value int32) []byte {
	b := make([]byte, eventSize)
	binary.NativeEndian.PutUint16(b[16:], typ)
	binary.NativeEndian.PutUint16(b[18:], code)
	binary.NativeEndian.PutUint32(b[20:], uint32(value))
	return b
}

// A device stream yields one toggle per lock key release, and reading
// ends when the device goes away.
func TestReadForwardsReleases(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	done := make(chan struct{})
	go func() {
		read(r, func(e Event) { got = append(got, e) })
		close(done)
	}()
	var stream []byte
	for _, e := range [][]byte{
		event(evKey, keyCapsLock, 1),
		event(evKey, keyCapsLock, 0),
		event(evKey, 30, 0),
		event(evKey, keyNumLock, 0),
	} {
		stream = append(stream, e...)
	}
	if _, err := w.Write(stream); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	<-done
	_ = r.Close()
	want := []Event{{Key: CapsLock}, {Key: NumLock}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("events %+v, want %+v (an unreadable LED reads off)", got, want)
	}
}

// Files that are not evdev keyboards are skipped, and stop returns.
func TestWatchSkipsNonKeyboards(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "event0"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	stop := watchDir(dir, func(Event) { called = true })
	stop()
	if called {
		t.Error("a non-keyboard produced a toggle")
	}
}

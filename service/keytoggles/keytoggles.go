// Package keytoggles watches the keyboards' lock keys through evdev
// (the Rust shell's osd/toggles.rs): a Caps, Num, or Scroll Lock
// release reports the key and its LED state once the kernel settled it.
package keytoggles

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Key is one lock key.
type Key uint8

// The watched lock keys.
const (
	CapsLock Key = iota
	NumLock
	ScrollLock
)

// Event is a lock key released with its LED now on or off.
type Event struct {
	Key    Key
	Active bool
}

// ledDelay is LED_READ_DELAY: the LED state lags the key release.
const ledDelay = 50 * time.Millisecond

// The evdev constants this package reads (linux/input-event-codes.h).
const (
	evKey          = 0x01
	evLed          = 0x11
	keyCapsLock    = 58
	keyNumLock     = 69
	keyScrollLock  = 70
	ledNumL        = 0
	ledCapsL       = 1
	ledScrollL     = 2
	eventSize      = 24 // struct input_event on 64-bit: timeval, type, code, value
	evMaxBytes     = 4  // EV_MAX 0x1f fits in 32 bits
	ledMaxBytes    = 2  // LED_MAX 0x0f fits in 16 bits
	iocRead        = 2
	iocDirShift    = 30
	iocSizeShift   = 16
	iocTypeShift   = 8
	evdevIocType   = 'E'
	eviocgbitBase  = 0x20
	eviocgledIocNr = 0x19
)

func ioc(nr, size uintptr) uintptr {
	return iocRead<<iocDirShift | size<<iocSizeShift | evdevIocType<<iocTypeShift | nr
}

// detect is detect_toggle: a key release (value 0) of a lock key.
func detect(typ, code uint16, value int32) (Key, bool) {
	if typ != evKey || value != 0 {
		return 0, false
	}
	switch code {
	case keyCapsLock:
		return CapsLock, true
	case keyNumLock:
		return NumLock, true
	case keyScrollLock:
		return ScrollLock, true
	}
	return 0, false
}

// ledBit is the LED a key drives.
func ledBit(k Key) uint {
	switch k {
	case NumLock:
		return ledNumL
	case ScrollLock:
		return ledScrollL
	}
	return ledCapsL
}

// hasBit reads bit n of an evdev bitmask.
func hasBit(mask []byte, n uint) bool {
	return int(n/8) < len(mask) && mask[n/8]&(1<<(n%8)) != 0
}

// parseEvent decodes one struct input_event.
func parseEvent(b []byte) (typ, code uint16, value int32) {
	return binary.NativeEndian.Uint16(b[16:]), binary.NativeEndian.Uint16(b[18:]), int32(binary.NativeEndian.Uint32(b[20:]))
}

// ioctlBytes fills buf through a read ioctl.
func ioctlBytes(f *os.File, req uintptr, buf []byte) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 { //nolint:gosec // audited: the request's size is len(buf)
		return errno
	}
	return nil
}

// isKeyboard is find_keyboards' filter: a device with keys and LEDs.
func isKeyboard(f *os.File) bool {
	types := make([]byte, evMaxBytes)
	if err := ioctlBytes(f, ioc(eviocgbitBase, evMaxBytes), types); err != nil {
		return false
	}
	return hasBit(types, evKey) && hasBit(types, evLed)
}

// ledOn is read_led_state: whether k's LED is lit; false when it
// cannot be read.
func ledOn(f *os.File, k Key) bool {
	leds := make([]byte, ledMaxBytes)
	if err := ioctlBytes(f, ioc(eviocgledIocNr, ledMaxBytes), leds); err != nil {
		return false
	}
	return hasBit(leds, ledBit(k))
}

// Watch opens every keyboard under /dev/input and calls fn, from the
// device's goroutine, for each lock key release. With none readable it
// logs how to grant access, as the Rust shell does. stop closes the
// devices and waits for the readers.
func Watch(fn func(Event)) (stop func()) {
	return watchDir("/dev/input", fn)
}

func watchDir(dir string, fn func(Event)) func() {
	paths, err := filepath.Glob(filepath.Join(dir, "event*"))
	if err != nil {
		log.Printf("keytoggles: list %s: %v", dir, err)
	}
	slices.Sort(paths)
	var (
		files []*os.File
		wg    sync.WaitGroup
	)
	for _, p := range paths {
		f, err := os.Open(p) //nolint:gosec // the evdev nodes under the fixed input dir
		if err != nil {
			continue
		}
		if !isKeyboard(f) {
			_ = f.Close()
			continue
		}
		files = append(files, f)
		wg.Go(func() { read(f, fn) })
	}
	if len(files) == 0 {
		log.Printf("toggle OSD disabled: no keyboard devices accessible, run 'sudo usermod -aG input $USER' and re-login")
	} else {
		log.Printf("keytoggles: %d keyboards found for toggle monitoring", len(files))
	}
	return func() {
		for _, f := range files {
			_ = f.Close()
		}
		wg.Wait()
	}
}

// read forwards one device's lock key releases until it closes.
func read(f *os.File, fn func(Event)) {
	buf := make([]byte, eventSize*64)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if !errors.Is(err, os.ErrClosed) && !errors.Is(err, io.EOF) {
				log.Printf("keytoggles: %s: %v", f.Name(), fmt.Errorf("read: %w", err))
			}
			return
		}
		for off := 0; off+eventSize <= n; off += eventSize {
			k, ok := detect(parseEvent(buf[off : off+eventSize]))
			if !ok {
				continue
			}
			time.Sleep(ledDelay)
			fn(Event{Key: k, Active: ledOn(f, k)})
		}
	}
}

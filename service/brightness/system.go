package brightness

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/internal/fswatch"
)

// hotplugSettle is the pause before a hotplug-triggered DDC scan, so
// DRM has settled and the monitor's I²C bus answers the probe.
const hotplugSettle = 500 * time.Millisecond

// driftInterval re-ticks subscribers as a cheap correction for level
// changes no watch reports (firmware hotkeys that never touch the
// brightness attribute); a var so tests can quiet it.
var driftInterval = time.Second

// System is the real source (backend/mod.rs): sysfs devices written
// through logind with a sysfs fallback, plus, when external monitors
// are enabled, DDC/CI monitors found by a detached scan and re-scanned
// on DRM hotplug.
type System struct {
	bus *dbus.Conn // logind; nil means sysfs writes only
	ddc *ddcManager

	mu      sync.Mutex
	subs    map[chan struct{}]struct{}
	pending map[string]uint32 // latest DDC target per monitor
	writing map[string]bool   // a writer goroutine owns the monitor

	watch  *fswatch.Watcher
	events ueventReader
	stop   chan struct{}
	done   sync.WaitGroup
	// settle is hotplugSettle; tests shorten it.
	settle time.Duration
}

// NewSystem starts the source. external enables DDC/CI discovery
// (the enable-external key); the first scan runs detached so internal
// panels are usable at once. A missing logind or uevent socket
// degrades (sysfs writes, no hotplug) instead of failing.
func NewSystem(external bool) *System {
	s := &System{
		subs:    map[chan struct{}]struct{}{},
		pending: map[string]uint32{},
		writing: map[string]bool{},
		stop:    make(chan struct{}),
		settle:  hotplugSettle,
	}
	if conn, err := sessionBus(); err == nil {
		s.bus = conn
	}
	if external {
		s.ddc = newDDCManager()
	}
	s.start()
	return s
}

func (s *System) start() {
	if w, err := fswatch.New(unix.IN_MODIFY, false); err == nil {
		s.watch = w
		s.rewatch()
	} else {
		log.Printf("brightness: %v", err)
	}
	if reader, err := listenUevents(); err == nil {
		s.events = reader
	} else {
		log.Printf("brightness: no hotplug: %v", err)
	}
	s.done.Go(s.loop)
	if s.ddc != nil {
		s.scanDDC(0)
	}
}

// Close stops the watchers and waits for the goroutines; later calls
// are no-ops.
func (s *System) Close() error {
	select {
	case <-s.stop:
		return nil
	default:
	}
	close(s.stop)
	if s.events != nil {
		_ = s.events.Close()
	}
	if s.watch != nil {
		_ = s.watch.Close()
	}
	s.done.Wait()
	return nil
}

// rewatch (re-)adds a watch per device directory; adding an existing
// one is a no-op for inotify.
func (s *System) rewatch() {
	entries, err := os.ReadDir(BacklightDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		_ = s.watch.Add(filepath.Join(BacklightDir, entry.Name()))
	}
}

// loop fans the watches out to subscribers.
func (s *System) loop() {
	var changed <-chan struct{}
	if s.watch != nil {
		changed = s.watch.Changed()
	}
	hotplug := make(chan uevent)
	if s.events != nil {
		s.done.Go(func() {
			for {
				ev, err := s.events.Next()
				if err != nil {
					return
				}
				select {
				case hotplug <- ev:
				case <-s.stop:
					return
				}
			}
		})
	}
	drift := time.NewTicker(driftInterval)
	defer drift.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-changed:
			s.broadcast()
		case <-drift.C:
			s.broadcast()
		case ev := <-hotplug:
			switch {
			case ev.isBacklightHotplug():
				if s.watch != nil {
					s.rewatch()
				}
				s.broadcast()
			case ev.isDRMHotplug() && s.ddc != nil:
				s.scanDDC(s.settle)
			}
		}
	}
}

// scanDDC re-scans detached after settle and ticks subscribers when
// the monitor set changed (scan_ddc).
func (s *System) scanDDC(settle time.Duration) {
	s.done.Go(func() {
		if settle > 0 {
			select {
			case <-time.After(settle):
			case <-s.stop:
				return
			}
		}
		added, removed := s.ddc.refresh()
		for _, name := range added {
			log.Printf("brightness: external DDC monitor connected: %s", name)
		}
		for _, name := range removed {
			log.Printf("brightness: external DDC monitor disconnected: %s", name)
		}
		if len(added)+len(removed) > 0 {
			s.broadcast()
		}
	})
}

func (s *System) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Devices reads every sysfs device, then the DDC monitors from their
// cached levels (a DDC read is too slow to make per refresh).
func (s *System) Devices(context.Context) ([]Device, error) {
	devices := Enumerate()
	if s.ddc != nil {
		devices = append(devices, s.ddc.devices()...)
	}
	return devices, nil
}

// clampPercent bounds a requested percentage to 0..100.
func clampPercent(percent float64) float64 {
	return min(max(percent, 0), 100)
}

// Set applies a clamped percentage. A DDC monitor's write is handed to
// its writer goroutine and Set returns at once: DDC I/O takes tens of
// milliseconds a transaction, and a slider drag coalesces to the
// latest value instead of queueing every step. Write failures are
// logged, as the Rust caller does with the error it gets back.
func (s *System) Set(_ context.Context, name string, percent float64) error {
	fraction := clampPercent(percent) / 100
	if s.ddc != nil {
		if d, ok := s.ddc.display(name); ok {
			s.queueDDC(name, uint32(fraction*float64(d.max)))
			return nil
		}
	}
	device, err := ReadDevice(name)
	if err != nil {
		return err
	}
	value := uint32(fraction * float64(device.Max))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bus != nil {
		if err := setLogind(s.bus, name, value); err == nil {
			return nil
		}
	}
	return writeSysfs(name, value)
}

// queueDDC records the latest target and starts the monitor's writer
// if none is running.
func (s *System) queueDDC(name string, value uint32) {
	s.mu.Lock()
	s.pending[name] = value
	if s.writing[name] {
		s.mu.Unlock()
		return
	}
	s.writing[name] = true
	s.mu.Unlock()
	s.done.Go(func() {
		for {
			s.mu.Lock()
			target, ok := s.pending[name]
			delete(s.pending, name)
			if !ok {
				delete(s.writing, name)
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			if err := s.ddc.setRaw(name, target); err != nil {
				log.Printf("%v", err)
				continue
			}
			// DDC has no kernel notification; report the new level.
			s.broadcast()
		}
	})
}

// Subscribe registers for change ticks until ctx ends or stop.
func (s *System) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	select {
	case <-s.stop:
		return nil, nil, errors.New("brightness: source closed")
	default:
	}
	internal := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs[internal] = struct{}{}
	s.mu.Unlock()
	ticks := make(chan struct{}, 1)
	quit := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(quit) }) }
	go func() {
		defer close(ticks)
		defer func() {
			s.mu.Lock()
			delete(s.subs, internal)
			s.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-quit:
				return
			case <-s.stop:
				return
			case <-internal:
				select {
				case ticks <- struct{}{}:
				default:
				}
			}
		}
	}()
	return ticks, stop, nil
}

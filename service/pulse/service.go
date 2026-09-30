// Package pulse is the audio service, the Go port of
// crates/wayle-audio: sinks, sources, and per-application streams kept
// live from PulseAudio's subscription events, the default devices,
// per-channel volume and mute, ports, stream routing, and record
// streams, all over the native protocol (package native), so it works
// against PulseAudio and pipewire-pulse alike with no libpulse.
package pulse

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"

	"github.com/stubbedev/wayle/service/pulse/native"
)

// clientName is the application.name the Rust backend's context used.
const clientName = "wayle-pulse"

// subscriptionMask is backend/events's interest set.
const subscriptionMask = native.MaskSink | native.MaskSource | native.MaskSinkInput |
	native.MaskSourceOutput | native.MaskServer

// ErrNoDefaultDevice reports that the server names no default device
// of the requested kind, or names one not discovered yet.
var ErrNoDefaultDevice = errors.New("pulse: no default device")

// ErrEmptyVolume rejects a volume with no channels (PulseAudio would
// answer PA_ERR_INVALID).
var ErrEmptyVolume = errors.New("pulse: volume has no channels")

// Service is the live audio state. Every change PulseAudio announces
// is folded in as it arrives; the getters return snapshots that the
// caller owns. It is safe for concurrent use.
type Service struct {
	conn  *native.Conn
	ready chan struct{} // closes once discovery filled the state
	abort chan struct{} // closes when discovery failed

	mu            sync.RWMutex
	outputs       map[uint32]OutputDevice
	inputs        map[uint32]InputDevice
	streams       map[StreamKey]AudioStream
	defaultSink   string
	defaultSource string
	subs          map[int]chan struct{}
	nextSub       int
}

// Connect dials the server (server "" resolves like libpulse:
// $PULSE_SERVER, then $XDG_RUNTIME_DIR/pulse/native), subscribes, and
// discovers every device, stream, and default before returning, so the
// first snapshot is complete.
func Connect(ctx context.Context, server string) (*Service, error) {
	s := &Service{
		ready:   make(chan struct{}),
		abort:   make(chan struct{}),
		outputs: map[uint32]OutputDevice{},
		inputs:  map[uint32]InputDevice{},
		streams: map[StreamKey]AudioStream{},
		subs:    map[int]chan struct{}{},
	}
	conn, err := native.Dial(ctx, native.Options{
		Server:      server,
		ClientName:  clientName,
		OnSubscribe: s.handleEvent,
	})
	if err != nil {
		return nil, fmt.Errorf("pulse: cannot connect to the server: %w", err)
	}
	s.conn = conn
	if err := s.discover(ctx); err != nil {
		close(s.abort)
		_ = conn.Close()
		return nil, err
	}
	close(s.ready)
	go func() {
		<-conn.Done()
		s.closeSubscribers()
	}()
	return s, nil
}

// discover is the initial refresh (Devices, Streams, ServerInfo),
// after the subscription so no change in between is lost: events
// queue until ready closes.
func (s *Service) discover(ctx context.Context) error {
	if err := s.conn.Subscribe(ctx, subscriptionMask); err != nil {
		return err
	}
	sinks, err := s.conn.Sinks(ctx)
	if err != nil {
		return err
	}
	sources, err := s.conn.Sources(ctx)
	if err != nil {
		return err
	}
	inputs, err := s.conn.SinkInputs(ctx)
	if err != nil {
		return err
	}
	outputs, err := s.conn.SourceOutputs(ctx)
	if err != nil {
		return err
	}
	info, err := s.conn.ServerInfo(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range sinks {
		s.outputs[d.Index] = fromSink(d)
	}
	for _, d := range sources {
		s.inputs[d.Index] = fromSource(d)
	}
	for _, st := range inputs {
		s.streams[StreamKey{Index: st.Index, Type: StreamPlayback}] = fromStream(st, StreamPlayback)
	}
	for _, st := range outputs {
		s.streams[StreamKey{Index: st.Index, Type: StreamRecord}] = fromStream(st, StreamRecord)
	}
	s.defaultSink, s.defaultSource = info.DefaultSink, info.DefaultSource
	return nil
}

// Close disconnects; subscriber channels close.
func (s *Service) Close() error { return s.conn.Close() }

// Done closes when the connection is gone. Like the Rust backend, the
// service does not reconnect: the state freezes at its last values.
func (s *Service) Done() <-chan struct{} { return s.conn.Done() }

// handleEvent folds one subscription event (backend/events): a
// removal drops the object, a new or changed object is re-queried by
// index, a server change re-reads the defaults. Queries that race a
// removal (no such entity) are dropped, as the Rust callbacks never
// fire for them.
func (s *Service) handleEvent(ev native.SubscribeEvent) {
	// Events queue until discovery is done; the channel also orders
	// the s.conn write before this goroutine reads it.
	select {
	case <-s.ready:
	case <-s.abort:
		return
	}
	ctx := context.Background()
	var changed bool
	var err error
	switch ev.Facility {
	case native.FacilitySink:
		changed, err = foldObject(s, ev, s.outputs, s.conn.Sink, fromSink)
	case native.FacilitySource:
		changed, err = foldObject(s, ev, s.inputs, s.conn.Source, fromSource)
	case native.FacilitySinkInput:
		changed, err = s.foldStream(ctx, ev, StreamPlayback, s.conn.SinkInput)
	case native.FacilitySourceOutput:
		changed, err = s.foldStream(ctx, ev, StreamRecord, s.conn.SourceOutput)
	case native.FacilityServer:
		if ev.Operation == native.OpChange {
			changed, err = s.refreshDefaults(ctx)
		}
	}
	if err != nil && !errors.Is(err, native.ErrNoEntity) {
		select {
		case <-s.conn.Done():
		default:
			log.Printf("pulse: refresh after %v: %v", ev, err)
		}
	}
	if changed {
		s.notify()
	}
}

func foldObject[T any](s *Service, ev native.SubscribeEvent, m map[uint32]T,
	query func(context.Context, uint32) (native.DeviceInfo, error), convert func(native.DeviceInfo) T,
) (bool, error) {
	if ev.Operation == native.OpRemove {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, had := m[ev.Index]
		delete(m, ev.Index)
		return had, nil
	}
	info, err := query(context.Background(), ev.Index)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	m[ev.Index] = convert(info)
	s.mu.Unlock()
	return true, nil
}

func (s *Service) foldStream(ctx context.Context, ev native.SubscribeEvent, t StreamType,
	query func(context.Context, uint32) (native.StreamInfo, error),
) (bool, error) {
	key := StreamKey{Index: ev.Index, Type: t}
	if ev.Operation == native.OpRemove {
		s.mu.Lock()
		defer s.mu.Unlock()
		_, had := s.streams[key]
		delete(s.streams, key)
		return had, nil
	}
	info, err := query(ctx, ev.Index)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	s.streams[key] = fromStream(info, t)
	s.mu.Unlock()
	return true, nil
}

func (s *Service) refreshDefaults(ctx context.Context) (bool, error) {
	info, err := s.conn.ServerInfo(ctx)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := s.defaultSink != info.DefaultSink || s.defaultSource != info.DefaultSource
	s.defaultSink, s.defaultSource = info.DefaultSink, info.DefaultSource
	return changed, nil
}

// Subscribe ticks after every applied change; ticks coalesce, so a
// slow reader sees the latest state on its next read rather than a
// backlog. The channel closes when ctx ends or the connection dies;
// stop unsubscribes early.
func (s *Service) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	select {
	case <-s.conn.Done():
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("pulse: subscribe: %w", s.conn.Err())
	default:
	}
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	s.mu.Unlock()
	quit := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(quit)
			s.mu.Lock()
			if c, ok := s.subs[id]; ok {
				delete(s.subs, id)
				close(c)
			}
			s.mu.Unlock()
		})
	}
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-quit:
		case <-s.conn.Done():
		}
	}()
	return ch, stop, nil
}

func (s *Service) notify() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Service) closeSubscribers() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ch := range s.subs {
		delete(s.subs, id)
		close(ch)
	}
}

func sortedValues[K comparable, V any](m map[K]V, less func(a, b V) int, clone func(V) V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, clone(v))
	}
	slices.SortFunc(out, less)
	return out
}

func byDeviceIndex[T interface{ key() DeviceKey }](a, b T) int {
	return cmp.Compare(a.key().Index, b.key().Index)
}

func (d OutputDevice) key() DeviceKey { return d.Key }
func (d InputDevice) key() DeviceKey  { return d.Key }

func (d OutputDevice) clone() OutputDevice {
	d.Device = d.Device.clone()
	return d
}

func (d InputDevice) clone() InputDevice {
	d.Device = d.Device.clone()
	return d
}

// OutputDevices lists every sink by index.
func (s *Service) OutputDevices() []OutputDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedValues(s.outputs, byDeviceIndex[OutputDevice], OutputDevice.clone)
}

// InputDevices lists every source by index, monitors included.
func (s *Service) InputDevices() []InputDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedValues(s.inputs, byDeviceIndex[InputDevice], InputDevice.clone)
}

// PlaybackStreams lists the applications playing audio.
func (s *Service) PlaybackStreams() []AudioStream { return s.streamsOf(StreamPlayback) }

// RecordingStreams lists the applications recording audio.
func (s *Service) RecordingStreams() []AudioStream { return s.streamsOf(StreamRecord) }

func (s *Service) streamsOf(t StreamType) []AudioStream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []AudioStream
	for k, st := range s.streams {
		if k.Type == t {
			out = append(out, st.clone())
		}
	}
	slices.SortFunc(out, func(a, b AudioStream) int { return cmp.Compare(a.Key.Index, b.Key.Index) })
	return out
}

// DefaultOutput is the default sink. The default resolves by name on
// every read, so a default naming a sink not discovered yet appears
// the moment it is, rather than the previous device lingering.
func (s *Service) DefaultOutput() (OutputDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return findByName(s.outputs, s.defaultSink, func(d OutputDevice) string { return d.Name })
}

// DefaultInput is the default source.
func (s *Service) DefaultInput() (InputDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return findByName(s.inputs, s.defaultSource, func(d InputDevice) string { return d.Name })
}

func findByName[T interface{ clone() T }](m map[uint32]T, name string, nameOf func(T) string) (T, bool) {
	var zero T
	if name == "" {
		return zero, false
	}
	for _, d := range m {
		if nameOf(d) == name {
			return d.clone(), true
		}
	}
	return zero, false
}

// OutputDevice snapshots one sink.
func (s *Service) OutputDevice(index uint32) (OutputDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.outputs[index]
	if !ok {
		return OutputDevice{}, &DeviceNotFoundError{Key: DeviceKey{Index: index, Type: DeviceOutput}}
	}
	return d.clone(), nil
}

// InputDevice snapshots one source.
func (s *Service) InputDevice(index uint32) (InputDevice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.inputs[index]
	if !ok {
		return InputDevice{}, &DeviceNotFoundError{Key: DeviceKey{Index: index, Type: DeviceInput}}
	}
	return d.clone(), nil
}

// AudioStream snapshots one stream.
func (s *Service) AudioStream(key StreamKey) (AudioStream, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.streams[key]
	if !ok {
		return AudioStream{}, &StreamNotFoundError{Key: key}
	}
	return st.clone(), nil
}

// device looks a key up, returning its name.
func (s *Service) device(key DeviceKey) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch key.Type {
	case DeviceOutput:
		if d, ok := s.outputs[key.Index]; ok {
			return d.Name, nil
		}
	case DeviceInput:
		if d, ok := s.inputs[key.Index]; ok {
			return d.Name, nil
		}
	}
	return "", &DeviceNotFoundError{Key: key}
}

func (s *Service) hasStream(key StreamKey) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.streams[key]; !ok {
		return &StreamNotFoundError{Key: key}
	}
	return nil
}

// SetDeviceVolume sets a device's per-channel volume.
func (s *Service) SetDeviceVolume(ctx context.Context, key DeviceKey, v Volume) error {
	if v.Channels() == 0 {
		return ErrEmptyVolume
	}
	if _, err := s.device(key); err != nil {
		return err
	}
	if key.Type == DeviceOutput {
		return s.conn.SetSinkVolume(ctx, key.Index, v.toPulse())
	}
	return s.conn.SetSourceVolume(ctx, key.Index, v.toPulse())
}

// SetDeviceMute mutes or unmutes a device.
func (s *Service) SetDeviceMute(ctx context.Context, key DeviceKey, muted bool) error {
	if _, err := s.device(key); err != nil {
		return err
	}
	if key.Type == DeviceOutput {
		return s.conn.SetSinkMute(ctx, key.Index, muted)
	}
	return s.conn.SetSourceMute(ctx, key.Index, muted)
}

// SetDevicePort switches a device's active port (headphones vs
// speakers).
func (s *Service) SetDevicePort(ctx context.Context, key DeviceKey, port string) error {
	if _, err := s.device(key); err != nil {
		return err
	}
	if key.Type == DeviceOutput {
		return s.conn.SetSinkPort(ctx, key.Index, port)
	}
	return s.conn.SetSourcePort(ctx, key.Index, port)
}

// SetDefault makes a device the default of its kind.
func (s *Service) SetDefault(ctx context.Context, key DeviceKey) error {
	name, err := s.device(key)
	if err != nil {
		return err
	}
	if key.Type == DeviceOutput {
		return s.conn.SetDefaultSink(ctx, name)
	}
	return s.conn.SetDefaultSource(ctx, name)
}

// SetStreamVolume sets a stream's per-channel volume.
func (s *Service) SetStreamVolume(ctx context.Context, key StreamKey, v Volume) error {
	if v.Channels() == 0 {
		return ErrEmptyVolume
	}
	if err := s.hasStream(key); err != nil {
		return err
	}
	if key.Type == StreamPlayback {
		return s.conn.SetSinkInputVolume(ctx, key.Index, v.toPulse())
	}
	return s.conn.SetSourceOutputVolume(ctx, key.Index, v.toPulse())
}

// SetStreamMute mutes or unmutes a stream.
func (s *Service) SetStreamMute(ctx context.Context, key StreamKey, muted bool) error {
	if err := s.hasStream(key); err != nil {
		return err
	}
	if key.Type == StreamPlayback {
		return s.conn.SetSinkInputMute(ctx, key.Index, muted)
	}
	return s.conn.SetSourceOutputMute(ctx, key.Index, muted)
}

// MoveStream routes a stream to another device: a playback stream to
// a sink, a recording stream to a source.
func (s *Service) MoveStream(ctx context.Context, key StreamKey, device DeviceKey) error {
	if device.Type != key.Type.deviceType() {
		return fmt.Errorf("pulse: a %s stream cannot move to an %s device", key.Type, device.Type)
	}
	if err := s.hasStream(key); err != nil {
		return err
	}
	if _, err := s.device(device); err != nil {
		return err
	}
	if key.Type == StreamPlayback {
		return s.conn.MoveSinkInput(ctx, key.Index, device.Index)
	}
	return s.conn.MoveSourceOutput(ctx, key.Index, device.Index)
}

package pulse

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/stubbedev/wayle/service/pulse/native"
)

// DeviceType is input (source) or output (sink) (types/device.rs).
type DeviceType int

// Device types.
const (
	DeviceInput DeviceType = iota
	DeviceOutput
)

func (t DeviceType) String() string {
	if t == DeviceOutput {
		return "Output"
	}
	return "Input"
}

// DeviceKey identifies a device: PulseAudio indexes sinks and sources
// separately, so the index alone is ambiguous.
type DeviceKey struct {
	Index uint32
	Type  DeviceType
}

// DeviceState is the device's run state.
type DeviceState int

// Device states; an invalid PulseAudio state reads as Offline
// (conversion/device.rs's fallback).
const (
	StateRunning DeviceState = iota
	StateIdle
	StateSuspended
	StateOffline
)

var deviceStateNames = [...]string{"Running", "Idle", "Suspended", "Offline"}

// String is the Rust variant name the DBus info map carries.
func (s DeviceState) String() string { return deviceStateNames[s] }

func deviceState(raw uint32) DeviceState {
	switch raw {
	case native.StateRunning:
		return StateRunning
	case native.StateIdle:
		return StateIdle
	case native.StateSuspended:
		return StateSuspended
	}
	return StateOffline
}

// DevicePort is one port; Available is false only when PulseAudio
// reports the port unavailable (unknown counts as available).
type DevicePort struct {
	Name        string
	Description string
	Priority    uint32
	Available   bool
}

// AudioFormat is one supported format: the encoding's name and its
// properties.
type AudioFormat struct {
	Encoding   string
	Properties map[string]string
}

// SampleSpec is the device or stream sample format.
type SampleSpec = native.SampleSpec

// ChannelMap is the per-channel position list.
type ChannelMap = native.ChannelMap

// OptIndex is an object index that may be absent (PA_INVALID_INDEX on
// the wire, Option<u32> in wayle-audio).
type OptIndex struct {
	index uint32
	ok    bool
}

func optIndex(raw uint32) OptIndex {
	return OptIndex{index: raw, ok: raw != native.InvalidIndex}
}

// Get returns the index and whether it is set.
func (o OptIndex) Get() (uint32, bool) { return o.index, o.ok }

// Device is what sinks and sources share (types/device.rs's
// DeviceInfo).
type Device struct {
	Key               DeviceKey
	Name              string
	Description       string
	Card              OptIndex
	OwnerModule       OptIndex
	Driver            string
	State             DeviceState
	Volume            Volume
	BaseVolume        Volume
	VolumeSteps       uint32
	Muted             bool
	Properties        map[string]string
	Ports             []DevicePort
	ActivePort        string // empty when the device has no active port
	Formats           []AudioFormat
	SampleSpec        SampleSpec
	ChannelMap        ChannelMap
	Latency           time.Duration
	ConfiguredLatency time.Duration
	Flags             uint32
}

// clone deep-copies the reference fields, so snapshots handed out
// never alias the service's state.
func (d Device) clone() Device {
	d.Properties = maps.Clone(d.Properties)
	d.Ports = slices.Clone(d.Ports)
	d.Formats = slices.Clone(d.Formats)
	for i := range d.Formats {
		d.Formats[i].Properties = maps.Clone(d.Formats[i].Properties)
	}
	d.ChannelMap = slices.Clone(d.ChannelMap)
	return d
}

// OutputDevice is a sink (core/device/output).
type OutputDevice struct {
	Device
	MonitorSource     uint32
	MonitorSourceName string
}

// InputDevice is a source (core/device/input); monitors of sinks are
// sources too.
type InputDevice struct {
	Device
	MonitorOfSink     OptIndex
	MonitorOfSinkName string // empty unless IsMonitor
}

// IsMonitor reports whether the source monitors a sink.
func (d InputDevice) IsMonitor() bool {
	_, ok := d.MonitorOfSink.Get()
	return ok
}

// StreamType is playback (sink input) or record (source output).
type StreamType int

// Stream types.
const (
	StreamPlayback StreamType = iota
	StreamRecord
)

func (t StreamType) String() string {
	if t == StreamRecord {
		return "Record"
	}
	return "Playback"
}

// deviceType is the device kind a stream of this type attaches to.
func (t StreamType) deviceType() DeviceType {
	if t == StreamRecord {
		return DeviceInput
	}
	return DeviceOutput
}

// StreamKey identifies a stream.
type StreamKey struct {
	Index uint32
	Type  StreamType
}

// StreamState is a stream's state. PulseAudio's introspection only
// distinguishes corked from running, so those are the two the service
// produces (conversion/stream.rs).
type StreamState int

// Stream states.
const (
	StreamRunning StreamState = iota
	StreamCorked
)

func (s StreamState) String() string {
	if s == StreamCorked {
		return "Corked"
	}
	return "Running"
}

// MediaInfo is the stream's media metadata; empty fields are unset.
type MediaInfo struct {
	Title    string
	Artist   string
	Album    string
	IconName string
}

// AudioStream is one application stream (core/stream). The
// application fields are empty, and PID zero, when the client did not
// set them.
type AudioStream struct {
	Key             StreamKey
	Name            string
	ApplicationName string
	Binary          string
	PID             uint32
	OwnerModule     OptIndex
	Client          OptIndex
	State           StreamState
	Volume          Volume
	Muted           bool
	Corked          bool
	HasVolume       bool
	VolumeWritable  bool
	DeviceIndex     uint32
	SampleSpec      SampleSpec
	ChannelMap      ChannelMap
	Properties      map[string]string
	Media           MediaInfo
	BufferLatency   time.Duration
	DeviceLatency   time.Duration
	ResampleMethod  string
	Driver          string
	Format          string
}

func (s AudioStream) clone() AudioStream {
	s.Properties = maps.Clone(s.Properties)
	s.ChannelMap = slices.Clone(s.ChannelMap)
	return s
}

// DeviceNotFoundError reports a lookup of a device the service does
// not hold (error.rs's DeviceNotFound).
type DeviceNotFoundError struct {
	Key DeviceKey
}

func (e *DeviceNotFoundError) Error() string {
	return fmt.Sprintf("device %d (%s) not found", e.Key.Index, e.Key.Type)
}

// StreamNotFoundError reports a lookup of an unknown stream.
type StreamNotFoundError struct {
	Key StreamKey
}

func (e *StreamNotFoundError) Error() string {
	return fmt.Sprintf("stream %d (%s) not found", e.Key.Index, e.Key.Type)
}

func usec(v uint64) time.Duration { return time.Duration(v) * time.Microsecond }

func fromDevice(d native.DeviceInfo, t DeviceType) Device {
	ports := make([]DevicePort, len(d.Ports))
	for i, p := range d.Ports {
		ports[i] = DevicePort{
			Name:        p.Name,
			Description: p.Description,
			Priority:    p.Priority,
			Available:   p.Available != native.PortAvailableNo,
		}
	}
	formats := make([]AudioFormat, len(d.Formats))
	for i, f := range d.Formats {
		formats[i] = AudioFormat{Encoding: f.Encoding.String(), Properties: f.Props}
	}
	return Device{
		Key:               DeviceKey{Index: d.Index, Type: t},
		Name:              d.Name,
		Description:       d.Description,
		Card:              optIndex(d.Card),
		OwnerModule:       optIndex(d.OwnerModule),
		Driver:            d.Driver,
		State:             deviceState(d.State),
		Volume:            volumeFromPulse(d.Volume),
		BaseVolume:        volumeFromPulseSingle(d.BaseVolume),
		VolumeSteps:       d.VolumeSteps,
		Muted:             d.Mute,
		Properties:        d.Props,
		Ports:             ports,
		ActivePort:        d.ActivePort,
		Formats:           formats,
		SampleSpec:        d.SampleSpec,
		ChannelMap:        d.ChannelMap,
		Latency:           usec(d.Latency),
		ConfiguredLatency: usec(d.ConfiguredLatency),
		Flags:             d.Flags,
	}
}

// fromSink is conversion/device.rs's from_sink.
func fromSink(d native.DeviceInfo) OutputDevice {
	return OutputDevice{
		Device:            fromDevice(d, DeviceOutput),
		MonitorSource:     d.Monitor,
		MonitorSourceName: d.MonitorName,
	}
}

// fromSource is from_source: a source is a monitor exactly when it
// names the sink it monitors.
func fromSource(d native.DeviceInfo) InputDevice {
	in := InputDevice{Device: fromDevice(d, DeviceInput), MonitorOfSink: optIndex(d.Monitor)}
	if in.IsMonitor() {
		in.MonitorOfSinkName = d.MonitorName
	}
	return in
}

// fromStream is conversion/stream.rs's from_sink_input /
// from_source_output.
func fromStream(s native.StreamInfo, t StreamType) AudioStream {
	state := StreamRunning
	if s.Corked {
		state = StreamCorked
	}
	var pid uint32
	if raw, ok := s.Props["application.process.id"]; ok {
		if v, err := strconv.ParseUint(raw, 10, 32); err == nil {
			pid = uint32(v)
		}
	}
	return AudioStream{
		Key:             StreamKey{Index: s.Index, Type: t},
		Name:            s.Name,
		ApplicationName: s.Props["application.name"],
		Binary:          s.Props["application.process.binary"],
		PID:             pid,
		OwnerModule:     optIndex(s.OwnerModule),
		Client:          optIndex(s.Client),
		State:           state,
		Volume:          volumeFromPulse(s.Volume),
		Muted:           s.Mute,
		Corked:          s.Corked,
		HasVolume:       s.HasVolume,
		VolumeWritable:  s.VolumeWritable,
		DeviceIndex:     s.Device,
		SampleSpec:      s.SampleSpec,
		ChannelMap:      s.ChannelMap,
		Properties:      s.Props,
		Media: MediaInfo{
			Title:    s.Props["media.title"],
			Artist:   s.Props["media.artist"],
			Album:    s.Props["media.album"],
			IconName: s.Props["application.icon_name"],
		},
		BufferLatency:  usec(s.BufferLatency),
		DeviceLatency:  usec(s.DeviceLatency),
		ResampleMethod: s.ResampleMethod,
		Driver:         s.Driver,
		Format:         s.Format.Encoding.String(),
	}
}

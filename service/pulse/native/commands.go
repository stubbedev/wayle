package native

import "fmt"

// Command codes (pulsecore/native-common.h's PA_COMMAND_*), the subset
// this client sends or receives.
const (
	CmdError                 = 0
	CmdReply                 = 2
	CmdCreateRecordStream    = 5
	CmdDeleteRecordStream    = 6
	CmdAuth                  = 8
	CmdSetClientName         = 9
	CmdGetServerInfo         = 20
	CmdGetSinkInfo           = 21
	CmdGetSinkInfoList       = 22
	CmdGetSourceInfo         = 23
	CmdGetSourceInfoList     = 24
	CmdGetSinkInputInfo      = 29
	CmdGetSinkInputInfoList  = 30
	CmdGetSourceOutputInfo   = 31
	CmdGetSourceOutputList   = 32
	CmdSubscribe             = 35
	CmdSetSinkVolume         = 36
	CmdSetSinkInputVolume    = 37
	CmdSetSourceVolume       = 38
	CmdSetSinkMute           = 39
	CmdSetSourceMute         = 40
	CmdSetDefaultSink        = 44
	CmdSetDefaultSource      = 45
	CmdRecordStreamKilled    = 65
	CmdSubscribeEvent        = 66
	CmdMoveSinkInput         = 67
	CmdMoveSourceOutput      = 68
	CmdSetSinkInputMute      = 69
	CmdSetSinkPort           = 96
	CmdSetSourcePort         = 97
	CmdSetSourceOutputVolume = 98
	CmdSetSourceOutputMute   = 99
)

var commandNames = map[uint32]string{
	CmdError: "ERROR", CmdReply: "REPLY",
	CmdCreateRecordStream: "CREATE_RECORD_STREAM", CmdDeleteRecordStream: "DELETE_RECORD_STREAM",
	CmdAuth: "AUTH", CmdSetClientName: "SET_CLIENT_NAME", CmdGetServerInfo: "GET_SERVER_INFO",
	CmdGetSinkInfo: "GET_SINK_INFO", CmdGetSinkInfoList: "GET_SINK_INFO_LIST",
	CmdGetSourceInfo: "GET_SOURCE_INFO", CmdGetSourceInfoList: "GET_SOURCE_INFO_LIST",
	CmdGetSinkInputInfo: "GET_SINK_INPUT_INFO", CmdGetSinkInputInfoList: "GET_SINK_INPUT_INFO_LIST",
	CmdGetSourceOutputInfo: "GET_SOURCE_OUTPUT_INFO", CmdGetSourceOutputList: "GET_SOURCE_OUTPUT_INFO_LIST",
	CmdSubscribe: "SUBSCRIBE", CmdSetSinkVolume: "SET_SINK_VOLUME",
	CmdSetSinkInputVolume: "SET_SINK_INPUT_VOLUME", CmdSetSourceVolume: "SET_SOURCE_VOLUME",
	CmdSetSinkMute: "SET_SINK_MUTE", CmdSetSourceMute: "SET_SOURCE_MUTE",
	CmdSetDefaultSink: "SET_DEFAULT_SINK", CmdSetDefaultSource: "SET_DEFAULT_SOURCE",
	CmdRecordStreamKilled: "RECORD_STREAM_KILLED", CmdSubscribeEvent: "SUBSCRIBE_EVENT",
	CmdMoveSinkInput: "MOVE_SINK_INPUT", CmdMoveSourceOutput: "MOVE_SOURCE_OUTPUT",
	CmdSetSinkInputMute: "SET_SINK_INPUT_MUTE", CmdSetSinkPort: "SET_SINK_PORT",
	CmdSetSourcePort: "SET_SOURCE_PORT", CmdSetSourceOutputVolume: "SET_SOURCE_OUTPUT_VOLUME",
	CmdSetSourceOutputMute: "SET_SOURCE_OUTPUT_MUTE",
}

// CommandName is the PA_COMMAND_* name, for error messages.
func CommandName(cmd uint32) string {
	if name, ok := commandNames[cmd]; ok {
		return name
	}
	return fmt.Sprintf("command %d", cmd)
}

// InvalidIndex is PA_INVALID_INDEX: "no such object" in index fields,
// "use the name" in requests.
const InvalidIndex = 0xFFFFFFFF

// CookieLength is PA_NATIVE_COOKIE_LENGTH.
const CookieLength = 256

// Facility is a subscription event's object class
// (PA_SUBSCRIPTION_EVENT_FACILITY_MASK bits).
type Facility uint32

// Subscription facilities (pulse/def.h).
const (
	FacilitySink         Facility = 0
	FacilitySource       Facility = 1
	FacilitySinkInput    Facility = 2
	FacilitySourceOutput Facility = 3
	FacilityModule       Facility = 4
	FacilityClient       Facility = 5
	FacilitySampleCache  Facility = 6
	FacilityServer       Facility = 7
	FacilityCard         Facility = 9
)

// Operation is a subscription event's change kind
// (PA_SUBSCRIPTION_EVENT_TYPE_MASK bits).
type Operation uint32

// Subscription operations.
const (
	OpNew    Operation = 0x00
	OpChange Operation = 0x10
	OpRemove Operation = 0x20
)

const (
	facilityMask  = 0x0F
	operationMask = 0x30
)

// SubscriptionMask selects the facilities a SUBSCRIBE asks for.
type SubscriptionMask uint32

// Subscription mask bits (pulse/def.h's PA_SUBSCRIPTION_MASK_*).
const (
	MaskSink         SubscriptionMask = 1 << FacilitySink
	MaskSource       SubscriptionMask = 1 << FacilitySource
	MaskSinkInput    SubscriptionMask = 1 << FacilitySinkInput
	MaskSourceOutput SubscriptionMask = 1 << FacilitySourceOutput
	MaskServer       SubscriptionMask = 1 << FacilityServer
)

// SubscribeEvent is one SUBSCRIBE_EVENT.
type SubscribeEvent struct {
	Facility  Facility
	Operation Operation
	Index     uint32
}

// decodeSubscribeEvent splits the event word into facility and
// operation. An operation outside new/change/remove is malformed.
func decodeSubscribeEvent(r *Reader) (SubscribeEvent, error) {
	word := r.U32()
	index := r.U32()
	if r.Err() != nil {
		return SubscribeEvent{}, r.Err()
	}
	op := Operation(word & operationMask)
	if op != OpNew && op != OpChange && op != OpRemove {
		return SubscribeEvent{}, fmt.Errorf("%w: subscription event %#x has no valid operation", ErrMalformed, word)
	}
	return SubscribeEvent{Facility: Facility(word & facilityMask), Operation: op, Index: index}, nil
}

// ServerInfo is the GET_SERVER_INFO reply. DefaultSink/DefaultSource
// are empty when the server has no default.
type ServerInfo struct {
	PackageName    string
	PackageVersion string
	UserName       string
	HostName       string
	SampleSpec     SampleSpec
	DefaultSink    string
	DefaultSource  string
	Cookie         uint32
	ChannelMap     ChannelMap
}

func decodeServerInfo(r *Reader) ServerInfo {
	return ServerInfo{
		PackageName:    r.Str(),
		PackageVersion: r.Str(),
		UserName:       r.Str(),
		HostName:       r.Str(),
		SampleSpec:     r.SampleSpec(),
		DefaultSink:    r.Str(),
		DefaultSource:  r.Str(),
		Cookie:         r.U32(),
		ChannelMap:     r.ChannelMap(),
	}
}

// Port availability (pulse/def.h's pa_port_available).
const (
	PortAvailableUnknown = 0
	PortAvailableNo      = 1
	PortAvailableYes     = 2
)

// Port is one device port.
type Port struct {
	Name        string
	Description string
	Priority    uint32
	Available   uint32
}

// DeviceInfo is one sink or source (protocol-native.c's
// sink_fill_tagstruct / source_fill_tagstruct share the layout). For a
// sink, Monitor/MonitorName name its monitor source; for a source they
// name the sink it monitors, InvalidIndex/"" when it is not a monitor.
type DeviceInfo struct {
	Index             uint32
	Name              string
	Description       string
	SampleSpec        SampleSpec
	ChannelMap        ChannelMap
	OwnerModule       uint32
	Volume            CVolume
	Mute              bool
	Monitor           uint32
	MonitorName       string
	Latency           uint64
	Driver            string
	Flags             uint32
	Props             PropList
	ConfiguredLatency uint64
	BaseVolume        Volume
	State             uint32
	VolumeSteps       uint32
	Card              uint32
	Ports             []Port
	ActivePort        string
	Formats           []FormatInfo
}

// Device states (pulse/def.h's pa_sink_state / pa_source_state, which
// share values).
const (
	StateRunning   = 0
	StateIdle      = 1
	StateSuspended = 2
)

func decodeDevice(r *Reader) DeviceInfo {
	d := DeviceInfo{
		Index:       r.U32(),
		Name:        r.Str(),
		Description: r.Str(),
		SampleSpec:  r.SampleSpec(),
		ChannelMap:  r.ChannelMap(),
		OwnerModule: r.U32(),
		Volume:      r.CVolume(),
		Mute:        r.Bool(),
		Monitor:     r.U32(),
		MonitorName: r.Str(),
		Latency:     r.Usec(),
		Driver:      r.Str(),
		Flags:       r.U32(),
	}
	d.Props = r.PropList()
	d.ConfiguredLatency = r.Usec()
	d.BaseVolume = r.Volume()
	d.State = r.U32()
	d.VolumeSteps = r.U32()
	d.Card = r.U32()
	nPorts := r.U32()
	for i := uint32(0); i < nPorts && r.Err() == nil; i++ {
		d.Ports = append(d.Ports, Port{
			Name:        r.Str(),
			Description: r.Str(),
			Priority:    r.U32(),
			Available:   r.U32(),
		})
	}
	d.ActivePort = r.Str()
	nFormats := r.U8()
	for i := uint8(0); i < nFormats && r.Err() == nil; i++ {
		d.Formats = append(d.Formats, r.FormatInfo())
	}
	return d
}

// StreamInfo is one sink input or source output. Device is the sink
// or source it is connected to.
type StreamInfo struct {
	Index          uint32
	Name           string
	OwnerModule    uint32
	Client         uint32
	Device         uint32
	SampleSpec     SampleSpec
	ChannelMap     ChannelMap
	Volume         CVolume
	BufferLatency  uint64
	DeviceLatency  uint64
	ResampleMethod string
	Driver         string
	Mute           bool
	Props          PropList
	Corked         bool
	HasVolume      bool
	VolumeWritable bool
	Format         FormatInfo
}

// decodeSinkInput is sink_input_fill_tagstruct's layout.
func decodeSinkInput(r *Reader) StreamInfo {
	s := StreamInfo{
		Index:       r.U32(),
		Name:        r.Str(),
		OwnerModule: r.U32(),
		Client:      r.U32(),
		Device:      r.U32(),
		SampleSpec:  r.SampleSpec(),
		ChannelMap:  r.ChannelMap(),
		Volume:      r.CVolume(),
	}
	s.BufferLatency = r.Usec()
	s.DeviceLatency = r.Usec()
	s.ResampleMethod = r.Str()
	s.Driver = r.Str()
	s.Mute = r.Bool()
	s.Props = r.PropList()
	s.Corked = r.Bool()
	s.HasVolume = r.Bool()
	s.VolumeWritable = r.Bool()
	s.Format = r.FormatInfo()
	return s
}

// decodeSourceOutput is source_output_fill_tagstruct's layout: the
// volume and mute trail the proplist (protocol 22).
func decodeSourceOutput(r *Reader) StreamInfo {
	s := StreamInfo{
		Index:       r.U32(),
		Name:        r.Str(),
		OwnerModule: r.U32(),
		Client:      r.U32(),
		Device:      r.U32(),
		SampleSpec:  r.SampleSpec(),
		ChannelMap:  r.ChannelMap(),
	}
	s.BufferLatency = r.Usec()
	s.DeviceLatency = r.Usec()
	s.ResampleMethod = r.Str()
	s.Driver = r.Str()
	s.Props = r.PropList()
	s.Corked = r.Bool()
	s.Volume = r.CVolume()
	s.Mute = r.Bool()
	s.HasVolume = r.Bool()
	s.VolumeWritable = r.Bool()
	s.Format = r.FormatInfo()
	return s
}

// decodeList reads entries until the reply body is exhausted (the
// *_INFO_LIST replies are the fill layouts back to back).
func decodeList[T any](r *Reader, decode func(*Reader) T) ([]T, error) {
	var out []T
	for !r.EOF() {
		item := decode(r)
		if r.Err() != nil {
			return nil, r.Err()
		}
		out = append(out, item)
	}
	return out, nil
}

// decodeOne reads a single-entry reply and rejects trailing bytes.
func decodeOne[T any](r *Reader, decode func(*Reader) T) (T, error) {
	item := decode(r)
	if r.Err() != nil {
		var zero T
		return zero, r.Err()
	}
	if !r.EOF() {
		var zero T
		return zero, fmt.Errorf("%w: %d trailing bytes", ErrMalformed, len(r.Rest()))
	}
	return item, nil
}

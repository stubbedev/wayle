package native

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ServerInfo queries the server's defaults.
func (c *Conn) ServerInfo(ctx context.Context) (ServerInfo, error) {
	return one(ctx, c, CmdGetServerInfo, nil, decodeServerInfo)
}

// Sinks lists every sink.
func (c *Conn) Sinks(ctx context.Context) ([]DeviceInfo, error) {
	return list(ctx, c, CmdGetSinkInfoList, decodeDevice)
}

// Sources lists every source, monitors included.
func (c *Conn) Sources(ctx context.Context) ([]DeviceInfo, error) {
	return list(ctx, c, CmdGetSourceInfoList, decodeDevice)
}

// SinkInputs lists every playback stream.
func (c *Conn) SinkInputs(ctx context.Context) ([]StreamInfo, error) {
	return list(ctx, c, CmdGetSinkInputInfoList, decodeSinkInput)
}

// SourceOutputs lists every recording stream.
func (c *Conn) SourceOutputs(ctx context.Context) ([]StreamInfo, error) {
	return list(ctx, c, CmdGetSourceOutputList, decodeSourceOutput)
}

// Sink queries one sink by index.
func (c *Conn) Sink(ctx context.Context, index uint32) (DeviceInfo, error) {
	return one(ctx, c, CmdGetSinkInfo, byIndexAndName(index), decodeDevice)
}

// Source queries one source by index.
func (c *Conn) Source(ctx context.Context, index uint32) (DeviceInfo, error) {
	return one(ctx, c, CmdGetSourceInfo, byIndexAndName(index), decodeDevice)
}

// SinkInput queries one playback stream by index.
func (c *Conn) SinkInput(ctx context.Context, index uint32) (StreamInfo, error) {
	return one(ctx, c, CmdGetSinkInputInfo, byIndex(index), decodeSinkInput)
}

// SourceOutput queries one recording stream by index.
func (c *Conn) SourceOutput(ctx context.Context, index uint32) (StreamInfo, error) {
	return one(ctx, c, CmdGetSourceOutputInfo, byIndex(index), decodeSourceOutput)
}

// Subscribe selects the facilities whose changes arrive at
// Options.OnSubscribe.
func (c *Conn) Subscribe(ctx context.Context, mask SubscriptionMask) error {
	var w Writer
	w.U32(uint32(mask))
	return c.exec(ctx, CmdSubscribe, &w)
}

// SetSinkVolume sets a sink's per-channel volume by index.
func (c *Conn) SetSinkVolume(ctx context.Context, index uint32, v CVolume) error {
	w := byIndexAndName(index)
	w.CVolume(v)
	return c.exec(ctx, CmdSetSinkVolume, w)
}

// SetSourceVolume sets a source's per-channel volume by index.
func (c *Conn) SetSourceVolume(ctx context.Context, index uint32, v CVolume) error {
	w := byIndexAndName(index)
	w.CVolume(v)
	return c.exec(ctx, CmdSetSourceVolume, w)
}

// SetSinkInputVolume sets a playback stream's volume.
func (c *Conn) SetSinkInputVolume(ctx context.Context, index uint32, v CVolume) error {
	w := byIndex(index)
	w.CVolume(v)
	return c.exec(ctx, CmdSetSinkInputVolume, w)
}

// SetSourceOutputVolume sets a recording stream's volume.
func (c *Conn) SetSourceOutputVolume(ctx context.Context, index uint32, v CVolume) error {
	w := byIndex(index)
	w.CVolume(v)
	return c.exec(ctx, CmdSetSourceOutputVolume, w)
}

// SetSinkMute mutes or unmutes a sink by index.
func (c *Conn) SetSinkMute(ctx context.Context, index uint32, mute bool) error {
	w := byIndexAndName(index)
	w.Bool(mute)
	return c.exec(ctx, CmdSetSinkMute, w)
}

// SetSourceMute mutes or unmutes a source by index.
func (c *Conn) SetSourceMute(ctx context.Context, index uint32, mute bool) error {
	w := byIndexAndName(index)
	w.Bool(mute)
	return c.exec(ctx, CmdSetSourceMute, w)
}

// SetSinkInputMute mutes or unmutes a playback stream.
func (c *Conn) SetSinkInputMute(ctx context.Context, index uint32, mute bool) error {
	w := byIndex(index)
	w.Bool(mute)
	return c.exec(ctx, CmdSetSinkInputMute, w)
}

// SetSourceOutputMute mutes or unmutes a recording stream.
func (c *Conn) SetSourceOutputMute(ctx context.Context, index uint32, mute bool) error {
	w := byIndex(index)
	w.Bool(mute)
	return c.exec(ctx, CmdSetSourceOutputMute, w)
}

// SetDefaultSink makes the named sink the default.
func (c *Conn) SetDefaultSink(ctx context.Context, name string) error {
	return c.exec(ctx, CmdSetDefaultSink, byName(name))
}

// SetDefaultSource makes the named source the default.
func (c *Conn) SetDefaultSource(ctx context.Context, name string) error {
	return c.exec(ctx, CmdSetDefaultSource, byName(name))
}

// MoveSinkInput routes a playback stream to another sink.
func (c *Conn) MoveSinkInput(ctx context.Context, index, sink uint32) error {
	return c.exec(ctx, CmdMoveSinkInput, moveArgs(index, sink))
}

// MoveSourceOutput routes a recording stream to another source.
func (c *Conn) MoveSourceOutput(ctx context.Context, index, source uint32) error {
	return c.exec(ctx, CmdMoveSourceOutput, moveArgs(index, source))
}

// SetSinkPort switches a sink's active port.
func (c *Conn) SetSinkPort(ctx context.Context, index uint32, port string) error {
	return c.exec(ctx, CmdSetSinkPort, portArgs(index, port))
}

// SetSourcePort switches a source's active port.
func (c *Conn) SetSourcePort(ctx context.Context, index uint32, port string) error {
	return c.exec(ctx, CmdSetSourcePort, portArgs(index, port))
}

// exec runs a command whose reply carries nothing.
func (c *Conn) exec(ctx context.Context, cmd uint32, body *Writer) error {
	_, err := c.request(ctx, cmd, body, nil)
	return err
}

func list[T any](ctx context.Context, c *Conn, cmd uint32, decode func(*Reader) T) ([]T, error) {
	r, err := c.request(ctx, cmd, nil, nil)
	if err != nil {
		return nil, err
	}
	items, err := decodeList(r, decode)
	if err != nil {
		return nil, replyError(cmd, err)
	}
	return items, nil
}

func one[T any](ctx context.Context, c *Conn, cmd uint32, args *Writer, decode func(*Reader) T) (T, error) {
	var zero T
	r, err := c.request(ctx, cmd, args, nil)
	if err != nil {
		return zero, err
	}
	item, err := decodeOne(r, decode)
	if err != nil {
		return zero, replyError(cmd, err)
	}
	return item, nil
}

// replyError names the command in a reply decoding error.
func replyError(cmd uint32, err error) error {
	return fmt.Errorf("pulse: %s reply: %w", CommandName(cmd), err)
}

func byIndex(index uint32) *Writer {
	var w Writer
	w.U32(index)
	return &w
}

// byIndexAndName addresses a device by index with a null name (the
// "index xor name" arguments).
func byIndexAndName(index uint32) *Writer {
	w := byIndex(index)
	w.NullString()
	return w
}

func byName(name string) *Writer {
	var w Writer
	w.String(name)
	return &w
}

func moveArgs(index, device uint32) *Writer {
	w := byIndex(index)
	w.U32(device)
	w.NullString()
	return w
}

func portArgs(index uint32, port string) *Writer {
	w := byIndexAndName(index)
	w.String(port)
	return w
}

// ErrStreamKilled ends a record stream the server killed (its source went
// away, or it was killed from outside).
var ErrStreamKilled = errors.New("pulse: record stream killed by the server")

// RecordParams describes a record stream.
type RecordParams struct {
	// SampleSpec is the capture format; the server converts to it.
	SampleSpec SampleSpec
	// ChannelMap must have SampleSpec.Channels entries.
	ChannelMap ChannelMap
	// Source is the source name; empty records from the default.
	Source string
	// FragSize is the preferred chunk size in bytes; zero lets the
	// server choose.
	FragSize uint32
	// Props become the stream's proplist (media.name and friends).
	Props PropList
}

// validate rejects what the server would refuse with PA_ERR_INVALID.
func (p RecordParams) validate() error {
	switch {
	case p.SampleSpec.Format.BytesPerSample() == 0:
		return fmt.Errorf("pulse: record: unsupported sample format %d", p.SampleSpec.Format)
	case p.SampleSpec.Channels == 0 || p.SampleSpec.Channels > 32:
		return fmt.Errorf("pulse: record: %d channels", p.SampleSpec.Channels)
	case p.SampleSpec.Rate == 0:
		return errors.New("pulse: record: zero sample rate")
	case len(p.ChannelMap) != int(p.SampleSpec.Channels):
		return fmt.Errorf("pulse: record: channel map has %d positions for %d channels", len(p.ChannelMap), p.SampleSpec.Channels)
	}
	return nil
}

// RecordStream is a live record stream. Its audio arrives at the
// handler given to CreateRecordStream.
type RecordStream struct {
	c       *Conn
	channel uint32
	info    RecordInfo
	onData  func([]byte)
	ended   chan struct{}
	once    sync.Once
	err     error
}

// RecordInfo is what the server granted for a record stream.
type RecordInfo struct {
	SourceOutput uint32
	MaxLength    uint32
	FragSize     uint32
	SampleSpec   SampleSpec
	ChannelMap   ChannelMap
	SourceIndex  uint32
	SourceName   string
	Suspended    bool
	Latency      uint64
	Format       FormatInfo
}

func decodeRecordReply(r *Reader) (uint32, RecordInfo) {
	channel := r.U32()
	info := RecordInfo{
		SourceOutput: r.U32(),
		MaxLength:    r.U32(),
		FragSize:     r.U32(),
		SampleSpec:   r.SampleSpec(),
		ChannelMap:   r.ChannelMap(),
		SourceIndex:  r.U32(),
		SourceName:   r.Str(),
		Suspended:    r.Bool(),
		Latency:      r.Usec(),
		Format:       r.FormatInfo(),
	}
	return channel, info
}

// CreateRecordStream opens a record stream (CREATE_RECORD_STREAM with
// libpulse's defaults for the flags it does not expose). onData
// receives each audio chunk on the connection's reader goroutine: it
// must not block, and it owns the slice.
func (c *Conn) CreateRecordStream(ctx context.Context, p RecordParams, onData func([]byte)) (*RecordStream, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	if onData == nil {
		return nil, errors.New("pulse: record: nil data handler")
	}
	fragSize := uint32(InvalidIndex)
	if p.FragSize > 0 {
		fragSize = p.FragSize
	}
	var w Writer
	w.SampleSpec(p.SampleSpec)
	w.ChannelMap(p.ChannelMap)
	w.U32(InvalidIndex)
	w.OptString(p.Source)
	w.U32(InvalidIndex) // maxlength: server default
	w.Bool(false)       // corked
	w.U32(fragSize)
	for range 7 { // no_remap .. variable_rate (v12)
		w.Bool(false)
	}
	w.Bool(false) // peak_detect (v13)
	w.Bool(true)  // adjust_latency: fragsize is the latency target
	w.PropList(p.Props)
	w.U32(InvalidIndex) // direct_on_input
	w.Bool(false)       // early_requests (v14)
	w.Bool(false)       // dont_inhibit_auto_suspend (v15)
	w.Bool(false)       // fail_on_suspend
	w.U8(0)             // n_formats (v22): use the sample spec
	norm := make(CVolume, p.SampleSpec.Channels)
	for i := range norm {
		norm[i] = VolumeNorm
	}
	w.CVolume(norm)
	w.Bool(false) // muted
	w.Bool(false) // volume_set
	w.Bool(false) // muted_set
	w.Bool(false) // relative_volume
	w.Bool(false) // passthrough

	s := &RecordStream{c: c, onData: onData, ended: make(chan struct{})}
	// Register on the reader goroutine as the reply is read, so no
	// audio frame that follows the reply is dropped.
	hook := func(r *Reader) error {
		channel, info := decodeRecordReply(r)
		if r.Err() != nil {
			return r.Err()
		}
		s.channel, s.info = channel, info
		c.mu.Lock()
		c.records[channel] = s
		c.mu.Unlock()
		return nil
	}
	if _, err := c.request(ctx, CmdCreateRecordStream, &w, hook); err != nil {
		return nil, err
	}
	return s, nil
}

// Info is what the server granted.
func (s *RecordStream) Info() RecordInfo { return s.info }

// Ended closes when the stream is gone: deleted, killed by the server,
// or lost with its connection. Err then says which.
func (s *RecordStream) Ended() <-chan struct{} { return s.ended }

// Err reports why the stream ended; nil while it runs.
func (s *RecordStream) Err() error {
	select {
	case <-s.ended:
		return s.err
	default:
		return nil
	}
}

func (s *RecordStream) end(err error) {
	s.once.Do(func() {
		s.err = err
		close(s.ended)
	})
}

// Close deletes the stream. Audio stops arriving before the server
// confirms; a stream that already ended closes without a request.
func (s *RecordStream) Close(ctx context.Context) error {
	s.c.mu.Lock()
	// The server may reuse a dead stream's channel, so only this
	// stream's own registration counts.
	live := s.c.records[s.channel] == s
	if live {
		delete(s.c.records, s.channel)
	}
	s.c.mu.Unlock()
	s.end(ErrClosed)
	if !live {
		return nil
	}
	return s.c.exec(ctx, CmdDeleteRecordStream, byIndex(s.channel))
}

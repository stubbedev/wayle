// Package pulsetest is a fake PulseAudio server for tests: a unix
// socket speaking the native protocol's real frames, backed by a
// scriptable object model (sinks, sources, streams, defaults) that
// answers introspection, applies control commands, emits subscription
// events the way pulsecore does, serves record streams, and can inject
// server errors, malformed frames, and disconnects.
package pulsetest

import (
	"bufio"
	"net"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/wayle/service/pulse/native"
)

// DefaultVersion is the protocol version the fake announces:
// PulseAudio 17 and pipewire-pulse both speak 35.
const DefaultVersion = 35

// Server is the fake. Its methods are safe for concurrent use.
type Server struct {
	ln   net.Listener
	path string

	mu            sync.Mutex
	version       uint32
	info          native.ServerInfo
	sinks         map[uint32]native.DeviceInfo
	sources       map[uint32]native.DeviceInfo
	sinkInputs    map[uint32]native.StreamInfo
	sourceOutputs map[uint32]native.StreamInfo
	failures      map[uint32]native.Error
	conns         map[*conn]struct{}
	records       []*Record
	calls         []uint32
	nextChannel   uint32
	nextIndex     uint32
	wg            sync.WaitGroup
}

// New starts a fake on a socket in a test temp dir; it shuts down with
// the test.
func New(t testing.TB) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "native")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("pulsetest: listen: %v", err)
	}
	s := &Server{
		ln:            ln,
		path:          path,
		version:       DefaultVersion,
		sinks:         map[uint32]native.DeviceInfo{},
		sources:       map[uint32]native.DeviceInfo{},
		sinkInputs:    map[uint32]native.StreamInfo{},
		sourceOutputs: map[uint32]native.StreamInfo{},
		failures:      map[uint32]native.Error{},
		conns:         map[*conn]struct{}{},
		nextIndex:     1000,
		info: native.ServerInfo{
			PackageName:    "pulsetest",
			PackageVersion: "1.0",
			UserName:       "tester",
			HostName:       "localhost",
			SampleSpec:     native.SampleSpec{Format: native.SampleS16LE, Channels: 2, Rate: 48000},
			ChannelMap:     native.ChannelMap{native.ChannelFrontLeft, native.ChannelFrontRight},
		},
	}
	s.wg.Add(1)
	go s.accept()
	t.Cleanup(s.Close)
	return s
}

// Addr is the server string for native.Options.Server / PULSE_SERVER.
func (s *Server) Addr() string { return "unix:" + s.path }

// Close stops accepting and drops every client.
func (s *Server) Close() {
	_ = s.ln.Close()
	s.Disconnect()
	s.wg.Wait()
}

// SetVersion changes the protocol version later handshakes announce.
func (s *Server) SetVersion(v uint32) {
	s.mu.Lock()
	s.version = v
	s.mu.Unlock()
}

// FailNext makes the next request of cmd fail with code.
func (s *Server) FailNext(cmd uint32, code native.Error) {
	s.mu.Lock()
	s.failures[cmd] = code
	s.mu.Unlock()
}

// Calls lists every command received so far, in order.
func (s *Server) Calls() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.calls)
}

// PutSink adds or replaces a sink, announcing it to subscribers.
func (s *Server) PutSink(d native.DeviceInfo) {
	put(s, s.sinks, d.Index, d, native.FacilitySink)
}

// PutSource adds or replaces a source.
func (s *Server) PutSource(d native.DeviceInfo) {
	put(s, s.sources, d.Index, d, native.FacilitySource)
}

// PutSinkInput adds or replaces a playback stream.
func (s *Server) PutSinkInput(st native.StreamInfo) {
	put(s, s.sinkInputs, st.Index, st, native.FacilitySinkInput)
}

// PutSourceOutput adds or replaces a recording stream.
func (s *Server) PutSourceOutput(st native.StreamInfo) {
	put(s, s.sourceOutputs, st.Index, st, native.FacilitySourceOutput)
}

func put[T any](s *Server, m map[uint32]T, index uint32, v T, f native.Facility) {
	s.mu.Lock()
	_, exists := m[index]
	m[index] = v
	s.mu.Unlock()
	op := native.OpNew
	if exists {
		op = native.OpChange
	}
	s.Emit(native.SubscribeEvent{Facility: f, Operation: op, Index: index})
}

// RemoveSink drops a sink and announces the removal.
func (s *Server) RemoveSink(index uint32) { s.remove(native.FacilitySink, index) }

// RemoveSource drops a source.
func (s *Server) RemoveSource(index uint32) { s.remove(native.FacilitySource, index) }

// RemoveSinkInput drops a playback stream.
func (s *Server) RemoveSinkInput(index uint32) { s.remove(native.FacilitySinkInput, index) }

// RemoveSourceOutput drops a recording stream.
func (s *Server) RemoveSourceOutput(index uint32) { s.remove(native.FacilitySourceOutput, index) }

func (s *Server) remove(f native.Facility, index uint32) {
	s.mu.Lock()
	switch f {
	case native.FacilitySink:
		delete(s.sinks, index)
	case native.FacilitySource:
		delete(s.sources, index)
	case native.FacilitySinkInput:
		delete(s.sinkInputs, index)
	case native.FacilitySourceOutput:
		delete(s.sourceOutputs, index)
	}
	s.mu.Unlock()
	s.Emit(native.SubscribeEvent{Facility: f, Operation: native.OpRemove, Index: index})
}

// SetDefaults names the default sink and source (empty for none) and
// announces a server change.
func (s *Server) SetDefaults(sink, source string) {
	s.mu.Lock()
	s.info.DefaultSink, s.info.DefaultSource = sink, source
	s.mu.Unlock()
	s.Emit(native.SubscribeEvent{Facility: native.FacilityServer, Operation: native.OpChange, Index: native.InvalidIndex})
}

// Defaults reports the current default names.
func (s *Server) Defaults() (sink, source string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info.DefaultSink, s.info.DefaultSource
}

// Sink snapshots one sink.
func (s *Server) Sink(index uint32) (native.DeviceInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.sinks[index]
	return d, ok
}

// Source snapshots one source.
func (s *Server) Source(index uint32) (native.DeviceInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.sources[index]
	return d, ok
}

// SinkInput snapshots one playback stream.
func (s *Server) SinkInput(index uint32) (native.StreamInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sinkInputs[index]
	return st, ok
}

// SourceOutput snapshots one recording stream.
func (s *Server) SourceOutput(index uint32) (native.StreamInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.sourceOutputs[index]
	return st, ok
}

// Emit sends a subscription event to every client subscribed to its
// facility.
func (s *Server) Emit(ev native.SubscribeEvent) {
	var w native.Writer
	w.U32(uint32(ev.Facility) | uint32(ev.Operation))
	w.U32(ev.Index)
	for _, c := range s.clients() {
		if c.subscribed(ev.Facility) {
			c.packet(native.CmdSubscribeEvent, native.ControlChannel, w.Bytes())
		}
	}
}

// SendRaw writes raw bytes to every client, for malformed-frame tests.
func (s *Server) SendRaw(b []byte) {
	for _, c := range s.clients() {
		c.write(b)
	}
}

// SendFrame writes one frame to every client.
func (s *Server) SendFrame(f native.Frame) {
	for _, c := range s.clients() {
		c.writeMu.Lock()
		_ = native.WriteFrame(c.nc, f)
		c.writeMu.Unlock()
	}
}

// Disconnect drops every client connection.
func (s *Server) Disconnect() {
	for _, c := range s.clients() {
		_ = c.nc.Close()
	}
}

// Clients counts the live connections.
func (s *Server) Clients() int {
	return len(s.clients())
}

func (s *Server) clients() []*conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		out = append(out, c)
	}
	return out
}

// Records lists the live record streams.
func (s *Server) Records() []*Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Record
	for _, r := range s.records {
		if !r.deleted {
			out = append(out, r)
		}
	}
	return out
}

// WaitRecords waits until n record streams are live and returns them.
func (s *Server) WaitRecords(t testing.TB, n int) []*Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if recs := s.Records(); len(recs) == n {
			return recs
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pulsetest: %d record streams, want %d", len(s.Records()), n)
	return nil
}

// Record is one record stream a client created.
type Record struct {
	c             *conn
	Channel       uint32
	SourceOutput  uint32
	Source        string
	SourceIndex   uint32
	SampleSpec    native.SampleSpec
	ChannelMap    native.ChannelMap
	FragSize      uint32
	AdjustLatency bool
	Props         native.PropList
	deleted       bool
}

// Push sends one audio chunk on the stream.
func (r *Record) Push(data []byte) {
	r.c.writeMu.Lock()
	defer r.c.writeMu.Unlock()
	_ = native.WriteFrame(r.c.nc, native.Frame{Channel: r.Channel, Payload: data})
}

// Kill ends the stream from the server side (RECORD_STREAM_KILLED).
func (r *Record) Kill() {
	s := r.c.s
	s.mu.Lock()
	r.deleted = true
	s.mu.Unlock()
	var w native.Writer
	w.U32(r.Channel)
	r.c.packet(native.CmdRecordStreamKilled, native.ControlChannel, w.Bytes())
	s.RemoveSourceOutput(r.SourceOutput)
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			return
		}
		c := &conn{s: s, nc: nc}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go c.serve()
	}
}

// conn is one client connection.
type conn struct {
	s       *Server
	nc      net.Conn
	writeMu sync.Mutex
	mu      sync.Mutex
	version uint32
	mask    native.SubscriptionMask
}

func (c *conn) subscribed(f native.Facility) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mask&(1<<f) != 0
}

func (c *conn) write(b []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, _ = c.nc.Write(b)
}

// packet writes one control packet with the given command and tag.
func (c *conn) packet(cmd, tag uint32, body []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = native.WriteFrame(c.nc, native.Frame{Channel: native.ControlChannel, Payload: native.EncodePacket(cmd, tag, body)})
}

func (c *conn) reply(tag uint32, w *native.Writer) {
	var body []byte
	if w != nil {
		body = w.Bytes()
	}
	c.packet(native.CmdReply, tag, body)
}

func (c *conn) fail(tag uint32, code native.Error) {
	var w native.Writer
	w.U32(uint32(code))
	c.packet(native.CmdError, tag, w.Bytes())
}

func (c *conn) serve() {
	defer c.s.wg.Done()
	defer func() {
		_ = c.nc.Close()
		c.s.mu.Lock()
		delete(c.s.conns, c)
		for _, r := range c.s.records {
			if r.c == c {
				r.deleted = true
			}
		}
		c.s.mu.Unlock()
	}()
	br := bufio.NewReader(c.nc)
	for {
		f, err := native.ReadFrame(br)
		if err != nil {
			return
		}
		if f.Channel != native.ControlChannel {
			continue // playback data: this fake records only
		}
		pkt, err := native.DecodePacket(f.Payload)
		if err != nil {
			return
		}
		if !c.handle(pkt) {
			// pulsecore's protocol_error: a request that does not
			// parse drops the client.
			return
		}
	}
}

// handle answers one request; false means the request did not parse.
func (c *conn) handle(pkt native.Packet) bool {
	s := c.s
	s.mu.Lock()
	s.calls = append(s.calls, pkt.Command)
	code, failing := s.failures[pkt.Command]
	delete(s.failures, pkt.Command)
	s.mu.Unlock()

	r := pkt.Body
	if pkt.Command == native.CmdAuth {
		return c.auth(pkt.Tag, r, code, failing)
	}
	h, ok := handlers[pkt.Command]
	if !ok {
		c.fail(pkt.Tag, native.ErrCommand)
		return true
	}
	return h(c, pkt.Tag, r, func() bool {
		if failing {
			c.fail(pkt.Tag, code)
		}
		return failing
	})
}

// handler parses its arguments, then calls failed(): when that reports
// an injected failure the handler returns without effect.
type handler func(c *conn, tag uint32, r *native.Reader, failed func() bool) bool

var handlers map[uint32]handler

func init() {
	handlers = map[uint32]handler{
		native.CmdSetClientName:         (*conn).setClientName,
		native.CmdGetServerInfo:         (*conn).serverInfo,
		native.CmdGetSinkInfoList:       (*conn).sinkList,
		native.CmdGetSourceInfoList:     (*conn).sourceList,
		native.CmdGetSinkInputInfoList:  (*conn).sinkInputList,
		native.CmdGetSourceOutputList:   (*conn).sourceOutputList,
		native.CmdGetSinkInfo:           (*conn).sinkInfo,
		native.CmdGetSourceInfo:         (*conn).sourceInfo,
		native.CmdGetSinkInputInfo:      (*conn).sinkInputInfo,
		native.CmdGetSourceOutputInfo:   (*conn).sourceOutputInfo,
		native.CmdSubscribe:             (*conn).subscribe,
		native.CmdSetSinkVolume:         deviceSetter(native.FacilitySink, setVolume),
		native.CmdSetSourceVolume:       deviceSetter(native.FacilitySource, setVolume),
		native.CmdSetSinkMute:           deviceSetter(native.FacilitySink, setMute),
		native.CmdSetSourceMute:         deviceSetter(native.FacilitySource, setMute),
		native.CmdSetSinkPort:           deviceSetter(native.FacilitySink, setPort),
		native.CmdSetSourcePort:         deviceSetter(native.FacilitySource, setPort),
		native.CmdSetSinkInputVolume:    streamSetter(native.FacilitySinkInput, setStreamVolume),
		native.CmdSetSourceOutputVolume: streamSetter(native.FacilitySourceOutput, setStreamVolume),
		native.CmdSetSinkInputMute:      streamSetter(native.FacilitySinkInput, setStreamMute),
		native.CmdSetSourceOutputMute:   streamSetter(native.FacilitySourceOutput, setStreamMute),
		native.CmdMoveSinkInput:         moveHandler(native.FacilitySinkInput),
		native.CmdMoveSourceOutput:      moveHandler(native.FacilitySourceOutput),
		native.CmdSetDefaultSink:        setDefaultHandler(true),
		native.CmdSetDefaultSource:      setDefaultHandler(false),
		native.CmdCreateRecordStream:    (*conn).createRecord,
		native.CmdDeleteRecordStream:    (*conn).deleteRecord,
	}
}

func (c *conn) auth(tag uint32, r *native.Reader, code native.Error, failing bool) bool {
	clientVersion := r.U32()
	cookie := r.Arbitrary()
	if r.Err() != nil || !r.EOF() || len(cookie) != native.CookieLength {
		return false
	}
	if failing {
		c.fail(tag, code)
		return true
	}
	c.s.mu.Lock()
	server := c.s.version
	c.s.mu.Unlock()
	c.mu.Lock()
	c.version = min(clientVersion&0xFFFF, server)
	c.mu.Unlock()
	var w native.Writer
	w.U32(server)
	c.reply(tag, &w)
	return true
}

func (c *conn) negotiated() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

func (c *conn) setClientName(tag uint32, r *native.Reader, failed func() bool) bool {
	if r.PropList(); r.Err() != nil || !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	var w native.Writer
	w.U32(7)
	c.reply(tag, &w)
	return true
}

func (c *conn) serverInfo(tag uint32, r *native.Reader, failed func() bool) bool {
	if !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	c.s.mu.Lock()
	info := c.s.info
	c.s.mu.Unlock()
	var w native.Writer
	fillServerInfo(&w, c.negotiated(), info)
	c.reply(tag, &w)
	return true
}

// sorted lists a map's values by index, like pulsecore's idxset walk.
func sorted[T any](m map[uint32]T) []T {
	keys := make([]uint32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]T, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func (c *conn) sinkList(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.listDevices(tag, r, failed, true)
}

func (c *conn) sourceList(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.listDevices(tag, r, failed, false)
}

func (c *conn) listDevices(tag uint32, r *native.Reader, failed func() bool, sinks bool) bool {
	if !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	c.s.mu.Lock()
	m := c.s.sources
	if sinks {
		m = c.s.sinks
	}
	all := sorted(m)
	c.s.mu.Unlock()
	var w native.Writer
	for _, d := range all {
		fillDevice(&w, c.negotiated(), d, sinks)
	}
	c.reply(tag, &w)
	return true
}

func (c *conn) sinkInputList(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.listStreams(tag, r, failed, true)
}

func (c *conn) sourceOutputList(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.listStreams(tag, r, failed, false)
}

func (c *conn) listStreams(tag uint32, r *native.Reader, failed func() bool, playback bool) bool {
	if !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	c.s.mu.Lock()
	m := c.s.sourceOutputs
	if playback {
		m = c.s.sinkInputs
	}
	all := sorted(m)
	c.s.mu.Unlock()
	var w native.Writer
	for _, st := range all {
		if playback {
			fillSinkInput(&w, c.negotiated(), st)
		} else {
			fillSourceOutput(&w, c.negotiated(), st)
		}
	}
	c.reply(tag, &w)
	return true
}

func (c *conn) sinkInfo(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.deviceInfo(tag, r, failed, true)
}

func (c *conn) sourceInfo(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.deviceInfo(tag, r, failed, false)
}

func (c *conn) deviceInfo(tag uint32, r *native.Reader, failed func() bool, sink bool) bool {
	index := r.U32()
	name, _ := r.String()
	if r.Err() != nil || !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	c.s.mu.Lock()
	m := c.s.sources
	if sink {
		m = c.s.sinks
	}
	d, ok := m[index]
	if name != "" {
		ok = false
		for _, cand := range m {
			if cand.Name == name {
				d, ok = cand, true
			}
		}
	}
	c.s.mu.Unlock()
	if !ok {
		c.fail(tag, native.ErrNoEntity)
		return true
	}
	var w native.Writer
	fillDevice(&w, c.negotiated(), d, sink)
	c.reply(tag, &w)
	return true
}

func (c *conn) sinkInputInfo(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.streamInfo(tag, r, failed, true)
}

func (c *conn) sourceOutputInfo(tag uint32, r *native.Reader, failed func() bool) bool {
	return c.streamInfo(tag, r, failed, false)
}

func (c *conn) streamInfo(tag uint32, r *native.Reader, failed func() bool, playback bool) bool {
	index := r.U32()
	if r.Err() != nil || !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	c.s.mu.Lock()
	m := c.s.sourceOutputs
	if playback {
		m = c.s.sinkInputs
	}
	st, ok := m[index]
	c.s.mu.Unlock()
	if !ok {
		c.fail(tag, native.ErrNoEntity)
		return true
	}
	var w native.Writer
	if playback {
		fillSinkInput(&w, c.negotiated(), st)
	} else {
		fillSourceOutput(&w, c.negotiated(), st)
	}
	c.reply(tag, &w)
	return true
}

func (c *conn) subscribe(tag uint32, r *native.Reader, failed func() bool) bool {
	mask := r.U32()
	if r.Err() != nil || !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	c.mu.Lock()
	c.mask = native.SubscriptionMask(mask)
	c.mu.Unlock()
	c.reply(tag, nil)
	return true
}

// deviceArg parses a device setter's argument after "index, name".
type deviceArg func(r *native.Reader) func(*native.DeviceInfo)

func setVolume(r *native.Reader) func(*native.DeviceInfo) {
	v := r.CVolume()
	return func(d *native.DeviceInfo) { d.Volume = v }
}

func setMute(r *native.Reader) func(*native.DeviceInfo) {
	m := r.Bool()
	return func(d *native.DeviceInfo) { d.Mute = m }
}

func setPort(r *native.Reader) func(*native.DeviceInfo) {
	p, _ := r.String()
	return func(d *native.DeviceInfo) { d.ActivePort = p }
}

// deviceSetter applies a sink/source setter by index and emits the
// change, as pulsecore does after the volume/mute/port hooks.
func deviceSetter(f native.Facility, arg deviceArg) handler {
	return func(c *conn, tag uint32, r *native.Reader, failed func() bool) bool {
		index := r.U32()
		_, named := r.String()
		apply := arg(r)
		if r.Err() != nil || !r.EOF() || named {
			return false
		}
		if failed() {
			return true
		}
		s := c.s
		s.mu.Lock()
		m := s.sources
		if f == native.FacilitySink {
			m = s.sinks
		}
		d, ok := m[index]
		if ok {
			apply(&d)
			m[index] = d
		}
		s.mu.Unlock()
		if !ok {
			c.fail(tag, native.ErrNoEntity)
			return true
		}
		c.reply(tag, nil)
		s.Emit(native.SubscribeEvent{Facility: f, Operation: native.OpChange, Index: index})
		return true
	}
}

type streamArg func(r *native.Reader) func(*native.StreamInfo)

func setStreamVolume(r *native.Reader) func(*native.StreamInfo) {
	v := r.CVolume()
	return func(st *native.StreamInfo) { st.Volume = v }
}

func setStreamMute(r *native.Reader) func(*native.StreamInfo) {
	m := r.Bool()
	return func(st *native.StreamInfo) { st.Mute = m }
}

func streamSetter(f native.Facility, arg streamArg) handler {
	return func(c *conn, tag uint32, r *native.Reader, failed func() bool) bool {
		index := r.U32()
		apply := arg(r)
		if r.Err() != nil || !r.EOF() {
			return false
		}
		if failed() {
			return true
		}
		c.updateStream(tag, f, index, apply)
		return true
	}
}

func (c *conn) updateStream(tag uint32, f native.Facility, index uint32, apply func(*native.StreamInfo)) {
	s := c.s
	s.mu.Lock()
	m := s.sourceOutputs
	if f == native.FacilitySinkInput {
		m = s.sinkInputs
	}
	st, ok := m[index]
	if ok {
		apply(&st)
		m[index] = st
	}
	s.mu.Unlock()
	if !ok {
		c.fail(tag, native.ErrNoEntity)
		return
	}
	c.reply(tag, nil)
	s.Emit(native.SubscribeEvent{Facility: f, Operation: native.OpChange, Index: index})
}

func moveHandler(f native.Facility) handler {
	return func(c *conn, tag uint32, r *native.Reader, failed func() bool) bool {
		index := r.U32()
		device := r.U32()
		_, named := r.String()
		if r.Err() != nil || !r.EOF() || named {
			return false
		}
		if failed() {
			return true
		}
		s := c.s
		s.mu.Lock()
		devices := s.sources
		if f == native.FacilitySinkInput {
			devices = s.sinks
		}
		_, exists := devices[device]
		s.mu.Unlock()
		if !exists {
			c.fail(tag, native.ErrNoEntity)
			return true
		}
		c.updateStream(tag, f, index, func(st *native.StreamInfo) { st.Device = device })
		return true
	}
}

func setDefaultHandler(sink bool) handler {
	return func(c *conn, tag uint32, r *native.Reader, failed func() bool) bool {
		name, ok := r.String()
		if r.Err() != nil || !r.EOF() || !ok {
			return false
		}
		if failed() {
			return true
		}
		s := c.s
		s.mu.Lock()
		m := s.sources
		if sink {
			m = s.sinks
		}
		found := false
		for _, d := range m {
			found = found || d.Name == name
		}
		if found {
			if sink {
				s.info.DefaultSink = name
			} else {
				s.info.DefaultSource = name
			}
		}
		s.mu.Unlock()
		if !found {
			c.fail(tag, native.ErrNoEntity)
			return true
		}
		c.reply(tag, nil)
		s.Emit(native.SubscribeEvent{Facility: native.FacilityServer, Operation: native.OpChange, Index: native.InvalidIndex})
		return true
	}
}

// createRecord parses CREATE_RECORD_STREAM at the negotiated version
// (the v12-v22 flag blocks), resolves the source like pa_namereg_get
// (null = default), and answers with the granted attributes.
func (c *conn) createRecord(tag uint32, r *native.Reader, failed func() bool) bool {
	v := c.negotiated()
	rec := &Record{c: c}
	rec.SampleSpec = r.SampleSpec()
	rec.ChannelMap = r.ChannelMap()
	sourceIndex := r.U32()
	rec.Source, _ = r.String()
	_ = r.U32() // maxlength
	_ = r.Bool()
	rec.FragSize = r.U32()
	if v >= 12 {
		for range 7 {
			_ = r.Bool()
		}
	}
	if v >= 13 {
		_ = r.Bool()
		rec.AdjustLatency = r.Bool()
		rec.Props = r.PropList()
		_ = r.U32()
	}
	if v >= 14 {
		_ = r.Bool()
	}
	if v >= 15 {
		_ = r.Bool()
		_ = r.Bool()
	}
	var vol native.CVolume
	if v >= 22 {
		n := r.U8()
		for range n {
			_ = r.FormatInfo()
		}
		vol = r.CVolume()
		for range 5 {
			_ = r.Bool()
		}
	}
	if r.Err() != nil || !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	if sourceIndex != native.InvalidIndex || rec.SampleSpec.FrameSize() == 0 ||
		len(rec.ChannelMap) != int(rec.SampleSpec.Channels) || (v >= 22 && len(vol) != int(rec.SampleSpec.Channels)) {
		c.fail(tag, native.ErrInvalid)
		return true
	}
	s := c.s
	s.mu.Lock()
	name := rec.Source
	if name == "" {
		name = s.info.DefaultSource
	}
	var source native.DeviceInfo
	found := false
	for _, d := range s.sources {
		if d.Name == name {
			source, found = d, true
		}
	}
	if found {
		rec.Channel = s.nextChannel
		s.nextChannel++
		rec.SourceOutput = s.nextIndex
		s.nextIndex++
		rec.SourceIndex = source.Index
		s.records = append(s.records, rec)
		s.sourceOutputs[rec.SourceOutput] = native.StreamInfo{
			Index: rec.SourceOutput, Name: rec.Props["media.name"], OwnerModule: native.InvalidIndex,
			Client: 7, Device: source.Index, SampleSpec: rec.SampleSpec, ChannelMap: rec.ChannelMap,
			Volume: vol, Props: rec.Props, HasVolume: true, VolumeWritable: true,
			Format: native.FormatInfo{Encoding: native.EncodingPCM, Props: native.PropList{}},
		}
	}
	s.mu.Unlock()
	if !found {
		c.fail(tag, native.ErrNoEntity)
		return true
	}
	var w native.Writer
	w.U32(rec.Channel)
	w.U32(rec.SourceOutput)
	w.U32(4 << 20)
	w.U32(rec.FragSize)
	w.SampleSpec(rec.SampleSpec)
	w.ChannelMap(rec.ChannelMap)
	w.U32(source.Index)
	w.String(source.Name)
	w.Bool(source.State == native.StateSuspended)
	w.Usec(11000)
	if v >= 22 {
		w.FormatInfo(native.FormatInfo{Encoding: native.EncodingPCM, Props: native.PropList{}})
	}
	c.reply(tag, &w)
	s.Emit(native.SubscribeEvent{Facility: native.FacilitySourceOutput, Operation: native.OpNew, Index: rec.SourceOutput})
	return true
}

func (c *conn) deleteRecord(tag uint32, r *native.Reader, failed func() bool) bool {
	channel := r.U32()
	if r.Err() != nil || !r.EOF() {
		return false
	}
	if failed() {
		return true
	}
	s := c.s
	s.mu.Lock()
	var rec *Record
	for _, cand := range s.records {
		if cand.c == c && cand.Channel == channel && !cand.deleted {
			rec = cand
		}
	}
	if rec != nil {
		rec.deleted = true
	}
	s.mu.Unlock()
	if rec == nil {
		c.fail(tag, native.ErrNoEntity)
		return true
	}
	c.reply(tag, nil)
	s.RemoveSourceOutput(rec.SourceOutput)
	return true
}

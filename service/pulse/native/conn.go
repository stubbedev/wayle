// Package native is a PulseAudio native-protocol client in pure Go:
// the pstream framing, the tagstruct encoding, the handshake, the
// introspection and control commands wayle-audio drives through
// libpulse, the subscription events, and record streams. PipeWire's
// pipewire-pulse serves the same protocol, so this is also how wayle
// talks to PipeWire.
//
// The client speaks protocol version 32 without shared memory, so
// every reply and every audio chunk arrives inline on the socket, and
// it decodes servers down to version 24 (PulseAudio 2.0), where every
// field it reads has a fixed layout.
package native

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion is the native protocol version this client speaks.
const ProtocolVersion = 32

// MinServerVersion is the oldest server protocol this client decodes.
const MinServerVersion = 24

// defaultTimeout bounds a request whose context has no deadline, so a
// wedged server cannot hang a caller forever.
const defaultTimeout = 5 * time.Second

// ErrClosed is the terminal error of a connection closed by Close.
var ErrClosed = errors.New("pulse: connection closed")

// Options configures Dial.
type Options struct {
	// Server is a PulseAudio server string ("unix:/path", "/path",
	// "tcp:host:port", optionally several separated by spaces). Empty
	// means $PULSE_SERVER, then $XDG_RUNTIME_DIR/pulse/native.
	Server string
	// ClientName becomes the client's application.name.
	ClientName string
	// Timeout bounds requests whose context has no deadline; zero
	// means five seconds.
	Timeout time.Duration
	// OnSubscribe receives subscription events in order, on one
	// goroutine that is not the socket reader, so it may issue
	// requests on the connection.
	OnSubscribe func(SubscribeEvent)
}

// pending is one request waiting for its reply. hook, when set, runs
// on the reader goroutine before the reply is handed over, for work
// that must happen before the next frame is read.
type pending struct {
	reply chan result
	hook  func(*Reader) error
}

type result struct {
	body *Reader
	err  error
}

// Conn is one authenticated connection. It is safe for concurrent use.
type Conn struct {
	nc       net.Conn
	version  uint32
	timeout  time.Duration
	onEvent  func(SubscribeEvent)
	events   eventQueue
	done     chan struct{}
	writeMu  sync.Mutex
	mu       sync.Mutex
	nextTag  uint32
	waiting  map[uint32]pending
	records  map[uint32]*RecordStream
	err      error
	loopDone chan struct{}
}

// Dial connects and authenticates, trying each address of the server
// string in turn.
func Dial(ctx context.Context, opts Options) (*Conn, error) {
	addrs, err := serverAddresses(opts.Server)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, addr := range addrs {
		var d net.Dialer
		nc, err := d.DialContext(ctx, addr.network, addr.address)
		if err != nil {
			lastErr = fmt.Errorf("pulse: dial %s: %w", addr, err)
			continue
		}
		c := newConn(nc, opts)
		if err := c.handshake(ctx, opts.ClientName); err != nil {
			c.fail(err)
			lastErr = fmt.Errorf("pulse: %s: %w", addr, err)
			continue
		}
		return c, nil
	}
	return nil, lastErr
}

func newConn(nc net.Conn, opts Options) *Conn {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	c := &Conn{
		nc:       nc,
		timeout:  timeout,
		onEvent:  opts.OnSubscribe,
		done:     make(chan struct{}),
		loopDone: make(chan struct{}),
		waiting:  map[uint32]pending{},
		records:  map[uint32]*RecordStream{},
	}
	c.events.signal = make(chan struct{}, 1)
	go c.readLoop()
	if c.onEvent != nil {
		go c.events.run(c.done, c.onEvent)
	}
	return c
}

// handshake is AUTH then SET_CLIENT_NAME (libpulse's setup_context).
func (c *Conn) handshake(ctx context.Context, clientName string) error {
	var auth Writer
	auth.U32(ProtocolVersion)
	auth.Arbitrary(loadCookie())
	r, err := c.request(ctx, CmdAuth, &auth, nil)
	if err != nil {
		return err
	}
	server, err := decodeOne(r, (*Reader).U32)
	if err != nil {
		return fmt.Errorf("pulse: AUTH reply: %w", err)
	}
	server &= 0xFFFF
	if server < MinServerVersion {
		return fmt.Errorf("pulse: server protocol version %d is older than %d", server, MinServerVersion)
	}
	c.version = min(server, ProtocolVersion)

	if clientName == "" {
		clientName = filepath.Base(os.Args[0])
	}
	props := PropList{
		"application.name":           clientName,
		"application.process.id":     strconv.Itoa(os.Getpid()),
		"application.process.binary": filepath.Base(os.Args[0]),
	}
	var name Writer
	name.PropList(props)
	_, err = c.request(ctx, CmdSetClientName, &name, nil)
	return err
}

// Version is the negotiated protocol version.
func (c *Conn) Version() uint32 { return c.version }

// Done closes when the connection is gone (closed, killed, or failed).
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err reports why the connection ended; nil while it is up.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close tears the connection down. Pending requests fail with
// ErrClosed.
func (c *Conn) Close() error {
	c.fail(ErrClosed)
	<-c.loopDone
	return nil
}

// fail records the terminal error once, closes the socket, and
// releases every waiter and record stream.
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	waiting := c.waiting
	records := c.records
	c.waiting = map[uint32]pending{}
	c.records = map[uint32]*RecordStream{}
	c.mu.Unlock()

	_ = c.nc.Close()
	close(c.done)
	for _, p := range waiting {
		p.reply <- result{err: err}
	}
	for _, s := range records {
		s.end(err)
	}
}

// readLoop demultiplexes frames: replies to their waiters, events to
// the queue, audio to its record stream.
func (c *Conn) readLoop() {
	defer close(c.loopDone)
	br := bufio.NewReaderSize(c.nc, 64<<10)
	for {
		f, err := ReadFrame(br)
		if err != nil {
			c.fail(fmt.Errorf("pulse: read: %w", err))
			return
		}
		if f.Channel != ControlChannel {
			c.deliverAudio(f)
			continue
		}
		pkt, err := DecodePacket(f.Payload)
		if err != nil {
			c.fail(err)
			return
		}
		c.dispatch(pkt)
	}
}

func (c *Conn) deliverAudio(f Frame) {
	c.mu.Lock()
	s := c.records[f.Channel]
	c.mu.Unlock()
	if s != nil && len(f.Payload) > 0 {
		s.onData(f.Payload)
	}
}

// dispatch routes one control packet. Server messages this client does
// not use are dropped; so are replies nobody waits for (a request
// whose caller gave up).
func (c *Conn) dispatch(pkt Packet) {
	switch pkt.Command {
	case CmdReply:
		c.resolve(pkt.Tag, pkt.Body, nil)
	case CmdError:
		code, err := decodeOne(pkt.Body, (*Reader).U32)
		if err == nil {
			err = Error(code)
		}
		c.resolve(pkt.Tag, nil, err)
	case CmdSubscribeEvent:
		ev, err := decodeSubscribeEvent(pkt.Body)
		if err == nil && c.onEvent != nil {
			c.events.push(ev)
		}
	case CmdRecordStreamKilled:
		channel, err := decodeOne(pkt.Body, (*Reader).U32)
		if err != nil {
			return
		}
		c.mu.Lock()
		s := c.records[channel]
		delete(c.records, channel)
		c.mu.Unlock()
		if s != nil {
			s.end(ErrStreamKilled)
		}
	}
}

func (c *Conn) resolve(tag uint32, body *Reader, err error) {
	c.mu.Lock()
	p, ok := c.waiting[tag]
	delete(c.waiting, tag)
	c.mu.Unlock()
	if !ok {
		return
	}
	if err == nil && p.hook != nil {
		err = p.hook(body)
	}
	p.reply <- result{body: body, err: err}
}

// request sends one command and waits for its reply body. hook runs on
// the reader goroutine with the reply before any later frame is read.
func (c *Conn) request(ctx context.Context, cmd uint32, body *Writer, hook func(*Reader) error) (*Reader, error) {
	if body == nil {
		body = &Writer{}
	}
	if body.Err() != nil {
		return nil, fmt.Errorf("pulse: %s: %w", CommandName(cmd), body.Err())
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}

	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, fmt.Errorf("pulse: %s: %w", CommandName(cmd), err)
	}
	tag := c.nextTag
	c.nextTag++
	if c.nextTag == ControlChannel {
		c.nextTag = 0
	}
	reply := make(chan result, 1)
	c.waiting[tag] = pending{reply: reply, hook: hook}
	c.mu.Unlock()

	if err := c.send(ctx, EncodePacket(cmd, tag, body.Bytes())); err != nil {
		c.fail(fmt.Errorf("pulse: write: %w", err))
		return nil, fmt.Errorf("pulse: %s: %w", CommandName(cmd), err)
	}
	select {
	case res := <-reply:
		if res.err != nil {
			return nil, fmt.Errorf("pulse: %s: %w", CommandName(cmd), res.err)
		}
		return res.body, nil
	case <-ctx.Done():
		c.mu.Lock()
		_, unclaimed := c.waiting[tag]
		delete(c.waiting, tag)
		c.mu.Unlock()
		if unclaimed {
			return nil, fmt.Errorf("pulse: %s: %w", CommandName(cmd), ctx.Err())
		}
		// The reader already claimed the reply (and ran its hook); it
		// is in flight on the buffered channel, so take it rather than
		// orphan whatever the hook set up.
		res := <-reply
		if res.err != nil {
			return nil, fmt.Errorf("pulse: %s: %w", CommandName(cmd), res.err)
		}
		return res.body, nil
	}
}

// send writes one control packet, bounded by the context deadline.
func (c *Conn) send(ctx context.Context, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.nc.SetWriteDeadline(deadline)
		defer func() { _ = c.nc.SetWriteDeadline(time.Time{}) }()
	}
	return WriteFrame(c.nc, Frame{Channel: ControlChannel, Payload: payload})
}

// eventQueue is an unbounded FIFO between the reader and the event
// goroutine: the reader never blocks on a slow handler, and no event
// is dropped.
type eventQueue struct {
	mu     sync.Mutex
	items  []SubscribeEvent
	signal chan struct{}
}

func (q *eventQueue) push(ev SubscribeEvent) {
	q.mu.Lock()
	q.items = append(q.items, ev)
	q.mu.Unlock()
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

func (q *eventQueue) run(done <-chan struct{}, handle func(SubscribeEvent)) {
	for {
		select {
		case <-done:
			return
		case <-q.signal:
		}
		for {
			q.mu.Lock()
			if len(q.items) == 0 {
				q.mu.Unlock()
				break
			}
			ev := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			select {
			case <-done:
				return
			default:
			}
			handle(ev)
		}
	}
}

// address is one dialable server.
type address struct {
	network string
	address string
}

func (a address) String() string { return a.network + ":" + a.address }

// systemSocket is libpulse's PA_SYSTEM_RUNTIME_PATH native socket,
// the system-wide instance.
const systemSocket = "/var/run/pulse/native"

// serverAddresses resolves a server string the way libpulse does:
// the explicit string, else $PULSE_SERVER, else context.c's default
// list - the per-user socket (pa_runtime_path: $PULSE_RUNTIME_PATH,
// else $XDG_RUNTIME_DIR/pulse), then the system-wide one. An entry
// prefixed "{id}" applies only on the machine whose machine-id or
// host name is id.
func serverAddresses(server string) ([]address, error) {
	if server == "" {
		server = os.Getenv("PULSE_SERVER")
	}
	if server == "" {
		var out []address
		if dir := os.Getenv("PULSE_RUNTIME_PATH"); dir != "" {
			out = append(out, address{network: "unix", address: filepath.Join(dir, "native")})
		} else if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
			out = append(out, address{network: "unix", address: filepath.Join(runtime, "pulse", "native")})
		}
		return append(out, address{network: "unix", address: systemSocket}), nil
	}
	var out []address
	for entry := range strings.FieldsSeq(server) {
		if rest, ok := strings.CutPrefix(entry, "{"); ok {
			id, tail, found := strings.Cut(rest, "}")
			if !found {
				return nil, fmt.Errorf("pulse: server string %q: unterminated {", entry)
			}
			if !isLocalMachine(id) {
				continue
			}
			entry = tail
		}
		addr, err := parseServerEntry(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, addr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pulse: server string %q names no usable server", server)
	}
	return out, nil
}

// parseServerEntry is one libpulse server string entry.
func parseServerEntry(entry string) (address, error) {
	const defaultPort = "4713"
	withPort := func(host string) string {
		if _, _, err := net.SplitHostPort(host); err == nil {
			return host
		}
		return net.JoinHostPort(strings.Trim(host, "[]"), defaultPort)
	}
	switch {
	case entry == "":
		return address{}, errors.New("pulse: empty server string entry")
	case strings.HasPrefix(entry, "/"):
		return address{network: "unix", address: entry}, nil
	case strings.HasPrefix(entry, "unix:"):
		return address{network: "unix", address: entry[len("unix:"):]}, nil
	case strings.HasPrefix(entry, "tcp4:"):
		return address{network: "tcp4", address: withPort(entry[len("tcp4:"):])}, nil
	case strings.HasPrefix(entry, "tcp6:"):
		return address{network: "tcp6", address: withPort(entry[len("tcp6:"):])}, nil
	case strings.HasPrefix(entry, "tcp:"):
		return address{network: "tcp", address: withPort(entry[len("tcp:"):])}, nil
	}
	return address{network: "tcp", address: withPort(entry)}, nil
}

// isLocalMachine matches a "{id}" server prefix against the machine-id
// and host name.
func isLocalMachine(id string) bool {
	if host, err := os.Hostname(); err == nil && host == id {
		return true
	}
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == id { //nolint:gosec // fixed machine-id paths
			return true
		}
	}
	return false
}

// loadCookie reads the auth cookie like libpulse: $PULSE_COOKIE, then
// the per-user cookie files. A missing or malformed cookie sends zeros,
// as libpulse does ("attempting to connect without"); servers that
// need no auth (pipewire-pulse, auth-anonymous) accept it.
func loadCookie() []byte {
	var paths []string
	if p := os.Getenv("PULSE_COOKIE"); p != "" {
		paths = append(paths, p)
	}
	config := os.Getenv("XDG_CONFIG_HOME")
	home, _ := os.UserHomeDir()
	if config == "" && home != "" {
		config = filepath.Join(home, ".config")
	}
	if config != "" {
		paths = append(paths, filepath.Join(config, "pulse", "cookie"))
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".pulse-cookie"))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // the cookie paths are libpulse's fixed locations and the user's own override
		if err == nil && len(data) == CookieLength {
			return data
		}
	}
	return make([]byte, CookieLength)
}

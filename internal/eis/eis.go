// Package eis is an EIS server: the receiving end of libei, which
// remote-desktop clients prefer over the portal's Notify* D-Bus calls
// (wayle-portal remotedesktop/eis.rs, which used reis). ConnectToEIS
// hands the app one end of a socket pair; Serve speaks the ei protocol
// on the other, advertises a seat with a pointer and a keyboard, and
// replays what the client emulates onto a Handler.
package eis

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
)

// Handler receives the emulated input.
type Handler interface {
	Motion(dx, dy float64)
	Button(code uint32, pressed bool)
	// Scroll is smooth scrolling in logical pixels.
	Scroll(dx, dy float64)
	// ScrollDiscrete is wheel scrolling in 120ths of a detent.
	ScrollDiscrete(dx, dy int32)
	Key(code uint32, pressed bool)
	Keysym(sym uint32, pressed bool)
}

// The interfaces served, at the versions this server implements; a
// client's interface_version negotiates each down to the lower.
const (
	ifHandshake  = "ei_handshake"
	ifConnection = "ei_connection"
	ifCallback   = "ei_callback"
	ifPingpong   = "ei_pingpong"
	ifSeat       = "ei_seat"
	ifDevice     = "ei_device"
	ifPointer    = "ei_pointer"
	ifScroll     = "ei_scroll"
	ifButton     = "ei_button"
	ifKeyboard   = "ei_keyboard"
	ifText       = "ei_text"
)

var supported = map[string]uint32{
	ifConnection: 1, ifCallback: 1, ifPingpong: 1, ifSeat: 1, ifDevice: 1,
	ifPointer: 1, ifScroll: 1, ifButton: 1, ifKeyboard: 1, ifText: 1,
}

// Seat capability bits: the server picks them (reis's DeviceCapability
// values, which libei's EI_DEVICE_CAP_* mirror).
const (
	capPointer  uint64 = 1 << 0
	capKeyboard uint64 = 1 << 2
	capScroll   uint64 = 1 << 4
	capButton   uint64 = 1 << 5
	capText     uint64 = 1 << 6
)

var capInterface = []struct {
	bit   uint64
	iface string
}{{capPointer, ifPointer}, {capKeyboard, ifKeyboard}, {capScroll, ifScroll}, {capButton, ifButton}, {capText, ifText}}

const deviceTypeVirtual = 1

// serverIDBase is where server-created object ids start; client ids
// count up from 1.
const serverIDBase = 0xff00_0000_0000_0000

// Pair makes a socket pair, serves h on one end and returns the other
// for the client.
func Pair(h Handler) (*os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("eis: socketpair: %w", err)
	}
	server := os.NewFile(uintptr(fds[0]), "eis-server")
	conn, err := net.FileConn(server)
	_ = server.Close()
	if err != nil {
		_ = syscall.Close(fds[1])
		return nil, fmt.Errorf("eis: %w", err)
	}
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		_ = conn.Close()
		_ = syscall.Close(fds[1])
		return nil, errors.New("eis: the socket pair is not a unix connection")
	}
	go func() {
		defer func() { _ = unix.Close() }()
		_ = Serve(unix, h)
	}()
	return os.NewFile(uintptr(fds[1]), "eis-client"), nil
}

// server is one connection's state.
type server struct {
	conn       *net.UnixConn
	h          Handler
	objects    map[uint64]string
	negotiated map[string]uint32
	nextID     uint64
	serial     uint32
	connection uint64
	seat       uint64
	bound      bool
}

// Serve runs the protocol on conn until the client disconnects. A
// clean disconnect is nil.
func Serve(conn *net.UnixConn, h Handler) error {
	s := &server{
		conn: conn, h: h, objects: map[uint64]string{0: ifHandshake},
		negotiated: map[string]uint32{}, nextID: serverIDBase,
	}
	// The server opens with its handshake version.
	if err := s.send(newMessage(0, 0).u32(1)); err != nil {
		return err
	}
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	oob := make([]byte, syscall.CmsgSpace(4*16))
	for {
		n, oobn, _, _, err := conn.ReadMsgUnix(chunk, oob)
		closeFds(oob[:oobn]) // nothing this server receives carries one
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if n == 0 {
			return nil
		}
		buf = append(buf, chunk[:n]...)
		msgs, rest, err := split(buf)
		if err != nil {
			return err
		}
		for _, m := range msgs {
			done, err := s.dispatch(m)
			if err != nil || done {
				return err
			}
		}
		buf = append(buf[:0], rest...)
	}
}

func closeFds(oob []byte) {
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return
	}
	for _, m := range msgs {
		if fds, err := syscall.ParseUnixRights(&m); err == nil {
			for _, fd := range fds {
				_ = syscall.Close(fd)
			}
		}
	}
}

func (s *server) send(w *writer) error {
	_, err := s.conn.Write(w.bytes())
	return err
}

func (s *server) newObject(iface string) uint64 {
	id := s.nextID
	s.nextID++
	s.objects[id] = iface
	return id
}

func (s *server) nextSerial() uint32 {
	s.serial++
	return s.serial
}

// dispatch handles one request; done is a client disconnect.
func (s *server) dispatch(m message) (done bool, err error) {
	r := &reader{b: m.args}
	switch s.objects[m.object] {
	case ifHandshake:
		err = s.handshake(m.opcode, r)
	case ifConnection:
		switch m.opcode {
		case 0: // sync(callback new_id, version)
			id := r.u64()
			_ = r.u32()
			if r.err == nil {
				err = s.send(newMessage(id, 0).u64(0)) // ei_callback.done
			}
		case 1: // disconnect
			return true, nil
		}
	case ifSeat:
		if m.opcode == 1 { // bind(capabilities)
			caps := r.u64()
			if r.err == nil {
				err = s.bind(caps)
			}
		}
	case ifPointer:
		if m.opcode == 1 {
			x, y := r.f32(), r.f32()
			if r.err == nil {
				s.h.Motion(float64(x), float64(y))
			}
		}
	case ifScroll:
		switch m.opcode {
		case 1:
			x, y := r.f32(), r.f32()
			if r.err == nil {
				s.h.Scroll(float64(x), float64(y))
			}
		case 2:
			x, y := r.i32(), r.i32()
			if r.err == nil {
				s.h.ScrollDiscrete(x, y)
			}
		}
	case ifButton:
		if m.opcode == 1 {
			code, state := r.u32(), r.u32()
			if r.err == nil {
				s.h.Button(code, state != 0)
			}
		}
	case ifKeyboard:
		if m.opcode == 1 {
			code, state := r.u32(), r.u32()
			if r.err == nil {
				s.h.Key(code, state != 0)
			}
		}
	case ifText:
		if m.opcode == 1 {
			sym, state := r.u32(), r.u32()
			if r.err == nil {
				s.h.Keysym(sym, state != 0)
			}
		}
	}
	// Everything else (device emulation bracketing, frames, pongs,
	// releases, unknown objects) needs no answer here.
	if err == nil {
		err = r.err
	}
	return false, err
}

// handshake runs the setup the ei_handshake object (id 0) carries.
func (s *server) handshake(opcode uint32, r *reader) error {
	switch opcode {
	case 4: // interface_version(name, version)
		name, version := r.str(), r.u32()
		if ours, ok := supported[name]; ok && r.err == nil {
			s.negotiated[name] = min(version, ours)
		}
	case 1: // finish
		for _, name := range []string{ifConnection, ifCallback, ifPingpong} {
			if _, ok := s.negotiated[name]; !ok {
				return fmt.Errorf("eis: the client did not negotiate %s", name)
			}
		}
		for name, v := range s.negotiated {
			if err := s.send(newMessage(0, 1).str(name).u32(v)); err != nil {
				return err
			}
		}
		s.connection = s.newObject(ifConnection)
		if err := s.send(newMessage(0, 2).u32(s.nextSerial()).u64(s.connection).u32(s.negotiated[ifConnection])); err != nil {
			return err
		}
		delete(s.objects, 0)
		return s.addSeat()
	}
	// handshake_version, context_type and name need no answer.
	return nil
}

// addSeat advertises the one seat, with each capability whose
// interface the client negotiated.
func (s *server) addSeat() error {
	v, ok := s.negotiated[ifSeat]
	if !ok {
		return nil
	}
	s.seat = s.newObject(ifSeat)
	if err := s.send(newMessage(s.connection, 1).u64(s.seat).u32(v)); err != nil {
		return err
	}
	if err := s.send(newMessage(s.seat, 1).str("wayle")); err != nil {
		return err
	}
	for _, c := range capInterface {
		if _, ok := s.negotiated[c.iface]; ok {
			if err := s.send(newMessage(s.seat, 2).u64(c.bit).str(c.iface)); err != nil {
				return err
			}
		}
	}
	return s.send(newMessage(s.seat, 3))
}

// bind adds the devices for the bound capabilities, once: a keyboard
// (with text for keysyms) and a pointer with buttons and scrolling.
// Each is resumed straight away, since a paused device cannot emulate.
func (s *server) bind(caps uint64) error {
	if s.bound {
		return nil
	}
	s.bound = true
	if caps&(capKeyboard|capText) != 0 {
		var ifaces []string
		if caps&capKeyboard != 0 {
			ifaces = append(ifaces, ifKeyboard)
		}
		if caps&capText != 0 {
			ifaces = append(ifaces, ifText)
		}
		if err := s.addDevice("wayle-keyboard", ifaces); err != nil {
			return err
		}
	}
	if caps&(capPointer|capButton|capScroll) != 0 {
		return s.addDevice("wayle-pointer", []string{ifPointer, ifButton, ifScroll})
	}
	return nil
}

func (s *server) addDevice(name string, ifaces []string) error {
	v, ok := s.negotiated[ifDevice]
	if !ok {
		return nil
	}
	dev := s.newObject(ifDevice)
	msgs := []*writer{
		newMessage(s.seat, 4).u64(dev).u32(v),
		newMessage(dev, 1).str(name),
		newMessage(dev, 2).u32(deviceTypeVirtual),
	}
	for _, iface := range ifaces {
		if iv, ok := s.negotiated[iface]; ok {
			msgs = append(msgs, newMessage(dev, 5).u64(s.newObject(iface)).str(iface).u32(iv))
		}
	}
	msgs = append(msgs, newMessage(dev, 6), newMessage(dev, 7).u32(s.nextSerial()))
	for _, m := range msgs {
		if err := s.send(m); err != nil {
			return err
		}
	}
	return nil
}

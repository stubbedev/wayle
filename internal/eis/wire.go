package eis

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// The ei wire format (libei proto/protocol.xml): a 16-byte header of
// the object id (u64), the message length including the header (u32)
// and the opcode (u32), then the arguments, each 4-byte aligned, in
// native byte order. A string is its length including the NUL (u32,
// 0 for null), the bytes, the NUL, and padding to 4.

const headerSize = 16

// order is the native byte order of every Linux platform wayle runs on.
var order = binary.LittleEndian

// message is one decoded message.
type message struct {
	object uint64
	opcode uint32
	args   []byte
}

var errShort = errors.New("eis: message ends inside an argument")

// reader walks a message's arguments.
type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil || len(r.b) < n {
		r.err = errShort
		return make([]byte, n)
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *reader) u32() uint32 { return order.Uint32(r.take(4)) }
func (r *reader) i32() int32  { return int32(r.u32()) }
func (r *reader) u64() uint64 { return order.Uint64(r.take(8)) }
func (r *reader) f32() float32 {
	return math.Float32frombits(r.u32())
}

func (r *reader) str() string {
	n := r.u32()
	if n == 0 {
		return ""
	}
	padded := (n + 3) &^ 3
	b := r.take(int(padded))
	if r.err == nil && b[n-1] != 0 {
		r.err = errors.New("eis: string not NUL-terminated")
	}
	return string(b[:n-1])
}

// writer builds one message.
type writer struct{ b []byte }

func newMessage(object uint64, opcode uint32) *writer {
	w := &writer{b: make([]byte, headerSize, 64)}
	order.PutUint64(w.b, object)
	order.PutUint32(w.b[12:], opcode)
	return w
}

func (w *writer) u32(v uint32) *writer { w.b = order.AppendUint32(w.b, v); return w }
func (w *writer) u64(v uint64) *writer { w.b = order.AppendUint64(w.b, v); return w }

func (w *writer) str(s string) *writer {
	w.u32(uint32(len(s) + 1))
	w.b = append(w.b, s...)
	w.b = append(w.b, 0)
	for len(w.b)%4 != 0 {
		w.b = append(w.b, 0)
	}
	return w
}

// bytes is the finished message with its length filled in.
func (w *writer) bytes() []byte {
	order.PutUint32(w.b[8:], uint32(len(w.b)))
	return w.b
}

// split takes the complete messages off the front of buf and returns
// the rest; a length below the header is a protocol violation.
func split(buf []byte) (msgs []message, rest []byte, err error) {
	for len(buf) >= headerSize {
		n := order.Uint32(buf[8:])
		if n < headerSize {
			return nil, nil, fmt.Errorf("eis: message length %d below the header", n)
		}
		if uint32(len(buf)) < n {
			break
		}
		msgs = append(msgs, message{object: order.Uint64(buf), opcode: order.Uint32(buf[12:]), args: buf[headerSize:n]})
		buf = buf[n:]
	}
	return msgs, buf, nil
}

package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// descriptorSize is the pstream frame header: length, channel, the
// two offset halves, and flags, five big-endian u32s (pstream.c's
// PA_PSTREAM_DESCRIPTOR_SIZE).
const descriptorSize = 20

// ControlChannel is the channel of command packets; every other
// channel number carries one stream's audio (memblocks).
const ControlChannel = 0xFFFFFFFF

// maxFrameSize is pstream.c's FRAME_SIZE_MAX_ALLOW: a longer frame is
// a protocol violation, never a real packet.
const maxFrameSize = 16 << 20

// shmFlags are the descriptor bits that mark shared-memory payloads
// (pstream.c's PA_FLAG_SHMMASK). This client never offers shm or
// memfd, so a frame carrying them is a protocol violation.
const shmFlags = 0xFF000000

// ErrProtocol marks a stream-level violation: an oversized frame,
// shared-memory data the client never negotiated, a packet without a
// command header. The connection cannot resynchronize after one.
var ErrProtocol = errors.New("pulse: protocol violation")

// Frame is one pstream frame.
type Frame struct {
	Channel uint32
	Offset  uint64
	Flags   uint32
	Payload []byte
}

// WriteFrame writes one frame: the descriptor, then the payload.
func WriteFrame(w io.Writer, f Frame) error {
	if len(f.Payload) > maxFrameSize {
		return fmt.Errorf("%w: frame of %d bytes exceeds %d", ErrProtocol, len(f.Payload), maxFrameSize)
	}
	buf := make([]byte, descriptorSize, descriptorSize+len(f.Payload))
	binary.BigEndian.PutUint32(buf[0:], uint32(len(f.Payload)))
	binary.BigEndian.PutUint32(buf[4:], f.Channel)
	binary.BigEndian.PutUint32(buf[8:], uint32(f.Offset>>32))
	binary.BigEndian.PutUint32(buf[12:], uint32(f.Offset))
	binary.BigEndian.PutUint32(buf[16:], f.Flags)
	buf = append(buf, f.Payload...)
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads one frame. An oversized length or shared-memory
// flags are ErrProtocol; a short read is the reader's error
// (io.ErrUnexpectedEOF mid-frame, io.EOF at a frame boundary).
func ReadFrame(r io.Reader) (Frame, error) {
	var desc [descriptorSize]byte
	if _, err := io.ReadFull(r, desc[:]); err != nil {
		return Frame{}, err
	}
	length := binary.BigEndian.Uint32(desc[0:])
	f := Frame{
		Channel: binary.BigEndian.Uint32(desc[4:]),
		Offset:  uint64(binary.BigEndian.Uint32(desc[8:]))<<32 | uint64(binary.BigEndian.Uint32(desc[12:])),
		Flags:   binary.BigEndian.Uint32(desc[16:]),
	}
	if length > maxFrameSize {
		return Frame{}, fmt.Errorf("%w: frame of %d bytes exceeds %d", ErrProtocol, length, maxFrameSize)
	}
	if f.Flags&shmFlags != 0 {
		return Frame{}, fmt.Errorf("%w: shared-memory frame (flags %#x) on a connection without shm", ErrProtocol, f.Flags)
	}
	f.Payload = make([]byte, length)
	if _, err := io.ReadFull(r, f.Payload); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return f, nil
}

// Packet is a decoded command packet: the command, its tag, and the
// tagstruct body after the two header fields.
type Packet struct {
	Command uint32
	Tag     uint32
	Body    *Reader
}

// EncodePacket builds a command packet payload: command and tag as
// 'L' fields, then the body.
func EncodePacket(command, tag uint32, body []byte) []byte {
	var w Writer
	w.U32(command)
	w.U32(tag)
	return append(w.buf, body...)
}

// DecodePacket splits a control-channel payload into its header and
// body. A payload without the two header fields is ErrProtocol.
func DecodePacket(payload []byte) (Packet, error) {
	r := NewReader(payload)
	command := r.U32()
	tag := r.U32()
	if r.Err() != nil {
		return Packet{}, fmt.Errorf("%w: packet header: %w", ErrProtocol, r.Err())
	}
	return Packet{Command: command, Tag: tag, Body: NewReader(r.Rest())}, nil
}

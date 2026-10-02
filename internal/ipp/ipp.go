// Package ipp is the slice of the Internet Printing Protocol (RFC 8010
// and 8011) the print dialog needs from CUPS: list the printers
// (CUPS-Get-Printers) and send a job (Print-Job), over the CUPS domain
// socket or CUPS_SERVER. Pure Go; no libcups.
package ipp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Operations.
const (
	OpPrintJob        uint16 = 0x0002
	OpCUPSGetPrinters uint16 = 0x4002
)

// Attribute group delimiters.
const (
	tagOperation byte = 0x01
	tagJob       byte = 0x02
	tagEnd       byte = 0x03
	tagPrinter   byte = 0x04
)

// Value tags.
const (
	TagInteger    byte = 0x21
	TagBoolean    byte = 0x22
	TagEnum       byte = 0x23
	TagRange      byte = 0x33
	TagText       byte = 0x41
	TagName       byte = 0x42
	TagKeyword    byte = 0x44
	TagURI        byte = 0x45
	TagCharset    byte = 0x47
	TagLanguage   byte = 0x48
	TagMimeMedia  byte = 0x49
	tagFirstValue byte = 0x10
)

// Value is one attribute value: an int for integer, enum and boolean
// (0 or 1), a [2]int32 for a range, a string for the text kinds.
type Value struct {
	Tag  byte
	Int  int32
	Low  int32 // range lower bound; Int is the upper
	Text string
}

// Attr is one named attribute with its values.
type Attr struct {
	Name   string
	Values []Value
}

// Int is an integer attribute.
func Int(name string, v int32) Attr { return Attr{name, []Value{{Tag: TagInteger, Int: v}}} }

// Enum is an enum attribute.
func Enum(name string, v int32) Attr { return Attr{name, []Value{{Tag: TagEnum, Int: v}}} }

// String is a text-kind attribute (name, keyword, uri, ...).
func String(tag byte, name string, vs ...string) Attr {
	a := Attr{Name: name}
	for _, v := range vs {
		a.Values = append(a.Values, Value{Tag: tag, Text: v})
	}
	return a
}

// Ranges is a 1-based rangeOfInteger attribute.
func Ranges(name string, rs [][2]int32) Attr {
	a := Attr{Name: name}
	for _, r := range rs {
		a.Values = append(a.Values, Value{Tag: TagRange, Low: r[0], Int: r[1]})
	}
	return a
}

// Group is one attribute group.
type Group struct {
	Tag   byte
	Attrs []Attr
}

// Get is the group's attribute name, false when absent.
func (g Group) Get(name string) (Attr, bool) {
	for _, a := range g.Attrs {
		if a.Name == name {
			return a, true
		}
	}
	return Attr{}, false
}

// Text is the first value of name as text, "" when absent.
func (g Group) Text(name string) string {
	if a, ok := g.Get(name); ok && len(a.Values) > 0 {
		return a.Values[0].Text
	}
	return ""
}

// Int is the first value of name as an integer, false when absent.
func (g Group) Int(name string) (int32, bool) {
	if a, ok := g.Get(name); ok && len(a.Values) > 0 {
		return a.Values[0].Int, true
	}
	return 0, false
}

// Message is a request (Code is the operation) or a response (Code is
// the status).
type Message struct {
	Code      uint16
	RequestID uint32
	Groups    []Group
}

// Encode writes the message: version 2.0, the code, the request id,
// the groups, the end tag.
func (m Message) Encode() []byte {
	var b bytes.Buffer
	b.Write([]byte{2, 0})
	_ = binary.Write(&b, binary.BigEndian, m.Code)
	_ = binary.Write(&b, binary.BigEndian, m.RequestID)
	for _, g := range m.Groups {
		b.WriteByte(g.Tag)
		for _, a := range g.Attrs {
			for i, v := range a.Values {
				name := a.Name
				if i > 0 {
					name = "" // an additional value of the same attribute
				}
				b.WriteByte(v.Tag)
				writeString(&b, name)
				writeValue(&b, v)
			}
		}
	}
	b.WriteByte(tagEnd)
	return b.Bytes()
}

func writeString(b *bytes.Buffer, s string) {
	_ = binary.Write(b, binary.BigEndian, uint16(len(s)))
	b.WriteString(s)
}

func writeValue(b *bytes.Buffer, v Value) {
	switch v.Tag {
	case TagInteger, TagEnum:
		_ = binary.Write(b, binary.BigEndian, uint16(4))
		_ = binary.Write(b, binary.BigEndian, v.Int)
	case TagBoolean:
		_ = binary.Write(b, binary.BigEndian, uint16(1))
		b.WriteByte(byte(v.Int & 1))
	case TagRange:
		_ = binary.Write(b, binary.BigEndian, uint16(8))
		_ = binary.Write(b, binary.BigEndian, v.Low)
		_ = binary.Write(b, binary.BigEndian, v.Int)
	default:
		writeString(b, v.Text)
	}
}

// errShort is a message cut off mid-field.
var errShort = errors.New("ipp: truncated message")

// Decode parses a message; trailing data after the end tag (a job's
// document) is ignored.
func Decode(data []byte) (Message, error) {
	r := bytes.NewReader(data)
	var head struct {
		Version   [2]byte
		Code      uint16
		RequestID uint32
	}
	if err := binary.Read(r, binary.BigEndian, &head); err != nil {
		return Message{}, errShort
	}
	m := Message{Code: head.Code, RequestID: head.RequestID}
	var cur *Group
	for {
		tag, err := r.ReadByte()
		if err != nil {
			return Message{}, errShort
		}
		switch {
		case tag == tagEnd:
			return m, nil
		case tag < tagFirstValue:
			m.Groups = append(m.Groups, Group{Tag: tag})
			cur = &m.Groups[len(m.Groups)-1]
			continue
		case cur == nil:
			return Message{}, fmt.Errorf("ipp: value tag %#x outside a group", tag)
		}
		name, err := readString(r)
		if err != nil {
			return Message{}, err
		}
		raw, err := readString(r)
		if err != nil {
			return Message{}, err
		}
		v, err := parseValue(tag, []byte(raw))
		if err != nil {
			return Message{}, err
		}
		if name == "" {
			if len(cur.Attrs) == 0 {
				return Message{}, errors.New("ipp: additional value with no attribute")
			}
			last := &cur.Attrs[len(cur.Attrs)-1]
			last.Values = append(last.Values, v)
			continue
		}
		cur.Attrs = append(cur.Attrs, Attr{Name: name, Values: []Value{v}})
	}
}

func readString(r *bytes.Reader) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return "", errShort
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", errShort
	}
	return string(buf), nil
}

func parseValue(tag byte, raw []byte) (Value, error) {
	v := Value{Tag: tag}
	switch tag {
	case TagInteger, TagEnum:
		if len(raw) != 4 {
			return v, fmt.Errorf("ipp: integer of %d bytes", len(raw))
		}
		v.Int = int32(binary.BigEndian.Uint32(raw))
	case TagBoolean:
		if len(raw) != 1 {
			return v, fmt.Errorf("ipp: boolean of %d bytes", len(raw))
		}
		v.Int = int32(raw[0])
	case TagRange:
		if len(raw) != 8 {
			return v, fmt.Errorf("ipp: range of %d bytes", len(raw))
		}
		v.Low = int32(binary.BigEndian.Uint32(raw[:4]))
		v.Int = int32(binary.BigEndian.Uint32(raw[4:]))
	default:
		v.Text = string(raw)
	}
	return v, nil
}

// StatusOK reports a successful status (successful-ok and its
// "ignored or substituted" variants).
func StatusOK(code uint16) bool { return code < 0x0100 }

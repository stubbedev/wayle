package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"reflect"
	"testing"
	"time"
)

// roundTrip encodes body into a method call carrying fds and decodes it
// back with them, as the unix transport does.
func roundTrip(t *testing.T, body any) ([]any, error) {
	t.Helper()
	msg := &Message{Type: TypeMethodCall, Headers: map[HeaderField]Variant{
		FieldPath: MakeVariant(ObjectPath("/x")), FieldMember: MakeVariant("M"),
		FieldSignature: MakeVariant(SignatureOf(body)),
	}, Body: []any{body}}
	n, err := msg.CountFds()
	if err != nil {
		t.Fatal(err)
	}
	msg.Headers[FieldUnixFDs] = MakeVariant(uint32(n))
	var buf bytes.Buffer
	fds, err := msg.EncodeToWithFDs(&buf, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMessageWithFDs(&buf, fds)
	if err != nil {
		return nil, err
	}
	return got.Body, nil
}

// TestFdArraysDecode is the wayle patch: an `ah`, bare or inside a
// variant (xdg-desktop-portal's attachment_fds), decoded to an empty
// body because the element type was UnixFDIndex.
func TestFdArraysDecode(t *testing.T) {
	for _, body := range []any{
		[]UnixFD{5, 7},
		map[string]Variant{"fds": MakeVariant([]UnixFD{5, 7})},
		UnixFD(5),
	} {
		got, err := roundTrip(t, body)
		if err != nil {
			t.Fatalf("%T: %v", body, err)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], body) {
			t.Errorf("%T decoded as %#v", body, got)
		}
	}
}

// TestFdArrayIndexPastTheFds is a malformed message, rejected rather
// than decoded empty.
func TestFdArrayIndexPastTheFds(t *testing.T) {
	dec := newDecoder(bytes.NewReader([]byte{4, 0, 0, 0, 9, 0, 0, 0}), binary.LittleEndian, []int{3})
	if _, err := dec.Decode(Signature{"ah"}); err == nil {
		t.Error("an out-of-range fd index decoded")
	}
}

// TestDecodedStructsReencode is the second wayle patch: a struct inside
// a variant decodes to []any, and re-encoding that must keep the
// struct's wire form, nested in arrays and dicts too.
func TestDecodedStructsReencode(t *testing.T) {
	type sv struct {
		S string
		V Variant
	}
	for _, value := range []any{
		sv{"themed", MakeVariant([]string{"a", "b"})},
		[]sv{{"x", MakeVariant(uint32(1))}, {"y", MakeVariant(true)}},
		map[string]sv{"k": {"bytes", MakeVariant([]byte{1, 2})}},
		struct {
			A byte
			B []sv
		}{7, []sv{{"z", MakeVariant(int64(-3))}}},
	} {
		body := map[string]Variant{"icon": MakeVariant(value)}
		first, err := roundTrip(t, body)
		if err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		// The decoded form, sent again, decodes to itself.
		second, err := roundTrip(t, first[0])
		if err != nil {
			t.Fatalf("%T re-encoded: %v", value, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Errorf("%T: %#v re-encoded as %#v", value, first, second)
		}
		if got := second[0].(map[string]Variant)["icon"].Signature(); got != SignatureOf(value) {
			t.Errorf("%T: signature %s, want %s", value, got, SignatureOf(value))
		}
	}
}

type fileReplier struct{ f *os.File }

func (r fileReplier) Take() (*os.File, *Error) { return r.f, nil }

// TestAFileReplyIsSentThenClosed is the third wayle patch: a method
// replying an *os.File sends its descriptor as 'h' and godbus closes
// the file after the reply.
func TestAFileReplyIsSentThenClosed(t *testing.T) {
	if SignatureOf((*os.File)(nil)).String() != "h" {
		t.Fatalf("*os.File signature = %s", SignatureOf((*os.File)(nil)))
	}
	server, client, cleanup := pairedConns(t)
	defer cleanup()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := server.Export(fileReplier{w}, "/f", "a.b"); err != nil {
		t.Fatal(err)
	}
	var fd UnixFD
	if err := client.Object(server.Names()[0], "/f").Call("a.b.Take", 0).Store(&fd); err != nil {
		t.Fatal(err)
	}
	got := os.NewFile(uintptr(fd), "got")
	if _, err := got.Write([]byte("through")); err != nil {
		t.Fatal(err)
	}
	_ = got.Close()
	// The server's copy is closed: with the received one closed too, the
	// read end sees EOF after the payload (not a read that waits on).
	_ = r.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, err := io.ReadAll(r)
	if err != nil || string(b) != "through" {
		t.Errorf("read %q, %v", b, err)
	}
	if err := w.Close(); err == nil {
		t.Error("the replied file was left open")
	}
}

// pairedConns is two session-bus connections, as the upstream tests use.
func pairedConns(t *testing.T) (server, client *Conn, cleanup func()) {
	t.Helper()
	server, err := ConnectSessionBus()
	if err != nil {
		t.Skipf("no session bus: %v", err)
	}
	client, err = ConnectSessionBus()
	if err != nil {
		_ = server.Close()
		t.Skipf("no session bus: %v", err)
	}
	return server, client, func() {
		_ = server.Close()
		_ = client.Close()
	}
}

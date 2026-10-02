package portal

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbustest"
	"github.com/stubbedev/wayle/internal/dbusx"
)

// fakeClip is the data-control device: the selection it holds, and the
// claims made on it.
type fakeClip struct {
	owner  dbustest.Var[func()]
	starts dbustest.Var[int]
	mimes  dbustest.Var[[]string]
	pass   dbustest.Var[func(string, *os.File)]
	data   string
}

func (f *fakeClip) own(mimes []string, pass func(string, *os.File)) error {
	f.mimes.Store(mimes)
	f.pass.Store(pass)
	return nil
}

func (f *fakeClip) read(mime string) (*os.File, error) {
	if mime != "text/plain" {
		return nil, errors.New("not offered")
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	go func() {
		_, _ = w.WriteString(f.data)
		_ = w.Close()
	}()
	return r, nil
}

func clipRig(t *testing.T, startErr error) (*rig, *fakeClip) {
	f := &fakeClip{data: "copied"}
	r := newRig(t, func(b *Backend) {
		b.clipboard = newClipboardBridge(b.conn, func(owner func()) (clipDevice, error) {
			f.starts.Update(func(n int) int { return n + 1 })
			if startErr != nil {
				return nil, startErr
			}
			f.owner.Store(owner)
			return f, nil
		})
	})
	return r, f
}

// clipSignal waits for the next Clipboard signal.
func (r *rig) clipSignal(t *testing.T, member string) *dbus.Signal {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-r.signals:
			if s.Name == ClipboardIface+"."+member {
				return s
			}
		case <-deadline:
			t.Fatalf("no %s", member)
			return nil
		}
	}
}

func (r *rig) noClipSignal(t *testing.T) {
	t.Helper()
	timeout := time.After(200 * time.Millisecond)
	for {
		select {
		case s := <-r.signals:
			if len(s.Name) > len(ClipboardIface) && s.Name[:len(ClipboardIface)] == ClipboardIface {
				t.Fatalf("unexpected %s %v", s.Name, s.Body)
			}
		case <-timeout:
			return
		}
	}
}

const (
	sessA = dbus.ObjectPath("/org/freedesktop/portal/desktop/session/1_2/a")
	sessB = dbus.ObjectPath("/org/freedesktop/portal/desktop/session/1_2/b")
)

func TestClipboardReadsAndFollowsTheSelection(t *testing.T) {
	r, f := clipRig(t, nil)
	// Before any session asks, there is no device and nothing to read.
	if err := r.obj.Call(ClipboardIface+".SelectionRead", 0, sessA, "text/plain").Err; err == nil {
		t.Error("read without RequestClipboard")
	}
	for range 2 {
		if err := r.obj.Call(ClipboardIface+".RequestClipboard", 0, sessA, Vardict{}).Err; err != nil {
			t.Fatal(err)
		}
	}
	if f.starts.Load() != 1 {
		t.Errorf("device started %d times", f.starts.Load())
	}
	var fd dbus.UnixFD
	if err := r.obj.Call(ClipboardIface+".SelectionRead", 0, sessA, "text/plain").Store(&fd); err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(os.NewFile(uintptr(fd), "read"))
	if string(got) != "copied" {
		t.Errorf("read %q", got)
	}
	if err := r.obj.Call(ClipboardIface+".SelectionRead", 0, sessA, "image/png").Err; err == nil {
		t.Error("an unoffered mime read")
	}

	f.owner.Load()()
	if s := r.clipSignal(t, "SelectionOwnerChanged"); s.Body[0] != sessA {
		t.Errorf("owner changed for %v", s.Body)
	}
	r.noClipSignal(t) // only the enabled session hears it
}

func TestClipboardServesTheSessionsSelection(t *testing.T) {
	r, f := clipRig(t, nil)
	_ = r.obj.Call(ClipboardIface+".RequestClipboard", 0, sessA, Vardict{}).Err
	if err := r.obj.Call(ClipboardIface+".SetSelection", 0, sessA, Vardict{"mime_types": dbus.MakeVariant([]string{"text/plain", "text/html"})}).Err; err != nil {
		t.Fatal(err)
	}
	if got := f.mimes.Load(); !slices.Equal(got, []string{"text/plain", "text/html"}) {
		t.Errorf("claimed %v", got)
	}
	_ = r.obj.Call(ClipboardIface+".SetSelection", 0, sessA, Vardict{"mime_type": dbus.MakeVariant("text/uri-list")}).Err
	if got := f.mimes.Load(); !slices.Equal(got, []string{"text/uri-list"}) {
		t.Errorf("single mime claimed %v", got)
	}

	// A receiver asks: the session is told, takes the pipe and fills it.
	recv, pipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer recv.Close()
	f.pass.Load()("text/uri-list", pipe)
	s := r.clipSignal(t, "SelectionTransfer")
	if s.Body[0] != sessA || s.Body[1] != "text/uri-list" || s.Body[2] != uint32(1) {
		t.Fatalf("transfer = %v", s.Body)
	}
	var fd dbus.UnixFD
	if err := r.obj.Call(ClipboardIface+".SelectionWrite", 0, sessA, uint32(1)).Store(&fd); err != nil {
		t.Fatal(err)
	}
	w := os.NewFile(uintptr(fd), "write")
	_, _ = w.WriteString("file:///x")
	_ = w.Close()
	_ = r.obj.Call(ClipboardIface+".SelectionWriteDone", 0, sessA, uint32(1), true).Err
	_ = recv.SetReadDeadline(time.Now().Add(2 * time.Second))
	if got, err := io.ReadAll(recv); err != nil || string(got) != "file:///x" {
		t.Errorf("receiver got %q, %v: the backend kept its copy open or lost the data", got, err)
	}
	// Bounded: a pipe handed out twice is already closed, and its reply
	// never arrives.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var de dbus.Error
	if err := r.obj.CallWithContext(ctx, ClipboardIface+".SelectionWrite", 0, sessA, uint32(1)).Err; !errors.As(err, &de) || de.Name != dbusx.ErrFailed {
		t.Errorf("a second SelectionWrite = %v, want the no-pending-transfer error", err)
	}

	// A transfer nobody took is closed when the selection changes.
	recv2, pipe2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer recv2.Close()
	f.pass.Load()("text/uri-list", pipe2)
	if s := r.clipSignal(t, "SelectionTransfer"); s.Body[2] != uint32(2) {
		t.Errorf("serial = %v", s.Body[2])
	}
	_ = r.obj.Call(ClipboardIface+".SetSelection", 0, sessA, Vardict{"mime_type": dbus.MakeVariant("text/plain")}).Err
	_ = recv2.SetReadDeadline(time.Now().Add(2 * time.Second))
	if got, err := io.ReadAll(recv2); err != nil || len(got) != 0 {
		t.Errorf("an abandoned transfer stayed open: %q, %v", got, err)
	}
}

func TestClipboardWithoutDataControl(t *testing.T) {
	r, f := clipRig(t, errors.New("no data-control"))
	_ = r.obj.Call(ClipboardIface+".RequestClipboard", 0, sessA, Vardict{}).Err
	_ = r.obj.Call(ClipboardIface+".RequestClipboard", 0, sessB, Vardict{}).Err
	if f.starts.Load() != 2 {
		t.Errorf("a failed start is not retried: %d", f.starts.Load())
	}
	if err := r.obj.Call(ClipboardIface+".SetSelection", 0, sessA, Vardict{"mime_type": dbus.MakeVariant("text/plain")}).Err; err != nil {
		t.Error(err)
	}
	if err := r.obj.Call(ClipboardIface+".SelectionRead", 0, sessA, "text/plain").Err; err == nil {
		t.Error("read without a device")
	}
}

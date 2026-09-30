package launcheripc

import (
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

// dialRaw connects the socket without sending an open frame.
func dialRaw() (net.Conn, error) { return net.Dial("unix", SocketPath()) }

func TestOpenFrameRoundTrips(t *testing.T) {
	opts := SessionOptions{
		Mode: new("drun"), Prompt: new("apps"), MultiSelect: true, Urgent: []uint32{0, 3},
		Width: &Width{Unit: WidthPercent, Value: 60}, KbOverrides: map[string]string{"cancel": "Control+q"},
	}
	encoded, err := json.Marshal(ClientFrame{Type: FrameOpen, Options: &opts, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"type":"open"`) || !strings.Contains(string(encoded), `"width":{"percent":60}`) {
		t.Errorf("encoded = %s", encoded)
	}
	var decoded ClientFrame
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Replace || !reflect.DeepEqual(*decoded.Options, opts) {
		t.Errorf("decoded = %+v", decoded.Options)
	}
}

func TestSerdeRequiredFieldsAreAlwaysWritten(t *testing.T) {
	// serde rejects an open frame without replace and a rows frame
	// without items, so a Go CLI must still talk to a Rust shell.
	open, _ := json.Marshal(ClientFrame{Type: FrameOpen})
	if !strings.Contains(string(open), `"replace":false`) || !strings.Contains(string(open), `"options":{}`) {
		t.Errorf("open = %s", open)
	}
	rows, _ := json.Marshal(ClientFrame{Type: FrameRows})
	if string(rows) != `{"type":"rows","items":[]}` {
		t.Errorf("rows = %s", rows)
	}
	done, _ := json.Marshal(ClientFrame{Type: FrameRowsDone})
	if string(done) != `{"type":"rows-done"}` {
		t.Errorf("rows-done = %s", done)
	}
	if _, err := json.Marshal(ClientFrame{Type: "nope"}); err == nil {
		t.Error("an unknown frame must not marshal")
	}
}

func TestDefaultOptionsLoadBackAndUnknownFieldsAreLenient(t *testing.T) {
	var opts SessionOptions
	if err := json.Unmarshal([]byte(`{"mode":"run","future-field":true,"prompt":null}`), &opts); err != nil {
		t.Fatal(err)
	}
	if opts.Mode == nil || *opts.Mode != "run" || opts.Prompt != nil {
		t.Errorf("opts = %+v", opts)
	}
}

func TestServerFramesMatchTheRustWire(t *testing.T) {
	for f, want := range map[*ServerFrame]string{
		{Type: FrameOpened}: `{"type":"opened"}`,
		new(Busy()):         `{"type":"busy"}`,
		new(Cancelled()):    `{"type":"cancelled","code":1}`,
		new(Result(10, []Selected{{Index: -1, Text: "custom text"}}, "quer")): `{"type":"result","code":10,"selected":[{"index":-1,"text":"custom text"}],"filter":"quer"}`,
		new(Result(0, nil, "")): `{"type":"result","code":0,"selected":[],"filter":""}`,
		new(Dump(nil)):          `{"type":"dump","items":[]}`,
	} {
		got, err := json.Marshal(*f)
		if err != nil || string(got) != want {
			t.Errorf("%+v = %s %v, want %s", *f, got, err, want)
		}
	}
	var back ServerFrame
	_ = json.Unmarshal([]byte(`{"type":"result","code":10,"selected":[{"index":-1,"text":"x"}],"filter":"q"}`), &back)
	if back.Code != 10 || back.Selected[0].Index != -1 || back.Filter != "q" {
		t.Errorf("decoded = %+v", back)
	}
}

func TestWidthTakesAllThreeRofiForms(t *testing.T) {
	for raw, want := range map[string]Width{
		"60":    {Unit: WidthPercent, Value: 60},
		"-30":   {Unit: WidthChars, Value: 30},
		"600px": {Unit: WidthPixels, Value: 600},
	} {
		got, err := ParseWidth(raw)
		if err != nil || got != want {
			t.Errorf("%q = %+v %v", raw, got, err)
		}
	}
	for _, bad := range []string{"wide", "12.5px", "px"} {
		if _, err := ParseWidth(bad); err == nil {
			t.Errorf("%q must be an error", bad)
		}
	}
	for w, want := range map[Width]string{
		{Unit: WidthChars, Value: 30}:   `{"chars":30}`,
		{Unit: WidthPixels, Value: 600}: `{"pixels":600}`,
	} {
		if got, _ := json.Marshal(w); string(got) != want {
			t.Errorf("%+v = %s", w, got)
		}
	}
	var w Width
	if err := json.Unmarshal([]byte(`{"inches":3}`), &w); err == nil {
		t.Error("an unknown width unit must be an error")
	}
}

// recordingHandler is a surface double.
type recordingHandler struct {
	opened chan *Session
	gone   chan uint64
}

func newRecordingHandler() *recordingHandler {
	return &recordingHandler{opened: make(chan *Session, 4), gone: make(chan uint64, 4)}
}

func (h *recordingHandler) Open(s *Session) { h.opened <- s }

func (h *recordingHandler) ClientGone(id uint64) { h.gone <- id }

func serve(t *testing.T) *recordingHandler {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	h := newRecordingHandler()
	stop, err := NewServer(h).Listen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return h
}

func TestASessionRepliesThroughTheSocket(t *testing.T) {
	h := serve(t)
	c, err := Open(SessionOptions{Mode: new("drun")}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	s := <-h.opened
	if s.Options.Mode == nil || *s.Options.Mode != "drun" {
		t.Fatalf("options = %+v", s.Options)
	}
	if f, _ := c.NextFrame(); f.Type != FrameOpened {
		t.Fatalf("first frame = %+v", f)
	}
	s.Reply(Result(0, []Selected{{Index: 2, Text: "c"}}, "q"))
	s.Reply(Cancelled()) // only the first reply counts
	f, err := c.NextFrame()
	if err != nil || f.Type != FrameResult || f.Selected[0].Text != "c" {
		t.Fatalf("result = %+v %v", f, err)
	}
	if _, err := c.NextFrame(); !errors.Is(err, ErrDisconnected) {
		t.Errorf("after the terminal frame the shell closes, got %v", err)
	}
}

func TestRowsStreamAndCloseOnRowsDone(t *testing.T) {
	h := serve(t)
	c, _ := Open(SessionOptions{Dmenu: true}, false)
	defer func() { _ = c.Close() }()
	s := <-h.opened
	_ = c.SendRows([]string{"alpha", "bravo"})
	_ = c.FinishRows()
	var got []string
	for chunk := range s.Rows {
		got = append(got, chunk...)
	}
	if !reflect.DeepEqual(got, []string{"alpha", "bravo"}) {
		t.Errorf("rows = %q", got)
	}
	// rows-done is not the client dying.
	select {
	case id := <-h.gone:
		t.Fatalf("rows-done read as a dead client %d", id)
	case <-time.After(100 * time.Millisecond):
	}
	s.Reply(Cancelled())
}

func TestAClientThatDiesIsReportedGone(t *testing.T) {
	h := serve(t)
	c, _ := Open(SessionOptions{}, false)
	s := <-h.opened
	_, _ = c.NextFrame()
	_ = c.Close()
	select {
	case id := <-h.gone:
		if id != s.ID {
			t.Errorf("gone id = %d, want %d", id, s.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a closed client must be reported gone")
	}
	s.Release()
}

func TestAConnectionWithoutAnOpenFrameIsDropped(t *testing.T) {
	h := serve(t)
	c, err := Open(SessionOptions{}, false)
	if err != nil {
		t.Fatal(err)
	}
	s := <-h.opened
	s.Release()
	_ = c.Close()
	// A rows frame first is not a session.
	raw, err := dialRaw()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	_, _ = raw.Write([]byte(`{"type":"rows","items":["x"]}` + "\n"))
	select {
	case <-h.opened:
		t.Fatal("a first frame that is not open must not start a session")
	case <-time.After(150 * time.Millisecond):
	}
}

package ipp

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestMessageRoundTrips(t *testing.T) {
	m := Message{Code: OpPrintJob, RequestID: 7, Groups: []Group{
		operation(PrinterURI("Office Laser"), String(TagKeyword, "requested-attributes", "a", "b", "c")),
		{Tag: tagJob, Attrs: []Attr{
			Int("copies", 3), Enum("print-quality", 5),
			{"printer-is-accepting-jobs", []Value{{Tag: TagBoolean, Int: 1}}},
			Ranges("page-ranges", [][2]int32{{1, 5}, {8, 8}}),
			Int("negative", -2),
		}},
	}}
	got, err := Decode(append(m.Encode(), "%PDF trailing document"...))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, m)
	}
	if got.Groups[0].Text("printer-uri") != "ipp://localhost/printers/Office%20Laser" {
		t.Errorf("printer-uri = %q", got.Groups[0].Text("printer-uri"))
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	good := Message{Code: 0, Groups: []Group{{Tag: tagPrinter, Attrs: []Attr{Int("copies", 1)}}}}.Encode()
	for name, data := range map[string][]byte{
		"empty":            nil,
		"no end tag":       good[:len(good)-1],
		"cut value":        good[:len(good)-3],
		"value before tag": {2, 0, 0, 0, 0, 0, 0, 1, TagInteger, 0, 1, 'a', 0, 4, 0, 0, 0, 1, tagEnd},
		"short integer":    {2, 0, 0, 0, 0, 0, 0, 1, tagPrinter, TagInteger, 0, 1, 'a', 0, 2, 0, 1, tagEnd},
		"orphan extra":     {2, 0, 0, 0, 0, 0, 0, 1, tagPrinter, TagInteger, 0, 0, 0, 4, 0, 0, 0, 1, tagEnd},
	} {
		if _, err := Decode(data); err == nil {
			t.Errorf("%s decoded", name)
		}
	}
}

// fakeCUPS answers like the scheduler: printers for CUPS-Get-Printers,
// a job id for Print-Job, recording what it was sent.
type fakeCUPS struct {
	mu       sync.Mutex
	paths    []string
	requests []Message
	docs     [][]byte
	status   uint16
}

func (f *fakeCUPS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	req, err := Decode(body)
	if err != nil || r.Header.Get("Content-Type") != "application/ipp" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.EscapedPath())
	f.requests = append(f.requests, req)
	f.docs = append(f.docs, body[len(req.Encode()):])
	status := f.status
	f.mu.Unlock()
	resp := Message{Code: status, RequestID: req.RequestID, Groups: []Group{operation("")}}
	if status >= 0x0400 {
		resp.Groups[0].Attrs = append(resp.Groups[0].Attrs, String(TagText, "status-message", "printer gone"))
	}
	switch req.Code {
	case OpCUPSGetPrinters:
		resp.Groups = append(resp.Groups,
			Group{Tag: tagPrinter, Attrs: []Attr{
				String(TagName, "printer-name", "office"), String(TagText, "printer-location", "2nd floor"),
				Enum("printer-state", StateIdle),
				{"printer-is-accepting-jobs", []Value{{Tag: TagBoolean, Int: 1}}},
			}},
			Group{Tag: tagPrinter, Attrs: []Attr{
				String(TagName, "printer-name", "lab"), Enum("printer-state", StateStopped),
				String(TagText, "printer-state-message", "Out of paper"),
				{"printer-is-accepting-jobs", []Value{{Tag: TagBoolean, Int: 0}}},
			}},
			Group{Tag: tagPrinter, Attrs: []Attr{String(TagText, "printer-location", "nameless")}},
		)
	case OpPrintJob:
		resp.Groups = append(resp.Groups, Group{Tag: tagJob, Attrs: []Attr{Int("job-id", 42)}})
	}
	w.Header().Set("Content-Type", "application/ipp")
	_, _ = w.Write(resp.Encode())
}

func TestClientListsPrinters(t *testing.T) {
	srv := httptest.NewServer(&fakeCUPS{})
	defer srv.Close()
	got, err := NewClient(strings.TrimPrefix(srv.URL, "http://")).Printers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Printer{
		{Name: "office", Location: "2nd floor", State: StateIdle, Accepting: true},
		{Name: "lab", StateMessage: "Out of paper", State: StateStopped, Accepting: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("printers = %+v, want %+v (a nameless queue is skipped)", got, want)
	}
}

func TestClientPrintsOverTheDomainSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "cups.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	cups := &fakeCUPS{}
	srv := &httptest.Server{Listener: l, Config: &http.Server{Handler: cups}}
	srv.Start()
	defer srv.Close()
	id, err := NewClient(sock).Print(context.Background(), Job{
		Printer: "office laser", Title: "report", User: "u", Format: "application/pdf",
		Attrs: []Attr{Int("copies", 2)}, Document: bytes.NewReader([]byte("%PDF-1.7 body")),
	})
	if err != nil || id != 42 {
		t.Fatalf("Print = %d, %v", id, err)
	}
	cups.mu.Lock()
	defer cups.mu.Unlock()
	if cups.paths[0] != "/printers/office%20laser" {
		t.Errorf("posted to %s", cups.paths[0])
	}
	req := cups.requests[0]
	op := req.Groups[0]
	if req.Code != OpPrintJob || op.Text("printer-uri") != "ipp://localhost/printers/office%20laser" ||
		op.Text("job-name") != "report" || op.Text("document-format") != "application/pdf" || op.Text("requesting-user-name") != "u" {
		t.Errorf("operation group = %+v", op)
	}
	if c, _ := req.Groups[1].Int("copies"); req.Groups[1].Tag != tagJob || c != 2 {
		t.Errorf("job group = %+v", req.Groups[1])
	}
	if string(cups.docs[0]) != "%PDF-1.7 body" {
		t.Errorf("document = %q", cups.docs[0])
	}
}

func TestClientReportsAFailedStatus(t *testing.T) {
	srv := httptest.NewServer(&fakeCUPS{status: 0x0406})
	defer srv.Close()
	_, err := NewClient(strings.TrimPrefix(srv.URL, "http://")).Print(context.Background(), Job{Printer: "x", Document: strings.NewReader("")})
	if !IsNotFound(err) || !strings.Contains(err.Error(), "printer gone") {
		t.Errorf("err = %v, want not-found with its message", err)
	}
	if IsNotFound(&StatusError{Code: 0x0400}) || IsNotFound(io.EOF) {
		t.Error("IsNotFound claimed another error")
	}
	if _, err := NewClient(filepath.Join(t.TempDir(), "none.sock")).Printers(context.Background()); err == nil {
		t.Error("a missing socket listed printers")
	}
}

func TestDefaultServer(t *testing.T) {
	t.Setenv("CUPS_SERVER", "/custom.sock")
	if got := DefaultServer(); got != "/custom.sock" {
		t.Errorf("with CUPS_SERVER = %q", got)
	}
	t.Setenv("CUPS_SERVER", "")
	saved := defaultSockets
	defer func() { defaultSockets = saved }()
	defaultSockets = []string{filepath.Join(t.TempDir(), "absent.sock")}
	if got := DefaultServer(); got != "localhost:631" {
		t.Errorf("no socket = %q, want localhost:631", got)
	}
	present := filepath.Join(t.TempDir(), "cups.sock")
	if l, err := net.Listen("unix", present); err == nil {
		defer func() { _ = l.Close() }()
	}
	defaultSockets = []string{present}
	if got := DefaultServer(); got != present {
		t.Errorf("socket present = %q", got)
	}
	if c := NewClient("printhost"); c.base != "http://printhost:631" {
		t.Errorf("a bare host = %s, want the IPP port", c.base)
	}
}

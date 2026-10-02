package ipp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// The CUPS scheduler's domain sockets, the default server where one
// exists (libcups' cupsServer order).
var defaultSockets = []string{"/run/cups/cups.sock", "/var/run/cups/cups.sock"}

// DefaultServer is libcups' server choice: $CUPS_SERVER (a socket path
// or host[:port]), else the scheduler's domain socket, else
// localhost:631.
func DefaultServer() string {
	if s := os.Getenv("CUPS_SERVER"); s != "" {
		return s
	}
	for _, s := range defaultSockets {
		if _, err := os.Stat(s); err == nil {
			return s
		}
	}
	return "localhost:631"
}

// Client talks IPP to one CUPS server.
type Client struct {
	http *http.Client
	// base is the HTTP origin requests go to; the printer URIs inside
	// them always name localhost, as libcups writes them.
	base string
	ids  atomic.Uint32
}

// requestTimeout bounds one request: CUPS answers at once, and a
// spooling document is local.
const requestTimeout = 30 * time.Second

// NewClient addresses server: an absolute socket path or host[:port].
func NewClient(server string) *Client {
	c := &Client{http: &http.Client{Timeout: requestTimeout}}
	if strings.HasPrefix(server, "/") {
		path := server
		c.http.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}
		c.base = "http://localhost"
		return c
	}
	if !strings.Contains(server, ":") {
		server += ":631"
	}
	c.base = "http://" + server
	return c
}

// Printer is one queue as the dialog lists it.
type Printer struct {
	Name, Location, StateMessage string
	// State is printer-state: 3 idle, 4 processing, 5 stopped.
	State     int32
	Accepting bool
}

// The printer-state values.
const (
	StateIdle       int32 = 3
	StateProcessing int32 = 4
	StateStopped    int32 = 5
)

// operation is the operation group every request opens with.
func operation(printerURI string, extra ...Attr) Group {
	attrs := []Attr{
		String(TagCharset, "attributes-charset", "utf-8"),
		String(TagLanguage, "attributes-natural-language", "en"),
		String(TagURI, "printer-uri", printerURI),
	}
	return Group{Tag: tagOperation, Attrs: append(attrs, extra...)}
}

// PrinterURI is the queue's URI as CUPS names it.
func PrinterURI(name string) string { return "ipp://localhost/printers/" + url.PathEscape(name) }

// Printers lists the queues (CUPS-Get-Printers).
func (c *Client) Printers(ctx context.Context) ([]Printer, error) {
	req := Message{Code: OpCUPSGetPrinters, Groups: []Group{operation("ipp://localhost/",
		String(TagKeyword, "requested-attributes",
			"printer-name", "printer-location", "printer-state", "printer-state-message", "printer-is-accepting-jobs"),
	)}}
	resp, err := c.do(ctx, "/", req, nil)
	if err != nil {
		return nil, err
	}
	var out []Printer
	for _, g := range resp.Groups {
		if g.Tag != tagPrinter {
			continue
		}
		p := Printer{Name: g.Text("printer-name"), Location: g.Text("printer-location"), StateMessage: g.Text("printer-state-message"), Accepting: true}
		if p.Name == "" {
			continue
		}
		if s, ok := g.Int("printer-state"); ok {
			p.State = s
		}
		if a, ok := g.Int("printer-is-accepting-jobs"); ok {
			p.Accepting = a != 0
		}
		out = append(out, p)
	}
	return out, nil
}

// Job is one document to spool.
type Job struct {
	Printer, Title, User string
	// Format is the document's MIME type.
	Format string
	// Attrs are the job template attributes (copies, sides, media...).
	Attrs    []Attr
	Document io.Reader
}

// Print sends a job (Print-Job) and returns its id.
func (c *Client) Print(ctx context.Context, j Job) (int32, error) {
	groups := []Group{operation(PrinterURI(j.Printer),
		String(TagName, "requesting-user-name", j.User),
		String(TagName, "job-name", j.Title),
		String(TagMimeMedia, "document-format", j.Format),
	)}
	if len(j.Attrs) > 0 {
		groups = append(groups, Group{Tag: tagJob, Attrs: j.Attrs})
	}
	resp, err := c.do(ctx, "/printers/"+url.PathEscape(j.Printer), Message{Code: OpPrintJob, Groups: groups}, j.Document)
	if err != nil {
		return 0, err
	}
	for _, g := range resp.Groups {
		if id, ok := g.Int("job-id"); ok {
			return id, nil
		}
	}
	return 0, nil
}

// do posts one request (and its document) and decodes the answer; an
// unsuccessful status is an error carrying status-message.
func (c *Client) do(ctx context.Context, path string, m Message, doc io.Reader) (Message, error) {
	m.RequestID = c.ids.Add(1)
	body := io.Reader(bytes.NewReader(m.Encode()))
	if doc != nil {
		body = io.MultiReader(body, doc)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, body)
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/ipp")
	res, err := c.http.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("ipp: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return Message{}, fmt.Errorf("ipp: HTTP %s", res.Status)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return Message{}, fmt.Errorf("ipp: %w", err)
	}
	resp, err := Decode(data)
	if err != nil {
		return Message{}, err
	}
	if !StatusOK(resp.Code) {
		msg := ""
		for _, g := range resp.Groups {
			if t := g.Text("status-message"); t != "" {
				msg = t
			}
		}
		return resp, &StatusError{Code: resp.Code, Message: msg}
	}
	return resp, nil
}

// StatusError is an unsuccessful IPP status.
type StatusError struct {
	Code    uint16
	Message string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("ipp: status %#04x: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("ipp: status %#04x", e.Code)
}

// IsNotFound reports client-error-not-found (no such printer, or no
// printers at all for CUPS-Get-Printers).
func IsNotFound(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == 0x0406
}

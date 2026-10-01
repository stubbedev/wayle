// Package icontest serves icon CDNs without the network, for tests of
// what installs icons.
package icontest

import (
	"io"
	"net/http"
	"strings"
	"sync"
)

// CDN answers GETs from a fixed set of files keyed by URL, 404 for the
// rest, and records every URL asked for.
type CDN struct {
	files map[string]string

	mu        sync.Mutex
	requested []string
}

// NewCDN serves files (URL to body).
func NewCDN(files map[string]string) *CDN { return &CDN{files: files} }

// Client is an HTTP client whose every request this CDN answers.
func (c *CDN) Client() *http.Client { return &http.Client{Transport: c} }

// Requested is every URL asked for, in order.
func (c *CDN) Requested() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.requested...)
}

// RoundTrip implements http.RoundTripper.
func (c *CDN) RoundTrip(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	c.mu.Lock()
	c.requested = append(c.requested, url)
	c.mu.Unlock()
	body, ok := c.files[url]
	status := http.StatusOK
	if !ok {
		status = http.StatusNotFound
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

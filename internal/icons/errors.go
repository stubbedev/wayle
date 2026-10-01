// Package icons ports crates/wayle-icons: the icon sources (Tabler,
// Simple Icons, Material, Lucide), fetching from their CDNs, the SVG to
// GTK-symbolic transform, local imports, the on-disk icon directory,
// format migration, and the config-driven sync behind `wayle icons`.
package icons

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"syscall"
)

// Error messages match error.rs's thiserror Display strings: the
// underlying cause of the I/O variants is carried but, as in Rust's
// to_string, not printed.

// FetchError is a non-success HTTP status from the CDN.
type FetchError struct {
	Slug   string
	Source string
	Status int
}

func (e *FetchError) Error() string {
	return fmt.Sprintf("cannot fetch icon '%s' from %s: HTTP %s", e.Slug, e.Source, statusText(e.Status))
}

// HTTPError is a failed request.
type HTTPError struct{ Detail string }

func (e *HTTPError) Error() string { return "HTTP request failed: " + e.Detail }

// ReadError is an unreadable file.
type ReadError struct {
	Path string
	Err  error
}

func (e *ReadError) Error() string { return fmt.Sprintf("cannot read file '%s'", e.Path) }
func (e *ReadError) Unwrap() error { return e.Err }

// WriteError is an icon that could not be written.
type WriteError struct {
	Path string
	Err  error
}

func (e *WriteError) Error() string { return fmt.Sprintf("cannot write icon to '%s'", e.Path) }
func (e *WriteError) Unwrap() error { return e.Err }

// DeleteError is an icon that could not be removed.
type DeleteError struct {
	Name string
	Err  error
}

func (e *DeleteError) Error() string { return fmt.Sprintf("cannot delete icon '%s'", e.Name) }
func (e *DeleteError) Unwrap() error { return e.Err }

// NotFoundError is a missing icon or path.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("icon '%s' not found", e.Name) }

// InvalidIconNameError is a name that would escape the icons directory.
type InvalidIconNameError struct{ Name string }

func (e *InvalidIconNameError) Error() string {
	return fmt.Sprintf("icon name '%s' must not contain '/', '\\', or '..'", e.Name)
}

// InvalidSourceError is an unknown source name.
type InvalidSourceError struct {
	Name      string
	Available string
}

func (e *InvalidSourceError) Error() string {
	return fmt.Sprintf("unknown icon source '%s', expected one of: %s", e.Name, e.Available)
}

// InvalidSVGError is content usvg rejects, or one with no extractable
// geometry.
type InvalidSVGError struct {
	Slug   string
	Reason string
}

func (e *InvalidSVGError) Error() string {
	return fmt.Sprintf("invalid SVG content for '%s': %s", e.Slug, e.Reason)
}

// reasonNoPaths is SvgValidationError::NoExtractablePaths.
const reasonNoPaths = "no extractable paths"

// DirectoryError is an icon directory that could not be created.
type DirectoryError struct {
	Path string
	Err  error
}

func (e *DirectoryError) Error() string {
	return fmt.Sprintf("cannot create icon directory '%s'", e.Path)
}
func (e *DirectoryError) Unwrap() error { return e.Err }

// ErrHomeNotSet is HomeNotSet.
var ErrHomeNotSet = errors.New("$HOME environment variable not set")

// rustStatusReasons are the http crate's canonical reasons where they
// differ from net/http's StatusText.
var rustStatusReasons = map[int]string{
	http.StatusRequestEntityTooLarge:        "Payload Too Large",
	http.StatusRequestURITooLong:            "URI Too Long",
	http.StatusRequestedRangeNotSatisfiable: "Range Not Satisfiable",
}

// statusText is http::StatusCode's Display: "404 Not Found".
func statusText(code int) string {
	reason, ok := rustStatusReasons[code]
	if !ok {
		reason = http.StatusText(code)
	}
	if reason == "" {
		reason = "<unknown status code>"
	}
	return fmt.Sprintf("%d %s", code, reason)
}

// IOErrorString renders err the way Rust's std::io::Error displays: an
// OS error as "<strerror> (os error N)".
func IOErrorString(err error) string {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		msg := errno.Error()
		if msg != "" {
			msg = strings.ToUpper(msg[:1]) + msg[1:]
		}
		return fmt.Sprintf("%s (os error %d)", msg, int(errno))
	}
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return IOErrorString(pe.Err)
	}
	return err.Error()
}

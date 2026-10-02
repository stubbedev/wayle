package portal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// email is org.freedesktop.impl.portal.Email (email.rs): the user's
// mail client opens a pre-filled compose window, through `xdg-email
// --attach` when there are attachments and a mailto: URI handed to
// xdg-open otherwise (or when xdg-email will not start).
type email struct{ spawn func([]string) error }

func emailIface(spawn func([]string) error) dbusx.Interface {
	return dbusx.Interface{
		Name:       "org.freedesktop.impl.portal.Email",
		Methods:    email{spawn},
		Properties: dbusx.Getters{"version": func() any { return uint32(3) }},
	}
}

// ComposeEmail opens the compose window.
func (e email) ComposeEmail(_ dbus.ObjectPath, _, _ string, options Vardict) (uint32, Vardict, *dbus.Error) {
	if attachments := copyAttachments(options); len(attachments) > 0 {
		err := e.spawn(xdgEmailArgv(options, attachments))
		if err == nil {
			return ResponseSuccess, Vardict{}, nil
		}
		warnf("email: xdg-email failed, falling back to mailto (attachments dropped): %v", err)
	}
	if err := e.spawn([]string{"xdg-open", mailto(options)}); err != nil {
		warnf("email: cannot launch xdg-open: %v", err)
		return ResponseOther, Vardict{}, nil
	}
	return ResponseSuccess, Vardict{}, nil
}

// recipients is the addresses list plus the single address.
func recipients(options Vardict) []string {
	to := stringList(options, "addresses")
	if s, ok := optString(options, "address"); ok {
		to = append(to, s)
	}
	return to
}

func xdgEmailArgv(options Vardict, attachments []string) []string {
	argv := []string{"xdg-email", "--utf8"}
	if s, ok := optString(options, "subject"); ok {
		argv = append(argv, "--subject", s)
	}
	if s, ok := optString(options, "body"); ok {
		argv = append(argv, "--body", s)
	}
	for _, cc := range stringList(options, "cc") {
		argv = append(argv, "--cc", cc)
	}
	for _, bcc := range stringList(options, "bcc") {
		argv = append(argv, "--bcc", bcc)
	}
	for _, path := range attachments {
		argv = append(argv, "--attach", path)
	}
	return append(argv, recipients(options)...)
}

// mailto builds the mailto: URI: the recipients, then cc, bcc, subject
// and body as query parameters.
func mailto(options Vardict) string {
	uri := "mailto:" + strings.Join(recipients(options), ",")
	var params []string
	for _, key := range []string{"cc", "bcc"} {
		if list := stringList(options, key); len(list) > 0 {
			params = append(params, key+"="+percentEncode(strings.Join(list, ","), ""))
		}
	}
	for _, key := range []string{"subject", "body"} {
		if s, ok := optString(options, key); ok {
			params = append(params, key+"="+percentEncode(s, ""))
		}
	}
	if len(params) > 0 {
		uri += "?" + strings.Join(params, "&")
	}
	return uri
}

// percentEncode keeps RFC 3986's unreserved bytes and those in keep,
// and escapes the rest.
func percentEncode(s, keep string) string {
	var b strings.Builder
	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-_.~", c) >= 0 || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// stringList reads an `as` option.
func stringList(options Vardict, key string) []string {
	list, _ := options[key].Value().([]string)
	return list
}

// copyAttachments copies each attachment_fds fd into a temp file and
// returns the paths: the fds are only valid during the call, and the
// files are left for the mail client to read.
func copyAttachments(options Vardict) []string {
	fds, _ := options["attachment_fds"].Value().([]dbus.UnixFD)
	var paths []string
	for i, fd := range fds {
		path := filepath.Join(os.TempDir(), fmt.Sprintf("wayle-email-%d-%d", os.Getpid(), i))
		if err := copyFd(fd, path); err != nil {
			warnf("email: attachment %d: %v", i, err)
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func copyFd(fd dbus.UnixFD, path string) error {
	src := os.NewFile(uintptr(fd), "attachment")
	defer func() { _ = src.Close() }()
	_, _ = src.Seek(0, io.SeekStart)
	dst, err := os.Create(path) //nolint:gosec // a fixed temp name
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	return err
}

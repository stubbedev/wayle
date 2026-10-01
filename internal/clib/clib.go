// Package clib is the shared purego plumbing for the native libraries
// wayle binds without cgo (libpam, libgstreamer): dlopen over a
// candidate list and C string copies.
package clib

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Open dlopens the first candidate that loads (RTLD_NOW|RTLD_GLOBAL),
// joining every failure when none does.
func Open(candidates []string) (uintptr, error) {
	var errs []string
	for _, name := range candidates {
		h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return h, nil
		}
		errs = append(errs, err.Error())
	}
	return 0, errors.New(strings.Join(errs, "; "))
}

// SystemCandidates is a soname through the loader's own search path
// (LD_LIBRARY_PATH, ld.so.cache), then the NixOS system profile — whose
// loader has no FHS default path — and the common distro locations.
func SystemCandidates(soname string) []string {
	return []string{
		soname,
		"/run/current-system/sw/lib/" + soname,
		"/usr/lib/" + soname,
		"/usr/lib64/" + soname,
		"/lib/x86_64-linux-gnu/" + soname,
		"/usr/lib/x86_64-linux-gnu/" + soname,
		"/lib/aarch64-linux-gnu/" + soname,
		"/usr/lib/aarch64-linux-gnu/" + soname,
	}
}

// GoString copies a NUL-terminated C string; nil is "".
func GoString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 { //nolint:gosec // audited: scans a NUL-terminated C string
		n++
	}
	return string(unsafe.Slice(p, n)) //nolint:gosec // audited: n bytes precede the NUL
}

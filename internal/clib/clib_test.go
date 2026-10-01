package clib

import (
	"slices"
	"testing"
)

func TestGoString(t *testing.T) {
	b := []byte("hello\x00world")
	if got := GoString(&b[0]); got != "hello" {
		t.Errorf("= %q", got)
	}
	if got := GoString(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	empty := []byte{0}
	if got := GoString(&empty[0]); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestOpen(t *testing.T) {
	if _, err := Open([]string{"libwayle-does-not-exist.so.9"}); err == nil {
		t.Error("a missing library opened")
	}
	if _, err := Open(SystemCandidates("libc.so.6")); err != nil {
		t.Errorf("libc: %v", err)
	}
	if c := SystemCandidates("libx.so.1"); c[0] != "libx.so.1" || !slices.Contains(c, "/run/current-system/sw/lib/libx.so.1") {
		t.Errorf("candidates = %v", c)
	}
}

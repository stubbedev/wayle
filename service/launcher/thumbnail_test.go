package launcher

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTheCachePathIsTheMd5OfTheFileURI(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/cache")
	path := ThumbnailCachePath("/home/user/Photos/me.png")
	sum := md5.Sum([]byte("file:///home/user/Photos/me.png"))
	if filepath.Base(path) != hex.EncodeToString(sum[:])+".png" {
		t.Errorf("name = %s", filepath.Base(path))
	}
	if filepath.Dir(path) != "/cache/thumbnails/normal" {
		t.Errorf("dir = %s", filepath.Dir(path))
	}
	// A relative XDG_CACHE_HOME is not honored.
	t.Setenv("XDG_CACHE_HOME", "relative")
	t.Setenv("HOME", "/home/u")
	if got := ThumbnailCacheDir(); got != "/home/u/.cache/thumbnails/normal" {
		t.Errorf("relative cache home = %s", got)
	}
}

func TestPreviewCmdPlaceholdersAreArgvNotAShellLine(t *testing.T) {
	th := NewThumbnailer(`mkthumb "{input}" "{output}" "{size}"`)
	argv, ok := th.Command("/tmp/a b.raw", "/cache/x.png")
	if !ok || !reflect.DeepEqual(argv, []string{"mkthumb", "/tmp/a b.raw", "/cache/x.png", "128"}) {
		t.Errorf("argv = %q", argv)
	}
	th = NewThumbnailer("mkthumb '{input}' '{output}'")
	argv, _ = th.Command("/tmp/x; rm -rf ~/.png", "/cache/x.png")
	if !reflect.DeepEqual(argv, []string{"mkthumb", "/tmp/x; rm -rf ~/.png", "/cache/x.png"}) {
		t.Errorf("a file name smuggled a command: %q", argv)
	}
}

func TestABrokenPreviewCmdProducesNothing(t *testing.T) {
	if _, ok := NewThumbnailer("mkthumb '{input}").Command("/tmp/a", "/cache/x.png"); ok {
		t.Error("half a command must not run")
	}
	if NewThumbnailer("   ").previewCmd != "" {
		t.Error("a blank preview-cmd must not shadow the system thumbnailers")
	}
}

func TestAThumbnailerDescriptorYieldsExecAndMimes(t *testing.T) {
	th, ok := ParseThumbnailer("[Thumbnailer Entry]\nTryExec=/bin/sh\nExec=gdk-pixbuf-thumbnailer -s %s %u %o\nMimeType=image/svg+xml;image/png;\n")
	if !ok || !reflect.DeepEqual(th.MimeTypes, []string{"image/svg+xml", "image/png"}) {
		t.Fatalf("parsed = %+v %v", th, ok)
	}
	argv, _ := th.Argv("/tmp/logo.svg", "/cache/x.png")
	if !reflect.DeepEqual(argv, []string{"gdk-pixbuf-thumbnailer", "-s", "128", "file:///tmp/logo.svg", "/cache/x.png"}) {
		t.Errorf("argv = %q", argv)
	}
}

func TestADescriptorMissingWhatItNeedsIsNotUsed(t *testing.T) {
	for name, body := range map[string]string{
		"no exec":         "[Thumbnailer Entry]\nMimeType=image/png;\n",
		"no mimetype":     "[Thumbnailer Entry]\nExec=thumb %i %o\n",
		"outside section": "Exec=thumb %i %o\nMimeType=image/png;\n",
		"try-exec absent": "[Thumbnailer Entry]\nTryExec=/nonexistent/thumbnailer\nExec=thumb %i %o\nMimeType=image/png;\n",
	} {
		if _, ok := ParseThumbnailer(body); ok {
			t.Errorf("%s: must not be used", name)
		}
	}
}

func TestGenerateRunsThePreviewCmdAndCaches(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "pic.raw")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	th := NewThumbnailer("cp {input} {output}")
	out, ok := th.Generate(context.Background(), file)
	if !ok || !strings.HasPrefix(out, ThumbnailCacheDir()) {
		t.Fatalf("generate = %q %v", out, ok)
	}
	if cached, ok := CachedThumbnail(file); !ok || cached != out {
		t.Errorf("cached = %q %v", cached, ok)
	}
	// An edited file invalidates the cached picture.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	if _, ok := CachedThumbnail(file); ok {
		t.Error("a thumbnail older than its file must not be reused")
	}
}

func TestAThumbnailerThatWritesNothingProducesNothing(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "pic.raw")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	if _, ok := NewThumbnailer("true {input}").Generate(context.Background(), file); ok {
		t.Error("a successful command that wrote no file must not count")
	}
}

func TestAFileWithNoThumbnailStillHasAnIconName(t *testing.T) {
	if MimeIcon("/tmp/notes.txt") == "" {
		t.Error("a mime icon must never be empty")
	}
}

package launcher

import (
	"context"
	"crypto/md5" //nolint:gosec // the freedesktop thumbnail spec names md5
	"encoding/hex"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/fileuri"
	"github.com/stubbedev/wayle/internal/mime"
	"github.com/stubbedev/wayle/internal/shlex"
)

// Thumbnails for row icons (rofi -preview-cmd and thumbnail://),
// thumbnail.rs. rofi-thumbnails(5)'s contract:
//
//   - a row asks for one by prefixing its icon with thumbnail://;
//   - the image is cached as the md5 of the file's URI under
//     $XDG_CACHE_HOME/thumbnails/<size>/ - the freedesktop spec, so a
//     thumbnail a file manager made is reused and one made here shows up
//     there;
//   - without -preview-cmd an XDG .thumbnailer for the mimetype makes
//     it (%s %u %i %o: size, URI, path, output);
//   - with -preview-cmd that command makes it ({input} {output} {size});
//   - a file nothing can thumbnail keeps its mimetype icon.
//
// Only the spec's normal size (128px) is generated: a launcher row is
// about 40px tall.

// ThumbnailSize is the freedesktop normal thumbnail size in pixels.
const ThumbnailSize = 128

// ThumbnailScheme is the icon prefix that asks for a thumbnail.
const ThumbnailScheme = "thumbnail://"

// ThumbnailCacheDir is $XDG_CACHE_HOME/thumbnails/normal (an absolute
// XDG_CACHE_HOME only), else ~/.cache/thumbnails/normal: the shared
// spec directory, deliberately not a wayle-private one.
func ThumbnailCacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(base) {
		base = ""
	}
	if base == "" {
		if home := os.Getenv("HOME"); home != "" {
			base = filepath.Join(home, ".cache")
		} else {
			base = os.TempDir()
		}
	}
	return filepath.Join(base, "thumbnails", "normal")
}

// ThumbnailCachePath is where file's thumbnail lives per the spec: the
// md5 of its file:// URI, as PNG.
func ThumbnailCachePath(file string) string {
	sum := md5.Sum([]byte(fileuri.FromPath(file))) //nolint:gosec // the spec's digest, not security
	return filepath.Join(ThumbnailCacheDir(), hex.EncodeToString(sum[:])+".png")
}

// CachedThumbnail returns an existing thumbnail no older than the file,
// so an edited image never keeps its old picture.
func CachedThumbnail(file string) (string, bool) {
	thumb := ThumbnailCachePath(file)
	made, err := os.Stat(thumb)
	if err != nil {
		return "", false
	}
	changed, err := os.Stat(file)
	if err != nil {
		return "", false
	}
	if made.ModTime().Before(changed.ModTime()) {
		return "", false
	}
	return thumb, true
}

// Thumbnailer makes thumbnails with -preview-cmd, else the system's
// XDG thumbnailers.
type Thumbnailer struct {
	previewCmd string
}

// NewThumbnailer uses previewCmd when it is set; a blank one is no
// command, so it never shadows the system thumbnailers.
func NewThumbnailer(previewCmd string) Thumbnailer {
	if strings.TrimSpace(previewCmd) == "" {
		previewCmd = ""
	}
	return Thumbnailer{previewCmd: previewCmd}
}

// Command is the argv that would produce file's thumbnail at output,
// false when nothing here can make one. The -preview-cmd argv is split
// before substituting and exec'd, never given to a shell: a file name
// is attacker-controlled in every mode that lists a directory.
func (t Thumbnailer) Command(file, output string) ([]string, bool) {
	if t.previewCmd != "" {
		argv := RenderArgv(t.previewCmd, Values(map[string]string{
			"input":  file,
			"output": output,
			"size":   strconv.Itoa(ThumbnailSize),
		}))
		if len(argv) == 0 {
			log.Printf("launcher: -preview-cmd %q does not parse as a command", t.previewCmd)
			return nil, false
		}
		return argv, true
	}
	th, ok := findThumbnailer(mime.System().TypeByName(file))
	if !ok {
		return nil, false
	}
	return th.Argv(file, output)
}

// Generate produces file's thumbnail and returns where it landed; a
// cached one is returned as is. A file nothing can thumbnail is a
// normal outcome (false), not an error: the caller keeps the fallback.
func (t Thumbnailer) Generate(ctx context.Context, file string) (string, bool) {
	if cached, ok := CachedThumbnail(file); ok {
		return cached, true
	}
	output := ThumbnailCachePath(file)
	argv, ok := t.Command(file, output)
	if !ok {
		return "", false
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		log.Printf("launcher: cannot create the thumbnail cache directory: %v", err)
		return "", false
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // the configured thumbnailer
	if err := cmd.Run(); err != nil {
		return "", false
	}
	// Exit status alone is not enough: a thumbnailer that succeeds
	// without writing leaves the row nothing to load.
	if st, err := os.Stat(output); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return output, true
}

// XDGThumbnailer is one installed .thumbnailer descriptor.
type XDGThumbnailer struct {
	// Exec still carries its %s %u %i %o.
	Exec      string
	MimeTypes []string
}

// Argv is the descriptor's command for file -> output.
func (x XDGThumbnailer) Argv(file, output string) ([]string, bool) {
	words, ok := shlex.Split(x.Exec)
	if !ok || len(words) == 0 {
		return nil, false
	}
	r := strings.NewReplacer("%s", strconv.Itoa(ThumbnailSize), "%u", fileuri.FromPath(file), "%i", file, "%o", output)
	for i, w := range words {
		words[i] = r.Replace(w)
	}
	return words, true
}

// ParseThumbnailer reads Exec and MimeType from a [Thumbnailer Entry]
// section. A TryExec that is not installed disqualifies the entry, as
// does a missing Exec or MimeType.
func ParseThumbnailer(contents string) (XDGThumbnailer, bool) {
	inSection := false
	var exec, tryExec string
	var mimes []string
	for line := range strings.SplitSeq(contents, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inSection = strings.EqualFold(line, "[Thumbnailer Entry]")
			continue
		}
		if !inSection || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Exec":
			exec = value
		case "TryExec":
			tryExec = value
		case "MimeType":
			mimes = mimes[:0]
			for m := range strings.SplitSeq(value, ";") {
				if m = strings.TrimSpace(m); m != "" {
					mimes = append(mimes, m)
				}
			}
		}
	}
	if tryExec != "" && !executableExists(tryExec) {
		return XDGThumbnailer{}, false
	}
	if exec == "" || len(mimes) == 0 {
		return XDGThumbnailer{}, false
	}
	return XDGThumbnailer{Exec: exec, MimeTypes: mimes}, true
}

func executableExists(program string) bool {
	if filepath.IsAbs(program) {
		st, err := os.Stat(program)
		return err == nil && st.Mode().IsRegular()
	}
	return InPath(program)
}

// findThumbnailer returns the first installed thumbnailer claiming
// mimeType, searching the spec's thumbnailers directories.
func findThumbnailer(mimeType string) (XDGThumbnailer, bool) {
	for _, dir := range mime.DataDirs() {
		entries, err := os.ReadDir(filepath.Join(dir, "thumbnailers"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".thumbnailer" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, "thumbnailers", e.Name())) //nolint:gosec // a system descriptor
			if err != nil {
				continue
			}
			th, ok := ParseThumbnailer(string(data))
			if ok && slices.Contains(th.MimeTypes, mimeType) {
				return th, true
			}
		}
	}
	return XDGThumbnailer{}, false
}

// MimeIcon is the icon-theme name for a file with no thumbnail: its
// mimetype's generic icon.
func MimeIcon(file string) string {
	db := mime.System()
	return db.GenericIcon(db.TypeByName(file))
}

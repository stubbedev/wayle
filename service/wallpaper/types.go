package wallpaper

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// FitMode is how an image is scaled to its monitor
// (types/fit_mode.rs).
type FitMode uint8

// Fit modes; Fill is the default.
const (
	// FitFill scales to cover the display, cropping the excess.
	FitFill FitMode = iota
	// FitFit scales to fit within the display, letterboxing.
	FitFit
	// FitCenter shows the image at its original size, centered.
	FitCenter
	// FitStretch stretches to exactly fill, ignoring aspect ratio.
	FitStretch
)

var fitModes = []string{"fill", "fit", "center", "stretch"}

// String is the mode's lowercase name.
func (m FitMode) String() string {
	if int(m) < len(fitModes) {
		return fitModes[m]
	}
	return fmt.Sprintf("FitMode(%d)", uint8(m))
}

// ParseFitMode reads a mode case-insensitively, as FitMode::from_str
// does for the D-Bus interface.
func ParseFitMode(s string) (FitMode, error) {
	if i := slices.Index(fitModes, strings.ToLower(s)); i >= 0 {
		return FitMode(i), nil
	}
	return 0, fmt.Errorf("Invalid fit mode: %s", s) //nolint:staticcheck // the Rust message, verbatim
}

// CyclingMode is the order images cycle in (types/cycling.rs).
type CyclingMode uint8

// Cycling modes; Sequential is the default.
const (
	// Sequential cycles in sorted path order.
	Sequential CyclingMode = iota
	// Shuffle cycles in a random order.
	Shuffle
)

var cyclingModes = []string{"sequential", "shuffle"}

// String is the mode's lowercase name.
func (m CyclingMode) String() string {
	if int(m) < len(cyclingModes) {
		return cyclingModes[m]
	}
	return fmt.Sprintf("CyclingMode(%d)", uint8(m))
}

// ParseCyclingMode reads a mode case-insensitively (CyclingMode::from_str).
func ParseCyclingMode(s string) (CyclingMode, error) {
	if i := slices.Index(cyclingModes, strings.ToLower(s)); i >= 0 {
		return CyclingMode(i), nil
	}
	return 0, fmt.Errorf("Invalid cycling mode: %s", s) //nolint:staticcheck // the Rust message, verbatim
}

// MonitorState is one monitor's wallpaper, scaling, and cycle position
// (types/monitor_state.rs). An empty Wallpaper means none is set.
type MonitorState struct {
	Wallpaper  string
	FitMode    FitMode
	CycleIndex int
}

// advance steps the cycle index forward, wrapping at count.
func (m *MonitorState) advance(count int) {
	if count > 0 {
		m.CycleIndex = (m.CycleIndex + 1) % count
	}
}

// previous steps the cycle index back, wrapping at zero.
func (m *MonitorState) previous(count int) {
	if count > 0 {
		if m.CycleIndex == 0 {
			m.CycleIndex = count - 1
		} else {
			m.CycleIndex--
		}
	}
}

// SupportedExtensions are the image extensions a cycling directory
// scan picks up, matched case-insensitively (SUPPORTED_EXTENSIONS).
var SupportedExtensions = []string{
	"png", "jpg", "jpeg", "gif", "bmp", "tga", "tiff", "webp", "pnm", "farbfeld", "svg", "jxl", "avif",
}

// Cycling is the shared cycling pool and timing
// (CyclingConfig). Per-monitor positions live in MonitorState.
type Cycling struct {
	Directory string
	Images    []string
	Mode      CyclingMode
	Interval  time.Duration
}

// ImageAt is the image at index, wrapping; false for an empty pool.
func (c *Cycling) ImageAt(index int) (string, bool) {
	if len(c.Images) == 0 {
		return "", false
	}
	return c.Images[index%len(c.Images)], true
}

// ImageCount is the pool size.
func (c *Cycling) ImageCount() int { return len(c.Images) }

func (c *Cycling) equal(o *Cycling) bool {
	if c == nil || o == nil {
		return c == o
	}
	return c.Directory == o.Directory && c.Mode == o.Mode && c.Interval == o.Interval && slices.Equal(c.Images, o.Images)
}

func (c *Cycling) clone() *Cycling {
	if c == nil {
		return nil
	}
	out := *c
	out.Images = slices.Clone(c.Images)
	return &out
}

// DirectoryNotFoundError is a cycling directory that does not exist.
type DirectoryNotFoundError struct{ Path string }

func (e *DirectoryNotFoundError) Error() string { return "directory not found: " + e.Path }

// NoImagesFoundError is a cycling directory without a supported image.
type NoImagesFoundError struct{ Path string }

func (e *NoImagesFoundError) Error() string { return "no images found in directory: " + e.Path }

// ImageNotFoundError is a wallpaper path that does not exist.
type ImageNotFoundError struct{ Path string }

func (e *ImageNotFoundError) Error() string { return "image not found: " + e.Path }

// newCycling scans directory into a pool ordered by mode
// (CyclingConfig::new).
func newCycling(directory string, mode CyclingMode, interval time.Duration, rng *rand.Rand) (*Cycling, error) {
	if !exists(directory) {
		return nil, &DirectoryNotFoundError{Path: directory}
	}
	images, err := scanImages(directory)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, &NoImagesFoundError{Path: directory}
	}
	orderImages(images, mode, rng)
	return &Cycling{Directory: directory, Images: images, Mode: mode, Interval: interval}, nil
}

// refreshed is the pool rescanned (CyclingConfig::refresh): an emptied
// directory empties the pool rather than failing.
func (c *Cycling) refreshed(rng *rand.Rand) (*Cycling, error) {
	images, err := scanImages(c.Directory)
	if err != nil {
		return nil, err
	}
	out := *c
	orderImages(images, c.Mode, rng)
	out.Images = images
	return &out, nil
}

func orderImages(images []string, mode CyclingMode, rng *rand.Rand) {
	switch mode {
	case Sequential:
		slices.Sort(images)
	case Shuffle:
		rng.Shuffle(len(images), func(i, j int) { images[i], images[j] = images[j], images[i] })
	}
}

// scanImages lists the directory's entries with a supported extension
// (scan_directory_for_images); like the Rust scan it does not look at
// the entry type, so a directory named x.png is listed too.
func scanImages(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("I/O error: %w", err)
	}
	images := []string{}
	for _, e := range entries {
		if ext, ok := extension(e.Name()); ok && slices.Contains(SupportedExtensions, strings.ToLower(ext)) {
			images = append(images, filepath.Join(directory, e.Name()))
		}
	}
	return images, nil
}

// extension is Path::extension: the text after the last dot, where a
// leading dot alone (".png") names the file rather than starting an
// extension.
func extension(name string) (string, bool) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return "", false
	}
	return name[i+1:], true
}

// exists is Path::exists: stat succeeds, following symlinks.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

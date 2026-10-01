package bar

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/wayle/service/pulse"
)

// deviceChoice is DeviceChoice: id is what the pipeline consumes ("" is
// the default or automatic entry), label what the picker shows.
type deviceChoice struct {
	id, label string
}

// microphoneSources is microphone_sources: "Default", then every
// non-monitor source by description (else name).
func microphoneSources(src pulse.Source) []deviceChoice {
	choices := []deviceChoice{{"", "Default"}}
	if src == nil {
		return choices
	}
	for _, d := range src.InputDevices() {
		if d.IsMonitor() {
			continue
		}
		label := d.Description
		if label == "" {
			label = d.Name
		}
		choices = append(choices, deviceChoice{d.Name, label})
	}
	return choices
}

// v4l2Capability mirrors struct v4l2_capability (104 bytes).
type v4l2Capability struct {
	driver       [16]byte
	card         [32]byte
	busInfo      [32]byte
	version      uint32
	capabilities uint32
	deviceCaps   uint32
	reserved     [3]uint32
}

// V4L2 constants (videodev2.h).
const (
	vidiocQuerycap      = 0x80685600
	v4l2CapVideoCapture = 0x00000001
	v4l2CapDeviceCaps   = 0x80000000
)

// captureCard is capture_card: the card name of a node that captures
// video, else false.
func captureCard(path string) (string, bool) {
	f, err := os.Open(path) //nolint:gosec // a /dev/videoN node
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	var c v4l2Capability
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), vidiocQuerycap, uintptr(unsafe.Pointer(&c))); errno != 0 { //nolint:gosec // audited: VIDIOC_QUERYCAP fills the 104-byte struct
		return "", false
	}
	caps := c.capabilities
	if caps&v4l2CapDeviceCaps != 0 {
		caps = c.deviceCaps
	}
	if caps&v4l2CapVideoCapture == 0 {
		return "", false
	}
	end := slices.Index(c.card[:], 0)
	if end < 0 {
		end = len(c.card)
	}
	card := strings.TrimSpace(string(c.card[:end]))
	return card, card != ""
}

// cameras is cameras(): "Automatic", then one entry per distinct card
// name at its lowest capture node, the nodes listed from sysDir
// (/sys/class/video4linux) and probed in devDir (/dev).
func cameras(sysDir, devDir string, probe func(path string) (string, bool)) []deviceChoice {
	choices := []deviceChoice{{"", "Automatic"}}
	entries, err := os.ReadDir(sysDir)
	if err != nil {
		return choices
	}
	var nodes []int
	for _, e := range entries {
		if n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "video")); err == nil && strings.HasPrefix(e.Name(), "video") {
			nodes = append(nodes, n)
		}
	}
	slices.Sort(nodes)
	seen := map[string]bool{}
	for _, n := range nodes {
		path := filepath.Join(devDir, "video"+strconv.Itoa(n))
		card, ok := probe(path)
		if !ok || seen[card] {
			continue
		}
		seen[card] = true
		choices = append(choices, deviceChoice{path, card})
	}
	return choices
}

// choiceIndex is index_of: the saved id's index, else the default 0.
func choiceIndex(choices []deviceChoice, id string) int {
	return max(slices.IndexFunc(choices, func(c deviceChoice) bool { return c.id == id }), 0)
}

// choiceLabels are the picker rows.
func choiceLabels(choices []deviceChoice) []string {
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = c.label
	}
	return labels
}

// recorderCameras lists the system's cameras; tests substitute it.
var recorderCameras = func() []deviceChoice {
	return cameras("/sys/class/video4linux", "/dev", captureCard)
}

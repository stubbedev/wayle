package recorder

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// DeviceChoice is DeviceChoice: ID is what the pipeline consumes (""
// is the default or automatic entry), Label what a picker shows.
type DeviceChoice struct {
	ID, Label string
}

// Source is an audio input as the microphone pickers see it.
type Source struct {
	Name, Description string
	// Monitor is a sink's monitor, never a microphone.
	Monitor bool
}

// MicrophoneChoices is microphone_sources: "Default", then every
// named non-monitor source by description (else name).
func MicrophoneChoices(sources []Source) []DeviceChoice {
	choices := []DeviceChoice{{"", "Default"}}
	for _, s := range sources {
		if s.Name == "" || s.Monitor {
			continue
		}
		label := s.Description
		if label == "" {
			label = s.Name
		}
		choices = append(choices, DeviceChoice{s.Name, label})
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

// Cameras is cameras(): "Automatic", then one entry per distinct card
// name at its lowest capture node.
func Cameras() []DeviceChoice {
	return cameras("/sys/class/video4linux", "/dev", captureCard)
}

// cameras lists the nodes from sysDir (/sys/class/video4linux) and
// probes them in devDir (/dev).
func cameras(sysDir, devDir string, probe func(path string) (string, bool)) []DeviceChoice {
	choices := []DeviceChoice{{"", "Automatic"}}
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
		choices = append(choices, DeviceChoice{path, card})
	}
	return choices
}

// ChoiceIndex is index_of: the saved id's index, else the default 0.
func ChoiceIndex(choices []DeviceChoice, id string) int {
	return max(slices.IndexFunc(choices, func(c DeviceChoice) bool { return c.ID == id }), 0)
}

// ChoiceLabels are the picker rows.
func ChoiceLabels(choices []DeviceChoice) []string {
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = c.Label
	}
	return labels
}

// WithSaved keeps a saved device the list does not offer (unplugged,
// or not enumerated yet) selectable: it is appended, labelled by its
// id. The default "" needs no entry.
func WithSaved(choices []DeviceChoice, id string) []DeviceChoice {
	if id == "" || slices.ContainsFunc(choices, func(c DeviceChoice) bool { return c.ID == id }) {
		return choices
	}
	return append(slices.Clip(choices), DeviceChoice{id, id})
}

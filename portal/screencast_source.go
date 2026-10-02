package portal

import (
	"fmt"
	"strconv"
	"strings"
)

// Source types of ScreenCast (the AvailableSourceTypes bits).
const (
	sourceMonitor uint32 = 1
	sourceWindow  uint32 = 2
	sourceVirtual uint32 = 4
)

// Cursor modes of ScreenCast (AvailableCursorModes bits).
const (
	cursorHidden   uint32 = 1
	cursorEmbedded uint32 = 2
	cursorMetadata uint32 = 4
)

// showCursor reports whether a cursor_mode puts the pointer in the
// frames: embedded, or metadata (unsupported, so embedded instead).
func showCursor(mode uint32) bool { return mode&(cursorEmbedded|cursorMetadata) != 0 }

// captureKind is what a captureTarget names.
type captureKind uint8

const (
	targetOutput captureKind = iota
	targetWindow
	targetRegion
)

// captureTarget is one source the user picked (source.rs
// CaptureTarget): an output by connector name, a window by its
// ext-foreign-toplevel identifier, or a region of an output in its
// logical coordinates.
type captureTarget struct {
	kind captureKind
	// name is the output connector, or the window identifier.
	name                string
	x, y, width, height int32
}

// sourceType is the target's AvailableSourceTypes bit.
func (t captureTarget) sourceType() uint32 {
	switch t.kind {
	case targetWindow:
		return sourceWindow
	case targetRegion:
		return sourceVirtual
	}
	return sourceMonitor
}

// payload is the target as the share picker writes it (and a restore
// token keeps it): screen:<name>, window:<id>, region:<out>@x,y,w,h.
func (t captureTarget) payload() string {
	switch t.kind {
	case targetWindow:
		return "window:" + t.name
	case targetRegion:
		return fmt.Sprintf("region:%s@%d,%d,%d,%d", t.name, t.x, t.y, t.width, t.height)
	}
	return "screen:" + t.name
}

// parseTarget reads one payload; anything malformed is no target.
func parseTarget(payload string) (captureTarget, bool) {
	if name, ok := strings.CutPrefix(payload, "screen:"); ok {
		return captureTarget{kind: targetOutput, name: name}, name != ""
	}
	if id, ok := strings.CutPrefix(payload, "window:"); ok {
		return captureTarget{kind: targetWindow, name: id}, id != ""
	}
	spec, ok := strings.CutPrefix(payload, "region:")
	if !ok {
		return captureTarget{}, false
	}
	out, rect, ok := strings.Cut(spec, "@")
	nums := strings.Split(rect, ",")
	if !ok || out == "" || len(nums) != 4 {
		return captureTarget{}, false
	}
	var v [4]int32
	for i, n := range nums {
		parsed, err := strconv.ParseInt(n, 10, 32)
		if err != nil {
			return captureTarget{}, false
		}
		v[i] = int32(parsed)
	}
	if v[2] <= 0 || v[3] <= 0 {
		return captureTarget{}, false
	}
	return captureTarget{kind: targetRegion, name: out, x: v[0], y: v[1], width: v[2], height: v[3]}, true
}

// parsePickerReply reads the share picker's answer:
// `[r]/<payload>[;<payload>...]`, r granting a restore token. Empty is
// a cancel; any malformed piece rejects the whole reply.
func parsePickerReply(reply string) (allowToken bool, targets []captureTarget, ok bool) {
	flags, payload, found := strings.Cut(reply, "/")
	if reply == "" || !found {
		return false, nil, false
	}
	for piece := range strings.SplitSeq(payload, ";") {
		t, ok := parseTarget(piece)
		if !ok {
			return false, nil, false
		}
		targets = append(targets, t)
	}
	return strings.Contains(flags, "r"), targets, true
}

// effectiveFPS clamps the requested rate to the output's refresh
// (unknown for a window: unclamped), never below 1.
func effectiveFPS(requested uint32, refreshMHz int32) uint32 {
	if refreshMHz > 0 {
		requested = min(requested, max(uint32(refreshMHz)/1000, 1))
	}
	return max(requested, 1)
}

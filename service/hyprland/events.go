package hyprland

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func decodeJSON[T any](reply string) (T, error) {
	var out T
	if err := json.Unmarshal([]byte(reply), &out); err != nil {
		return out, fmt.Errorf("hyprland: decode response: %w", err)
	}
	return out, nil
}

// EventKind identifies a parsed event-stream line: the wire kind as
// sent after the ">>" separator's left side. The named constants cover
// the events the shell switches on; anything else parses with its raw
// kind preserved, so new compositor events are never lost.
type EventKind string

// The events the shell consumes. Everything else parses to KindOther
// with the raw payload kept, so new compositor events are never lost.
const (
	EventWorkspace      EventKind = "workspace"
	EventWorkspaceV2    EventKind = "workspacev2"
	EventFocusedMon     EventKind = "focusedmon"
	EventFocusedMonV2   EventKind = "focusedmonv2"
	EventCreateWorkspc  EventKind = "createworkspace"
	EventCreateWspcV2   EventKind = "createworkspacev2"
	EventDestroyWorkspc EventKind = "destroyworkspace"
	EventDestroyWspcV2  EventKind = "destroyworkspacev2"
	EventMoveWorkspace  EventKind = "moveworkspace"
	EventOpenWindow     EventKind = "openwindow"
	EventCloseWindow    EventKind = "closewindow"
	EventMoveWindow     EventKind = "movewindow"
	EventMoveWindowV2   EventKind = "movewindowv2"
	EventFullscreen     EventKind = "fullscreen"
	EventUrgent         EventKind = "urgent"
)

// Event is one parsed line from the event socket. Fields not named by
// the event kind stay at their zero values.
type Event struct {
	Kind EventKind
	// Name is the workspace or monitor name the event carries.
	Name string
	// ID is the numeric workspace/monitor id on v2 events (-1 when the
	// event carries no id).
	ID int
	// Payload is the raw data after the ">>" separator.
	Payload string
}

// ParseEvent splits one socket2 line into an Event. Lines without the
// ">>" separator fail the parse — the Rust shell warns and drops those,
// and so does the caller here (ok=false).
func ParseEvent(line string) (Event, bool) {
	kind, data, found := strings.Cut(line, ">>")
	if !found {
		return Event{}, false
	}
	ev := Event{Kind: EventKind(kind), Payload: data, ID: -1}
	switch ev.Kind {
	case EventWorkspace:
		ev.Name = data
	case EventWorkspaceV2, EventCreateWspcV2, EventDestroyWspcV2:
		ev.ID, ev.Name = splitIDName(data)
	case EventCreateWorkspc, EventDestroyWorkspc:
		ev.Name = data
	case EventFocusedMon:
		monitor, ws, _ := strings.Cut(data, ",")
		ev.Name, ev.ID = monitor, wsNameToID(ws)
	case EventFocusedMonV2:
		id, monitor, _ := strings.Cut(data, ",")
		ev.ID, ev.Name = atoiOr(id), monitor
	case EventMoveWorkspace, EventMoveWindow:
		// "name,monitor" / "address,monitor"
		_, monitor, _ := strings.Cut(data, ",")
		ev.Name = monitor
	case EventMoveWindowV2:
		// "address,workspaceid,monitorname"
		parts := strings.SplitN(data, ",", 3)
		if len(parts) == 3 {
			ev.ID, ev.Name = atoiOr(parts[1]), parts[2]
		}
	case EventOpenWindow:
		// "address,workspacename,class,title"
		parts := strings.SplitN(data, ",", 3)
		if len(parts) >= 2 {
			ev.Name = parts[1]
		}
	case EventCloseWindow:
		ev.Name = data
	case EventFullscreen, EventUrgent:
		// payload only
	}
	return ev, true
}

// splitIDName decodes the "id,name" payload of v2 workspace events.
func splitIDName(data string) (int, string) {
	id, name, found := strings.Cut(data, ",")
	if !found {
		return -1, data
	}
	return atoiOr(id), name
}

// wsNameToID converts a v1 workspace name to its numeric form:
// "name:3" carries the id after the colon, plain names parse directly,
// special workspaces have no number and map to -1.
func wsNameToID(name string) int {
	if rest, found := strings.CutPrefix(name, "name:"); found {
		return atoiOr(rest)
	}
	if id, err := strconv.Atoi(name); err == nil {
		return id
	}
	return -1
}

func atoiOr(s string) int {
	id, _ := strconv.Atoi(s)
	return id
}

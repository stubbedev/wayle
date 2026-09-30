package sharepreview

import (
	"log"
	"strconv"
	"strings"
)

// Toplevel is one shareable window (toplevel.rs): an entry of the
// XDPH_WINDOW_SHARING_LIST xdg-desktop-portal-hyprland hands the share
// picker, or of the generic ext-foreign-toplevel-list fallback.
type Toplevel struct {
	// ID is the XDPH toplevel id - its own wlr-foreign-toplevel resource
	// id, which only XDPH resolves - or a display-only index for
	// fallback entries.
	ID uint64
	// Class and Title are the window's class (app id) and title.
	Class, Title string
	// WindowAddress is the Hyprland window address when XDPH sent one
	// ([HA>]); zero with HasAddress false otherwise.
	WindowAddress uint64
	HasAddress    bool
	// Identifier is the stable ext-foreign-toplevel-list identifier of a
	// fallback entry, empty for XDPH entries (which use ID).
	Identifier string
}

// The XDPH list separators: id, class, title, and window address
// terminators.
const (
	sepID      = "[HC>]"
	sepClass   = "[HT>]"
	sepTitle   = "[HE>]"
	sepAddress = "[HA>]"
)

// ParseList parses an XDPH_WINDOW_SHARING_LIST value
// (`<id>[HC>]<class>[HT>]<title>[HE>]<address>[HA>]...`, see XDPH's
// ScreencopyShared.cpp). The address field is optional for older
// Hyprland releases. Parsing stops at the first malformed entry,
// keeping what came before, and logs why - toplevel.rs's behavior,
// including its reading of separators from the start of the remaining
// text.
func ParseList(list string) []Toplevel {
	var out []Toplevel
	rest := list
	for rest != "" {
		idEnd := strings.Index(rest, sepID)
		if idEnd < 0 {
			log.Print("share picker: found no toplevel id separator")
			break
		}
		id, err := strconv.ParseUint(rest[:idEnd], 10, 64)
		if err != nil {
			log.Print("share picker: toplevel id cannot be parsed to unsigned integer")
			break
		}
		classEnd := strings.Index(rest, sepClass)
		if classEnd < 0 || classEnd < idEnd+len(sepID) {
			log.Print("share picker: found no toplevel class separator")
			break
		}
		titleEnd := strings.Index(rest, sepTitle)
		if titleEnd < 0 || titleEnd < classEnd+len(sepClass) {
			log.Print("share picker: found no toplevel title separator")
			break
		}
		tl := Toplevel{
			ID:    id,
			Class: rest[idEnd+len(sepID) : classEnd],
			Title: rest[classEnd+len(sepClass) : titleEnd],
		}
		addrEnd := strings.Index(rest, sepAddress)
		if addrEnd < 0 {
			// Compatibility with Hyprland releases before [HA>].
			log.Print("share picker: found no toplevel window separator")
			rest = rest[titleEnd+len(sepTitle):]
		} else {
			if addrEnd < titleEnd+len(sepTitle) {
				log.Print("share picker: window address cannot be parsed to unsigned integer")
				break
			}
			addr, err := strconv.ParseUint(rest[titleEnd+len(sepTitle):addrEnd], 10, 64)
			if err != nil {
				log.Print("share picker: window address cannot be parsed to unsigned integer")
				break
			}
			tl.WindowAddress, tl.HasAddress = addr, true
			rest = rest[addrEnd+len(sepAddress):]
		}
		out = append(out, tl)
	}
	return out
}

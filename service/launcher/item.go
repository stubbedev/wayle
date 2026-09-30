// Package launcher is the launcher engine, the rofi-replacement core
// (crates/wayle-launcher): the Mode contract, the matching and ranking
// engine, run history with frecency, the rofi hook and template
// grammar, and the Session that ties them together. Pure logic, no
// widgets - the surface lives in shell/launcher. The modes themselves
// are in service/launcher/modes.
package launcher

// ItemFlags is a row's presentation and behavior bits (rofi row
// metadata).
type ItemFlags uint8

// Row flags.
const (
	// FlagUrgent styles the row urgent (rofi -u, row option urgent).
	FlagUrgent ItemFlags = 1 << iota
	// FlagActive styles the row active (rofi -a, row option active).
	FlagActive
	// FlagNonselectable marks a row that cannot be activated.
	FlagNonselectable
	// FlagPermanent keeps a row visible whatever the filter.
	FlagPermanent
	// FlagMarkup marks the display text as Pango markup.
	FlagMarkup
)

// Icon is where a row icon comes from: an IconName, an IconFile, or an
// IconThumbnail. A nil Icon is no icon.
type Icon interface{ isIcon() }

// IconName is a freedesktop icon-theme name.
type IconName string

// IconFile is an image file on disk.
type IconFile string

// IconThumbnail is a thumbnail of Path, generated on demand (rofi
// thumbnail:// and -preview-cmd). It carries its own Fallback because a
// file nothing can thumbnail still has to show something, and the row
// is drawn before any thumbnail exists.
type IconThumbnail struct {
	// Path is the file to picture.
	Path string
	// Fallback is the icon-theme name shown until (or instead of) the
	// thumbnail.
	Fallback string
}

func (IconName) isIcon()      {}
func (IconFile) isIcon()      {}
func (IconThumbnail) isIcon() {}

// Item is one list entry. Its index in the mode's item slice is its
// identity: modes get that index back on activate and delete.
type Item struct {
	// Display is the text shown (Pango markup when FlagMarkup is set).
	Display string
	// MatchText is what the matcher sees: the display plus invisible
	// meta keywords.
	MatchText string
	// Icon is the optional row icon.
	Icon Icon
	// Info is opaque per-row data handed back on selection (ROFI_INFO);
	// nil when the row carries none.
	Info *string
	// Flags are the row's presentation and behavior bits.
	Flags ItemFlags
}

// NewItem is a plain item whose display text doubles as its match text.
func NewItem(display string) Item {
	return Item{Display: display, MatchText: display}
}

// Has reports whether every bit of f is set.
func (f ItemFlags) Has(bits ItemFlags) bool { return f&bits == bits }

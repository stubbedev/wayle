package modes

import (
	"context"
	"strconv"

	"github.com/stubbedev/wayle/service/clipboard"
	"github.com/stubbedev/wayle/service/launcher"
)

// ClipboardHistory is what the clipboard mode needs from the session's
// clipboard service; *clipboard.Clipboard implements it.
type ClipboardHistory interface {
	Entries() []clipboard.Entry
	Forget(id uint64) bool
	Clear()
	Copy(id uint64) bool
}

// Row icons, one per clipboard.Kind (clipboard.rs).
const (
	iconText  = "ld-file-text-symbolic"
	iconFiles = "ld-folder-symbolic"
	iconImage = "ld-image-symbolic"
	iconOther = "ld-layers-symbolic"
)

// previewChars is how much of an entry a row shows.
const previewChars = 160

// kb-custom-1 forgets the selected entry, kb-custom-2 the whole
// history: a clipboard history is exactly where a password ends up when
// a password manager forgets to mark it.
const (
	forgetOne uint8 = 1
	forgetAll uint8 = 2
)

// Clipboard lists the session's clipboard history (clipboard.rs): the
// cliphist | rofi -dmenu | wl-copy pipeline without a second daemon.
// Accepting a row puts it back under the mime it arrived with, so a
// copied file goes back as a file and an image as an image.
type Clipboard struct {
	history ClipboardHistory
	// ids are the entry ids in row order, so a row index resolves to
	// the entry it was drawn for even after something new arrives.
	ids []uint64
}

// NewClipboard builds the mode; a nil history is a compositor without
// data-control, which the mode says rather than looking empty.
func NewClipboard(history ClipboardHistory) *Clipboard { return &Clipboard{history: history} }

// Name is "clipboard".
func (*Clipboard) Name() string { return "clipboard" }

func (c *Clipboard) state() launcher.ModeState {
	if c.history == nil {
		c.ids = nil
		msg := "No clipboard history: the compositor does not support wlr-data-control."
		return launcher.ModeState{Prompt: "clipboard", Message: &msg, NoCustom: true, UseHotKeys: true}
	}
	entries := c.history.Entries()
	c.ids = make([]uint64, len(entries))
	items := make([]launcher.Item, len(entries))
	for i, e := range entries {
		c.ids[i] = e.ID
		preview := e.Preview(previewChars)
		info := strconv.FormatUint(e.ID, 10)
		items[i] = launcher.Item{Display: preview, MatchText: preview, Info: &info, Icon: iconFor(e)}
	}
	// Typed text matching nothing must not land on the clipboard: this
	// mode restores, it does not compose.
	return launcher.ModeState{Items: items, Prompt: "clipboard", NoCustom: true, UseHotKeys: true}
}

// idAt is the entry id a row was drawn for.
func (c *Clipboard) idAt(target launcher.Target) (uint64, bool) {
	i, ok := target.Row()
	if !ok || int(i) >= len(c.ids) {
		return 0, false
	}
	return c.ids[i], true
}

// Load lists the history, most recent first.
func (c *Clipboard) Load(context.Context) launcher.ModeState { return c.state() }

// Activate restores the entry; kb-custom-1/2 forget one or all.
func (c *Clipboard) Activate(_ context.Context, target launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	id, ok := c.idAt(target)
	if c.history == nil || !ok {
		return launcher.ActionNothing{}
	}
	if k, ok := kind.(launcher.ActivateKbCustom); ok {
		switch k.N {
		case forgetOne:
			c.history.Forget(id)
			return launcher.ActionReload{State: c.state()}
		case forgetAll:
			c.history.Clear()
			return launcher.ActionReload{State: c.state()}
		}
	}
	// Every other accept restores, the alternate one included: there
	// is no second thing to do with a clipboard entry.
	c.history.Copy(id)
	return launcher.ActionClose{}
}

// AllowsCustom is false.
func (*Clipboard) AllowsCustom() bool { return false }

// iconFor is a row's icon: one copied file is pictured by a thumbnail
// of itself; everything else, a multi-file copy included, gets a name.
func iconFor(e clipboard.Entry) launcher.Icon {
	var name string
	switch e.Kind() {
	case clipboard.KindText:
		name = iconText
	case clipboard.KindFiles:
		name = iconFiles
	case clipboard.KindImage:
		name = iconImage
	default:
		name = iconOther
	}
	if e.Kind() == clipboard.KindFiles {
		if paths := e.Paths(); len(paths) == 1 {
			return launcher.IconThumbnail{Path: paths[0], Fallback: name}
		}
	}
	return launcher.IconName(name)
}

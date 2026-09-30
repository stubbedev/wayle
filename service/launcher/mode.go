package launcher

import "context"

// ActivateKind is how an entry was accepted: ActivateDefault,
// ActivateAlt, ActivateCustom, or ActivateKbCustom.
type ActivateKind interface{ isActivateKind() }

// ActivateDefault is the plain accept (Enter).
type ActivateDefault struct{}

// ActivateAlt is the alternate accept (Shift+Enter: run in a terminal,
// the window alt command).
type ActivateAlt struct{}

// ActivateCustom accepts typed text that matches no row.
type ActivateCustom struct{ Text string }

// ActivateKbCustom is kb-custom-N, N in 1..19 (dmenu and script exit
// codes 10..28).
type ActivateKbCustom struct{ N uint8 }

func (ActivateDefault) isActivateKind()  {}
func (ActivateAlt) isActivateKind()      {}
func (ActivateCustom) isActivateKind()   {}
func (ActivateKbCustom) isActivateKind() {}

// Target is what an accept is about: one row, or no row (the typed
// text). The zero Target is no row, so a forgotten index can never
// silently mean row 0.
type Target struct {
	row uint32
	ok  bool
}

// RowTarget targets row index i of the mode's items.
func RowTarget(i uint32) Target { return Target{row: i, ok: true} }

// NoRow targets the typed text rather than a row.
var NoRow = Target{}

// Row returns the targeted index, false when the target is no row.
func (t Target) Row() (uint32, bool) { return t.row, t.ok }

// Selection is one accepted row handed back to a dmenu client.
type Selection struct {
	// Index is the row's input index; -1 for accepted custom input.
	Index int64
	// Text is the row text, or the custom input.
	Text string
}

// Action is what the surface should do after a mode handled an event.
type Action interface{ isAction() }

// ActionNothing means nothing to do (a state refresh at most).
type ActionNothing struct{}

// ActionClose is done: hide the surface.
type ActionClose struct{}

// ActionReload replaces the list, prompt, and message with new state.
type ActionReload struct{ State ModeState }

// ActionSwitchMode switches to the named mode (script switch-mode).
type ActionSwitchMode struct{ Name string }

// ActionSetInput replaces the query text.
type ActionSetInput struct{ Text string }

// ActionCopy puts Text on the clipboard and ends the session (calc,
// emoji).
type ActionCopy struct{ Text string }

// ActionExit ends the session with an exit code and the accepted rows
// (dmenu), forwarded to the waiting CLI.
type ActionExit struct {
	// Code is the process exit code: 0 accept, 10..28 kb-custom-N.
	Code int
	// Selected are the accepted rows in input order.
	Selected []Selection
}

func (ActionNothing) isAction()    {}
func (ActionClose) isAction()      {}
func (ActionReload) isAction()     {}
func (ActionSwitchMode) isAction() {}
func (ActionSetInput) isAction()   {}
func (ActionCopy) isAction()       {}
func (ActionExit) isAction()       {}

// ModeState is everything the surface needs to render a mode's list.
type ModeState struct {
	// Items are the rows; the slice index is the row identity.
	Items []Item
	// Prompt is the text left of the input.
	Prompt string
	// Message is the optional (markup) row between input and list; nil
	// for none.
	Message *string
	// MarkupRows renders every row's text as Pango markup.
	MarkupRows bool
	// MultiSelect allows toggling several rows before accepting.
	MultiSelect bool
	// NoCustom rejects custom (non-row) input.
	NoCustom bool
	// UseHotKeys routes kb-custom-N to the mode instead of closing.
	UseHotKeys bool
	// KeepSelection keeps the selected position across a reload
	// (script keep-selection).
	KeepSelection bool
	// NewSelection is an absolute selection to apply (script
	// new-selection); nil for none.
	NewSelection *uint32
	// KeepFilter keeps the typed filter across a reload (script
	// keep-filter).
	KeepFilter bool
}

// Mode is one launch mode (drun, run, window, ssh, script, dmenu, ...).
// Modes run on the session's engine goroutine; nothing calls them
// concurrently. The optional behaviors are the Querier, MultiActivator,
// Deleter, CustomPolicy, and Subsetter interfaces.
type Mode interface {
	// Name is the canonical name ("drun", or a script mode's name).
	Name() string
	// Load produces the initial list state.
	Load(ctx context.Context) ModeState
	// Activate handles an accept of a row or of custom input; input is
	// the query text at accept time (ROFI_INPUT).
	Activate(ctx context.Context, target Target, kind ActivateKind, input string) Action
}

// Querier is a mode whose rows are the query's answer rather than
// something to search through (calc). Query returns the new state, or
// false to leave the list as it is. It runs on every keystroke, so
// anything that waits for the world belongs in Load.
type Querier interface {
	Query(query string) (ModeState, bool)
}

// MultiActivator handles a multi-select accept (dmenu). Without it the
// session activates the first index.
type MultiActivator interface {
	ActivateMany(ctx context.Context, indices []uint32, input string) Action
}

// Deleter handles shift-delete on a row (history removal, window
// close). Without it the key does nothing.
type Deleter interface {
	Delete(ctx context.Context, index uint32) Action
}

// CustomPolicy lets a mode refuse typed text that matches no row.
// Without it custom input is allowed.
type CustomPolicy interface {
	AllowsCustom() bool
}

// Subsetter is combi's !bang support: the item mask for a bang prefix
// (true = shown), false when the bang names no sub-mode.
type Subsetter interface {
	Subset(bang string) ([]bool, bool)
}

// AllowsCustom reports whether m accepts typed text that matches no
// row: the CustomPolicy answer, true for modes without one.
func AllowsCustom(m Mode) bool {
	if p, ok := m.(CustomPolicy); ok {
		return p.AllowsCustom()
	}
	return true
}

// ActivateMany forwards a multi-select accept: the mode's own
// MultiActivator, else an accept of the first index (Mode's default).
func ActivateMany(ctx context.Context, m Mode, indices []uint32, input string) Action {
	if ma, ok := m.(MultiActivator); ok {
		return ma.ActivateMany(ctx, indices, input)
	}
	if len(indices) == 0 {
		return ActionNothing{}
	}
	return m.Activate(ctx, RowTarget(indices[0]), ActivateDefault{}, input)
}

// Delete forwards shift-delete: the mode's own Deleter, else nothing.
func Delete(ctx context.Context, m Mode, index uint32) Action {
	if d, ok := m.(Deleter); ok {
		return d.Delete(ctx, index)
	}
	return ActionNothing{}
}

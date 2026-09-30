package launcher

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrNoModes is a session asked to run with no modes.
var ErrNoModes = errors.New("launcher: a session needs at least one mode")

// Session is one open launcher invocation (session.rs): the loaded
// modes, the active one, its match state, and the multi-select set. The
// surface drives it from one goroutine: query edits, accepts, deletes,
// and mode switches.
type Session struct {
	modes  []Mode
	active int
	state  ModeState
	// query is ROFI_INPUT, a combi !bang prefix already stripped.
	query  string
	subset []bool
	// Engine is the matcher the surface reads matches from.
	Engine *MatchEngine
	// completer is -completer-mode: the mode kb-mode-complete opens to
	// complete the query rather than to launch something.
	completer     string
	completingNow bool
	completeFrom  int
}

// NewSession builds a session over modes, the first active. An empty
// mode list is ErrNoModes.
func NewSession(modes []Mode, opts MatcherOptions) (*Session, error) {
	if len(modes) == 0 {
		return nil, ErrNoModes
	}
	return &Session{modes: modes, Engine: NewMatchEngine(opts)}, nil
}

// SetCompleter names the mode kb-mode-complete opens; empty is none.
func (s *Session) SetCompleter(mode string) { s.completer = mode }

// Completing reports whether the completer is open.
func (s *Session) Completing() bool { return s.completingNow }

// OpenCompleter opens the completer, remembering where to come back
// to. False when none is configured, it is not a loaded mode, it is the
// mode already showing, or it is already open - the key then does
// nothing rather than switching somewhere arbitrary.
func (s *Session) OpenCompleter(ctx context.Context) bool {
	if s.completingNow || s.completer == "" {
		return false
	}
	index := s.indexOf(s.completer)
	if index < 0 || index == s.active {
		return false
	}
	from := s.active
	s.SwitchTo(ctx, index)
	s.completingNow, s.completeFrom = true, from
	return true
}

// CancelCompleter closes the completer without taking anything from it.
func (s *Session) CancelCompleter(ctx context.Context) {
	if !s.completingNow {
		return
	}
	s.completingNow = false
	s.SwitchTo(ctx, s.completeFrom)
}

// completeWith takes the completer's row as the query and returns to
// the mode that asked: accepting fills the box instead of launching.
func (s *Session) completeWith(ctx context.Context, target Target) Action {
	if !s.completingNow {
		return ActionNothing{}
	}
	s.completingNow = false
	text, found := "", false
	if i, ok := target.Row(); ok && int(i) < len(s.Engine.Items()) {
		text, found = s.Engine.Items()[i].Display, true
	}
	s.SwitchTo(ctx, s.completeFrom)
	if !found {
		return ActionNothing{}
	}
	return ActionSetInput{Text: text}
}

// SetQuery updates the query. A leading `!bang ` restricts a combi mode
// to the matching sub-mode; the remainder is the real query. A mode
// that answers the query (Querier) rebuilds its list first, so the
// engine matches against the answer.
func (s *Session) SetQuery(query string) {
	bang, rest, hasBang := splitBang(query)
	s.subset = nil
	if hasBang {
		if sub, ok := s.modes[s.active].(Subsetter); ok {
			if mask, ok := sub.Subset(bang); ok {
				s.subset = mask
			}
		}
	}
	if s.subset != nil {
		s.query = rest
	} else {
		s.query = query
	}
	if q, ok := s.modes[s.active].(Querier); ok {
		if state, changed := q.Query(s.query); changed {
			s.applyState(state)
		}
	}
	s.Engine.SetQuery(s.query)
}

// Query is the current query, bang stripped.
func (s *Session) Query() string { return s.query }

// Matched returns the ranked matched indices with a combi !bang mask
// applied.
func (s *Session) Matched() []uint32 {
	matched := s.Engine.Matched()
	if s.subset == nil {
		return matched
	}
	return slices.DeleteFunc(matched, func(i uint32) bool {
		return int(i) < len(s.subset) && !s.subset[i]
	})
}

// ModeNames lists the loaded modes (tabs, kb-mode-next order).
func (s *Session) ModeNames() []string {
	names := make([]string, len(s.modes))
	for i, m := range s.modes {
		names[i] = m.Name()
	}
	return names
}

// ActiveIndex is the active mode's index.
func (s *Session) ActiveIndex() int { return s.active }

// State is the active mode's state (prompt, message, flags).
func (s *Session) State() ModeState { return s.state }

// Load loads (or reloads) the active mode into the engine.
func (s *Session) Load(ctx context.Context) {
	s.applyState(s.modes[s.active].Load(ctx))
}

// SwitchTo switches to the mode at index (wrapping) and loads it.
func (s *Session) SwitchTo(ctx context.Context, index int) {
	n := len(s.modes)
	s.active = ((index % n) + n) % n
	s.subset = nil
	s.Load(ctx)
}

// SwitchNext is kb-mode-next.
func (s *Session) SwitchNext(ctx context.Context) { s.SwitchTo(ctx, s.active+1) }

// SwitchPrevious is kb-mode-previous.
func (s *Session) SwitchPrevious(ctx context.Context) { s.SwitchTo(ctx, s.active-1) }

// SwitchToNamed switches to a mode by name; false when unknown.
func (s *Session) SwitchToNamed(ctx context.Context, name string) bool {
	index := s.indexOf(name)
	if index < 0 {
		return false
	}
	s.SwitchTo(ctx, index)
	return true
}

func (s *Session) indexOf(name string) int {
	for i, m := range s.modes {
		if m.Name() == name {
			return i
		}
	}
	return -1
}

// Activate forwards an accept to the active mode and resolves the
// session-internal actions (reload, switch-mode); the rest is the
// surface's to interpret. While the completer is open an accept fills
// the query instead of launching. Custom input a mode refuses (or the
// state marks no-custom) does nothing.
func (s *Session) Activate(ctx context.Context, target Target, kind ActivateKind) Action {
	if s.completingNow {
		return s.completeWith(ctx, target)
	}
	if _, custom := kind.(ActivateCustom); custom && (!AllowsCustom(s.modes[s.active]) || s.state.NoCustom) {
		return ActionNothing{}
	}
	return s.apply(ctx, s.modes[s.active].Activate(ctx, target, kind, s.query))
}

// ActivateMany forwards a multi-select accept (dmenu).
func (s *Session) ActivateMany(ctx context.Context, indices []uint32) Action {
	return s.apply(ctx, ActivateMany(ctx, s.modes[s.active], indices, s.query))
}

// Delete forwards shift-delete to the active mode.
func (s *Session) Delete(ctx context.Context, index uint32) Action {
	return s.apply(ctx, Delete(ctx, s.modes[s.active], index))
}

// apply resolves Reload and SwitchMode inside the session; a switch to
// an unknown mode closes.
func (s *Session) apply(ctx context.Context, action Action) Action {
	switch a := action.(type) {
	case ActionReload:
		s.applyState(a.State)
		return ActionNothing{}
	case ActionSwitchMode:
		if s.SwitchToNamed(ctx, a.Name) {
			return ActionNothing{}
		}
		return ActionClose{}
	case nil:
		return ActionNothing{}
	}
	return action
}

// applyState installs a state; mode-level markup becomes a per-item
// flag so the row renderer has one source of truth.
func (s *Session) applyState(state ModeState) {
	items := state.Items
	if state.MarkupRows {
		for i := range items {
			items[i].Flags |= FlagMarkup
		}
	}
	state.Items = nil
	s.state = state
	s.Engine.SetItems(items)
}

// splitBang splits a leading `!bang ` off a query: "!win term" is
// ("win", "term"); a lone "!win" (no space yet) counts too.
func splitBang(query string) (bang, rest string, ok bool) {
	after, found := strings.CutPrefix(query, "!")
	if !found {
		return "", query, false
	}
	if i := strings.IndexFunc(after, unicode.IsSpace); i >= 0 {
		_, size := utf8.DecodeRuneInString(after[i:])
		return after[:i], after[i+size:], true
	}
	return after, "", true
}

package launcher

import (
	"context"
	"log"
	"math"
	"slices"
	"time"

	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/launcheripc"
	"github.com/stubbedev/wayle/internal/pango"
	engine "github.com/stubbedev/wayle/service/launcher"
	"github.com/stubbedev/wayle/service/launcher/modes"
)

// dumpDebounce is how long -dump waits for the matches to settle.
const dumpDebounce = 150 * time.Millisecond

// Window is the launcher's layer surface (*app.LayerWindow).
type Window interface {
	Close()
	SetSize(width, height uint32) error
	SetFocus(w widget.Widget)
}

// Deps are what the shell hands the surface.
type Deps struct {
	// Invoke runs fn on the UI loop (app.Invoke).
	Invoke func(fn func())
	// After runs fn on the UI loop after d; the returned stop cancels.
	After func(d time.Duration, fn func()) (stop func())
	// Config is the live config.
	Config func() *config.Config
	// Open maps the layer surface for one session.
	Open func(cfg app.LayerConfig) (Window, error)
	// Font paints every label.
	Font render.Font
	// Ink is the default label color before the stylesheet applies.
	Ink render.Color
	// Sheet is the shell's stylesheet (the .launcher-* rules), and
	// SheetPriority its priority, which a session's -font/-style CSS
	// sits above.
	Sheet         *widget.Stylesheet
	SheetPriority int
	// MonitorWidth is the width a percent -width resolves against (the
	// compositor picks the output, so the first one stands in, as the
	// Rust surface's fallback does before it is realized).
	MonitorWidth func() int
	// Clipboard is the history the clipboard mode reads (nil: none).
	Clipboard modes.ClipboardHistory
	// Copy puts calc's result on the clipboard.
	Copy func(text string)
	// HeldMods are the modifiers held now, for pointer bindings.
	HeldMods func() engine.MouseModifiers
	// OpenHistory opens the launch history (a test seam).
	OpenHistory func() (*engine.History, error)
	// FireHook runs a -on-* hook (engine.FireHook; a test seam).
	FireHook func(command string, ctx engine.HookContext)
}

// pendingSelection is where the next match list puts the selection.
type pendingSelection struct {
	keep bool
	at   *int
}

// active is one live session (ActiveSession).
type active struct {
	sess       *launcheripc.Session
	act        *actor
	ui         uiSettings
	history    *engine.History
	modePrompt string
	modeName   string
	// decided: the user accepted or cancelled, so tearing the list down
	// fires no -on-selection-changed.
	decided bool
	// dialog is -e: message only, any bound key closes with code 0.
	dialog bool
	dump   bool
	// dumpStop cancels the pending -dump reply.
	dumpStop func()
	pending  pendingSelection
	win      Window
	views    *views
	thumbs   engine.Thumbnailer
	ctx      context.Context
	cancel   context.CancelFunc
	keys     []keyBinding
	mouse    []mouseEntry
}

// Surface is the launcher (shell/launcher/mod.rs): it serves the
// launcher socket's sessions on the UI loop.
type Surface struct {
	d     Deps
	cur   *active
	model matchModel
	multi multiSelect
	sel   int
}

// New builds the surface over its collaborators.
func New(d Deps) *Surface {
	if d.FireHook == nil {
		d.FireHook = engine.FireHook
	}
	return &Surface{d: d, sel: -1}
}

// Open implements launcheripc.Handler: the session moves onto the loop.
func (s *Surface) Open(sess *launcheripc.Session) {
	s.d.Invoke(func() { s.openSession(sess) })
}

// ClientGone implements launcheripc.Handler: a CLI that went away ends
// its session if it is still the live one, and its connection is
// released either way.
func (s *Surface) ClientGone(id uint64) {
	s.d.Invoke(func() {
		if s.cur != nil && s.cur.sess.ID == id {
			s.end(nil)
		}
	})
}

// openSession is open_session.
func (s *Surface) openSession(sess *launcheripc.Session) {
	if s.cur != nil {
		if !sess.Replace {
			sess.Reply(launcheripc.ServerFrame{Type: launcheripc.FrameBusy})
			return
		}
		s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameCancelled, Code: 1})
	}
	cfg := s.d.Config()
	setup := buildSession(sess.Options, cfg, sess.Rows, setupDeps{clipboard: s.d.Clipboard, openHistory: s.d.OpenHistory})
	dialog := setup.ui.errorMessage != nil
	if len(setup.modes) == 0 && !dialog {
		log.Printf("launcher: no usable modes in session request")
		// The one thing wayle's launcher calls a menu error: it was
		// asked for a menu it cannot build.
		s.d.FireHook(setup.ui.hooks.MenuError, engine.HookContext{Error: "no usable modes"})
		sess.Reply(launcheripc.ServerFrame{Type: launcheripc.FrameCancelled, Code: 1})
		if setup.history != nil {
			_ = setup.history.Close()
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &active{
		sess: sess, ui: setup.ui, history: setup.history, dialog: dialog, dump: sess.Options.Dump,
		thumbs: engine.NewThumbnailer(setup.ui.previewCmd), ctx: ctx, cancel: cancel,
		keys: compileKeys(setup.ui.keybindings), mouse: compileMouse(setup.ui.mouseBindings),
	}
	s.multi = multiSelect{picked: map[uint32]bool{}, ballotSelected: setup.ui.ballotSelected, ballotUnselected: setup.ui.ballotUnselected}
	s.model.update(nil, nil)
	s.sel = -1
	s.cur = a
	a.views = s.buildViews(a)
	if !dialog {
		esess, err := engine.NewSession(setup.modes, setup.matcher)
		if err != nil {
			log.Printf("launcher: %v", err)
			s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameCancelled, Code: 1})
			return
		}
		esess.SetCompleter(setup.ui.completer)
		a.act = startActor(esess, setup.initialMode, &sessionSink{s: s, a: a}, s.d.Invoke)
	}
	if a.dump {
		if a.ui.filter != nil && a.act != nil {
			a.act.send(cmdSetQuery{query: *a.ui.filter})
		}
		return
	}
	s.reveal(a)
}

// sessionSink delivers one session's engine events, dropping any that
// arrive after the session ended.
type sessionSink struct {
	s *Surface
	a *active
}

func (k *sessionSink) live() bool { return k.s.cur == k.a }

func (k *sessionSink) onState(st engineState) {
	if k.live() {
		k.s.onState(st)
	}
}

func (k *sessionSink) onMatches(m engineMatches) {
	if k.live() {
		k.s.onMatches(m)
	}
}

func (k *sessionSink) onAction(act engine.Action) {
	if k.live() {
		k.s.onAction(act)
	}
}

// onMatches is EngineEvent::Matches: the list swaps, the selection
// lands where the session asked, -select/-selected-row apply once, and
// -auto-select accepts a lone result.
func (s *Surface) onMatches(m engineMatches) {
	a := s.cur
	previous := s.sel
	s.model.update(m.items, m.matched)
	a.views.list.reset(s.model.len())
	if n := s.model.len(); n > 0 {
		target := 0
		switch {
		case a.pending.at != nil:
			target = min(*a.pending.at, n-1)
		case a.pending.keep:
			target = min(max(previous, 0), n-1)
		}
		s.selectRow(target)
		s.applyOneShotSelection()
		if a.ui.autoSelect && n == 1 && a.views.entry.Text() != "" {
			s.activatePosition(0)
		}
	} else {
		s.selectRow(-1)
	}
	s.resize()
	s.armDump()
}

// onState is on_state: prompt, message, tabs, the selection intent, the
// multi-select flag, and the filter a script reload clears.
func (s *Surface) onState(st engineState) {
	a := s.cur
	s.multi.enabled = st.multi
	if !st.multi {
		clear(s.multi.picked)
	}
	if st.afterActivate && !st.keepFilter && a.views.entry.Text() != "" {
		a.views.entry.SetText("")
	}
	switch {
	case st.newSelection != nil:
		at := int(*st.newSelection)
		a.pending = pendingSelection{at: &at}
	case st.keepSelection:
		a.pending = pendingSelection{keep: true}
	default:
		a.pending = pendingSelection{}
	}
	a.modePrompt = st.prompt
	// A state arrives on every reload, so the hook fires on a change of
	// mode, not on being told the mode again.
	name := ""
	if st.activeMode >= 0 && st.activeMode < len(st.modeNames) {
		name = st.modeNames[st.activeMode]
	}
	if name != "" && name != a.modeName {
		s.d.FireHook(a.ui.hooks.ModeChanged, engine.HookContext{Input: a.views.entry.Text(), Mode: name})
	}
	a.modeName = name
	shown := st.prompt
	if a.ui.prompt != nil {
		shown = *a.ui.prompt
	} else if named, ok := a.ui.displayNames[st.prompt]; ok {
		shown = named
	}
	a.views.setPrompt(shown)
	message := st.message
	if a.ui.errorMessage != nil {
		message = a.ui.errorMessage
	} else if a.ui.mesg != nil {
		message = a.ui.mesg
	}
	a.views.setMessage(message)
	if a.ui.sidebar {
		s.rebuildTabs(st.modeNames, st.activeMode)
	}
	s.resize()
}

// onAction is EngineEvent::Action: the terminal and input actions.
func (s *Surface) onAction(act engine.Action) {
	filter := s.cur.views.entry.Text()
	switch v := act.(type) {
	case engine.ActionClose:
		s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameResult, Filter: filter})
	case engine.ActionExit:
		sel := make([]launcheripc.Selected, len(v.Selected))
		for i, x := range v.Selected {
			sel[i] = launcheripc.Selected{Index: x.Index, Text: x.Text}
		}
		s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameResult, Code: v.Code, Selected: sel, Filter: filter})
	case engine.ActionSetInput:
		s.cur.views.entry.SetText(v.Text)
	case engine.ActionCopy:
		if s.d.Copy != nil {
			s.d.Copy(v.Text)
		}
		s.end(&launcheripc.ServerFrame{
			Type:     launcheripc.FrameResult,
			Selected: []launcheripc.Selected{{Index: 0, Text: v.Text}}, Filter: filter,
		})
	}
}

// applyOneShotSelection is apply_one_shot_selection: -selected-row,
// else -select, on the session's first populated list.
func (s *Surface) applyOneShotSelection() {
	a := s.cur
	switch {
	case a.ui.selectedRow != nil:
		s.selectRow(min(int(*a.ui.selectedRow), s.model.len()-1))
		a.ui.selectedRow = nil
	case a.ui.selectRow != nil:
		if pos, ok := s.model.findPosition(*a.ui.selectRow); ok {
			s.selectRow(pos)
		}
		a.ui.selectRow = nil
	}
}

// selectRow moves the selection, firing -on-selection-changed when it
// changed.
func (s *Surface) selectRow(pos int) {
	if pos == s.sel {
		return
	}
	s.sel = pos
	if s.cur != nil {
		s.cur.views.list.setSelected(pos)
		s.fireSelectionHook()
	}
}

// fireSelectionHook is fire_selection_hook.
func (s *Surface) fireSelectionHook() {
	a := s.cur
	if a.decided || a.ui.hooks.SelectionChanged == "" {
		return
	}
	text := s.model.textAt(s.sel)
	s.d.FireHook(a.ui.hooks.SelectionChanged, engine.HookContext{Input: a.views.entry.Text(), Entry: text, Mode: a.modeName})
}

// fireDecisionHook is fire_decision_hook: -on-entry-accepted and
// -on-menu-canceled, fired before the decision tears the session down.
func (s *Surface) fireDecisionHook(action keyAction) {
	a := s.cur
	if !a.ui.hooks.Any() {
		return
	}
	selected := s.model.textAt(s.sel)
	typed := a.views.entry.Text()
	var command, entry string
	decided := true
	switch {
	case action == keyCancel:
		command, entry = a.ui.hooks.MenuCanceled, selected
	case action == keyAccept && s.model.len() == 0:
		// With no rows there is nothing to accept but the typed text.
		command, entry = a.ui.hooks.EntryAccepted, typed
	case action == keyAccept:
		command, entry = a.ui.hooks.EntryAccepted, selected
	case action == keyAcceptAlt && !s.multi.enabled:
		// In multi-select the alternate accept toggles a row instead.
		command, entry = a.ui.hooks.EntryAccepted, selected
	case action == keyAcceptCustom:
		command, entry = a.ui.hooks.EntryAccepted, typed
	default:
		decided = false
	}
	a.decided = a.decided || decided
	s.d.FireHook(command, engine.HookContext{Input: typed, Entry: entry, Mode: a.modeName})
}

// onKey is on_key.
func (s *Surface) onKey(b keyBinding) {
	a := s.cur
	if a == nil {
		return
	}
	if a.dialog {
		// -e: any bound action dismisses with success.
		s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameResult})
		return
	}
	s.fireDecisionHook(b.action)
	switch b.action {
	case keyCancel:
		s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameCancelled, Code: 1})
	case keyAccept:
		picked := make([]uint32, 0, len(s.multi.picked))
		for i := range s.multi.picked {
			picked = append(picked, i)
		}
		slices.Sort(picked)
		switch {
		case len(picked) > 0:
			s.send(cmdActivateMulti{indices: picked})
		case s.model.len() == 0:
			s.activateCustom()
		default:
			s.activatePosition(s.sel)
		}
	case keyAcceptAlt:
		if s.multi.enabled {
			// rofi multi-select: Shift+Enter toggles and advances.
			s.togglePicked()
			s.moveSelection(1)
		} else if i, ok := s.model.itemIndex(s.sel); ok {
			s.send(cmdActivate{target: engine.RowTarget(i), kind: engine.ActivateAlt{}})
		}
	case keyAcceptCustom:
		s.activateCustom()
	case keyCustom:
		target := engine.Target{}
		if i, ok := s.model.itemIndex(s.sel); ok {
			target = engine.RowTarget(i)
		}
		s.send(cmdActivate{target: target, kind: engine.ActivateKbCustom{N: b.custom}})
	case keyDeleteEntry:
		if i, ok := s.model.itemIndex(s.sel); ok {
			s.send(cmdDelete{index: i})
		}
	case keyModeNext:
		s.send(cmdModeNext{})
	case keyModePrevious:
		s.send(cmdModePrevious{})
	case keyModeComplete:
		s.send(cmdModeComplete{})
	case keyRowUp:
		s.moveSelection(-1)
	case keyRowDown:
		s.moveSelection(1)
	case keyRowFirst:
		if s.model.len() > 0 {
			s.selectRow(0)
		}
	case keyRowLast:
		if n := s.model.len(); n > 0 {
			s.selectRow(n - 1)
		}
	case keyPagePrev:
		s.moveSelection(-a.ui.lines)
	case keyPageNext:
		s.moveSelection(a.ui.lines)
	}
}

// onMouse is on_mouse: a click accepts the row it landed on, not
// whatever was selected when the press came.
func (s *Surface) onMouse(action mouseAction, row int) {
	switch action {
	case mouseSelect:
		if row >= 0 {
			s.selectRow(row)
		}
	case mouseAccept:
		if row >= 0 {
			s.selectRow(row)
		}
		s.onKey(keyBinding{action: keyAccept})
	case mouseAcceptCustom:
		s.onKey(keyBinding{action: keyAcceptCustom})
	case mouseRowUp:
		s.moveSelection(-1)
	case mouseRowDown:
		s.moveSelection(1)
	}
}

// rowPressed runs the button bindings for a press on row pos.
func (s *Surface) rowPressed(pos int, button engine.MouseButton, presses int) {
	if s.cur == nil {
		return
	}
	for _, act := range lookupButton(s.cur.mouse, button, presses, s.heldMods()) {
		s.onMouse(act, pos)
	}
}

// listScrolled runs the wheel bindings; false lets the list scroll.
func (s *Surface) listScrolled(dy int) bool {
	if s.cur == nil || dy == 0 {
		return false
	}
	dir := engine.ScrollDown
	if dy < 0 {
		dir = engine.ScrollUp
	}
	actions := lookupScroll(s.cur.mouse, dir, s.heldMods())
	for _, act := range actions {
		s.onMouse(act, -1)
	}
	return len(actions) > 0
}

func (s *Surface) heldMods() engine.MouseModifiers {
	if s.d.HeldMods == nil {
		return 0
	}
	return s.d.HeldMods()
}

// keyCaptured is the capture-phase key path: a bound key acts, every
// other key reaches the entry.
func (s *Surface) keyCaptured(press app.Accel) bool {
	if s.cur == nil {
		return false
	}
	b, ok := lookupKey(s.cur.keys, press)
	if !ok {
		return false
	}
	s.onKey(b)
	return true
}

// togglePicked flips the selected row in the multi-select set.
func (s *Surface) togglePicked() {
	i, ok := s.model.itemIndex(s.sel)
	if !ok {
		return
	}
	if s.multi.picked[i] {
		delete(s.multi.picked, i)
	} else {
		s.multi.picked[i] = true
	}
	s.cur.views.list.refresh(s.sel)
}

// moveSelection is move_selection: single steps wrap under -cycle, the
// rest clamp.
func (s *Surface) moveSelection(delta int) {
	n := s.model.len()
	if n == 0 {
		return
	}
	cur := max(s.sel, 0)
	var target int
	if s.cur.ui.cycle && (delta == 1 || delta == -1) {
		target = ((cur+delta)%n + n) % n
	} else {
		target = min(max(cur+delta, 0), n-1)
	}
	s.selectRow(target)
}

// activatePosition activates the row at pos.
func (s *Surface) activatePosition(pos int) {
	if i, ok := s.model.itemIndex(pos); ok {
		s.send(cmdActivate{target: engine.RowTarget(i), kind: engine.ActivateDefault{}})
	}
}

// activateCustom accepts the typed text.
func (s *Surface) activateCustom() {
	s.send(cmdActivate{target: engine.Target{}, kind: engine.ActivateCustom{Text: s.cur.views.entry.Text()}})
}

func (s *Surface) send(c engineCmd) {
	if s.cur != nil && s.cur.act != nil {
		s.cur.act.send(c)
	}
}

// armDump schedules the -dump reply once the matches settle.
func (s *Surface) armDump() {
	a := s.cur
	if !a.dump {
		return
	}
	if a.dumpStop != nil {
		a.dumpStop()
	}
	a.dumpStop = s.d.After(dumpDebounce, func() {
		if s.cur == a {
			s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameDump, Items: s.model.texts()})
		}
	})
}

// end is end_session: stop the engine, answer the CLI (or release it),
// close the surface.
func (s *Surface) end(frame *launcheripc.ServerFrame) {
	a := s.cur
	if a == nil {
		return
	}
	s.cur = nil
	if a.act != nil {
		a.act.stop()
	}
	if a.dumpStop != nil {
		a.dumpStop()
	}
	a.cancel()
	if frame != nil {
		a.sess.Reply(*frame)
	} else {
		a.sess.Release()
	}
	if a.history != nil {
		_ = a.history.Close()
	}
	if a.win != nil {
		a.win.Close()
	}
	clear(s.multi.picked)
	s.model.update(nil, nil)
	s.sel = -1
}

// reveal maps the surface over the configured location and focuses the
// entry.
func (s *Surface) reveal(a *active) {
	width := s.resolveWidth(a.ui)
	anchor, margin := locationAnchors(a.ui.location, a.ui.offsetX, a.ui.offsetY)
	h := a.views.height(width)
	win, err := s.d.Open(app.LayerConfig{
		Layer:         app.LayerOverlay,
		Anchor:        anchor,
		Margin:        margin,
		Width:         uint32(width),
		Height:        uint32(max(h, 1)),
		ExclusiveZone: -1,
		Keyboard:      app.KeyboardOnDemand,
		Namespace:     "wayle-launcher",
		Root:          a.views.root,
		KeyCapture:    s.keyCaptured,
		OnClosed: func() {
			// The compositor took the surface away (output gone).
			if s.cur == a {
				a.win = nil
				s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameCancelled, Code: 1})
			}
		},
	})
	if err != nil {
		log.Printf("launcher: cannot map the surface: %v", err)
		s.end(&launcheripc.ServerFrame{Type: launcheripc.FrameCancelled, Code: 1})
		return
	}
	a.win = win
	a.views.width = width
	win.SetFocus(a.views.entry)
}

// resize keeps the surface as tall as its content: the list grows to
// -lines rows unless -fixed-num-lines holds it there.
func (s *Surface) resize() {
	a := s.cur
	if a == nil || a.win == nil {
		return
	}
	h := a.views.height(a.views.width)
	if h != a.views.lastH {
		a.views.lastH = h
		if err := a.win.SetSize(uint32(a.views.width), uint32(max(h, 1))); err != nil {
			log.Printf("launcher: resize: %v", err)
		}
	}
}

// resolveWidth is resolve_width: [launcher] width unless -width
// overrode it, a percent against the monitor and a character count
// against the font's advance of "0".
func (s *Surface) resolveWidth(ui uiSettings) int {
	w := ui.widthOverride
	if w == nil {
		return max(ui.width, 1)
	}
	switch w.Unit {
	case launcheripc.WidthPixels:
		return max(int(w.Value), 1)
	case launcheripc.WidthPercent:
		monitor := ui.width
		if s.d.MonitorWidth != nil {
			if m := s.d.MonitorWidth(); m > 0 {
				monitor = m
			}
		}
		return max(int(float64(monitor)*w.Value/100), 1)
	case launcheripc.WidthChars:
		advance := max(int(math.Ceil(s.d.Font.Shape("0", fontPx).Advance())), 1)
		return max(advance*int(w.Value), 1)
	}
	return max(ui.width, 1)
}

// locationAnchors is apply_location: the edges a location anchors and
// the -xoffset/-yoffset margins, signed from the anchor (a positive x
// moves right: away from Left, into Right).
func locationAnchors(loc config.LauncherLocation, x, y int) (app.Anchor, app.Margins) {
	var anchor app.Anchor
	switch loc {
	case config.LauncherNorth:
		anchor = app.AnchorTop
	case config.LauncherNorthEast:
		anchor = app.AnchorTop | app.AnchorRight
	case config.LauncherEast:
		anchor = app.AnchorRight
	case config.LauncherSouthEast:
		anchor = app.AnchorBottom | app.AnchorRight
	case config.LauncherSouth:
		anchor = app.AnchorBottom
	case config.LauncherSouthWest:
		anchor = app.AnchorBottom | app.AnchorLeft
	case config.LauncherWest:
		anchor = app.AnchorLeft
	case config.LauncherNorthWest:
		anchor = app.AnchorTop | app.AnchorLeft
	}
	var m app.Margins
	if anchor&app.AnchorLeft != 0 {
		m.Left = int32(x)
	}
	if anchor&app.AnchorRight != 0 {
		m.Right = int32(-x)
	}
	if anchor&app.AnchorTop != 0 {
		m.Top = int32(y)
	}
	if anchor&app.AnchorBottom != 0 {
		m.Bottom = int32(-y)
	}
	return anchor, m
}

// rebuildTabs is rebuild_tabs: one button per mode, the active one
// marked.
func (s *Surface) rebuildTabs(names []string, activeMode int) {
	v := s.cur.views
	v.tabs.Clear()
	for i, name := range names {
		label := name
		if shown, ok := s.cur.ui.displayNames[name]; ok {
			label = shown
		}
		b := widget.NewButton(widget.NewLabel(s.d.Font, fontPx, label, s.d.Ink), 4, 6)
		b.AddClass("launcher-tab")
		if i == activeMode {
			b.AddClass("active")
		}
		index := i
		b.OnClick = func() { s.send(cmdModeTo{index: index}) }
		v.tabs.Append(b, true)
	}
}

// messageText is the message line's text: the markup's text, since the
// line wraps and gelm's wrapping label is plain (a known deviation: the
// Rust line renders the markup's weights and colors).
func messageText(markup string) string {
	if text, ok := pango.PlainText(markup); ok {
		return text
	}
	return markup
}

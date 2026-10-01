package launcher

import (
	"context"
	"sync"

	engine "github.com/stubbedev/wayle/service/launcher"
)

// The engine actor (engine_actor.rs): one goroutine per session owns
// the engine.Session - mode loads run scripts and read the disk, so
// they stay off the UI loop - and talks to the surface over a command
// channel in and delivered closures out.

// engineCmd is a surface -> engine command.
type engineCmd interface{ isEngineCmd() }

type (
	cmdSetQuery struct{ query string }
	cmdActivate struct {
		target engine.Target
		kind   engine.ActivateKind
	}
	cmdActivateMulti struct{ indices []uint32 }
	cmdDelete        struct{ index uint32 }
	cmdModeNext      struct{}
	cmdModePrevious  struct{}
	cmdModeTo        struct{ index int }
	// cmdModeComplete opens the completer, or closes it when open: the
	// same key does both, so one opened by accident is no trap.
	cmdModeComplete struct{}
)

func (cmdSetQuery) isEngineCmd()      {}
func (cmdActivate) isEngineCmd()      {}
func (cmdActivateMulti) isEngineCmd() {}
func (cmdDelete) isEngineCmd()        {}
func (cmdModeNext) isEngineCmd()      {}
func (cmdModePrevious) isEngineCmd()  {}
func (cmdModeTo) isEngineCmd()        {}
func (cmdModeComplete) isEngineCmd()  {}

// engineState is a (re)loaded mode's state for the surface.
type engineState struct {
	prompt     string
	message    *string
	activeMode int
	modeNames  []string
	multi      bool
	// afterActivate marks a reload from an activation (script, dmenu)
	// rather than a mode switch; it governs filter clearing.
	afterActivate bool
	keepFilter    bool
	keepSelection bool
	newSelection  *uint32
}

// engineMatches is a fresh match result.
type engineMatches struct {
	items   []engine.Item
	matched []uint32
}

// engineSink receives the actor's events; the surface implements it
// and every call arrives through deliver, on the UI loop.
type engineSink interface {
	onState(engineState)
	onMatches(engineMatches)
	onAction(engine.Action)
}

// actor is one session's engine goroutine. Its queue is unbounded and
// ordered, as the Rust actor's channel is: a query typed while a mode
// loads must still arrive, after the keys before it.
type actor struct {
	mu     sync.Mutex
	queue  []engineCmd
	wake   chan struct{}
	cancel context.CancelFunc
}

// startActor runs the session's engine; deliver hops onto the UI loop.
func startActor(sess *engine.Session, initial int, sink engineSink, deliver func(func())) *actor {
	ctx, cancel := context.WithCancel(context.Background())
	a := &actor{wake: make(chan struct{}, 1), cancel: cancel}
	go a.run(ctx, sess, initial, sink, deliver)
	return a
}

// send queues a command; after stop nothing reads it.
func (a *actor) send(c engineCmd) {
	a.mu.Lock()
	a.queue = append(a.queue, c)
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// next takes the oldest queued command, waiting for one; false once
// the actor stopped.
func (a *actor) next(ctx context.Context) (engineCmd, bool) {
	for {
		a.mu.Lock()
		if len(a.queue) > 0 {
			c := a.queue[0]
			a.queue = a.queue[1:]
			a.mu.Unlock()
			return c, true
		}
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false
		case <-a.wake:
		}
	}
}

// stop ends the actor: modes in flight see their context cancelled.
func (a *actor) stop() {
	a.cancel()
}

func (a *actor) run(ctx context.Context, sess *engine.Session, initial int, sink engineSink, deliver func(func())) {
	sendState := func(afterActivate bool) {
		st := sess.State()
		ev := engineState{
			prompt: st.Prompt, message: st.Message, activeMode: sess.ActiveIndex(), modeNames: sess.ModeNames(),
			multi: st.MultiSelect, afterActivate: afterActivate, keepFilter: st.KeepFilter,
			keepSelection: st.KeepSelection, newSelection: st.NewSelection,
		}
		deliver(func() { sink.onState(ev) })
	}
	pushMatches := func() {
		ev := engineMatches{items: sess.Engine.Items(), matched: sess.Matched()}
		deliver(func() { sink.onMatches(ev) })
	}
	dispatch := func(action engine.Action) {
		if _, nothing := action.(engine.ActionNothing); nothing {
			// The session already resolved reloads and switches; refresh
			// the surface on anything non-terminal.
			sendState(true)
			pushMatches()
			return
		}
		deliver(func() { sink.onAction(action) })
	}
	sess.SwitchTo(ctx, initial)
	sendState(false)
	pushMatches()
	for {
		cmd, ok := a.next(ctx)
		if !ok {
			return
		}
		switch c := cmd.(type) {
		case cmdSetQuery:
			sess.SetQuery(c.query)
			pushMatches()
		case cmdActivate:
			dispatch(sess.Activate(ctx, c.target, c.kind))
		case cmdActivateMulti:
			dispatch(sess.ActivateMany(ctx, c.indices))
		case cmdDelete:
			dispatch(sess.Delete(ctx, c.index))
		case cmdModeNext:
			sess.SwitchNext(ctx)
			sendState(false)
			pushMatches()
		case cmdModePrevious:
			sess.SwitchPrevious(ctx)
			sendState(false)
			pushMatches()
		case cmdModeTo:
			sess.SwitchTo(ctx, c.index)
			sendState(false)
			pushMatches()
		case cmdModeComplete:
			if sess.Completing() {
				sess.CancelCompleter(ctx)
			} else if !sess.OpenCompleter(ctx) {
				continue
			}
			sendState(false)
			pushMatches()
		}
	}
}

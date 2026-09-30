// Package auth is the backend-agnostic authentication conversation
// (crates/wayle-auth): a backend — PAM for the lock screen, greetd for
// the greeter — asks one or more questions (password, OTP, "your
// password expired, enter a new one") and the UI answers each in turn.
//
// A backend implements Conversation and drives the whole exchange on a
// worker goroutine, calling ask whenever it needs the user. Spawn wires
// ask to the UI: every prompt goes out through onEvent, and the UI
// replies through the returned Handle. The package has no GUI
// dependency, so the lock screen and the greeter drive it identically.
package auth

import (
	"errors"
	"log"
	"os"
	"sync"
)

// PromptKind is what a prompt wants from the user.
type PromptKind uint8

// Prompt kinds.
const (
	// PromptSecret is echo-off input (password, PIN).
	PromptSecret PromptKind = iota
	// PromptVisible is echo-on input (username, OTP).
	PromptVisible
	// PromptInfo is informational text; no answer.
	PromptInfo
	// PromptError is error text; no answer.
	PromptError
)

// String names the kind for logs.
func (k PromptKind) String() string {
	switch k {
	case PromptSecret:
		return "secret"
	case PromptVisible:
		return "visible"
	case PromptInfo:
		return "info"
	case PromptError:
		return "error"
	}
	return "unknown"
}

// Prompt is one thing a backend wants from the user.
type Prompt struct {
	Kind PromptKind
	Text string
}

// WantsInput reports whether the prompt expects a typed answer.
func (p Prompt) WantsInput() bool { return p.Kind == PromptSecret || p.Kind == PromptVisible }

// EventKind is what an Event reports.
type EventKind uint8

// Event kinds.
const (
	// EventPrompt: the backend asks something (see Prompt.WantsInput).
	EventPrompt EventKind = iota
	// EventSuccess: authentication succeeded; the conversation is over.
	EventSuccess
	// EventFailure: authentication failed or was aborted; the
	// conversation is over, a retry starts a fresh one.
	EventFailure
)

// Event is surfaced to the UI as a conversation progresses.
type Event struct {
	Kind EventKind
	// Prompt is set for EventPrompt.
	Prompt Prompt
	// Reason is set for EventFailure.
	Reason string
}

// Ask is how a backend reaches the user. For prompts that want input
// it blocks until the UI answers and returns the answer, with ok false
// when the user cancelled; for info and error prompts it returns
// ("", false) at once.
type Ask func(Prompt) (answer string, ok bool)

// ErrCancelled is the failure a backend reports when the user
// cancelled an input prompt.
var ErrCancelled = errors.New("cancelled")

// Conversation is a backend that drives one authentication to
// completion. Run executes on a worker goroutine (blocking I/O is
// fine) and must never touch the GUI. username may be empty to let
// the backend ask for it. The error is the failure reason.
type Conversation interface {
	Run(username string, ask Ask) error
}

// reply is one UI answer; ok false is a cancellation.
type reply struct {
	text string
	ok   bool
}

// Handle is the UI's side of a running conversation: it answers the
// pending prompt. The zero value is not usable; Spawn returns one.
type Handle struct {
	replies chan reply
	cancel  chan struct{}
	once    sync.Once
}

// Answer replies to the prompt on screen. A reply nothing waits for —
// the conversation already ended — is dropped, never blocking the UI.
func (h *Handle) Answer(text string) { h.send(reply{text: text, ok: true}) }

// Cancel aborts the conversation: a backend blocked in ask sees a
// cancellation, and every later ask does too. Idempotent.
func (h *Handle) Cancel() { h.once.Do(func() { close(h.cancel) }) }

func (h *Handle) send(r reply) {
	select {
	case h.replies <- r:
	default:
		// The UI answers only a prompt on screen, one at a time, so a
		// full slot means nobody is asking: drop rather than block the
		// event loop.
	}
}

// Spawn runs conv on a worker goroutine. Prompts reach onEvent (called
// from the worker: marshal it onto the UI loop), and the UI answers
// through the returned Handle. Exactly one terminal EventSuccess or
// EventFailure is emitted before the worker exits.
func Spawn(conv Conversation, username string, onEvent func(Event)) *Handle {
	h := &Handle{replies: make(chan reply, 1), cancel: make(chan struct{})}
	go func() {
		ask := func(p Prompt) (string, bool) {
			onEvent(Event{Kind: EventPrompt, Prompt: p})
			if !p.WantsInput() {
				return "", false
			}
			select {
			case r := <-h.replies:
				return r.text, r.ok
			case <-h.cancel:
				return "", false
			}
		}
		if err := conv.Run(username, ask); err != nil {
			log.Printf("auth: conversation failed: %v", err)
			onEvent(Event{Kind: EventFailure, Reason: err.Error()})
			return
		}
		onEvent(Event{Kind: EventSuccess})
	}()
	return h
}

// CurrentUsername is the session user's login name from the
// environment: USER, then LOGNAME, then "". A locked graphical session
// always carries one of them.
func CurrentUsername() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("LOGNAME")
}

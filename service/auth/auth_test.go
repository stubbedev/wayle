package auth

import (
	"errors"
	"testing"
	"time"
)

// scripted is a Conversation that asks the given prompts in order and
// records the answers; err is its verdict.
type scripted struct {
	prompts []Prompt
	answers []string
	oks     []bool
	err     error
}

func (s *scripted) Run(_ string, ask Ask) error {
	for _, p := range s.prompts {
		a, ok := ask(p)
		s.answers = append(s.answers, a)
		s.oks = append(s.oks, ok)
		if p.WantsInput() && !ok {
			return ErrCancelled
		}
	}
	return s.err
}

// collect gathers the events of one spawned conversation.
func collect(t *testing.T, conv Conversation, script func(*Handle, Event)) []Event {
	t.Helper()
	events := make(chan Event, 16)
	var h *Handle
	ready := make(chan struct{})
	h = Spawn(conv, "alice", func(ev Event) {
		<-ready
		if script != nil {
			script(h, ev)
		}
		events <- ev
	})
	close(ready)
	var got []Event
	for {
		select {
		case ev := <-events:
			got = append(got, ev)
			if ev.Kind != EventPrompt {
				return got
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("conversation never ended; events so far: %+v", got)
		}
	}
}

// TestSpawnRoutesPromptsAndAnswers: input prompts wait for the UI's
// answer, info prompts do not, and exactly one terminal event ends
// the conversation.
func TestSpawnRoutesPromptsAndAnswers(t *testing.T) {
	conv := &scripted{prompts: []Prompt{
		{Kind: PromptInfo, Text: "hello"},
		{Kind: PromptSecret, Text: "Password:"},
		{Kind: PromptVisible, Text: "OTP:"},
	}}
	got := collect(t, conv, func(h *Handle, ev Event) {
		if ev.Kind == EventPrompt && ev.Prompt.WantsInput() {
			h.Answer("answer-to-" + ev.Prompt.Text)
		}
	})
	if len(got) != 4 || got[3].Kind != EventSuccess {
		t.Fatalf("events = %+v, want 3 prompts then success", got)
	}
	if conv.answers[0] != "" || conv.oks[0] {
		t.Errorf("info prompt returned (%q, %v), want no answer", conv.answers[0], conv.oks[0])
	}
	if conv.answers[1] != "answer-to-Password:" || conv.answers[2] != "answer-to-OTP:" {
		t.Errorf("answers = %q", conv.answers)
	}
}

// TestSpawnCancel: a cancelled input prompt aborts, surfacing one
// failure; answering or cancelling again after the end never blocks.
func TestSpawnCancel(t *testing.T) {
	conv := &scripted{prompts: []Prompt{{Kind: PromptSecret, Text: "Password:"}}}
	var handle *Handle
	got := collect(t, conv, func(h *Handle, ev Event) {
		handle = h
		if ev.Kind == EventPrompt {
			h.Cancel()
		}
	})
	if last := got[len(got)-1]; last.Kind != EventFailure || last.Reason != ErrCancelled.Error() {
		t.Fatalf("terminal event = %+v, want the cancellation failure", last)
	}
	done := make(chan struct{})
	go func() {
		handle.Answer("late")
		handle.Answer("later")
		handle.Cancel()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("answering a finished conversation blocked")
	}
}

func TestSpawnReportsBackendFailure(t *testing.T) {
	got := collect(t, &scripted{err: errors.New("authentication failed: nope")}, nil)
	if len(got) != 1 || got[0].Kind != EventFailure || got[0].Reason != "authentication failed: nope" {
		t.Fatalf("events = %+v", got)
	}
}

func TestCurrentUsername(t *testing.T) {
	t.Setenv("USER", "alice")
	t.Setenv("LOGNAME", "bob")
	if got := CurrentUsername(); got != "alice" {
		t.Errorf("USER set: %q", got)
	}
	t.Setenv("USER", "")
	if got := CurrentUsername(); got != "bob" {
		t.Errorf("LOGNAME fallback: %q", got)
	}
	t.Setenv("LOGNAME", "")
	if got := CurrentUsername(); got != "" {
		t.Errorf("neither set: %q", got)
	}
}

func TestPromptWantsInput(t *testing.T) {
	for kind, want := range map[PromptKind]bool{PromptSecret: true, PromptVisible: true, PromptInfo: false, PromptError: false} {
		if got := (Prompt{Kind: kind}).WantsInput(); got != want {
			t.Errorf("%v WantsInput = %v", kind, got)
		}
	}
}

package launcher

import (
	"context"
	"testing"
)

// staticMode is session.rs's StaticMode test double.
type staticMode struct {
	name      string
	entries   []string
	activated []Target
}

func (m *staticMode) Name() string { return m.name }

func (m *staticMode) Load(context.Context) ModeState {
	return ModeState{Items: items(m.entries...), Prompt: m.name}
}

func (m *staticMode) Activate(_ context.Context, target Target, _ ActivateKind, _ string) Action {
	m.activated = append(m.activated, target)
	return ActionClose{}
}

func newTestSession(t *testing.T) *Session {
	t.Helper()
	s, err := NewSession([]Mode{
		&staticMode{name: "alpha", entries: []string{"a1", "a2"}},
		&staticMode{name: "beta", entries: []string{"b1"}},
	}, DefaultMatcherOptions())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionNeedsAMode(t *testing.T) {
	if _, err := NewSession(nil, DefaultMatcherOptions()); err == nil {
		t.Fatal("a session over no modes must be refused")
	}
}

func TestLoadPopulatesEngineAndPrompt(t *testing.T) {
	s := newTestSession(t)
	s.Load(context.Background())
	if s.State().Prompt != "alpha" || len(s.Engine.Items()) != 2 {
		t.Errorf("state = %+v, items = %d", s.State(), len(s.Engine.Items()))
	}
}

func TestModeSwitchingWrapsAndLoads(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.Load(ctx)
	s.SwitchNext(ctx)
	if s.ActiveIndex() != 1 || len(s.Engine.Items()) != 1 {
		t.Fatalf("after next: %d / %d items", s.ActiveIndex(), len(s.Engine.Items()))
	}
	s.SwitchNext(ctx)
	if s.ActiveIndex() != 0 {
		t.Errorf("next wraps to 0, got %d", s.ActiveIndex())
	}
	s.SwitchPrevious(ctx)
	if s.ActiveIndex() != 1 {
		t.Errorf("previous wraps to 1, got %d", s.ActiveIndex())
	}
}

func TestSwitchToNamedUnknownIsFalse(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	if s.SwitchToNamed(ctx, "nope") {
		t.Error("an unknown mode must not switch")
	}
	if !s.SwitchToNamed(ctx, "beta") || s.ActiveIndex() != 1 {
		t.Error("a known mode switches")
	}
}

func TestActivatePassesThroughClose(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.Load(ctx)
	if _, ok := s.Activate(ctx, RowTarget(0), ActivateDefault{}).(ActionClose); !ok {
		t.Error("the mode's Close must reach the surface")
	}
}

func TestACompleterFillsTheQueryInsteadOfLaunching(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.SetCompleter("beta")
	s.Load(ctx)
	if !s.OpenCompleter(ctx) || !s.Completing() || s.ActiveIndex() != 1 {
		t.Fatal("the completer opens on beta")
	}
	got, ok := s.Activate(ctx, RowTarget(0), ActivateDefault{}).(ActionSetInput)
	if !ok || got.Text != "b1" {
		t.Fatalf("accept = %#v, want SetInput b1", got)
	}
	if s.Completing() || s.ActiveIndex() != 0 {
		t.Error("back to the original mode after completing")
	}
}

func TestCancellingTheCompleterTakesNothing(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.SetCompleter("beta")
	s.Load(ctx)
	s.OpenCompleter(ctx)
	s.CancelCompleter(ctx)
	if s.Completing() || s.ActiveIndex() != 0 {
		t.Fatal("cancel returns to alpha")
	}
	if _, ok := s.Activate(ctx, RowTarget(0), ActivateDefault{}).(ActionClose); !ok {
		t.Error("a later accept behaves normally again")
	}
}

func TestACompleterThatIsNotLoadedDoesNothing(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.SetCompleter("nope")
	s.Load(ctx)
	if s.OpenCompleter(ctx) || s.Completing() || s.ActiveIndex() != 0 {
		t.Error("an unloaded completer must not switch anywhere")
	}
	s.SetCompleter("alpha")
	if s.OpenCompleter(ctx) {
		t.Error("the completer cannot be the mode already showing")
	}
	s.SetCompleter("")
	if s.OpenCompleter(ctx) {
		t.Error("no completer configured means the key is inert")
	}
}

func TestTheCompleterCannotBeOpenedTwice(t *testing.T) {
	ctx := context.Background()
	s := newTestSession(t)
	s.SetCompleter("beta")
	s.Load(ctx)
	if !s.OpenCompleter(ctx) || s.OpenCompleter(ctx) {
		t.Fatal("the second open must be refused")
	}
	if s.completeFrom != 0 {
		t.Errorf("the mode to return to was overwritten: %d", s.completeFrom)
	}
}

// customRefuser refuses typed text.
type customRefuser struct{ staticMode }

func (customRefuser) AllowsCustom() bool { return false }

func TestCustomInputARefusingModeNeverSees(t *testing.T) {
	ctx := context.Background()
	m := &customRefuser{staticMode{name: "keys", entries: []string{"x"}}}
	s, _ := NewSession([]Mode{m}, DefaultMatcherOptions())
	s.Load(ctx)
	if _, ok := s.Activate(ctx, NoRow, ActivateCustom{Text: "typed"}).(ActionNothing); !ok {
		t.Error("custom input to a refusing mode is nothing")
	}
	if len(m.activated) != 0 {
		t.Error("the mode must not be called")
	}
	if _, ok := s.Activate(ctx, RowTarget(0), ActivateDefault{}).(ActionClose); !ok {
		t.Error("a row accept still reaches it")
	}
}

func TestSplitBang(t *testing.T) {
	for _, tc := range []struct {
		in, bang, rest string
		ok             bool
	}{
		{"!win term", "win", "term", true},
		{"!win", "win", "", true},
		{"win term", "", "win term", false},
		{"!", "", "", true},
	} {
		bang, rest, ok := splitBang(tc.in)
		if bang != tc.bang || rest != tc.rest || ok != tc.ok {
			t.Errorf("splitBang(%q) = %q %q %v", tc.in, bang, rest, ok)
		}
	}
}

// subsetMode is a combi stand-in whose bang "b" keeps only row 1.
type subsetMode struct{ staticMode }

func (subsetMode) Subset(bang string) ([]bool, bool) {
	if bang != "b" {
		return nil, false
	}
	return []bool{false, true}, true
}

func TestABangMasksAndIsStripped(t *testing.T) {
	ctx := context.Background()
	s, _ := NewSession([]Mode{&subsetMode{staticMode{name: "combi", entries: []string{"x1", "x2"}}}}, DefaultMatcherOptions())
	s.Load(ctx)
	s.SetQuery("!b x")
	if got := s.Matched(); len(got) != 1 || got[0] != 1 {
		t.Errorf("masked matches = %v, want [1]", got)
	}
	if s.Query() != "x" {
		t.Errorf("query = %q, want the bang stripped", s.Query())
	}
	// An unknown bang is plain query text.
	s.SetQuery("!zz x")
	if s.Query() != "!zz x" {
		t.Errorf("unknown bang query = %q", s.Query())
	}
}

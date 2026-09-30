package cli

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestJaroMatchesStrsim(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"tr", "track", 0.8},
		{"", "", 1},
		{"abc", "", 0},
		{"martha", "marhta", 0.944444},
		{"xyz", "abc", 0},
	}
	for _, c := range cases {
		if got := jaro(c.a, c.b); math.Abs(got-c.want) > 1e-5 {
			t.Errorf("jaro(%q, %q) = %f, want %f", c.a, c.b, got, c.want)
		}
	}
}

func TestDidYouMeanThresholdAndOrder(t *testing.T) {
	if got := didYouMean("audi", []string{"audio", "idle", "config"}); len(got) != 1 || got[0] != "audio" {
		t.Errorf("audi: %v", got)
	}
	if got := didYouMean("zzz", []string{"audio", "idle"}); len(got) != 0 {
		t.Errorf("no candidate should clear 0.7: %v", got)
	}
	// Least similar first: callers take the last.
	got := didYouMean("stat", []string{"status", "stats"})
	if len(got) != 2 || got[1] != "stats" {
		t.Errorf("order: %v", got)
	}
}

func TestColorChoice(t *testing.T) {
	env := func(vars map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
	}
	cases := []struct {
		name     string
		terminal bool
		vars     map[string]string
		want     bool
	}{
		{"tty with TERM", true, map[string]string{"TERM": "xterm"}, true},
		{"tty without TERM", true, nil, false},
		{"dumb tty", true, map[string]string{"TERM": "dumb"}, false},
		{"pipe", false, map[string]string{"TERM": "xterm"}, false},
		{"NO_COLOR wins", true, map[string]string{"TERM": "xterm", "NO_COLOR": "1", "CLICOLOR_FORCE": "1"}, false},
		{"empty NO_COLOR ignored", true, map[string]string{"TERM": "xterm", "NO_COLOR": ""}, true},
		{"CLICOLOR_FORCE on a pipe", false, map[string]string{"CLICOLOR_FORCE": "1"}, true},
		{"CLICOLOR_FORCE=0 is off", false, map[string]string{"CLICOLOR_FORCE": "0"}, false},
		{"CLICOLOR=0", true, map[string]string{"TERM": "xterm", "CLICOLOR": "0"}, false},
		{"CI tty without TERM", true, map[string]string{"CI": "true"}, true},
	}
	for _, c := range cases {
		if got := colorChoice(c.terminal, env(c.vars)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStripRemovesEveryEscape(t *testing.T) {
	in := styleLiteral.wrap("a") + "\x1b[1;33mb\x1b[0m" + "c"
	if got := strip(in); got != "abc" {
		t.Errorf("strip = %q", got)
	}
	if got := strip("plain"); got != "plain" {
		t.Errorf("plain = %q", got)
	}
}

// testTree is a small tree exercising typed values, defaults, and the
// handler contract.
func testTree(seen *Matches) *Command {
	return &Command{
		Name: "tool", About: "A tool", Version: "1.0",
		Subcommands: []*Command{
			{
				Name: "go", About: "Go somewhere",
				Args: []*Arg{
					{ID: "count", Long: "count", Short: 'c', Value: U32, Defaults: []string{"3"}},
					{ID: "mode", Long: "mode", Value: Enum(PossibleValue{Name: "fast"}, PossibleValue{Name: "slow"})},
					{ID: "verbose", Long: "verbose", Short: 'v'},
					{ID: "files", Multiple: true},
				},
				Run: func(m *Matches) error { *seen = *m; return nil },
			},
			{Name: "fail", About: "Fail", Run: func(*Matches) error { return errors.New("boom") }},
			{Name: "exit", About: "Exit", Run: func(*Matches) error { return ExitError{Code: 7} }},
		},
	}
}

func runTool(t *testing.T, args ...string) (Matches, string, string, int) {
	t.Helper()
	var seen Matches
	var out, errOut bytes.Buffer
	code := Run(testTree(&seen), args, Output{Stdout: &out, Stderr: &errOut})
	return seen, out.String(), errOut.String(), code
}

func TestRunParsesTypedValuesAndDefaults(t *testing.T) {
	m, _, stderr, code := runTool(t, "go", "-vc", "5", "--mode=slow", "a", "b")
	if code != 0 {
		t.Fatalf("code %d: %s", code, stderr)
	}
	if n, _ := Value[uint32](&m, "count"); n != 5 {
		t.Errorf("count = %d", n)
	}
	if mode, _ := Value[string](&m, "mode"); mode != "slow" {
		t.Errorf("mode = %q", mode)
	}
	if !m.Flag("verbose") {
		t.Error("verbose not set")
	}
	if files := Values[string](&m, "files"); strings.Join(files, ",") != "a,b" {
		t.Errorf("files = %v", files)
	}

	m, _, _, _ = runTool(t, "go")
	if n, ok := Value[uint32](&m, "count"); !ok || n != 3 {
		t.Errorf("default count = %d, %v", n, ok)
	}
	if _, ok := Value[string](&m, "mode"); ok {
		t.Error("absent mode without a default must be absent")
	}
	if m.Flag("verbose") {
		t.Error("absent flag must be false")
	}
}

func TestRunRejectsBadValuesWithUsageExit(t *testing.T) {
	_, _, stderr, code := runTool(t, "go", "--count", "-1")
	if code != 2 || !strings.Contains(stderr, "unexpected argument '-1' found") {
		t.Errorf("negative count: code %d %q", code, stderr)
	}
	_, _, stderr, code = runTool(t, "go", "--count=-1")
	if code != 2 || !strings.Contains(stderr, "-1 is not in 0..=4294967295") {
		t.Errorf("range: code %d %q", code, stderr)
	}
	_, _, stderr, code = runTool(t, "go", "--mode", "medium")
	if code != 2 || !strings.Contains(stderr, "[possible values: fast, slow]") {
		t.Errorf("enum: code %d %q", code, stderr)
	}
	_, _, stderr, code = runTool(t, "go", "--verbose=yes")
	if code != 2 || !strings.Contains(stderr, "unexpected value 'yes' for '--verbose' found; no more were expected") {
		t.Errorf("flag value: code %d %q", code, stderr)
	}
}

func TestRunHandlerOutcomes(t *testing.T) {
	_, _, stderr, code := runTool(t, "fail")
	if code != 1 || stderr != "Error: boom\n" {
		t.Errorf("fail: code %d %q", code, stderr)
	}
	_, _, stderr, code = runTool(t, "exit")
	if code != 7 || stderr != "" {
		t.Errorf("exit: code %d %q", code, stderr)
	}
	_, stdout, _, code := runTool(t, "-V")
	if code != 0 || stdout != "tool 1.0\n" {
		t.Errorf("version: code %d %q", code, stdout)
	}
	_, _, stderr, code = runTool(t, "go", "-V")
	if code != 2 || !strings.Contains(stderr, "unexpected argument '-V' found") {
		t.Errorf("version is root-only: code %d %q", code, stderr)
	}
}

func TestValueTypeMismatchPanics(t *testing.T) {
	m, _, _, _ := runTool(t, "go", "--count", "1")
	defer func() {
		if recover() == nil {
			t.Error("reading a u32 arg as string must panic")
		}
	}()
	Value[string](&m, "count")
}

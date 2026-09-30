package launcher

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func hookCtx() HookContext {
	return HookContext{Input: "fire", Entry: "Firefox", Mode: "drun", Error: "no modes"}
}

func TestHookPlaceholdersAreFilled(t *testing.T) {
	if got := HookArgv("notify-send {mode} {entry} {input}", hookCtx()); !reflect.DeepEqual(got, []string{"notify-send", "drun", "Firefox", "fire"}) {
		t.Errorf("got %q", got)
	}
	if got := HookArgv("say {error}", hookCtx()); !reflect.DeepEqual(got, []string{"say", "no modes"}) {
		t.Errorf("got %q", got)
	}
}

func TestARowCannotSmuggleASecondCommand(t *testing.T) {
	c := hookCtx()
	c.Entry = "x; rm -rf ~"
	if got := HookArgv("preview {entry}", c); !reflect.DeepEqual(got, []string{"preview", "x; rm -rf ~"}) {
		t.Errorf("the row must be one argument: %q", got)
	}
}

func TestAShellVariableStaysLiteral(t *testing.T) {
	if got := HookArgv("echo $HOME", hookCtx()); !reflect.DeepEqual(got, []string{"echo", "$HOME"}) {
		t.Errorf("got %q", got)
	}
}

func TestAnUnknownHookPlaceholderRendersEmpty(t *testing.T) {
	if got := HookArgv("echo a{nope}b", hookCtx()); !reflect.DeepEqual(got, []string{"echo", "ab"}) {
		t.Errorf("got %q", got)
	}
}

func TestNothingRunsForABlankOrBrokenCommand(t *testing.T) {
	for _, cmd := range []string{"", "   ", "echo 'unbalanced", "{nope}"} {
		if got := HookArgv(cmd, hookCtx()); len(got) != 0 {
			t.Errorf("%q = %q, want nothing", cmd, got)
		}
	}
}

func TestASessionWithNoHooksAsksForNoWork(t *testing.T) {
	if (Hooks{}).Any() {
		t.Error("no hooks must report none")
	}
	if !(Hooks{MenuCanceled: "true"}).Any() {
		t.Error("one hook must report some")
	}
}

func TestFireHookRunsDetachedWithoutAShell(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hook")
	// touch receives the entry as one literal argument; a shell would
	// have split it at the space and expanded nothing.
	c := hookCtx()
	c.Entry = out
	FireHook("touch {entry}", c)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(out); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the hook never ran")
}

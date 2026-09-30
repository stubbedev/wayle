package launcher

import (
	"log"
	"strings"
)

// Hooks are the commands run when something happens in the menu (rofi
// -on-*), hooks.rs. rofi's contract, pinned against rofi 2.0.0:
//
//   - the command string is shell-parsed and exec'd directly, never
//     handed to `sh -c`: a $HOME stays literal and a `;` is one more
//     argument, so a row holding shell metacharacters cannot turn a
//     hook into a second command;
//   - {input}, {entry}, {mode}, and {error} are substituted where the
//     event has one;
//   - nothing arrives on stdin and no ROFI_* variables are exported;
//   - the child is detached; the menu does not wait for it.
//
// Where rofi is inconsistent, wayle is not: rofi substitutes only for
// -on-selection-changed and -on-entry-accepted and hands the others
// their placeholders verbatim. Substituting everywhere is a superset of
// what a rofi script can rely on.
//
// An empty string is no hook.
type Hooks struct {
	// SelectionChanged is -on-selection-changed.
	SelectionChanged string
	// EntryAccepted is -on-entry-accepted.
	EntryAccepted string
	// ModeChanged is -on-mode-changed.
	ModeChanged string
	// MenuCanceled is -on-menu-canceled.
	MenuCanceled string
	// MenuError is -on-menu-error.
	MenuError string
}

// Any reports whether any hook is set, so a session that asked for none
// skips the work of collecting row text.
func (h Hooks) Any() bool {
	return h.SelectionChanged != "" || h.EntryAccepted != "" || h.ModeChanged != "" ||
		h.MenuCanceled != "" || h.MenuError != ""
}

// HookContext is what an event knows, for the placeholders.
type HookContext struct {
	// Input is {input}: the query text at the event.
	Input string
	// Entry is {entry}: the row the event is about.
	Entry string
	// Mode is {mode}: the active mode's name.
	Mode string
	// Error is {error}: what went wrong, for -on-menu-error.
	Error string
}

// HookArgv substitutes ctx into command and returns the argv to exec.
// Empty when the command is blank, does not shell-parse, or renders to
// nothing but empty placeholders.
func HookArgv(command string, ctx HookContext) []string {
	argv := RenderArgv(command, func(key string) (string, bool) {
		switch key {
		case "input":
			return ctx.Input, true
		case "entry":
			return ctx.Entry, true
		case "mode":
			return ctx.Mode, true
		case "error":
			return ctx.Error, true
		}
		// An unknown placeholder renders empty, as templates do
		// everywhere else in the launcher.
		return "", false
	})
	for _, arg := range argv {
		if strings.TrimSpace(arg) != "" {
			return argv
		}
	}
	return nil
}

// FireHook runs command with ctx substituted, detached. A blank command
// does nothing; an unparseable one is logged and does nothing.
func FireHook(command string, ctx HookContext) {
	if command == "" {
		return
	}
	argv := HookArgv(command, ctx)
	if len(argv) == 0 {
		if strings.TrimSpace(command) != "" {
			log.Printf("launcher: hook %q does not parse as a command", command)
		}
		return
	}
	RunArgv(argv)
}

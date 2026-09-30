package modes

import (
	"bufio"
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/wayle/service/launcher"
)

// IsIgnored reports whether an entry is one the user keeps out of
// history (rofi -ignored-prefixes). It matches the command - the
// executable for run, the desktop id for drun - because that is what
// history stores; an empty prefix matches nothing.
func IsIgnored(entry string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(entry, p) {
			return true
		}
	}
	return false
}

// RunConfig is the run mode's knobs (rofi's -run-* family).
type RunConfig struct {
	// RunCommand is the plain accept template ({cmd}).
	RunCommand string
	// ShellCommand is the alt accept template ({terminal}, {cmd}).
	ShellCommand string
	// ListCommand adds its stdout lines as entries.
	ListCommand string
	// Terminal is the terminal emulator; empty autodetects.
	Terminal string
	// MaxHistory caps the recorded history.
	MaxHistory uint32
	// IgnoredPrefixes keeps matching commands out of history.
	IgnoredPrefixes []string
}

// DefaultRunConfig is RunConfig::default.
func DefaultRunConfig() RunConfig {
	return RunConfig{RunCommand: "{cmd}", ShellCommand: "{terminal} -e {cmd}", MaxHistory: 25}
}

// Run executes commands found on $PATH (run.rs).
type Run struct {
	cfg      RunConfig
	history  *launcher.History
	commands []string
}

// NewRun builds the mode; a nil history disables recency and recording.
func NewRun(cfg RunConfig, history *launcher.History) *Run {
	return &Run{cfg: cfg, history: history}
}

// Name is "run".
func (*Run) Name() string { return "run" }

func (r *Run) execute(command string, inTerminal bool) {
	var rendered string
	if inTerminal {
		terminal := launcher.DetectTerminal(r.cfg.Terminal)
		rendered = launcher.Render(r.cfg.ShellCommand, launcher.Values(map[string]string{"cmd": command, "terminal": terminal}))
	} else {
		rendered = launcher.Render(r.cfg.RunCommand, launcher.Values(map[string]string{"cmd": command}))
	}
	launcher.RunShell(rendered)
	if IsIgnored(command, r.cfg.IgnoredPrefixes) || r.history == nil {
		return
	}
	if err := r.history.Record("run", command, r.cfg.MaxHistory); err != nil {
		log.Printf("launcher: run history record failed: %v", err)
	}
}

// Load lists the $PATH executables (plus the list command's lines),
// recently used first, then alphabetically.
func (r *Run) Load(ctx context.Context) launcher.ModeState {
	names := scanPath()
	if strings.TrimSpace(r.cfg.ListCommand) != "" {
		for _, n := range listCommandEntries(ctx, r.cfg.ListCommand) {
			names[n] = true
		}
	}
	var recent []string
	if r.history != nil {
		recent, _ = r.history.Recent("run")
	}
	r.commands = orderByRecent(names, recent)
	items := make([]launcher.Item, len(r.commands))
	for i, c := range r.commands {
		items[i] = launcher.NewItem(c)
	}
	return launcher.ModeState{Items: items, Prompt: "run"}
}

// Activate runs the row's command or the typed one; Alt runs it in a
// terminal.
func (r *Run) Activate(_ context.Context, target launcher.Target, kind launcher.ActivateKind, _ string) launcher.Action {
	var command string
	if row, ok := target.Row(); ok {
		if int(row) >= len(r.commands) {
			return launcher.ActionNothing{}
		}
		command = r.commands[row]
	} else if custom, ok := kind.(launcher.ActivateCustom); ok {
		command = custom.Text
	} else {
		return launcher.ActionNothing{}
	}
	if strings.TrimSpace(command) == "" {
		return launcher.ActionNothing{}
	}
	_, alt := kind.(launcher.ActivateAlt)
	r.execute(command, alt)
	return launcher.ActionClose{}
}

// Delete forgets the row's history entry and reloads.
func (r *Run) Delete(ctx context.Context, index uint32) launcher.Action {
	if r.history == nil || int(index) >= len(r.commands) {
		return launcher.ActionNothing{}
	}
	if err := r.history.Remove("run", r.commands[index]); err != nil {
		log.Printf("launcher: run history delete failed: %v", err)
	}
	return launcher.ActionReload{State: r.Load(ctx)}
}

// orderByRecent is recently used first (rofi's run order), then the
// alphabetical rest.
func orderByRecent(names map[string]bool, recent []string) []string {
	var ordered []string
	for _, e := range recent {
		if names[e] {
			ordered = append(ordered, e)
		}
	}
	var rest []string
	for n := range names {
		if !slices.Contains(recent, n) {
			rest = append(rest, n)
		}
	}
	slices.Sort(rest)
	return append(ordered, rest...)
}

// scanPath collects the executable file names on $PATH.
func scanPath() map[string]bool {
	names := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		collectExecutables(dir, names)
	}
	return names
}

// collectExecutables adds dir's executable files. Symlinks are
// followed: on NixOS nearly every PATH entry is one (the Rust scan's
// DirEntry metadata did not follow them, which left run mode close to
// empty there).
func collectExecutables(dir string, names map[string]bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		st, err := os.Stat(filepath.Join(dir, e.Name())) //nolint:gosec // a PATH directory listing
		if err != nil || !st.Mode().IsRegular() || st.Mode()&0o111 == 0 {
			continue
		}
		names[e.Name()] = true
	}
}

// listCommandEntries runs -run-list-command and returns its non-empty
// stdout lines.
func listCommandEntries(ctx context.Context, command string) []string {
	out, err := exec.CommandContext(ctx, "sh", "-c", command).Output() //nolint:gosec // the user's list command
	if err != nil && len(out) == 0 {
		if exitErr := (*exec.ExitError)(nil); !errors.As(err, &exitErr) {
			log.Printf("launcher: run-list-command %q failed: %v", command, err)
		}
		return nil
	}
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(strings.ToValidUTF8(string(out), "�")))
	for sc.Scan() {
		if line := strings.TrimSuffix(sc.Text(), "\r"); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

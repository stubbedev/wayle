package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Output is where a run writes, and whether each stream takes ANSI
// styling (clap colors help on stdout and errors on stderr
// independently).
type Output struct {
	Stdout      io.Writer
	Stderr      io.Writer
	StdoutColor bool
	StderrColor bool
}

// StdOutput is the process's own streams with the auto color choice.
func StdOutput() Output {
	return Output{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		StdoutColor: ColorEnabled(os.Stdout),
		StderrColor: ColorEnabled(os.Stderr),
	}
}

// ExitError ends a run with Code and prints nothing: for handlers
// whose exit status is their result (the portal backend's).
type ExitError struct{ Code int }

func (e ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

// Run parses args (without argv[0]) against root, runs the matched
// command, and returns the process exit status: 0 on success and for
// help/version, 2 for a usage error (clap's), 1 when the handler fails
// (printed as "Error: <err>", as wayle's main.rs does).
func Run(root *Command, args []string, out Output) int {
	c := build(root, nil, false)
	m, perr := parse(c, args)
	if perr != nil {
		w, color := out.Stderr, out.StderrColor
		if perr.toStdout() {
			w, color = out.Stdout, out.StdoutColor
		}
		text := perr.render()
		if !color {
			text = strip(text)
		}
		_, _ = io.WriteString(w, text)
		return perr.ExitCode()
	}
	for m.sub != nil {
		m = m.sub
	}
	m.stdout, m.stderr = out.Stdout, out.Stderr
	if m.cmd.decl == nil || m.cmd.decl.Run == nil {
		// Unreachable for a valid tree: validate requires a subcommand
		// wherever there is no handler.
		_, _ = fmt.Fprintf(out.Stderr, "Error: %s has no handler\n", m.cmd.bin)
		return 1
	}
	if err := m.cmd.decl.Run(m); err != nil {
		if exit, ok := errors.AsType[ExitError](err); ok {
			return exit.Code
		}
		_, _ = fmt.Fprintf(out.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

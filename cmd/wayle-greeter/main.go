// Command wayle-greeter is the greetd greeter (crates/wayle-greeter):
// the lock screen's credential prompt over a session picker and the
// login users, run as the single client of a kiosk compositor. On
// success greetd starts the chosen session and replaces this process.
//
//	wayle-greeter [--config PATH] [--sessions DIR]... [--xsessions DIR]...
//	              [--state PATH] [--env KEY=VAL]... [-- <session argv...>]
//	wayle-greeter apply-config [--config PATH] <STAGED.toml>
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/greeter"
)

func main() {
	args := os.Args[1:]
	// The privileged config writer wayle-settings runs through pkexec:
	// handled first, a plain side-effect-free CLI.
	if len(args) > 0 && args[0] == "apply-config" {
		os.Exit(greeter.RunApplyConfig(args[1:], os.Stdout, os.Stderr))
	}

	opts, err := greeter.ParseOptions(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg, err := config.LoadGreeter(opts.ConfigPath, config.StderrDiagnostics)
	if err != nil {
		log.Printf("greeter: config load failed; using defaults: %v", err)
	}

	// Wayland and X11 sessions, sorted by name, plus the explicit
	// `-- <argv>` as a Custom entry (or the only one).
	sessions := greeter.Discover(opts.SessionDirs, greeter.Wayland)
	sessions = append(sessions, greeter.Discover(opts.XSessionDirs, greeter.X11)...)
	greeter.SortSessions(sessions)
	if len(opts.Command) > 0 {
		sessions = append(sessions, greeter.Session{ID: "custom", Name: greeter.CustomSessionName, Exec: opts.Command})
	}
	if len(sessions) == 0 {
		fmt.Fprintf(os.Stderr, "no sessions found in %q or %q and no `-- <argv>` fallback given; nothing to log into\n",
			opts.SessionDirs, opts.XSessionDirs)
		os.Exit(2)
	}

	err = greeter.Run(greeter.Init{
		Config:      cfg,
		Sessions:    sessions,
		Users:       greeter.LoadUsers(),
		LastSession: greeter.LoadLast(opts.StatePath),
		LastUser:    greeter.LoadLast(greeter.LastUserPath(opts.StatePath)),
		StatePath:   opts.StatePath,
		SessionEnv:  opts.Env,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "wayle-greeter:", err)
		os.Exit(1)
	}
}

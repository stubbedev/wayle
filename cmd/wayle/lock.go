package main

import "github.com/stubbedev/wayle/internal/cli"

// lockCommand is app.rs's Lock.
func lockCommand() *cli.Command {
	return &cli.Command{
		Name:  "lock",
		About: "Lock the session via Wayle's lock screen",
		Run:   notPorted("lock"),
	}
}

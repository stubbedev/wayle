package main

import (
	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/shell/bar"
)

// shellCommand is app.rs's Shell: the desktop shell in the foreground.
func shellCommand() *cli.Command {
	return &cli.Command{
		Name:  "shell",
		About: "Run the desktop shell in the foreground",
		Run:   func(*cli.Matches) error { return bar.Run() },
	}
}

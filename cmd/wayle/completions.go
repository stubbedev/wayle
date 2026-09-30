package main

import (
	"github.com/stubbedev/wayle/internal/cli"
)

// completionsCommand is app.rs's Completions: clap_complete's script
// for the named shell, generated from the same tree that parses.
func completionsCommand() *cli.Command {
	return &cli.Command{
		Name:  "completions",
		About: "Generate shell completions",
		Args: []*cli.Arg{
			{ID: "shell", Required: true, Value: cli.Enum(cli.CompletionShells...), Help: "Shell to generate completions for"},
		},
		Run: func(m *cli.Matches) error {
			shell, _ := cli.Value[string](m, "shell")
			return cli.WriteCompletions(m.Stdout(), rootCommand(), shell)
		},
	}
}

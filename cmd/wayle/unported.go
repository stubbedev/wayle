package main

import (
	"fmt"

	"github.com/stubbedev/wayle/internal/cli"
)

// notPorted is the handler of a subcommand whose behavior is not
// ported yet: its argument tree already parses exactly as in Rust, and
// running it exits 1 naming the command.
func notPorted(path string) func(*cli.Matches) error {
	return func(*cli.Matches) error {
		return fmt.Errorf("`wayle %s` is not ported to the Go shell yet", path)
	}
}

package main

import (
	"fmt"
	"os"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/logging"
)

// selfManaged are the commands main.rs runs outside the shared CLI
// setup: the shell and the portal set up their own logging, and
// completions only print.
var selfManaged = map[string]bool{"shell": true, "portal": true, "completions": true}

// withCLIMode wraps every other command's handler in main.rs's CLI
// setup: file-only logging (tracing.rs's init_cli_mode, "wayle.<day>.log")
// and init.rs's ensure_directories. Failures are reported and ignored,
// as in Rust.
func withCLIMode(root *cli.Command) *cli.Command {
	for _, sub := range root.Subcommands {
		if !selfManaged[sub.Name] {
			wrapRuns(sub)
		}
	}
	return root
}

func wrapRuns(c *cli.Command) {
	if run := c.Run; run != nil {
		c.Run = func(m *cli.Matches) error {
			closer, err := logging.Setup("wayle", nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize tracing: %v\n", err)
			} else {
				defer func() { _ = closer.Close() }()
			}
			if err := ensureDirectories(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to ensure directories: %v\n", err)
			}
			return run(m)
		}
	}
	for _, sub := range c.Subcommands {
		wrapRuns(sub)
	}
}

// ensureDirectories is init.rs's: the config directory exists.
func ensureDirectories() error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755) //nolint:gosec // the user config dir, as create_dir_all makes it (0777 less umask)
}

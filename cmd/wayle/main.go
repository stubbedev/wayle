// Command wayle is the Go rewrite's entry point: the wayle/src/main.rs
// CLI with the same clap tree (help, flags, errors, exit codes, shell
// completions), declared once in internal/cli form. Each top-level
// subcommand lives in its own file as a <name>Command constructor;
// anything not ported yet keeps its full argument tree and exits 1
// naming itself instead of silently doing nothing.
package main

import (
	"os"
	"path/filepath"

	"github.com/stubbedev/wayle/internal/cli"
)

// version is the workspace version (Cargo.toml [workspace.package]);
// release builds may override it with -ldflags "-X main.version=...".
var version = "0.8.53"

// rootCommand is wayle/src/cli/app.rs's Cli: the subcommands in the
// Rust enum's order, which is the order help and completions list.
func rootCommand() *cli.Command {
	return &cli.Command{
		Name:    "wayle",
		About:   "Wayland services CLI",
		Version: version,
		Subcommands: []*cli.Command{
			audioCommand(),
			configCommand(),
			iconsCommand(),
			mediaCommand(),
			notifyCommand(),
			panelCommand(),
			powerCommand(),
			systrayCommand(),
			wallpaperCommand(),
			idleCommand(),
			launcherCommand(),
			lockCommand(),
			vpnCommand(),
			recorderCommand(),
			screenshotCommand(),
			widgetCommand(),
			toastCommand(),
			portalCommand(),
			shellCommand(),
			completionsCommand(),
		},
	}
}

func main() {
	os.Exit(run(os.Args, cli.StdOutput()))
}

// run is main without the process exit. Invoked as `rofi` (a symlink
// or hardlink), the whole argument vector is rofi-style launcher flags
// (main.rs's invoked_as_rofi).
func run(argv []string, out cli.Output) int {
	args := argv[1:]
	if len(argv) > 0 && filepath.Base(argv[0]) == "rofi" {
		args = append([]string{"launcher"}, args...)
	}
	return cli.Run(rootCommand(), args, out)
}

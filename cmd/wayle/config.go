package main

import "github.com/stubbedev/wayle/internal/cli"

// configCommand is wayle/src/cli/config/commands.rs.
func configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		About: "Configuration management commands",
		Subcommands: []*cli.Command{
			{
				Name:  "get",
				About: "Get the value of a configuration path",
				Args: []*cli.Arg{
					{ID: "path", Required: true, Help: `The configuration path to retrieve (e.g., "modules.battery.enabled")`},
				},
				Run: notPorted("config get"),
			},
			{
				Name:  "set",
				About: "Set the value of a configuration path",
				Args: []*cli.Arg{
					{ID: "path", Required: true, Help: `The configuration path to set (e.g., "modules.battery.enabled")`},
					{ID: "value", Required: true, Help: "The value to set (use JSON format for complex types)"},
				},
				Run: notPorted("config set"),
			},
			{
				Name:  "reset",
				About: "Reset a configuration path to its default value",
				Args: []*cli.Arg{
					{ID: "path", Required: true, Help: `The configuration path to reset (e.g., "bar.button_gap")`},
				},
				Run: notPorted("config reset"),
			},
			{
				Name:  "schema",
				About: "Output JSON Schema for the configuration (for editor intellisense)",
				Args: []*cli.Arg{
					{ID: "stdout", Long: "stdout", Help: "Print to stdout instead of writing to config directory"},
				},
				Run: notPorted("config schema"),
			},
			{
				Name:  "default",
				About: "Output the default configuration as TOML",
				Args: []*cli.Arg{
					{ID: "stdout", Long: "stdout", Help: "Print to stdout instead of writing config.toml.example"},
				},
				Run: notPorted("config default"),
			},
			{
				Name:  "docs",
				About: "Generate markdown reference pages for every registered schema",
				Args: []*cli.Arg{
					{ID: "out", Long: "out", Value: cli.Path, Defaults: []string{"docs/config"}, Help: "Output directory for the generated pages"},
					{ID: "only", Long: "only", Value: cli.String, Help: "Regenerate only the named module"},
				},
				Run: notPorted("config docs"),
			},
		},
	}
}

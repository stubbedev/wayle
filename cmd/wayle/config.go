package main

import (
	"fmt"
	"io"
	"os"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/internal/cli"
)

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
				Run: withConfigDir(func(m *cli.Matches, dir string) error {
					path, _ := cli.Value[string](m, "path")
					return configGet(dir, path, m.Stdout())
				}),
			},
			{
				Name:  "set",
				About: "Set the value of a configuration path",
				Args: []*cli.Arg{
					{ID: "path", Required: true, Help: `The configuration path to set (e.g., "modules.battery.enabled")`},
					{ID: "value", Required: true, Help: "The value to set (use JSON format for complex types)"},
				},
				Run: withConfigDir(func(m *cli.Matches, dir string) error {
					path, _ := cli.Value[string](m, "path")
					value, _ := cli.Value[string](m, "value")
					return configSet(dir, path, value, m.Stdout())
				}),
			},
			{
				Name:  "reset",
				About: "Reset a configuration path to its default value",
				Args: []*cli.Arg{
					{ID: "path", Required: true, Help: `The configuration path to reset (e.g., "bar.button_gap")`},
				},
				Run: withConfigDir(func(m *cli.Matches, dir string) error {
					path, _ := cli.Value[string](m, "path")
					return configReset(dir, path, m.Stdout())
				}),
			},
			{
				Name:  "schema",
				About: "Output JSON Schema for the configuration (for editor intellisense)",
				Args: []*cli.Arg{
					{ID: "stdout", Long: "stdout", Help: "Print to stdout instead of writing to config directory"},
				},
				Run: withConfigDir(func(m *cli.Matches, dir string) error {
					return configSchema(dir, m.Flag("stdout"), m.Stdout())
				}),
			},
			{
				Name:  "default",
				About: "Output the default configuration as TOML",
				Args: []*cli.Arg{
					{ID: "stdout", Long: "stdout", Help: "Print to stdout instead of writing config.toml.example"},
				},
				Run: withConfigDir(func(m *cli.Matches, dir string) error {
					return configDefault(dir, m.Flag("stdout"), m.Stdout())
				}),
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

// withConfigDir resolves the config directory for a `wayle config`
// handler.
func withConfigDir(run func(*cli.Matches, string) error) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		return run(m, dir)
	}
}

func configGet(dir, path string, stdout io.Writer) error {
	svc := config.Load(dir, config.StderrDiagnostics)
	value, err := svc.GetByPath(path)
	if err != nil {
		return fmt.Errorf("Failed to get config at '%s': %w", path, err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	_, err = fmt.Fprintln(stdout, config.FormatGetValue(value))
	return err
}

func configSet(dir, path, raw string, stdout io.Writer) error {
	svc := config.Load(dir, config.StderrDiagnostics)
	if err := svc.SetByPath(path, config.ParseCLIValue(raw)); err != nil {
		return fmt.Errorf("Failed to set config at '%s': %w", path, err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	if err := svc.Save(); err != nil {
		return fmt.Errorf("Failed to save config: %w", err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	stored, err := svc.GetByPath(path)
	if err != nil {
		return fmt.Errorf("Failed to read back value: %w", err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	_, err = fmt.Fprintf(stdout, "Set %s = %s\n", path, config.TOMLInline(stored))
	return err
}

func configReset(dir, path string, stdout io.Writer) error {
	svc := config.Load(dir, config.StderrDiagnostics)
	cleared, err := svc.ResetByPath(path)
	if err != nil {
		return fmt.Errorf("cannot reset '%s': %w", path, err)
	}
	if !cleared {
		_, err = fmt.Fprintf(stdout, "No runtime override at %s\n", path)
		return err
	}
	if err := svc.Save(); err != nil {
		return fmt.Errorf("cannot save config: %w", err)
	}
	effective, err := svc.GetByPath(path)
	if err != nil {
		return fmt.Errorf("cannot read value: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "Reset %s (now using: %s)\n", path, config.TOMLInline(effective))
	return err
}

func configSchema(dir string, toStdout bool, stdout io.Writer) error {
	if toStdout {
		_, err := fmt.Fprintln(stdout, config.GenerateSchema())
		return err
	}
	if err := config.EnsureSchemaCurrent(dir); err != nil {
		return fmt.Errorf("Failed to write schema files: %w", err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	_, err := fmt.Fprintf(stdout, "Written:\n  %s\n  %s\n", config.SchemaPath(dir), config.TombiPath(dir))
	return err
}

func configDefault(dir string, toStdout bool, stdout io.Writer) error {
	content := config.DefaultTOML()
	if toStdout {
		_, err := fmt.Fprintln(stdout, content)
		return err
	}
	path := config.ExamplePath(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the user's config dir
		return fmt.Errorf("Failed to create config directory: %w", err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // a reference file, not secret
		return fmt.Errorf("Failed to write example config: %w", err) //nolint:staticcheck // the Rust CLI's capitalized message
	}
	_, err := fmt.Fprintf(stdout, "Written:\n  %s\n", path)
	return err
}

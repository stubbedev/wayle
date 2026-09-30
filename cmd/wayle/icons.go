package main

import "github.com/stubbedev/wayle/internal/cli"

// examplesHeader is style.rs's styled_header!("Examples:"): raw ANSI
// that the help writer strips when the stream is not colored.
const examplesHeader = "\x1b[1;33mExamples:\x1b[0m"

const iconsInstallHelp = examplesHeader + "\n" +
	"    wayle icons install tabler home settings bell\n" +
	"        -> tb-home-symbolic, tb-settings-symbolic, tb-bell-symbolic\n" +
	"\n" +
	"    wayle icons install simple-icons firefox spotify\n" +
	"        -> si-firefox-symbolic, si-spotify-symbolic\n" +
	"\n" +
	"Run 'wayle icons sources' to see all available icon sources.\n" +
	"Icons are saved to ~/.local/share/wayle/icons/ as GTK symbolic icons."

const iconsImportHelp = examplesHeader + "\n" +
	"    wayle icons import ~/Downloads/my-icon.svg my-icon\n" +
	"        -> cm-my-icon-symbolic\n" +
	"\n" +
	"    wayle icons import ~/exported-icons/\n" +
	"        -> Imports all SVGs, preserves names with known prefixes\n" +
	"\n" +
	"Icons without a known prefix (tb-, tbf-, si-, md-, ld-) get 'cm-' added."

const iconsSyncHelp = examplesHeader + "\n" +
	"    wayle icons sync\n" +
	"        -> install every config-referenced icon not yet on disk\n" +
	"\n" +
	"    wayle icons sync --dry-run\n" +
	"        -> list what would be installed; no downloads\n" +
	"\n" +
	"Useful when sharing dotfiles across machines. User-imported `cm-` icons\n" +
	"cannot be auto-installed and are listed as skipped."

// iconsCommand is wayle/src/cli/icons/commands.rs.
func iconsCommand() *cli.Command {
	return &cli.Command{
		Name:  "icons",
		About: "Icon management commands",
		Subcommands: []*cli.Command{
			{Name: "setup", About: "Install bundled icons required by Wayle components", Run: notPorted("icons setup")},
			{
				Name:          "install",
				About:         "Install icons from a CDN source",
				AfterLongHelp: iconsInstallHelp,
				Args: []*cli.Arg{
					{ID: "source", Required: true, Help: "Source name (run 'wayle icons sources' to see available sources)"},
					{ID: "slugs", Required: true, Multiple: true, Help: "Icon slugs to install (e.g., home settings bell)"},
				},
				Run: notPorted("icons install"),
			},
			{
				Name:          "import",
				About:         "Import local SVG file(s) as icons",
				AfterLongHelp: iconsImportHelp,
				Args: []*cli.Arg{
					{ID: "path", Required: true, Value: cli.Path, Help: "Path to SVG file or directory"},
					{ID: "name", Help: "Icon name (required for single file, ignored for directory)"},
				},
				Run: notPorted("icons import"),
			},
			{
				Name:  "remove",
				About: "Remove installed icons",
				Args: []*cli.Arg{
					{ID: "names", Required: true, Multiple: true, Help: "Icon names to remove (e.g., tb-home-symbolic si-firefox-symbolic)"},
				},
				Run: notPorted("icons remove"),
			},
			{Name: "sources", About: "List available icon sources", Run: notPorted("icons sources")},
			{
				Name:  "list",
				About: "List installed icons",
				Args: []*cli.Arg{
					{ID: "source", Short: 's', Long: "source", Value: cli.String, Help: "Filter by source prefix (e.g., tb, si, md)"},
					{ID: "interactive", Short: 'i', Long: "interactive", Help: "Interactive fuzzy search (requires fzf)"},
				},
				Run: notPorted("icons list"),
			},
			{Name: "open", About: "Open the icons directory in file manager", Run: notPorted("icons open")},
			{
				Name:  "export",
				About: "Export all installed icons to a directory",
				Args: []*cli.Arg{
					{ID: "destination", Required: true, Value: cli.Path, Help: "Destination directory for exported icons"},
				},
				Run: notPorted("icons export"),
			},
			{
				Name:          "sync",
				About:         "Install any icons referenced in config but not yet on disk",
				AfterLongHelp: iconsSyncHelp,
				Args: []*cli.Arg{
					{ID: "dry_run", Long: "dry-run", Help: "Preview what would be installed without making changes"},
				},
				Run: notPorted("icons sync"),
			},
		},
	}
}

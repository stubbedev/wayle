package main

import "github.com/stubbedev/wayle/internal/cli"

// portalCommand is wayle/src/cli/portal/commands.rs. The subcommand is
// optional: a bare `wayle portal` runs the backend, so the installed
// D-Bus/systemd units can keep execing it.
func portalCommand() *cli.Command {
	allowToken := &cli.Arg{ID: "allow_token", Long: "allow-token", Help: `Pre-check the "allow restore token" box`}
	return &cli.Command{
		Name:               "portal",
		About:              "xdg-desktop-portal backend, screencast picker stub, and dialog previewer",
		SubcommandOptional: true,
		Run:                runPortal,
		Subcommands: []*cli.Command{
			{
				Name:  "run",
				About: "Run the xdg-desktop-portal backend (the default when no subcommand is given, so the installed D-Bus/systemd units can keep execing `wayle portal`)",
				Run:   runPortal,
			},
			{
				Name:  "share-picker",
				About: "xdg-desktop-portal-hyprland screencast picker stub (invoked by the portal, not by hand)",
				Args:  []*cli.Arg{allowToken},
				Run:   runSharePicker,
			},
			{
				Name:  "show",
				About: "Preview a portal dialog UI without an application request (developer tool; talks to the running shell over D-Bus, same as the real backend)",
				Subcommands: []*cli.Command{
					{
						Name:  "file-chooser",
						About: "File open/save dialog (`org.freedesktop.impl.portal.FileChooser`)",
						Args: []*cli.Arg{
							{ID: "save", Long: "save", Help: "Show the save dialog instead of the open dialog"},
							{ID: "multiple", Long: "multiple", Help: "Allow selecting multiple files (open only)"},
							{ID: "directory", Long: "directory", Help: "Select a directory instead of files (open only)"},
						},
						Run: notPorted("portal show file-chooser"),
					},
					{
						Name:  "screenshot",
						About: "Screenshot capture (`org.freedesktop.impl.portal.Screenshot`)",
						Args: []*cli.Arg{
							{ID: "mode", Long: "mode", Value: cli.String, Defaults: []string{"region"}, Help: "Capture mode: `region`, `output`, `screen`, or `window`"},
							{ID: "target", Long: "target", Value: cli.String, Defaults: []string{""}, Help: "Output connector name (used by `output` mode)"},
						},
						Run: notPorted("portal show screenshot"),
					},
					{Name: "color", About: "Interactive color picker (`Screenshot.PickColor`)", Run: notPorted("portal show color")},
					{
						Name:  "print",
						About: "Printer selection dialog (`org.freedesktop.impl.portal.Print`); only the printer/settings prepare step is shown, no document is spooled",
						Run:   notPorted("portal show print"),
					},
					{
						Name:  "screen-cast",
						About: "Screencast source picker (`org.freedesktop.impl.portal.ScreenCast`)",
						Args: []*cli.Arg{
							allowToken,
							{ID: "multiple", Long: "multiple", Help: "Allow selecting multiple sources"},
						},
						Run: notPorted("portal show screen-cast"),
					},
					{Name: "access", About: "Generic grant/deny access prompt (`org.freedesktop.impl.portal.Access`)", Run: notPorted("portal show access")},
					{Name: "account", About: "Account info sharing consent (`org.freedesktop.impl.portal.Account`)", Run: notPorted("portal show account")},
					{Name: "app-chooser", About: "Application chooser (`org.freedesktop.impl.portal.AppChooser`)", Run: notPorted("portal show app-chooser")},
					{Name: "dynamic-launcher", About: "Dynamic launcher install confirmation (`org.freedesktop.impl.portal.DynamicLauncher`)", Run: notPorted("portal show dynamic-launcher")},
					{
						Name:  "wallpaper",
						About: "Wallpaper preview confirmation (`org.freedesktop.impl.portal.Wallpaper` `show-preview`)",
						Args: []*cli.Arg{
							{ID: "uri", Long: "uri", Value: cli.String, Defaults: []string{""}, Help: "`file://` image URI to preview (defaults to a placeholder)"},
						},
						Run: notPorted("portal show wallpaper"),
					},
				},
			},
		},
	}
}

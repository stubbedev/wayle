package main

import (
	"fmt"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/sni"
)

// systrayCommand is wayle/src/cli/systray/commands.rs.
func systrayCommand() *cli.Command {
	return &cli.Command{
		Name:  "systray",
		About: "System tray commands",
		Subcommands: []*cli.Command{
			{Name: "list", About: "List all system tray items", Run: withSystray(systrayList)},
			{
				Name:  "activate",
				About: "Activate a tray item by ID",
				Args: []*cli.Arg{
					{ID: "id", ValueName: "ID", Required: true, Help: "Tray item ID to activate"},
				},
				Run: withSystray(systrayActivate),
			},
			{Name: "status", About: "Show system tray status", Run: withSystray(systrayStatus)},
		},
	}
}

func withSystray(run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return withDaemon("System tray", sni.DaemonName, sni.DaemonPath, sni.DaemonName, run)
}

func systrayList(m *cli.Matches, p *daemonProxy) error {
	var items []sni.ListRow
	if err := p.call("list tray items", "List", []any{&items}); err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(m.Stdout(), "No system tray items")
		return nil
	}
	fmt.Fprintln(m.Stdout(), "System tray items:")
	for _, it := range items {
		title := ""
		if it.Title != "" && it.Title != it.ID {
			title = " \"" + it.Title + "\""
		}
		icon := ""
		if it.IconName != "" {
			icon = " (" + it.IconName + ")"
		}
		fmt.Fprintf(m.Stdout(), "  %s%s%s [%s]\n", it.ID, title, icon, it.Status)
	}
	return nil
}

func systrayActivate(m *cli.Matches, p *daemonProxy) error {
	id, _ := cli.Value[string](m, "id")
	if err := p.call("activate tray item", "Activate", nil, id); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Activated: %s\n", id)
	return nil
}

func systrayStatus(m *cli.Matches, p *daemonProxy) error {
	var count uint32
	var watcher bool
	if err := p.prop("get item count", "Count", &count); err != nil {
		return err
	}
	if err := p.prop("get watcher status", "IsWatcher", &watcher); err != nil {
		return err
	}
	status := "inactive"
	if watcher {
		status = "active"
	}
	fmt.Fprintf(m.Stdout(), "Tray items: %d\n", count)
	fmt.Fprintf(m.Stdout(), "StatusNotifierWatcher: %s\n", status)
	return nil
}

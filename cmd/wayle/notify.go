package main

import (
	"fmt"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/notifications"
)

// notifyCommand is wayle/src/cli/notify/commands.rs.
func notifyCommand() *cli.Command {
	return &cli.Command{
		Name:  "notify",
		About: "Notification control commands",
		Subcommands: []*cli.Command{
			{Name: "list", About: "List all notifications", Run: withNotify(notifyList)},
			{
				Name:  "dismiss",
				About: "Dismiss a notification by ID",
				Args: []*cli.Arg{
					{ID: "id", ValueName: "ID", Required: true, Value: cli.U32, Help: "Notification ID to dismiss"},
				},
				Run: withNotify(notifyDismiss),
			},
			{Name: "dismiss-all", About: "Dismiss all notifications", Run: withNotify(notifyDismissAll)},
			{Name: "dnd", About: "Toggle Do Not Disturb mode", Run: withNotify(notifyDND)},
			{Name: "status", About: "Show notification status", Run: withNotify(notifyStatus)},
		},
	}
}

func withNotify(run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return withDaemon("Notification", notifications.WayleName, notifications.WaylePath, notifications.WayleName, run)
}

func notifyList(m *cli.Matches, p *daemonProxy) error {
	var rows []notifications.ListRow
	if err := p.call("list notifications", "List", []any{&rows}); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(m.Stdout(), "No notifications")
		return nil
	}
	fmt.Fprintln(m.Stdout(), "Notifications:")
	for _, r := range rows {
		fmt.Fprintf(m.Stdout(), "  [%d] %s: %s\n", r.ID, r.App, r.Summary)
		if r.Body != "" {
			fmt.Fprintf(m.Stdout(), "      %s\n", r.Body)
		}
	}
	return nil
}

func notifyDismiss(m *cli.Matches, p *daemonProxy) error {
	id, _ := cli.Value[uint32](m, "id")
	if err := p.call("dismiss notification", "Dismiss", nil, id); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Dismissed notification %d\n", id)
	return nil
}

func notifyDismissAll(m *cli.Matches, p *daemonProxy) error {
	if err := p.call("dismiss notifications", "DismissAll", nil); err != nil {
		return err
	}
	fmt.Fprintln(m.Stdout(), "Dismissed all notifications")
	return nil
}

func notifyDND(m *cli.Matches, p *daemonProxy) error {
	if err := p.call("toggle DND", "ToggleDnd", nil); err != nil {
		return err
	}
	var dnd bool
	if err := p.prop("get DND state", "Dnd", &dnd); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Do Not Disturb: %s\n", enabledWord(dnd))
	return nil
}

func notifyStatus(m *cli.Matches, p *daemonProxy) error {
	var count, popups, duration uint32
	var dnd bool
	if err := p.prop("get notification count", "Count", &count); err != nil {
		return err
	}
	if err := p.prop("get popup count", "PopupCount", &popups); err != nil {
		return err
	}
	if err := p.prop("get DND state", "Dnd", &dnd); err != nil {
		return err
	}
	if err := p.prop("get popup duration", "PopupDuration", &duration); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Notifications: %d\n", count)
	fmt.Fprintf(m.Stdout(), "Active popups: %d\n", popups)
	fmt.Fprintf(m.Stdout(), "Do Not Disturb: %s\n", enabledWord(dnd))
	fmt.Fprintf(m.Stdout(), "Popup duration: %dms\n", duration)
	return nil
}

func enabledWord(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

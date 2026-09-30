package main

import (
	"context"
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

func withNotify(run func(context.Context, *cli.Matches, *notifications.Client) error) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		client, err := notifications.Connect()
		if err != nil {
			return err
		}
		defer func() { _ = client.Close() }()
		return run(context.Background(), m, client)
	}
}

func notifyList(ctx context.Context, m *cli.Matches, client *notifications.Client) error {
	rows, err := client.List(ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(m.Stdout(), "No notifications")
		return nil
	}
	for _, r := range rows {
		fmt.Fprintf(m.Stdout(), "%d %s: %s\n", r.ID, r.App, r.Summary)
		if r.Body != "" {
			fmt.Fprintf(m.Stdout(), "    %s\n", r.Body)
		}
	}
	return nil
}

func notifyDismiss(ctx context.Context, m *cli.Matches, client *notifications.Client) error {
	id, _ := cli.Value[uint32](m, "id")
	return client.Dismiss(ctx, id)
}

func notifyDismissAll(ctx context.Context, _ *cli.Matches, client *notifications.Client) error {
	return client.DismissAll(ctx)
}

func notifyDND(ctx context.Context, m *cli.Matches, client *notifications.Client) error {
	on, err := client.ToggleDND(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Do Not Disturb: %s\n", enabledWord(on))
	return nil
}

func notifyStatus(ctx context.Context, m *cli.Matches, client *notifications.Client) error {
	count, popups, dnd, err := client.Status(ctx)
	if err != nil {
		return err
	}
	dndState := "off"
	if dnd {
		dndState = "on"
	}
	fmt.Fprintf(m.Stdout(), "Notifications: %d\n", count)
	fmt.Fprintf(m.Stdout(), "Active popups: %d\n", popups)
	fmt.Fprintf(m.Stdout(), "Do Not Disturb: %s\n", dndState)
	return nil
}

func enabledWord(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

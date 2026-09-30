package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/idleinhibit"
)

// idleCommand is wayle/src/cli/idle/commands.rs.
func idleCommand() *cli.Command {
	adjust := []*cli.Arg{{ID: "value", ValueName: "VALUE", Required: true, Help: "+N to add, -N to subtract, N to set absolute"}}
	return &cli.Command{
		Name:  "idle",
		About: "Idle inhibit control commands",
		Subcommands: []*cli.Command{
			{
				Name:  "on",
				About: "Enable idle inhibition",
				Args: []*cli.Arg{
					{ID: "minutes", ValueName: "MINUTES", Value: cli.U32, Help: "Duration in minutes (omit to use default duration)"},
					{ID: "indefinite", Short: 'i', Long: "indefinite", Help: "Force indefinite mode (ignore default duration)"},
				},
				Run: withIdle(idleOn),
			},
			{Name: "off", About: "Disable idle inhibition", Run: withIdle(idleOff)},
			{Name: "duration", About: "Adjust timer duration (upper limit)", AllowHyphenValues: true, Args: adjust, Run: withIdle(idleDuration)},
			{Name: "remaining", About: "Adjust remaining time on active timer", AllowHyphenValues: true, Args: adjust, Run: withIdle(idleRemaining)},
			{Name: "status", About: "Show current idle inhibit status", Run: withIdle(idleStatus)},
			{
				Name:  "toggle",
				About: "Toggle idle inhibition on/off",
				Args: []*cli.Arg{
					{ID: "indefinite", Short: 'i', Long: "indefinite", Help: "Use indefinite mode when enabling"},
				},
				Run: withIdle(idleToggle),
			},
		},
	}
}

func withIdle(run func(context.Context, *cli.Matches, *idleinhibit.DBus) error) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		client, err := idleinhibit.Connect()
		if err != nil {
			return err
		}
		defer func() { _ = client.Close() }()
		return run(context.Background(), m, client)
	}
}

func idleOn(ctx context.Context, m *cli.Matches, client *idleinhibit.DBus) error {
	if minutes, ok := cli.Value[uint32](m, "minutes"); ok {
		if err := client.SetDuration(ctx, minutes); err != nil {
			return err
		}
	}
	return client.Enable(ctx, m.Flag("indefinite"))
}

func idleOff(ctx context.Context, _ *cli.Matches, client *idleinhibit.DBus) error {
	return client.Disable(ctx)
}

func idleDuration(ctx context.Context, m *cli.Matches, client *idleinhibit.DBus) error {
	value, _ := cli.Value[string](m, "value")
	minutes, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("idle duration: %w", err)
	}
	return client.SetDuration(ctx, uint32(minutes))
}

func idleToggle(ctx context.Context, m *cli.Matches, client *idleinhibit.DBus) error {
	indefinite := m.Flag("indefinite")
	snap, err := client.Status(ctx)
	if err != nil {
		return err
	}
	if snap.Active {
		if err := client.Disable(ctx); err != nil {
			return err
		}
		fmt.Fprintln(m.Stdout(), "Disabled")
		return nil
	}
	if err := client.Enable(ctx, indefinite); err != nil {
		return err
	}
	if indefinite {
		fmt.Fprintln(m.Stdout(), "Enabled (indefinite)")
		return nil
	}
	fmt.Fprintf(m.Stdout(), "Enabled for %d minutes\n", snap.DurationMins)
	return nil
}

func idleRemaining(ctx context.Context, m *cli.Matches, client *idleinhibit.DBus) error {
	value, _ := cli.Value[string](m, "value")
	if value == "" {
		return errors.New("idle remaining needs minutes (±m)")
	}
	if value[0] == '+' || value[0] == '-' {
		delta, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return fmt.Errorf("idle remaining: %w", err)
		}
		if err := client.AdjustRemaining(ctx, int32(delta)); err != nil {
			return err
		}
		if delta >= 0 {
			fmt.Fprintf(m.Stdout(), "Added %d minutes to remaining\n", delta)
		} else {
			fmt.Fprintf(m.Stdout(), "Subtracted %d minutes from remaining\n", -delta)
		}
		return nil
	}
	minutes, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("idle remaining: %w", err)
	}
	if err := client.SetRemaining(ctx, uint32(minutes)); err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Set remaining to %d minutes\n", minutes)
	return nil
}

func idleStatus(ctx context.Context, m *cli.Matches, client *idleinhibit.DBus) error {
	snap, err := client.Status(ctx)
	if err != nil {
		return err
	}
	duration := "indefinite"
	if snap.DurationMins != 0 {
		duration = strconv.FormatUint(uint64(snap.DurationMins), 10) + " min"
	}
	if !snap.Active {
		fmt.Fprintf(m.Stdout(), "Inactive (duration: %s)\n", duration)
		return nil
	}
	if snap.DurationMins == 0 {
		fmt.Fprintln(m.Stdout(), "Active (indefinite)")
		return nil
	}
	fmt.Fprintf(m.Stdout(), "Active (%d:%02d remaining, %s duration)\n", snap.RemainingS/60, snap.RemainingS%60, duration)
	return nil
}

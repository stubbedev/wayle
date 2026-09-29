// Command wayle is the Go rewrite's entry point. Subcommands port
// over one by one; anything not ported yet exits with an error naming
// it instead of silently doing nothing.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/shell/bar"
)

const usage = `wayle (Go rewrite)

Usage:
  wayle shell        run the shell (bar only for now)
  wayle idle <cmd>   idle inhibition: on|off|toggle|duration|remaining|status

Not ported yet: audio, config, icons, launch, lock, media, notify,
panel, power, recorder, screenshot, systray, toast, wallpaper, widget.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "shell":
		err = bar.Run()
	case "idle":
		err = runIdle(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = fmt.Errorf("%q is not ported to the Go shell yet", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wayle:", err)
		os.Exit(1)
	}
}

// runIdle drives the shell's idle-inhibit daemon the way wayle/src/cli/idle
// does: on|off|toggle [--indefinite], duration <m>, remaining <±m>, status.
func runIdle(args []string) error {
	indefinite := false
	var positional []string
	for _, arg := range args {
		switch arg {
		case "--indefinite", "-i":
			indefinite = true
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) == 0 {
		return errors.New("idle needs a command: on|off|toggle|duration|remaining|status")
	}
	client, err := idleinhibit.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	switch positional[0] {
	case "on":
		err = client.Enable(ctx, indefinite)
	case "off":
		err = client.Disable(ctx)
	case "toggle":
		err = runIdleToggle(ctx, client, indefinite)
	case "duration":
		minutes, parseErr := strconv.ParseUint(arg(positional), 10, 32)
		if parseErr != nil {
			return fmt.Errorf("idle duration: %w", parseErr)
		}
		err = client.SetDuration(ctx, uint32(minutes))
	case "remaining":
		err = runIdleRemaining(ctx, client, arg(positional))
	case "status":
		err = printIdleStatus(ctx, client)
	default:
		err = fmt.Errorf("unknown idle command %q", positional[0])
	}
	return err
}

// arg returns positional[1] or "" when absent.
func arg(positional []string) string {
	if len(positional) > 1 {
		return positional[1]
	}
	return ""
}

func runIdleToggle(ctx context.Context, client *idleinhibit.DBus, indefinite bool) error {
	snap, err := client.Status(ctx)
	if err != nil {
		return err
	}
	if snap.Active {
		if err := client.Disable(ctx); err != nil {
			return err
		}
		fmt.Println("Disabled")
		return nil
	}
	if err := client.Enable(ctx, indefinite); err != nil {
		return err
	}
	if indefinite {
		fmt.Println("Enabled (indefinite)")
		return nil
	}
	fmt.Printf("Enabled for %d minutes\n", snap.DurationMins)
	return nil
}

func runIdleRemaining(ctx context.Context, client *idleinhibit.DBus, value string) error {
	if value == "" {
		return errors.New("idle remaining needs minutes (±m)")
	}
	if value[0] == '+' || value[0] == '-' {
		delta, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return fmt.Errorf("idle remaining: %w", err)
		}
		err = client.AdjustRemaining(ctx, int32(delta))
		if err == nil {
			if delta >= 0 {
				fmt.Printf("Added %d minutes to remaining\n", delta)
			} else {
				fmt.Printf("Subtracted %d minutes from remaining\n", -delta)
			}
		}
		return err
	}
	minutes, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return fmt.Errorf("idle remaining: %w", err)
	}
	if err := client.SetRemaining(ctx, uint32(minutes)); err != nil {
		return err
	}
	fmt.Printf("Set remaining to %d minutes\n", minutes)
	return nil
}

func printIdleStatus(ctx context.Context, client *idleinhibit.DBus) error {
	snap, err := client.Status(ctx)
	if err != nil {
		return err
	}
	duration := "indefinite"
	if snap.DurationMins != 0 {
		duration = strconv.FormatUint(uint64(snap.DurationMins), 10) + " min"
	}
	if !snap.Active {
		fmt.Printf("Inactive (duration: %s)\n", duration)
		return nil
	}
	if snap.DurationMins == 0 {
		fmt.Println("Active (indefinite)")
		return nil
	}
	remainingMins := snap.RemainingS / 60
	remainingSecs := snap.RemainingS % 60
	fmt.Printf("Active (%d:%02d remaining, %s duration)\n", remainingMins, remainingSecs, duration)
	return nil
}

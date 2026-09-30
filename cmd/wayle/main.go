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
	"time"

	"github.com/stubbedev/wayle/internal/widgetipc"
	"github.com/stubbedev/wayle/service/idleinhibit"
	"github.com/stubbedev/wayle/service/notifications"
	"github.com/stubbedev/wayle/service/recorder"
	"github.com/stubbedev/wayle/shell/bar"
)

const usage = `wayle (Go rewrite)

Usage:
  wayle shell        run the shell (bar only for now)
  wayle idle <cmd>   idle inhibition: on|off|toggle|duration|remaining|status
  wayle notify <cmd> notifications: list|dismiss|dismiss-all|dnd|status
  wayle recorder <cmd> recording: toggle|start|stop|pause|resume|status
  wayle vpn sso-callback <uri>  hand a browser sign-in back to the shell
  wayle screenshot <cmd> capture: region|output [NAME]|window
  wayle toast [flags] custom toast: --label --icon --percentage
                     --duration --preset --class
  wayle wallpaper <cmd> wallpapers: set|cycle|stop|next|previous|info|
                     theming-monitor

Not ported yet: audio, config, icons, launcher, lock, media, panel,
power, systray, widget.
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
	case "notify":
		err = runNotify(os.Args[2:])
	case "recorder":
		err = runRecorder(os.Args[2:])
	case "vpn":
		err = runVPN(os.Args[2:], os.Stdout)
	case "screenshot":
		err = runScreenshot(os.Args[2:], os.Stdout)
	case "toast":
		err = runToast(os.Args[2:])
	case "wallpaper":
		err = runWallpaper(os.Args[2:])
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

// runToast sends one custom toast (wayle/src/cli/toast.rs). Either
// --label or --preset must be given.
func runToast(args []string) error {
	var req widgetipc.ToastRequest
	for i := 0; i < len(args); i++ {
		value := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--label", "-l":
			v := value()
			req.Label = &v
		case "--icon":
			v := value()
			req.Icon = &v
		case "--percentage":
			pct, err := strconv.ParseFloat(value(), 64)
			if err != nil {
				return fmt.Errorf("toast percentage: %w", err)
			}
			req.Percentage = &pct
		case "--duration":
			ms, err := strconv.ParseUint(value(), 10, 32)
			if err != nil {
				return fmt.Errorf("toast duration: %w", err)
			}
			d := uint32(ms)
			req.DurationMS = &d
		case "--preset":
			v := value()
			req.Preset = &v
		case "--class":
			v := value()
			req.Class = &v
		default:
			return fmt.Errorf("unknown toast flag %q", args[i])
		}
	}
	if req.Label == nil && req.Preset == nil {
		return errors.New("a toast needs a label or --preset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return widgetipc.SendToast(ctx, req)
}

// runRecorder drives the shell's recorder daemon the way
// wayle/src/cli/recorder does.
func runRecorder(args []string) error {
	if len(args) == 0 {
		return errors.New("recorder needs a command: toggle|start|stop|pause|resume|status")
	}
	client, err := recorder.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	switch args[0] {
	case "toggle":
		status, err := client.Toggle(ctx)
		if err == nil {
			fmt.Printf("Recording: %s\n", status)
		}
		return err
	case "start":
		status, err := client.Start(ctx)
		if err == nil {
			fmt.Printf("Recording: %s\n", status)
		}
		return err
	case "stop":
		status, err := client.Stop(ctx)
		if err == nil {
			fmt.Printf("Recording: %s\n", status)
		}
		return err
	case "pause":
		_, err := client.SetPaused(ctx, true)
		return err
	case "resume":
		_, err := client.SetPaused(ctx, false)
		return err
	case "status":
		snap, err := client.Status(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Recording: %s\n", snap.Status)
		fmt.Printf("Elapsed: %s\n", recorder.FormatElapsed(snap.ElapsedSecs))
		if snap.OutputPath != "" {
			fmt.Printf("Output: %s\n", snap.OutputPath)
		}
		return nil
	default:
		return fmt.Errorf("unknown recorder command %q", args[0])
	}
}

// runNotify drives the shell's notification daemon the way
// wayle/src/cli/notify does: list|dismiss <id>|dismiss-all|dnd|status.
func runNotify(args []string) error {
	if len(args) == 0 {
		return errors.New("notify needs a command: list|dismiss|dismiss-all|dnd|status")
	}
	client, err := notifications.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	switch args[0] {
	case "list":
		rows, err := client.List(ctx)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			fmt.Println("No notifications")
			return nil
		}
		for _, r := range rows {
			fmt.Printf("%d %s: %s\n", r.ID, r.App, r.Summary)
			if r.Body != "" {
				fmt.Printf("    %s\n", r.Body)
			}
		}
		return nil
	case "dismiss":
		if len(args) < 2 {
			return errors.New("dismiss needs a notification id")
		}
		id, err := strconv.ParseUint(args[1], 10, 32)
		if err != nil {
			return fmt.Errorf("dismiss: %w", err)
		}
		return client.Dismiss(ctx, uint32(id))
	case "dismiss-all":
		return client.DismissAll(ctx)
	case "dnd":
		on, err := client.ToggleDND(ctx)
		if err != nil {
			return err
		}
		state := "disabled"
		if on {
			state = "enabled"
		}
		fmt.Printf("Do Not Disturb: %s\n", state)
		return nil
	case "status":
		count, popups, dnd, err := client.Status(ctx)
		if err != nil {
			return err
		}
		dndState := "off"
		if dnd {
			dndState = "on"
		}
		fmt.Printf("Notifications: %d\n", count)
		fmt.Printf("Active popups: %d\n", popups)
		fmt.Printf("Do Not Disturb: %s\n", dndState)
		return nil
	default:
		return fmt.Errorf("unknown notify command %q", args[0])
	}
}

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

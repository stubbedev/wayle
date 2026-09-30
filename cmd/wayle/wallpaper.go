package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/dbuscli"
	"github.com/stubbedev/wayle/service/wallpaper"
)

// wallpaperService names the service in "not running" errors.
const wallpaperService = "Wallpaper"

// wallpaperFitArgs are the --fit values the CLI accepts (FitModeArg).
// "tile" passes the CLI and is refused by the daemon, as in Rust.
var wallpaperFitArgs = []string{"fill", "fit", "center", "tile", "stretch"}

// wallpaperModeArgs are the --mode values (CyclingModeArg).
var wallpaperModeArgs = []string{"sequential", "shuffle"}

// runWallpaper drives the shell's wallpaper daemon the way
// wayle/src/cli/wallpaper does: set|cycle|stop|next|previous|info|
// theming-monitor.
func runWallpaper(args []string) error {
	return wallpaperCommand(context.Background(), args, os.Stdout)
}

// wallpaperFlags splits args into positionals and --flag values,
// accepting "--flag value", "--flag=value", and the short aliases.
func wallpaperFlags(args []string, aliases map[string]string) (positional []string, flags map[string]string, err error) {
	flags = map[string]string{}
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name, value, inline := strings.Cut(arg, "=")
		long, ok := aliases[name]
		if !ok {
			return nil, nil, fmt.Errorf("unexpected argument '%s' found", arg)
		}
		if !inline {
			if len(args) == 0 {
				return nil, nil, fmt.Errorf("a value is required for '%s' but none was supplied", long)
			}
			value, args = args[0], args[1:]
		}
		flags[long] = value
	}
	return positional, flags, nil
}

func oneOf(flag, value string, valid []string) error {
	if slices.Contains(valid, value) {
		return nil
	}
	return fmt.Errorf("invalid value '%s' for '%s' [possible values: %s]", value, flag, strings.Join(valid, ", "))
}

func wallpaperCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("wallpaper needs a command: set|cycle|stop|next|previous|info|theming-monitor")
	}
	cmd, rest := args[0], args[1:]
	var aliases map[string]string
	switch cmd {
	case "set":
		aliases = map[string]string{"-f": "--fit", "--fit": "--fit", "--monitor": "--monitor"}
	case "cycle":
		aliases = map[string]string{"-i": "--interval", "--interval": "--interval", "-m": "--mode", "--mode": "--mode"}
	case "info":
		aliases = map[string]string{"--monitor": "--monitor"}
	case "stop", "next", "previous", "theming-monitor":
		aliases = map[string]string{}
	default:
		return fmt.Errorf("unknown wallpaper command %q", cmd)
	}
	positional, flags, err := wallpaperFlags(rest, aliases)
	if err != nil {
		return err
	}
	switch cmd {
	case "set", "cycle", "theming-monitor":
		if len(positional) != 1 {
			return fmt.Errorf("wallpaper %s needs exactly one argument", cmd)
		}
	default:
		if len(positional) != 0 {
			return fmt.Errorf("unexpected argument '%s' found", positional[0])
		}
	}
	// Validate before dialing, as clap parses before the handler runs.
	interval := uint32(300)
	mode := "sequential"
	if fit, ok := flags["--fit"]; ok {
		if err := oneOf("--fit <FIT>", fit, wallpaperFitArgs); err != nil {
			return err
		}
	}
	if v, ok := flags["--interval"]; ok {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid value '%s' for '--interval <INTERVAL>': %w", v, err)
		}
		interval = uint32(n)
	}
	if v, ok := flags["--mode"]; ok {
		if err := oneOf("--mode <MODE>", v, wallpaperModeArgs); err != nil {
			return err
		}
		mode = v
	}

	client, err := wallpaper.Connect()
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	call := func(op string, err error) error {
		if err != nil {
			return dbuscli.FormatError(wallpaperService, op, err)
		}
		return nil
	}
	monitor := flags["--monitor"]
	_, hasMonitor := flags["--monitor"]

	switch cmd {
	case "set":
		path := positional[0]
		if fit, ok := flags["--fit"]; ok {
			if err := call("set fit mode", client.SetFitMode(ctx, fit, monitor)); err != nil {
				return err
			}
		}
		if err := call("set wallpaper", client.SetWallpaper(ctx, path, monitor)); err != nil {
			return err
		}
		if hasMonitor {
			fmt.Fprintf(out, "Wallpaper set to %s on %s\n", path, monitor)
		} else {
			fmt.Fprintf(out, "Wallpaper set to %s\n", path)
		}
	case "cycle":
		dir := positional[0]
		if err := call("start cycling", client.StartCycling(ctx, dir, interval, mode)); err != nil {
			return err
		}
		fmt.Fprintf(out, "Started cycling wallpapers from %s every %d seconds\n", dir, interval)
	case "stop":
		if err := call("stop cycling", client.StopCycling(ctx)); err != nil {
			return err
		}
		fmt.Fprintln(out, "Wallpaper cycling stopped")
	case "next":
		if err := call("advance wallpaper", client.Next(ctx)); err != nil {
			return err
		}
		fmt.Fprintln(out, "Advanced to next wallpaper")
	case "previous":
		if err := call("go to previous wallpaper", client.Previous(ctx)); err != nil {
			return err
		}
		fmt.Fprintln(out, "Went back to previous wallpaper")
	case "info":
		return wallpaperInfo(ctx, client, monitor, hasMonitor, out)
	case "theming-monitor":
		m := positional[0]
		if err := call("set theming monitor", client.SetThemingMonitor(ctx, m)); err != nil {
			return err
		}
		if m == "" {
			fmt.Fprintln(out, "Theming monitor: default")
		} else {
			fmt.Fprintf(out, "Theming monitor: %s\n", m)
		}
	}
	return nil
}

// wallpaperInfo prints info.rs's block. Without --monitor it queries
// the "" monitor, which the daemon answers with no wallpaper and the
// default fit - the Rust CLI's behavior.
func wallpaperInfo(ctx context.Context, client *wallpaper.Client, monitor string, hasMonitor bool, out io.Writer) error {
	path, err := client.WallpaperForMonitor(ctx, monitor)
	if err != nil {
		return dbuscli.FormatError(wallpaperService, "get wallpaper", err)
	}
	fit, err := client.GetFitMode(ctx, monitor)
	if err != nil {
		return dbuscli.FormatError(wallpaperService, "get fit mode", err)
	}
	cycling, err := client.GetIsCycling(ctx)
	if err != nil {
		return dbuscli.FormatError(wallpaperService, "get cycling state", err)
	}
	if hasMonitor {
		fmt.Fprintf(out, "Wallpaper Information (%s)\n-----------------------------\n", monitor)
	} else {
		fmt.Fprint(out, "Wallpaper Information\n---------------------\n")
	}
	if path == "" {
		fmt.Fprintln(out, "Current:    (none)")
	} else {
		fmt.Fprintf(out, "Current:    %s\n", path)
	}
	fmt.Fprintf(out, "Fit Mode:   %s\n", fit)
	state := "inactive"
	if cycling {
		state = "active"
	}
	fmt.Fprintf(out, "Cycling:    %s\n", state)
	return nil
}

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/wallpaper"
)

// wallpaperService names the service in "not running" errors.
const wallpaperService = "Wallpaper"

// wallpaperCommand is wayle/src/cli/wallpaper/commands.rs. The fit and
// mode values are validated by the parser before the daemon is dialed;
// "tile" passes the CLI and is refused by the daemon, as in Rust.
func wallpaperCommand() *cli.Command {
	return &cli.Command{
		Name:  "wallpaper",
		About: "Wallpaper control commands",
		Subcommands: []*cli.Command{
			{
				Name:  "set",
				About: "Set wallpaper from an image file",
				Args: []*cli.Arg{
					{ID: "path", Required: true, Value: cli.Path, Help: "Path to wallpaper image"},
					{ID: "fit", Short: 'f', Long: "fit", Help: "Image fit mode", Value: cli.Enum(
						cli.PossibleValue{Name: "fill", Help: "Scale to cover entire display"},
						cli.PossibleValue{Name: "fit", Help: "Scale to fit within display"},
						cli.PossibleValue{Name: "center", Help: "Display at original size, centered"},
						cli.PossibleValue{Name: "tile", Help: "Tile the image"},
						cli.PossibleValue{Name: "stretch", Help: "Stretch to fill"},
					)},
					{ID: "monitor", Long: "monitor", Value: cli.String, Help: "Target monitor (e.g., DP-1, HDMI-A-1). If omitted, applies to all monitors"},
				},
				Run: withWallpaper(wallpaperSet),
			},
			{
				Name:  "cycle",
				About: "Start cycling wallpapers from a directory",
				Args: []*cli.Arg{
					{ID: "directory", Required: true, Value: cli.Path, Help: "Directory containing wallpaper images"},
					{ID: "interval", Short: 'i', Long: "interval", Value: cli.U32, Defaults: []string{"300"}, Help: "Interval in seconds between changes"},
					{ID: "mode", Short: 'm', Long: "mode", Defaults: []string{"sequential"}, Help: "Cycling mode", Value: cli.Enum(
						cli.PossibleValue{Name: "sequential", Help: "Cycle in alphabetical order"},
						cli.PossibleValue{Name: "shuffle", Help: "Cycle in random order"},
					)},
				},
				Run: withWallpaper(wallpaperCycle),
			},
			{Name: "stop", About: "Stop wallpaper cycling", Run: withWallpaper(wallpaperStop)},
			{Name: "next", About: "Skip to next wallpaper", Run: withWallpaper(wallpaperNext)},
			{Name: "previous", About: "Go back to previous wallpaper", Run: withWallpaper(wallpaperPrevious)},
			{
				Name:  "info",
				About: "Display current wallpaper information",
				Args: []*cli.Arg{
					{ID: "monitor", Long: "monitor", Value: cli.String, Help: "Target monitor (e.g., DP-1, HDMI-A-1). If omitted, shows global state"},
				},
				Run: withWallpaper(wallpaperInfo),
			},
			{
				Name:  "theming-monitor",
				About: "Set which monitor to use for color extraction",
				Args: []*cli.Arg{
					{ID: "monitor", Required: true, Help: "Monitor connector name (e.g., DP-1). Use empty string for default"},
				},
				Run: withWallpaper(wallpaperThemingMonitor),
			},
		},
	}
}

// withWallpaper dials the daemon around a handler.
func withWallpaper(run func(context.Context, *cli.Matches, *wallpaper.Client, io.Writer) error) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		client, err := wallpaper.Connect()
		if err != nil {
			return err
		}
		defer func() { _ = client.Close() }()
		return run(context.Background(), m, client, m.Stdout())
	}
}

func wallpaperCall(op string, err error) error {
	if err != nil {
		return dbusError(wallpaperService, op, err, false)
	}
	return nil
}

func wallpaperSet(ctx context.Context, m *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	path, _ := cli.Value[string](m, "path")
	monitor, hasMonitor := cli.Value[string](m, "monitor")
	if fit, ok := cli.Value[string](m, "fit"); ok {
		if err := wallpaperCall("set fit mode", client.SetFitMode(ctx, fit, monitor)); err != nil {
			return err
		}
	}
	if err := wallpaperCall("set wallpaper", client.SetWallpaper(ctx, path, monitor)); err != nil {
		return err
	}
	if hasMonitor {
		fmt.Fprintf(out, "Wallpaper set to %s on %s\n", path, monitor)
	} else {
		fmt.Fprintf(out, "Wallpaper set to %s\n", path)
	}
	return nil
}

func wallpaperCycle(ctx context.Context, m *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	dir, _ := cli.Value[string](m, "directory")
	interval, _ := cli.Value[uint32](m, "interval")
	mode, _ := cli.Value[string](m, "mode")
	if err := wallpaperCall("start cycling", client.StartCycling(ctx, dir, interval, mode)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Started cycling wallpapers from %s every %d seconds\n", dir, interval)
	return nil
}

func wallpaperStop(ctx context.Context, _ *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	if err := wallpaperCall("stop cycling", client.StopCycling(ctx)); err != nil {
		return err
	}
	fmt.Fprintln(out, "Wallpaper cycling stopped")
	return nil
}

func wallpaperNext(ctx context.Context, _ *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	if err := wallpaperCall("advance wallpaper", client.Next(ctx)); err != nil {
		return err
	}
	fmt.Fprintln(out, "Advanced to next wallpaper")
	return nil
}

func wallpaperPrevious(ctx context.Context, _ *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	if err := wallpaperCall("go to previous wallpaper", client.Previous(ctx)); err != nil {
		return err
	}
	fmt.Fprintln(out, "Went back to previous wallpaper")
	return nil
}

func wallpaperThemingMonitor(ctx context.Context, m *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	monitor, _ := cli.Value[string](m, "monitor")
	if err := wallpaperCall("set theming monitor", client.SetThemingMonitor(ctx, monitor)); err != nil {
		return err
	}
	if monitor == "" {
		fmt.Fprintln(out, "Theming monitor: default")
	} else {
		fmt.Fprintf(out, "Theming monitor: %s\n", monitor)
	}
	return nil
}

// wallpaperInfo prints info.rs's block. Without --monitor it queries
// the "" monitor, which the daemon answers with no wallpaper and the
// default fit - the Rust CLI's behavior.
func wallpaperInfo(ctx context.Context, m *cli.Matches, client *wallpaper.Client, out io.Writer) error {
	monitor, hasMonitor := cli.Value[string](m, "monitor")
	path, err := client.WallpaperForMonitor(ctx, monitor)
	if err != nil {
		return dbusError(wallpaperService, "get wallpaper", err, false)
	}
	fit, err := client.GetFitMode(ctx, monitor)
	if err != nil {
		return dbusError(wallpaperService, "get fit mode", err, false)
	}
	cycling, err := client.GetIsCycling(ctx)
	if err != nil {
		return dbusError(wallpaperService, "get cycling state", err, false)
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

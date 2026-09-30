package main

import (
	"context"
	"fmt"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/service/recorder"
)

// recorderCommand is wayle/src/cli/recorder/commands.rs.
func recorderCommand() *cli.Command {
	return &cli.Command{
		Name:  "recorder",
		About: "Screen recorder control commands",
		Subcommands: []*cli.Command{
			{Name: "start", About: "Start recording", Run: withRecorder(recorderStart)},
			{Name: "stop", About: "Stop recording", Run: withRecorder(recorderStop)},
			{Name: "toggle", About: "Toggle recording on/off", Run: withRecorder(recorderToggle)},
			{Name: "pause", About: "Pause the active recording", Run: withRecorder(recorderPause)},
			{Name: "resume", About: "Resume a paused recording", Run: withRecorder(recorderResume)},
			{Name: "status", About: "Show current recorder status", Run: withRecorder(recorderStatus)},
		},
	}
}

func withRecorder(run func(context.Context, *cli.Matches, *recorder.Client) error) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		client, err := recorder.Connect()
		if err != nil {
			return err
		}
		defer func() { _ = client.Close() }()
		return run(context.Background(), m, client)
	}
}

func printRecording(m *cli.Matches, status string, err error) error {
	if err == nil {
		fmt.Fprintf(m.Stdout(), "Recording: %s\n", status)
	}
	return err
}

func recorderStart(ctx context.Context, m *cli.Matches, c *recorder.Client) error {
	status, err := c.Start(ctx)
	return printRecording(m, status, err)
}

func recorderStop(ctx context.Context, m *cli.Matches, c *recorder.Client) error {
	status, err := c.Stop(ctx)
	return printRecording(m, status, err)
}

func recorderToggle(ctx context.Context, m *cli.Matches, c *recorder.Client) error {
	status, err := c.Toggle(ctx)
	return printRecording(m, status, err)
}

func recorderPause(ctx context.Context, _ *cli.Matches, c *recorder.Client) error {
	_, err := c.SetPaused(ctx, true)
	return err
}

func recorderResume(ctx context.Context, _ *cli.Matches, c *recorder.Client) error {
	_, err := c.SetPaused(ctx, false)
	return err
}

func recorderStatus(ctx context.Context, m *cli.Matches, c *recorder.Client) error {
	snap, err := c.Status(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.Stdout(), "Recording: %s\n", snap.Status)
	fmt.Fprintf(m.Stdout(), "Elapsed: %s\n", recorder.FormatElapsed(snap.ElapsedSecs))
	if snap.OutputPath != "" {
		fmt.Fprintf(m.Stdout(), "Output: %s\n", snap.OutputPath)
	}
	return nil
}

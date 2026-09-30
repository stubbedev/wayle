package main

import (
	"context"
	"errors"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/widgetipc"
)

// toastCommand is app.rs's Toast.
func toastCommand() *cli.Command {
	return &cli.Command{
		Name:  "toast",
		About: "Show a custom on-screen toast",
		Args: []*cli.Arg{
			{ID: "label", Help: "Toast text. Optional when `--preset` supplies one"},
			{ID: "icon", Long: "icon", Value: cli.String, Help: "Icon name shown beside the text"},
			{ID: "percentage", Long: "percentage", Value: cli.F64, Help: "Progress percentage (0-100); shows a progress bar when set"},
			{ID: "duration", Long: "duration", Value: cli.U32, Help: "Auto-dismiss duration in milliseconds (toast config default when unset)"},
			{ID: "preset", Long: "preset", Value: cli.String, Help: "Preset id from `[[toasts.presets]]` to base this toast on"},
			{ID: "class", Long: "class", Value: cli.String, Help: "Extra CSS class applied to the toast for custom styling"},
		},
		Run: runToast,
	}
}

// runToast sends one custom toast (wayle/src/cli/toast.rs). Either a
// label or --preset must be given.
func runToast(m *cli.Matches) error {
	req := widgetipc.ToastRequest{
		Label:      optional[string](m, "label"),
		Icon:       optional[string](m, "icon"),
		Percentage: optional[float64](m, "percentage"),
		DurationMS: optional[uint32](m, "duration"),
		Preset:     optional[string](m, "preset"),
		Class:      optional[string](m, "class"),
	}
	if req.Label == nil && req.Preset == nil {
		return errors.New("a toast needs a label or --preset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), socketTimeout)
	defer cancel()
	return widgetipc.SendToast(ctx, req)
}

// optional is an arg's value as a pointer, nil when absent (a Rust
// Option<T> field).
func optional[T any](m *cli.Matches, id string) *T {
	v, ok := cli.Value[T](m, id)
	if !ok {
		return nil
	}
	return &v
}

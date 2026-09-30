package bar

import (
	"context"
	"log"
	"os/exec"

	"github.com/stubbedev/wayle/config"
)

// runClickAction dispatches one binding, the Go counterpart of
// dropdown_registry.rs's dispatch_action: dropdown panels open through
// the registry (not ported yet, so they log), brightness steps and
// toggles drive every backlight natively, wayle builtins run
// in-process, and everything else shells out.
func runClickAction(ctx ModuleContext, action config.ClickAction) {
	switch action.Kind {
	case config.ClickNone:
	case config.ClickDropdown:
		// The dropdown panels arrive with the OSD shell; until then
		// the binding is a no-op, exactly like an unregistered
		// dropdown in the Rust registry.
		log.Printf("click: dropdown %q not available yet", action.Dropdown)
	case config.ClickShell:
		runShellBuiltin(ctx, action.Command)
	case config.ClickBrightness:
		stepBrightness(ctx, float64(action.Brightness))
	case config.ClickBrightnessToggle:
		toggleBrightness(ctx)
	}
}

// runShellBuiltin handles the `wayle …` verbs in-process (the Rust
// try_builtin path), falling back to a real subprocess for anything
// else.
func runShellBuiltin(ctx ModuleContext, cmd string) {
	verb, _ := splitBuiltin(cmd)
	if mode, target, ok := screenshotBuiltin(verb); ok && ctx.Screenshot != nil {
		ctx.Screenshot(mode, target)
		return
	}
	switch verb {
	case "audio output-mute":
		toggleSourceMute(ctx, false)
	case "audio input-mute":
		toggleSourceMute(ctx, true)
	default:
		if verb != "" {
			log.Printf("click: builtin %q not available yet, shelling out", verb)
		}
		if cmd == "" {
			return
		}
		if err := exec.Command("sh", "-c", cmd).Start(); err != nil { //nolint:gosec // the command comes from the user's own config
			log.Printf("click: %s: %v", cmd, err)
		}
	}
}

// splitBuiltin reports the verb of a `wayle <area> <verb> …` command,
// empty when the command is not one.
func splitBuiltin(cmd string) (string, string) {
	const prefix = "wayle "
	if len(cmd) < len(prefix) || cmd[:len(prefix)] != prefix {
		return "", cmd
	}
	return cmd[len(prefix):], ""
}

// toggleSourceMute flips the default sink's (input=false) or source's
// (input=true) mute.
func toggleSourceMute(ctx ModuleContext, input bool) {
	if ctx.Pulse == nil {
		return
	}
	go func() {
		if input {
			dev, err := ctx.Pulse.DefaultSource(context.Background())
			if err != nil {
				log.Printf("click: input-mute: %v", err)
				return
			}
			if err := ctx.Pulse.SetSourceMuted(context.Background(), !dev.Muted); err != nil {
				log.Printf("click: input-mute: %v", err)
			}
			return
		}
		dev, err := ctx.Pulse.DefaultSink(context.Background())
		if err != nil {
			log.Printf("click: output-mute: %v", err)
			return
		}
		if err := ctx.Pulse.SetMuted(context.Background(), !dev.Muted); err != nil {
			log.Printf("click: output-mute: %v", err)
		}
	}()
}

// stepBrightness moves every backlight by delta percent, floored at
// the configured minimum (the dimmer can never scroll fully dark; 0%
// is reserved for the toggle).
func stepBrightness(ctx ModuleContext, delta float64) {
	if ctx.Brightness == nil {
		return
	}
	min := float64(ctx.Config.Brightness.MinBright)
	if min < 0 {
		min = 0
	}
	if min > 100 {
		min = 100
	}
	go func() {
		devices, err := ctx.Brightness.Devices(context.Background())
		if err != nil || len(devices) == 0 {
			return
		}
		for _, device := range devices {
			target := device.Percentage() + delta
			if target < min {
				target = min
			}
			if target > 100 {
				target = 100
			}
			if err := ctx.Brightness.Set(context.Background(), device.Name, target); err != nil {
				log.Printf("click: brightness %s: %v", device.Name, err)
			}
		}
	}()
}

// toggleBrightness decides one target state from the whole set (any
// monitor lit means blackout all, all dark restores all) so the
// monitors stay in lockstep.
func toggleBrightness(ctx ModuleContext) {
	if ctx.Brightness == nil {
		return
	}
	go func() {
		devices, err := ctx.Brightness.Devices(context.Background())
		if err != nil || len(devices) == 0 {
			return
		}
		goDark := false
		for _, device := range devices {
			if device.Brightness > 0 {
				goDark = true
				break
			}
		}
		for _, device := range devices {
			target := 0.0
			if !goDark {
				target = 100
			}
			if err := ctx.Brightness.Set(context.Background(), device.Name, target); err != nil {
				log.Printf("click: brightness toggle %s: %v", device.Name, err)
			}
		}
	}()
}

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/rusterr"
	"github.com/stubbedev/wayle/service/shellipc"
)

// panelCommand is wayle/src/cli/panel/commands.rs.
func panelCommand() *cli.Command {
	monitor := func(verb string) []*cli.Arg {
		return []*cli.Arg{{ID: "monitor", Help: `Monitor connector name (e.g., "DP-1"). Omit to ` + verb + " all"}}
	}
	return &cli.Command{
		Name:  "panel",
		About: "Panel management commands",
		Subcommands: []*cli.Command{
			{Name: "start", About: "Start the panel daemon", Run: panelStart},
			{Name: "stop", About: "Stop the panel daemon", Run: panelStop},
			{Name: "restart", About: "Restart the panel daemon", Run: panelRestart},
			{Name: "status", About: "Check panel status", Run: panelStatus},
			{Name: "settings", About: "Open panel settings", Run: panelSettings},
			{Name: "inspect", About: "Open GTK Inspector for debugging", Run: panelInspect},
			{Name: "hide", About: "Hide the bar on a monitor", Args: monitor("hide"), Run: barVisibility("hide bar", "BarHide", "All bars hidden", "Bar hidden on ")},
			{Name: "show", About: "Show the bar on a monitor", Args: monitor("show"), Run: barVisibility("show bar", "BarShow", "All bars shown", "Bar shown on ")},
			{Name: "toggle", About: "Toggle bar visibility on a monitor", Args: monitor("toggle"), Run: barVisibility("toggle bar", "BarToggle", "All bars toggled", "Bar toggled on ")},
		},
	}
}

// panelConnect is panel/proxy.rs's connect.
func panelConnect() (*dbus.Conn, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, cliMessage("D-Bus session unavailable: " + err.Error())
	}
	return conn, nil
}

// panelRunning is proxy.rs's is_running.
func panelRunning() (bool, error) {
	conn, err := panelConnect()
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	running, err := shellipc.IsRunning(conn)
	if err != nil {
		return false, cliMessage("Failed to query D-Bus: " + err.Error())
	}
	return running, nil
}

func panelStart(m *cli.Matches) error {
	if running, _ := panelRunning(); running {
		fmt.Fprintln(m.Stdout(), "Panel is already running")
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return cliMessage("Failed to resolve executable: " + err.Error())
	}
	cmd := exec.Command(exe, "shell") //nolint:gosec // our own binary
	if err := cmd.Start(); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return cliMessage("Permission denied when starting panel")
		}
		return cliMessage("Failed to start panel: " + rusterr.IO(err))
	}
	_ = cmd.Process.Release()
	fmt.Fprintln(m.Stdout(), "Panel started")
	return nil
}

// shutdownTimeout is proxy.rs's SHUTDOWN_TIMEOUT.
const shutdownTimeout = 5 * time.Second

func panelStop(m *cli.Matches) error {
	if running, _ := panelRunning(); !running {
		return cliMessage("Panel is not running")
	}
	conn, err := panelConnect()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	released, err := watchRelease(conn)
	if err != nil {
		return err
	}
	if err := activateAction(conn, shellipc.ActionQuit); err != nil {
		return cliMessage("Failed to stop panel: " + rusterr.Zbus(err))
	}
	select {
	case <-released:
	case <-time.After(shutdownTimeout):
		return cliMessage("Timeout waiting for panel to stop")
	}
	fmt.Fprintln(m.Stdout(), "Panel stopped")
	return nil
}

// watchRelease is proxy.rs's wait_for_shutdown subscription: it fires
// once the application id has no owner.
func watchRelease(conn *dbus.Conn) (<-chan struct{}, error) {
	if err := conn.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchArg(0, shellipc.AppID),
	); err != nil {
		return nil, cliMessage("Failed to subscribe to name changes: " + err.Error())
	}
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	done := make(chan struct{})
	go func() {
		for sig := range signals {
			if len(sig.Body) == 3 && sig.Body[0] == shellipc.AppID && sig.Body[2] == "" {
				close(done)
				return
			}
		}
	}()
	return done, nil
}

func activateAction(conn *dbus.Conn, action string) error {
	return conn.Object(shellipc.AppID, shellipc.AppPath).Call(
		shellipc.ActionsIface+".Activate", 0, action, []dbus.Variant{}, map[string]dbus.Variant{}).Err
}

func panelRestart(m *cli.Matches) error {
	if running, _ := panelRunning(); running {
		if err := panelStop(m); err != nil {
			return err
		}
	}
	return panelStart(m)
}

func panelStatus(m *cli.Matches) error {
	running, err := panelRunning()
	if err != nil {
		return cliMessage("Cannot determine panel status: " + err.Error())
	}
	if running {
		fmt.Fprintln(m.Stdout(), "Panel is running")
	} else {
		fmt.Fprintln(m.Stdout(), "Panel is not running")
	}
	return nil
}

// panelSettings is settings.rs: wayle-settings beside this binary,
// else from PATH.
func panelSettings(*cli.Matches) error {
	program := "wayle-settings"
	if exe, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), "wayle-settings")
		if _, err := os.Stat(sibling); err == nil {
			program = sibling
		}
	}
	cmd := exec.Command(program)
	if err := cmd.Start(); err != nil {
		switch {
		case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
			return cliMessage("wayle-settings not found. Is Wayle installed correctly?")
		case errors.Is(err, fs.ErrPermission):
			return cliMessage("Permission denied when starting wayle-settings")
		default:
			return cliMessage("Failed to launch settings: " + rusterr.IO(err))
		}
	}
	_ = cmd.Process.Release()
	return nil
}

func panelInspect(m *cli.Matches) error {
	if running, _ := panelRunning(); !running {
		return cliMessage("Panel is not running")
	}
	conn, err := panelConnect()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := activateAction(conn, shellipc.ActionInspect); err != nil {
		return cliMessage("Failed to open inspector: " + rusterr.Zbus(err))
	}
	fmt.Fprintln(m.Stdout(), "GTK Inspector opened")
	return nil
}

// barVisibility is hide.rs / show.rs / toggle.rs.
func barVisibility(op, method, all, one string) func(*cli.Matches) error {
	return func(m *cli.Matches) error {
		conn, err := panelConnect()
		if err != nil {
			return err
		}
		p := &daemonProxy{label: "Shell", iface: shellipc.ServiceName, conn: conn, obj: conn.Object(shellipc.ServiceName, shellipc.ServicePath)}
		defer p.Close()
		monitor, _ := cli.Value[string](m, "monitor")
		if err := p.call(op, method, nil, monitor); err != nil {
			return err
		}
		if monitor == "" {
			fmt.Fprintln(m.Stdout(), all)
		} else {
			fmt.Fprintln(m.Stdout(), one+monitor)
		}
		return nil
	}
}

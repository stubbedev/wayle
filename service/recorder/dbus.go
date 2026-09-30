package recorder

import (
	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// D-Bus identity (the Rust RecorderService's registration).
const (
	ServiceName = "com.wayle.Recorder1"
	ServicePath = "/com/wayle/Recorder"
)

// Daemon is the com.wayle.Recorder1 object
// (wayle-shell-core/src/services/recorder/dbus.rs).
type Daemon struct {
	state *State
}

// NewDaemon wraps the state for export.
func NewDaemon(state *State) *Daemon { return &Daemon{state: state} }

// Export serves the interface under its well-known name; the returned
// release drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	return dbusx.Serve(conn, dbusx.Service{
		Name:      ServiceName,
		Path:      ServicePath,
		Interface: ServiceName,
		Methods:   d,
		Properties: dbusx.Getters{
			"Active":  func() any { return d.state.Snapshot().Active },
			"Paused":  func() any { return d.state.Snapshot().Paused },
			"Elapsed": func() any { return d.state.Snapshot().ElapsedSecs },
			"File":    func() any { return d.state.Snapshot().OutputPath },
		},
	})
}

// Start begins a recording.
func (d *Daemon) Start() *dbus.Error {
	d.state.Start()
	return nil
}

// Stop finalizes the recording.
func (d *Daemon) Stop() *dbus.Error {
	d.state.Stop()
	return nil
}

// Toggle starts or stops.
func (d *Daemon) Toggle() *dbus.Error {
	d.state.Toggle()
	return nil
}

// Pause pauses the recording.
func (d *Daemon) Pause() *dbus.Error {
	d.state.SetPaused(true)
	return nil
}

// Resume resumes a paused recording.
func (d *Daemon) Resume() *dbus.Error {
	d.state.SetPaused(false)
	return nil
}

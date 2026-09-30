package pulse

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/dbusx"
)

// Wayle's own interface over the audio service, which `wayle audio`
// drives (wayle-audio/src/dbus).
const (
	DaemonName = "com.wayle.Audio1"
	DaemonPath = dbus.ObjectPath("/com/wayle/Audio")
)

const daemonTimeout = 5 * time.Second

// Daemon is the com.wayle.Audio1 object (AudioDaemon).
type Daemon struct {
	mixer Mixer
}

func daemonCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), daemonTimeout)
}

// defaultEndpoint is the service's default_output/default_input.
func (d *Daemon) defaultEndpoint(kind Kind) (Endpoint, bool) {
	ctx, cancel := daemonCtx()
	defer cancel()
	name, err := d.mixer.DefaultName(ctx, kind)
	if err != nil || name == "" {
		return Endpoint{}, false
	}
	devices, err := d.mixer.Endpoints(ctx, kind)
	if err != nil {
		return Endpoint{}, false
	}
	for _, ep := range devices {
		if ep.Name == name {
			return ep, true
		}
	}
	return Endpoint{}, false
}

func noDefault(kind Kind) *dbus.Error {
	if kind == Input {
		return dbusx.Failed("No default input device")
	}
	return dbusx.Failed("No default output device")
}

func clampPercent(v float64) float64 { return math.Max(0, math.Min(100, v)) }

func (d *Daemon) setVolume(kind Kind, percent float64) (float64, *dbus.Error) {
	ep, ok := d.defaultEndpoint(kind)
	if !ok {
		return 0, noDefault(kind)
	}
	clamped := clampPercent(percent)
	ctx, cancel := daemonCtx()
	defer cancel()
	if err := d.mixer.SetEndpointVolume(ctx, kind, ep.Name, clamped); err != nil {
		return 0, dbusx.Failed(err.Error())
	}
	return clamped, nil
}

func (d *Daemon) adjustVolume(kind Kind, delta float64) (float64, *dbus.Error) {
	ep, ok := d.defaultEndpoint(kind)
	if !ok {
		return 0, noDefault(kind)
	}
	next := clampPercent(ep.Volume + delta)
	ctx, cancel := daemonCtx()
	defer cancel()
	if err := d.mixer.SetEndpointVolume(ctx, kind, ep.Name, next); err != nil {
		return 0, dbusx.Failed(err.Error())
	}
	return next, nil
}

func (d *Daemon) setMute(kind Kind, muted bool) *dbus.Error {
	ep, ok := d.defaultEndpoint(kind)
	if !ok {
		return noDefault(kind)
	}
	ctx, cancel := daemonCtx()
	defer cancel()
	if err := d.mixer.SetEndpointMute(ctx, kind, ep.Name, muted); err != nil {
		return dbusx.Failed(err.Error())
	}
	return nil
}

func (d *Daemon) toggleMute(kind Kind) (bool, *dbus.Error) {
	ep, ok := d.defaultEndpoint(kind)
	if !ok {
		return false, noDefault(kind)
	}
	if err := d.setMute(kind, !ep.Muted); err != nil {
		return false, err
	}
	return !ep.Muted, nil
}

func (d *Daemon) setDefault(kind Kind, index uint32) *dbus.Error {
	ctx, cancel := daemonCtx()
	defer cancel()
	devices, err := d.mixer.Endpoints(ctx, kind)
	if err != nil {
		return dbusx.Failed(err.Error())
	}
	for _, ep := range devices {
		if ep.Index == index {
			if err := d.mixer.SetDefault(ctx, kind, ep.Name); err != nil {
				return dbusx.Failed(err.Error())
			}
			return nil
		}
	}
	return dbusx.Failed("device not found: " + strconv.FormatUint(uint64(index), 10))
}

// DeviceRow is one ListSinks/ListSources row: (index, name, description).
type DeviceRow struct {
	Index       uint32
	Name        string
	Description string
}

func (d *Daemon) list(kind Kind) []DeviceRow {
	ctx, cancel := daemonCtx()
	defer cancel()
	devices, _ := d.mixer.Endpoints(ctx, kind)
	rows := make([]DeviceRow, len(devices))
	for i, ep := range devices {
		rows[i] = DeviceRow{ep.Index, ep.Name, ep.Description}
	}
	return rows
}

func (d *Daemon) info(kind Kind) (map[string]string, *dbus.Error) {
	ep, ok := d.defaultEndpoint(kind)
	if !ok {
		return nil, noDefault(kind)
	}
	info := map[string]string{
		"index":       strconv.FormatUint(uint64(ep.Index), 10),
		"name":        ep.Name,
		"description": ep.Description,
		"volume":      strconv.FormatFloat(ep.Volume, 'f', 0, 64),
		"muted":       strconv.FormatBool(ep.Muted),
		"state":       ep.State,
	}
	if ep.ActivePort != "" {
		info["active_port"] = ep.ActivePort
	}
	return info, nil
}

// SetOutputVolume sets the default output to a clamped percentage.
func (d *Daemon) SetOutputVolume(v float64) (float64, *dbus.Error) { return d.setVolume(Output, v) }

// AdjustOutputVolume shifts the default output by delta percent.
func (d *Daemon) AdjustOutputVolume(delta float64) (float64, *dbus.Error) {
	return d.adjustVolume(Output, delta)
}

// SetOutputMute sets the default output's mute.
func (d *Daemon) SetOutputMute(muted bool) *dbus.Error { return d.setMute(Output, muted) }

// ToggleOutputMute flips the default output's mute.
func (d *Daemon) ToggleOutputMute() (bool, *dbus.Error) { return d.toggleMute(Output) }

// SetDefaultSink makes the sink at index the default.
func (d *Daemon) SetDefaultSink(index uint32) *dbus.Error { return d.setDefault(Output, index) }

// SetDefaultSource makes the source at index the default.
func (d *Daemon) SetDefaultSource(index uint32) *dbus.Error { return d.setDefault(Input, index) }

// SetInputVolume sets the default input to a clamped percentage.
func (d *Daemon) SetInputVolume(v float64) (float64, *dbus.Error) { return d.setVolume(Input, v) }

// AdjustInputVolume shifts the default input by delta percent.
func (d *Daemon) AdjustInputVolume(delta float64) (float64, *dbus.Error) {
	return d.adjustVolume(Input, delta)
}

// SetInputMute sets the default input's mute.
func (d *Daemon) SetInputMute(muted bool) *dbus.Error { return d.setMute(Input, muted) }

// ToggleInputMute flips the default input's mute.
func (d *Daemon) ToggleInputMute() (bool, *dbus.Error) { return d.toggleMute(Input) }

// ListSinks lists the outputs.
func (d *Daemon) ListSinks() ([]DeviceRow, *dbus.Error) { return d.list(Output), nil }

// ListSources lists the inputs.
func (d *Daemon) ListSources() ([]DeviceRow, *dbus.Error) { return d.list(Input), nil }

// GetDefaultSinkInfo describes the default output.
func (d *Daemon) GetDefaultSinkInfo() (map[string]string, *dbus.Error) { return d.info(Output) }

// GetDefaultSourceInfo describes the default input.
func (d *Daemon) GetDefaultSourceInfo() (map[string]string, *dbus.Error) { return d.info(Input) }

// ServeDaemon exports the interface over mixer.
func ServeDaemon(conn *dbus.Conn, mixer Mixer) (func(), error) {
	d := &Daemon{mixer: mixer}
	volume := func(kind Kind) func() any {
		return func() any {
			ep, _ := d.defaultEndpoint(kind)
			return ep.Volume
		}
	}
	muted := func(kind Kind) func() any {
		return func() any {
			ep, _ := d.defaultEndpoint(kind)
			return ep.Muted
		}
	}
	name := func(kind Kind) func() any {
		return func() any {
			ep, _ := d.defaultEndpoint(kind)
			return ep.Name
		}
	}
	count := func(kind Kind) func() any {
		return func() any { return uint32(len(d.list(kind))) }
	}
	return dbusx.Serve(conn, dbusx.Service{
		Name:      DaemonName,
		Path:      DaemonPath,
		Interface: DaemonName,
		Methods:   d,
		Properties: dbusx.Getters{
			"OutputVolume":  volume(Output),
			"OutputMuted":   muted(Output),
			"InputVolume":   volume(Input),
			"InputMuted":    muted(Input),
			"DefaultSink":   name(Output),
			"DefaultSource": name(Input),
			"SinkCount":     count(Output),
			"SourceCount":   count(Input),
		},
	})
}

package pulse

import (
	"context"
	"fmt"
	"strconv"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// D-Bus identity (crates/wayle-audio/src/dbus/mod.rs).
const (
	ServiceName = "com.wayle.Audio1"
	ServicePath = "/com/wayle/Audio"
	// Interface is the object's interface name.
	Interface = "com.wayle.Audio1"
)

const (
	propertiesInterface = "org.freedesktop.DBus.Properties"
	errPropertyReadOnly = "org.freedesktop.DBus.Error.PropertyReadOnly"
	errUnknownProperty  = "org.freedesktop.DBus.Error.UnknownProperty"
	errFailed           = "org.freedesktop.DBus.Error.Failed"
)

// Daemon serves the audio service on the session bus
// (dbus/server.rs's AudioDaemon) for the `wayle audio` CLI and
// scripts.
type Daemon struct {
	svc *Service
}

// NewDaemon wraps the service for export.
func NewDaemon(svc *Service) *Daemon { return &Daemon{svc: svc} }

// Export exports the object, its properties, and its introspection
// data, then requests the well-known name; release drops the name.
func (d *Daemon) Export(conn *dbus.Conn) (func(), error) {
	obj := &audioObject{svc: d.svc}
	if err := conn.Export(obj, ServicePath, Interface); err != nil {
		return nil, fmt.Errorf("pulse: export: %w", err)
	}
	if err := conn.Export(&audioProperties{svc: d.svc}, ServicePath, propertiesInterface); err != nil {
		return nil, fmt.Errorf("pulse: export properties: %w", err)
	}
	if err := conn.Export(introspect.NewIntrospectable(introspection()), ServicePath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return nil, fmt.Errorf("pulse: export introspection: %w", err)
	}
	reply, err := conn.RequestName(ServiceName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("pulse: request name: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, fmt.Errorf("pulse: %s is already owned", ServiceName)
	}
	return func() { _, _ = conn.ReleaseName(ServiceName) }, nil
}

// audioObject carries the com.wayle.Audio1 methods; godbus exports
// every method returning *dbus.Error.
type audioObject struct {
	svc *Service
}

var (
	// The Rust daemon's fdo::Error::Failed messages.
	errNoOutput = dbus.NewError(errFailed, []any{"No default output device"})
	errNoInput  = dbus.NewError(errFailed, []any{"No default input device"})
)

func failed(err error) *dbus.Error { return dbus.MakeFailedError(err) }

func (o *audioObject) output() (OutputDevice, *dbus.Error) {
	d, ok := o.svc.DefaultOutput()
	if !ok {
		return OutputDevice{}, errNoOutput
	}
	return d, nil
}

func (o *audioObject) input() (InputDevice, *dbus.Error) {
	d, ok := o.svc.DefaultInput()
	if !ok {
		return InputDevice{}, errNoInput
	}
	return d, nil
}

// setPercent applies a 0-100 percentage over the device's channels.
func (o *audioObject) setPercent(d Device, percent float64) *dbus.Error {
	v := VolumeFromPercentage(percent, d.Volume.Channels())
	if err := o.svc.SetDeviceVolume(context.Background(), d.Key, v); err != nil {
		return failed(err)
	}
	return nil
}

func (o *audioObject) setMute(d Device, muted bool) *dbus.Error {
	if err := o.svc.SetDeviceMute(context.Background(), d.Key, muted); err != nil {
		return failed(err)
	}
	return nil
}

// SetOutputVolume sets the default output to a 0-100 percentage and
// returns the clamped value.
func (o *audioObject) SetOutputVolume(volume float64) (float64, *dbus.Error) {
	d, derr := o.output()
	if derr != nil {
		return 0, derr
	}
	clamped := clampPercent(volume)
	return clamped, o.setPercent(d.Device, clamped)
}

// AdjustOutputVolume moves the default output by delta percentage
// points and returns the new, clamped volume.
func (o *audioObject) AdjustOutputVolume(delta float64) (float64, *dbus.Error) {
	d, derr := o.output()
	if derr != nil {
		return 0, derr
	}
	next := clampPercent(d.Volume.AveragePercentage() + delta)
	return next, o.setPercent(d.Device, next)
}

// SetOutputMute sets the default output's mute.
func (o *audioObject) SetOutputMute(muted bool) *dbus.Error {
	d, derr := o.output()
	if derr != nil {
		return derr
	}
	return o.setMute(d.Device, muted)
}

// ToggleOutputMute flips the default output's mute and returns the
// new state.
func (o *audioObject) ToggleOutputMute() (bool, *dbus.Error) {
	d, derr := o.output()
	if derr != nil {
		return false, derr
	}
	return !d.Muted, o.setMute(d.Device, !d.Muted)
}

// SetInputVolume sets the default input to a 0-100 percentage.
func (o *audioObject) SetInputVolume(volume float64) (float64, *dbus.Error) {
	d, derr := o.input()
	if derr != nil {
		return 0, derr
	}
	clamped := clampPercent(volume)
	return clamped, o.setPercent(d.Device, clamped)
}

// AdjustInputVolume moves the default input by delta points.
func (o *audioObject) AdjustInputVolume(delta float64) (float64, *dbus.Error) {
	d, derr := o.input()
	if derr != nil {
		return 0, derr
	}
	next := clampPercent(d.Volume.AveragePercentage() + delta)
	return next, o.setPercent(d.Device, next)
}

// SetInputMute sets the default input's mute.
func (o *audioObject) SetInputMute(muted bool) *dbus.Error {
	d, derr := o.input()
	if derr != nil {
		return derr
	}
	return o.setMute(d.Device, muted)
}

// ToggleInputMute flips the default input's mute.
func (o *audioObject) ToggleInputMute() (bool, *dbus.Error) {
	d, derr := o.input()
	if derr != nil {
		return false, derr
	}
	return !d.Muted, o.setMute(d.Device, !d.Muted)
}

// SetDefaultSink makes the sink with this index the default.
func (o *audioObject) SetDefaultSink(index uint32) *dbus.Error {
	return o.setDefault(DeviceKey{Index: index, Type: DeviceOutput})
}

// SetDefaultSource makes the source with this index the default.
func (o *audioObject) SetDefaultSource(index uint32) *dbus.Error {
	return o.setDefault(DeviceKey{Index: index, Type: DeviceInput})
}

func (o *audioObject) setDefault(key DeviceKey) *dbus.Error {
	if err := o.svc.SetDefault(context.Background(), key); err != nil {
		return failed(err)
	}
	return nil
}

// DeviceEntry is one ListSinks/ListSources row, (index, name,
// description) on the wire.
type DeviceEntry struct {
	Index       uint32
	Name        string
	Description string
}

// ListSinks lists the outputs by index (the Rust daemon walks a
// HashMap, so its order is arbitrary; index order is the stable one).
func (o *audioObject) ListSinks() ([]DeviceEntry, *dbus.Error) {
	var out []DeviceEntry
	for _, d := range o.svc.OutputDevices() {
		out = append(out, DeviceEntry{Index: d.Key.Index, Name: d.Name, Description: d.Description})
	}
	return nonNil(out), nil
}

// ListSources lists the inputs, monitors included.
func (o *audioObject) ListSources() ([]DeviceEntry, *dbus.Error) {
	var out []DeviceEntry
	for _, d := range o.svc.InputDevices() {
		out = append(out, DeviceEntry{Index: d.Key.Index, Name: d.Name, Description: d.Description})
	}
	return nonNil(out), nil
}

// nonNil keeps an empty list an empty array on the wire.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// GetDefaultSinkInfo describes the default output.
func (o *audioObject) GetDefaultSinkInfo() (map[string]string, *dbus.Error) {
	d, derr := o.output()
	if derr != nil {
		return nil, derr
	}
	return deviceInfoMap(d.Device), nil
}

// GetDefaultSourceInfo describes the default input.
func (o *audioObject) GetDefaultSourceInfo() (map[string]string, *dbus.Error) {
	d, derr := o.input()
	if derr != nil {
		return nil, derr
	}
	return deviceInfoMap(d.Device), nil
}

// deviceInfoMap is get_default_sink_info's dictionary: the volume is
// the rounded average percentage ({:.0}), the state its variant name.
func deviceInfoMap(d Device) map[string]string {
	info := map[string]string{
		"index":       strconv.FormatUint(uint64(d.Key.Index), 10),
		"name":        d.Name,
		"description": d.Description,
		"volume":      strconv.FormatFloat(d.Volume.AveragePercentage(), 'f', 0, 64),
		"muted":       strconv.FormatBool(d.Muted),
		"state":       d.State.String(),
	}
	if d.ActivePort != "" {
		info["active_port"] = d.ActivePort
	}
	return info
}

// audioProperties is org.freedesktop.DBus.Properties for the object:
// every property is computed on read, like the zbus getters.
type audioProperties struct {
	svc *Service
}

// propertyNames lists the read-only properties in introspection order.
var propertyNames = []string{
	"OutputVolume", "OutputMuted", "InputVolume", "InputMuted",
	"DefaultSink", "DefaultSource", "SinkCount", "SourceCount",
}

func (p *audioProperties) value(name string) (any, bool) {
	out, hasOut := p.svc.DefaultOutput()
	in, hasIn := p.svc.DefaultInput()
	switch name {
	case "OutputVolume":
		if !hasOut {
			return 0.0, true
		}
		return out.Volume.AveragePercentage(), true
	case "OutputMuted":
		return hasOut && out.Muted, true
	case "InputVolume":
		if !hasIn {
			return 0.0, true
		}
		return in.Volume.AveragePercentage(), true
	case "InputMuted":
		return hasIn && in.Muted, true
	case "DefaultSink":
		return out.Name, true
	case "DefaultSource":
		return in.Name, true
	case "SinkCount":
		return uint32(len(p.svc.OutputDevices())), true
	case "SourceCount":
		return uint32(len(p.svc.InputDevices())), true
	}
	return nil, false
}

// Get implements org.freedesktop.DBus.Properties.Get.
func (p *audioProperties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != Interface {
		return dbus.Variant{}, dbus.NewError(errUnknownProperty, []any{"unknown interface " + iface})
	}
	v, ok := p.value(name)
	if !ok {
		return dbus.Variant{}, dbus.NewError(errUnknownProperty, []any{"unknown property " + name})
	}
	return dbus.MakeVariant(v), nil
}

// GetAll implements org.freedesktop.DBus.Properties.GetAll.
func (p *audioProperties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != Interface {
		return nil, dbus.NewError(errUnknownProperty, []any{"unknown interface " + iface})
	}
	out := make(map[string]dbus.Variant, len(propertyNames))
	for _, name := range propertyNames {
		v, _ := p.value(name)
		out[name] = dbus.MakeVariant(v)
	}
	return out, nil
}

// Set implements org.freedesktop.DBus.Properties.Set: all read-only.
func (p *audioProperties) Set(_, name string, _ dbus.Variant) *dbus.Error {
	return dbus.NewError(errPropertyReadOnly, []any{"property " + name + " is read-only"})
}

func introspection() *introspect.Node {
	arg := func(name, sig, dir string) introspect.Arg {
		return introspect.Arg{Name: name, Type: sig, Direction: dir}
	}
	method := func(name string, args ...introspect.Arg) introspect.Method {
		return introspect.Method{Name: name, Args: args}
	}
	props := make([]introspect.Property, 0, len(propertyNames))
	sigs := map[string]string{
		"OutputVolume": "d", "OutputMuted": "b", "InputVolume": "d", "InputMuted": "b",
		"DefaultSink": "s", "DefaultSource": "s", "SinkCount": "u", "SourceCount": "u",
	}
	for _, name := range propertyNames {
		props = append(props, introspect.Property{Name: name, Type: sigs[name], Access: "read"})
	}
	return &introspect.Node{
		Name: ServicePath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{Name: propertiesInterface, Methods: []introspect.Method{
				method("Get", arg("interface_name", "s", "in"), arg("property_name", "s", "in"), arg("value", "v", "out")),
				method("GetAll", arg("interface_name", "s", "in"), arg("props", "a{sv}", "out")),
				method("Set", arg("interface_name", "s", "in"), arg("property_name", "s", "in"), arg("value", "v", "in")),
			}},
			{
				Name: Interface,
				Methods: []introspect.Method{
					method("SetOutputVolume", arg("volume", "d", "in"), arg("", "d", "out")),
					method("AdjustOutputVolume", arg("delta", "d", "in"), arg("", "d", "out")),
					method("SetOutputMute", arg("muted", "b", "in")),
					method("ToggleOutputMute", arg("", "b", "out")),
					method("SetInputVolume", arg("volume", "d", "in"), arg("", "d", "out")),
					method("AdjustInputVolume", arg("delta", "d", "in"), arg("", "d", "out")),
					method("SetInputMute", arg("muted", "b", "in")),
					method("ToggleInputMute", arg("", "b", "out")),
					method("SetDefaultSink", arg("device_index", "u", "in")),
					method("SetDefaultSource", arg("device_index", "u", "in")),
					method("ListSinks", arg("", "a(uss)", "out")),
					method("ListSources", arg("", "a(uss)", "out")),
					method("GetDefaultSinkInfo", arg("", "a{ss}", "out")),
					method("GetDefaultSourceInfo", arg("", "a{ss}", "out")),
				},
				Properties: props,
			},
		},
	}
}

// Client is the CLI side (dbus/client.rs's AudioProxy).
type Client struct {
	obj dbus.BusObject
}

// NewClient addresses the daemon over conn.
func NewClient(conn *dbus.Conn) *Client {
	return &Client{obj: conn.Object(ServiceName, ServicePath)}
}

func (c *Client) call(ctx context.Context, method string, out any, args ...any) error {
	call := c.obj.CallWithContext(ctx, Interface+"."+method, 0, args...)
	if call.Err != nil {
		return call.Err
	}
	if out == nil {
		return nil
	}
	return call.Store(out)
}

func property[T any](ctx context.Context, c *Client, name string) (T, error) {
	var v dbus.Variant
	var zero T
	call := c.obj.CallWithContext(ctx, propertiesInterface+".Get", 0, Interface, name)
	if call.Err != nil {
		return zero, call.Err
	}
	if err := call.Store(&v); err != nil {
		return zero, err
	}
	out, ok := v.Value().(T)
	if !ok {
		return zero, fmt.Errorf("pulse: property %s is %s, want %T", name, v.Signature(), zero)
	}
	return out, nil
}

// SetOutputVolume sets the output volume percentage.
func (c *Client) SetOutputVolume(ctx context.Context, volume float64) (float64, error) {
	var out float64
	return out, c.call(ctx, "SetOutputVolume", &out, volume)
}

// AdjustOutputVolume shifts the output volume.
func (c *Client) AdjustOutputVolume(ctx context.Context, delta float64) (float64, error) {
	var out float64
	return out, c.call(ctx, "AdjustOutputVolume", &out, delta)
}

// SetOutputMute sets the output mute.
func (c *Client) SetOutputMute(ctx context.Context, muted bool) error {
	return c.call(ctx, "SetOutputMute", nil, muted)
}

// ToggleOutputMute flips the output mute.
func (c *Client) ToggleOutputMute(ctx context.Context) (bool, error) {
	var out bool
	return out, c.call(ctx, "ToggleOutputMute", &out)
}

// SetInputVolume sets the input volume percentage.
func (c *Client) SetInputVolume(ctx context.Context, volume float64) (float64, error) {
	var out float64
	return out, c.call(ctx, "SetInputVolume", &out, volume)
}

// AdjustInputVolume shifts the input volume.
func (c *Client) AdjustInputVolume(ctx context.Context, delta float64) (float64, error) {
	var out float64
	return out, c.call(ctx, "AdjustInputVolume", &out, delta)
}

// SetInputMute sets the input mute.
func (c *Client) SetInputMute(ctx context.Context, muted bool) error {
	return c.call(ctx, "SetInputMute", nil, muted)
}

// ToggleInputMute flips the input mute.
func (c *Client) ToggleInputMute(ctx context.Context) (bool, error) {
	var out bool
	return out, c.call(ctx, "ToggleInputMute", &out)
}

// SetDefaultSink makes a sink the default by index.
func (c *Client) SetDefaultSink(ctx context.Context, index uint32) error {
	return c.call(ctx, "SetDefaultSink", nil, index)
}

// SetDefaultSource makes a source the default by index.
func (c *Client) SetDefaultSource(ctx context.Context, index uint32) error {
	return c.call(ctx, "SetDefaultSource", nil, index)
}

// ListSinks lists the outputs.
func (c *Client) ListSinks(ctx context.Context) ([]DeviceEntry, error) {
	var out []DeviceEntry
	return out, c.call(ctx, "ListSinks", &out)
}

// ListSources lists the inputs.
func (c *Client) ListSources(ctx context.Context) ([]DeviceEntry, error) {
	var out []DeviceEntry
	return out, c.call(ctx, "ListSources", &out)
}

// GetDefaultSinkInfo describes the default output.
func (c *Client) GetDefaultSinkInfo(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	return out, c.call(ctx, "GetDefaultSinkInfo", &out)
}

// GetDefaultSourceInfo describes the default input.
func (c *Client) GetDefaultSourceInfo(ctx context.Context) (map[string]string, error) {
	var out map[string]string
	return out, c.call(ctx, "GetDefaultSourceInfo", &out)
}

// OutputVolume reads the OutputVolume property.
func (c *Client) OutputVolume(ctx context.Context) (float64, error) {
	return property[float64](ctx, c, "OutputVolume")
}

// OutputMuted reads the OutputMuted property.
func (c *Client) OutputMuted(ctx context.Context) (bool, error) {
	return property[bool](ctx, c, "OutputMuted")
}

// InputVolume reads the InputVolume property.
func (c *Client) InputVolume(ctx context.Context) (float64, error) {
	return property[float64](ctx, c, "InputVolume")
}

// InputMuted reads the InputMuted property.
func (c *Client) InputMuted(ctx context.Context) (bool, error) {
	return property[bool](ctx, c, "InputMuted")
}

// DefaultSink reads the DefaultSink property.
func (c *Client) DefaultSink(ctx context.Context) (string, error) {
	return property[string](ctx, c, "DefaultSink")
}

// DefaultSource reads the DefaultSource property.
func (c *Client) DefaultSource(ctx context.Context) (string, error) {
	return property[string](ctx, c, "DefaultSource")
}

// SinkCount reads the SinkCount property.
func (c *Client) SinkCount(ctx context.Context) (uint32, error) {
	return property[uint32](ctx, c, "SinkCount")
}

// SourceCount reads the SourceCount property.
func (c *Client) SourceCount(ctx context.Context) (uint32, error) {
	return property[uint32](ctx, c, "SourceCount")
}

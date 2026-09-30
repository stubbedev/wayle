package pulse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Kind selects sinks (outputs) or sources (inputs).
type Kind int

// Kinds.
const (
	Output Kind = iota
	Input
)

func (k Kind) plural() string {
	if k == Input {
		return "sources"
	}
	return "sinks"
}

func (k Kind) singular() string {
	if k == Input {
		return "source"
	}
	return "sink"
}

// normVolume is PA_VOLUME_NORM, 100%.
const normVolume = 65536

// Endpoint is one sink or source as wayle-audio's device model sees it:
// the PulseAudio index, identity, linear channel-mean volume, mute,
// state, and active port.
type Endpoint struct {
	Index       uint32
	Name        string
	Description string
	// Volume is the channel mean in percent (1.0 linear = 100).
	Volume float64
	Muted  bool
	// State is DeviceState's Debug form: Running, Idle, Suspended, or
	// Offline.
	State string
	// ActivePort is the active port's name, "" when the device has none.
	ActivePort string
}

// Mixer is the device-level control the audio daemon needs.
type Mixer interface {
	// Endpoints lists every sink or source, in index order.
	Endpoints(ctx context.Context, kind Kind) ([]Endpoint, error)
	// DefaultName names the default sink or source ("" when none).
	DefaultName(ctx context.Context, kind Kind) (string, error)
	// SetEndpointVolume sets every channel of the named device to percent.
	SetEndpointVolume(ctx context.Context, kind Kind, name string, percent float64) error
	// SetEndpointMute sets the named device's mute.
	SetEndpointMute(ctx context.Context, kind Kind, name string, muted bool) error
	// SetDefault makes the named device the default.
	SetDefault(ctx context.Context, kind Kind, name string) error
}

// pactlDevice is one element of `pactl -f json list sinks|sources`.
type pactlDevice struct {
	Index       uint32 `json:"index"`
	State       string `json:"state"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Mute        bool   `json:"mute"`
	Volume      map[string]struct {
		Value uint32 `json:"value"`
	} `json:"volume"`
	ActivePort *string `json:"active_port"`
}

// parseEndpoints decodes pactl's JSON listing.
func parseEndpoints(raw []byte) ([]Endpoint, error) {
	var devices []pactlDevice
	if err := json.Unmarshal(raw, &devices); err != nil {
		return nil, fmt.Errorf("pulse: parse device list: %w", err)
	}
	out := make([]Endpoint, 0, len(devices))
	for _, d := range devices {
		ep := Endpoint{
			Index:       d.Index,
			Name:        d.Name,
			Description: d.Description,
			Muted:       d.Mute,
			State:       deviceState(d.State),
		}
		if d.ActivePort != nil {
			ep.ActivePort = *d.ActivePort
		}
		if len(d.Volume) > 0 {
			var sum float64
			for _, ch := range d.Volume {
				sum += float64(ch.Value) / normVolume
			}
			ep.Volume = sum / float64(len(d.Volume)) * 100
		}
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

// deviceState is convert_sink_state/convert_source_state.
func deviceState(pactl string) string {
	switch strings.ToUpper(pactl) {
	case "RUNNING":
		return "Running"
	case "IDLE":
		return "Idle"
	case "SUSPENDED":
		return "Suspended"
	default:
		return "Offline"
	}
}

// Endpoints lists the devices through pactl's JSON output.
func (p *Pactl) Endpoints(ctx context.Context, kind Kind) ([]Endpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out, err := p.run(ctx, "-f", "json", "list", kind.plural())
	if err != nil {
		return nil, err
	}
	return parseEndpoints([]byte(out))
}

// DefaultName reads the default device's name.
func (p *Pactl) DefaultName(ctx context.Context, kind Kind) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out, err := p.run(ctx, "get-default-"+kind.singular())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// SetEndpointVolume applies one percentage to every channel.
func (p *Pactl) SetEndpointVolume(ctx context.Context, kind Kind, name string, percent float64) error {
	if name == "" {
		return errors.New("pulse: no device name")
	}
	value := strconv.FormatInt(int64(percent/100*normVolume+0.5), 10)
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.run(ctx, "set-"+kind.singular()+"-volume", name, value)
	return err
}

// SetEndpointMute sets the device's mute.
func (p *Pactl) SetEndpointMute(ctx context.Context, kind Kind, name string, muted bool) error {
	state := "0"
	if muted {
		state = "1"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.run(ctx, "set-"+kind.singular()+"-mute", name, state)
	return err
}

// SetDefault makes the device the default.
func (p *Pactl) SetDefault(ctx context.Context, kind Kind, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.run(ctx, "set-default-"+kind.singular(), name)
	return err
}

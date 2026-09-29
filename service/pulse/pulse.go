// Package pulse reads and sets audio volume through pactl, the pure-Go
// stand-in for crates/wayle-audio's libpulse FFI: snapshot state from
// pactl queries, live updates from `pactl subscribe`, changes through
// pactl setters. The same trust boundary as the cava capture path.
package pulse

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Device is one sink snapshot.
type Device struct {
	Name    string
	Desc    string
	Volume  float64 // percent, channel mean
	Muted   bool
	Default bool
}

// Source is the module's seam.
type Source interface {
	// DefaultSink returns the default output's snapshot.
	DefaultSink(ctx context.Context) (Device, error)
	// DefaultSource returns the default input's snapshot.
	DefaultSource(ctx context.Context) (Device, error)
	// Subscribe ticks on PulseAudio state changes. The channel closes
	// when ctx completes; stop terminates the listener.
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
	// SetVolume applies a clamped percentage to the default sink.
	SetVolume(ctx context.Context, percent float64) error
	// SetMuted toggles or sets mute on the default sink.
	SetMuted(ctx context.Context, muted bool) error
	// SetSourceMuted sets mute on the default input.
	SetSourceMuted(ctx context.Context, muted bool) error
}

// Pactl runs the real pactl.
type Pactl struct {
	mu sync.Mutex
}

// New builds the pactl source.
func New() *Pactl { return &Pactl{} }

// run executes pactl and returns stdout.
func (p *Pactl) run(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "pactl", args...).Output() //nolint:gosec // args are fixed literals at every call site; the pactl trust boundary is documented in the package comment
	if err != nil {
		return "", fmt.Errorf("pulse: pactl %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

var percentRe = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)

// DefaultSink queries the default sink's name, description, volume,
// and mute.
func (p *Pactl) DefaultSink(ctx context.Context) (Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.defaultSinkLocked(ctx)
}

func (p *Pactl) defaultSinkLocked(ctx context.Context) (Device, error) {
	name, err := p.run(ctx, "get-default-sink")
	if err != nil {
		return Device{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Device{}, errors.New("pulse: no default sink")
	}
	dev := Device{Name: name, Default: true}

	// The long listing carries the description; blocks start at
	// "Sink #N" and the properties are "Name: ..." / "Description: ...".
	if listing, err := p.run(ctx, "list", "sinks"); err == nil {
		dev.Desc = sinkDescription(listing, name)
	}
	if volOut, err := p.run(ctx, "get-sink-volume", "@DEFAULT_SINK@"); err == nil {
		dev.Volume = meanPercent(volOut)
	}
	if muteOut, err := p.run(ctx, "get-sink-mute", "@DEFAULT_SINK@"); err == nil {
		dev.Muted = strings.Contains(muteOut, "yes")
	}
	return dev, nil
}

// sinkDescription extracts one sink's Description from the long form.
func sinkDescription(listing, name string) string {
	lines := strings.Split(listing, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "Name: "+name {
			continue
		}
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			if strings.HasPrefix(trimmed, "Sink #") {
				break
			}
			if desc, found := strings.CutPrefix(trimmed, "Description: "); found {
				return desc
			}
		}
	}
	return ""
}

// meanPercent averages the per-channel percentages in a pactl volume
// listing ("front-left: 45054 / 69% / ...").
func meanPercent(listing string) float64 {
	matches := percentRe.FindAllStringSubmatch(listing, -1)
	if len(matches) == 0 {
		return 0
	}
	var sum float64
	for _, m := range matches {
		value, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		sum += value
	}
	return sum / float64(len(matches))
}

// SetVolume clamps and applies the default sink volume.
func (p *Pactl) SetVolume(ctx context.Context, percent float64) error {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.run(ctx, "set-sink-volume", "@DEFAULT_SINK@",
		strconv.FormatFloat(percent, 'f', -1, 64)+"%")
	return err
}

// SetMuted sets the default sink's mute state.
func (p *Pactl) SetMuted(ctx context.Context, muted bool) error {
	state := "0"
	if muted {
		state = "1"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.run(ctx, "set-sink-mute", "@DEFAULT_SINK@", state)
	return err
}

// DefaultSource queries the default input's volume and mute.
func (p *Pactl) DefaultSource(ctx context.Context) (Device, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	name, err := p.run(ctx, "get-default-source")
	if err != nil {
		return Device{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Device{}, errors.New("pulse: no default source")
	}
	dev := Device{Name: name}
	if volOut, err := p.run(ctx, "get-source-volume", "@DEFAULT_SOURCE@"); err == nil {
		dev.Volume = meanPercent(volOut)
	}
	if muteOut, err := p.run(ctx, "get-source-mute", "@DEFAULT_SOURCE@"); err == nil {
		dev.Muted = strings.Contains(muteOut, "yes")
	}
	return dev, nil
}

// SetSourceMuted sets the default input's mute state.
func (p *Pactl) SetSourceMuted(ctx context.Context, muted bool) error {
	state := "0"
	if muted {
		state = "1"
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.run(ctx, "set-source-mute", "@DEFAULT_SOURCE@", state)
	return err
}

// Subscribe tails `pactl subscribe` and ticks per event line.
func (p *Pactl) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	cmd := exec.CommandContext(ctx, "pactl", "subscribe")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("pulse: subscribe pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("pulse: subscribe: %w", err)
	}
	ticks := make(chan struct{}, 1)
	go func() {
		defer close(ticks)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case ticks <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()
	stop := func() { _ = cmd.Process.Kill() }
	return ticks, stop, nil
}

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stubbedev/wayle/internal/cli"
	"github.com/stubbedev/wayle/internal/rustparse"
	"github.com/stubbedev/wayle/service/pulse"
)

// audioCommand is wayle/src/cli/audio/commands.rs.
func audioCommand() *cli.Command {
	level := func() []*cli.Arg {
		return []*cli.Arg{{ID: "level", ValueName: "LEVEL", Help: "Volume level (0-100) or relative adjustment (+5, -10)"}}
	}
	return &cli.Command{
		Name:  "audio",
		About: "Audio control commands",
		Subcommands: []*cli.Command{
			{Name: "output-volume", About: "Get or set output volume level", AllowHyphenValues: true, Args: level(), Run: withAudio(outputSide.volume)},
			{Name: "output-mute", About: "Toggle output mute state", Run: withAudio(outputSide.toggleMute)},
			{Name: "input-volume", About: "Get or set input volume level", AllowHyphenValues: true, Args: level(), Run: withAudio(inputSide.volume)},
			{Name: "input-mute", About: "Toggle input mute state", Run: withAudio(inputSide.toggleMute)},
			{Name: "sinks", About: "List available audio sinks (outputs)", Run: withAudio(outputSide.list)},
			{Name: "sources", About: "List available audio sources (inputs)", Run: withAudio(inputSide.list)},
			{Name: "status", About: "Show current audio status", Run: withAudio(audioStatus)},
		},
	}
}

func withAudio(run func(*cli.Matches, *daemonProxy) error) func(*cli.Matches) error {
	return withDaemon("Audio", pulse.ServiceName, pulse.ServicePath, pulse.Interface, run)
}

// audioSide is one direction of the Audio1 interface: the output and
// input commands are the same code over different member names and
// operation texts (output_volume.rs / input_volume.rs, *_mute.rs,
// sinks.rs / sources.rs).
type audioSide struct {
	member      string // "Output" / "Input"
	opQualifier string // "" / "input " in the get texts
	listMember  string // "Sinks" / "Sources"
	listOp      string
	defaultProp string
	defaultOp   string
	empty       string
	heading     string
}

var (
	outputSide = audioSide{
		member: "Output", listMember: "Sinks", listOp: "list sinks",
		defaultProp: "DefaultSink", defaultOp: "get default sink",
		empty: "No audio sinks found", heading: "Audio outputs:",
	}
	inputSide = audioSide{
		member: "Input", opQualifier: "input ", listMember: "Sources", listOp: "list sources",
		defaultProp: "DefaultSource", defaultOp: "get default source",
		empty: "No audio sources found", heading: "Audio inputs:",
	}
)

func (s audioSide) readVolume(p *daemonProxy) (float64, bool, error) {
	var volume float64
	var muted bool
	if err := p.prop("get "+s.opQualifier+"volume", s.member+"Volume", &volume); err != nil {
		return 0, false, err
	}
	if err := p.prop("get "+s.opQualifier+"mute state", s.member+"Muted", &muted); err != nil {
		return 0, false, err
	}
	return volume, muted, nil
}

func printVolume(m *cli.Matches, volume float64, muted bool) {
	suffix := ""
	if muted {
		suffix = " (muted)"
	}
	fmt.Fprintf(m.Stdout(), "Volume: %s%%%s\n", strconv.FormatFloat(volume, 'f', 0, 64), suffix)
}

// volume shows or sets the level: "+N"/"-N" adjust, a bare N sets.
func (s audioSide) volume(m *cli.Matches, p *daemonProxy) error {
	value, set := cli.Value[string](m, "level")
	if !set {
		volume, muted, err := s.readVolume(p)
		if err != nil {
			return err
		}
		printVolume(m, volume, muted)
		return nil
	}
	var muted bool
	if err := p.prop("get "+s.opQualifier+"mute state", s.member+"Muted", &muted); err != nil {
		return err
	}
	var next float64
	switch {
	case strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-"):
		delta, err := rustparse.Float(value[1:])
		if err != nil {
			return cliMessage("Invalid volume delta: " + value)
		}
		if value[0] == '-' {
			delta = -delta
		}
		if err := p.call("adjust "+s.opQualifier+"volume", "Adjust"+s.member+"Volume", []any{&next}, delta); err != nil {
			return err
		}
	default:
		level, err := rustparse.Float(value)
		if err != nil {
			return cliMessage("Invalid volume level: " + value)
		}
		if err := p.call("set "+s.opQualifier+"volume", "Set"+s.member+"Volume", []any{&next}, level); err != nil {
			return err
		}
	}
	printVolume(m, next, muted)
	return nil
}

func (s audioSide) toggleMute(m *cli.Matches, p *daemonProxy) error {
	var muted bool
	if err := p.call("toggle "+s.opQualifier+"mute", "Toggle"+s.member+"Mute", []any{&muted}); err != nil {
		return err
	}
	if muted {
		fmt.Fprintln(m.Stdout(), "Muted")
	} else {
		fmt.Fprintln(m.Stdout(), "Unmuted")
	}
	return nil
}

func (s audioSide) list(m *cli.Matches, p *daemonProxy) error {
	var rows []pulse.DeviceEntry
	if err := p.call(s.listOp, "List"+s.listMember, []any{&rows}); err != nil {
		return err
	}
	var def string
	if err := p.prop(s.defaultOp, s.defaultProp, &def); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(m.Stdout(), s.empty)
		return nil
	}
	fmt.Fprintln(m.Stdout(), s.heading)
	for _, row := range rows {
		marker := ""
		if row.Name == def {
			marker = " *"
		}
		fmt.Fprintf(m.Stdout(), "  %s%s\n", row.Description, marker)
	}
	return nil
}

// audioStatus is status.rs.
func audioStatus(m *cli.Matches, p *daemonProxy) error {
	volume, muted, err := outputSide.readVolume(p)
	if err != nil {
		return err
	}
	var sink string
	var sinks, sources uint32
	if err := p.prop("get default sink", "DefaultSink", &sink); err != nil {
		return err
	}
	if err := p.prop("get sink count", "SinkCount", &sinks); err != nil {
		return err
	}
	if err := p.prop("get source count", "SourceCount", &sources); err != nil {
		return err
	}
	printVolume(m, volume, muted)
	fmt.Fprintf(m.Stdout(), "Default output: %s\n", sink)
	fmt.Fprintf(m.Stdout(), "Outputs: %d, Inputs: %d\n", sinks, sources)
	return nil
}

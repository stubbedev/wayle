package config

// CavaConfig is ported from crates/wayle-config/src/schemas/modules/cava/mod.rs.
//
// Audio frequency bars visualising the output stream.
type CavaConfig struct {
	// Number of frequency bars.
	Bars BarCount `cfg:"bars"`
	// Visualization update rate in frames per second.
	Framerate Framerate `cfg:"framerate"`
	// Stereo channel visualization (splits bars between left and right).
	Stereo bool `cfg:"stereo"`
	// Noise reduction filter strength.
	NoiseReduction NormalizedF64 `cfg:"noise-reduction"`
	// Monstercat-style smoothing across adjacent bars (0.0 = off).
	Monstercat float64 `cfg:"monstercat"`
	// Wave-style smoothing (0 = off).
	Waves uint32 `cfg:"waves"`
	// Low frequency cutoff in Hz.
	LowCutoff FrequencyHz `cfg:"low-cutoff"`
	// High frequency cutoff in Hz.
	HighCutoff FrequencyHz `cfg:"high-cutoff"`
	// Audio capture backend.
	Input CavaInput `cfg:"input"`
	// Audio source identifier ("auto" for automatic selection).
	Source string `cfg:"source"`
	// Visualization rendering style.
	Style CavaStyle `cfg:"style"`
	// Bar growth direction.
	Direction CavaDirection `cfg:"direction"`
	// Bar color.
	Color ColorValue `cfg:"color"`
	// Module background color.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Width of each frequency bar in pixels.
	BarWidth uint32 `cfg:"bar-width"`
	// Gap between frequency bars in pixels.
	BarGap uint32 `cfg:"bar-gap"`
	// Padding at the ends of the visualizer. Accepts a scale multiplier or
	// pixels (e.g. `"8px"`).
	InternalPadding Size `cfg:"internal-padding"`
	// Display border around the visualizer.
	BorderShow bool `cfg:"border-show"`
	// Border color.
	BorderColor ColorValue `cfg:"border-color"`
	// Action on left click.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsCava returns the schema defaults.
func DefaultsCava() CavaConfig {
	return CavaConfig{
		Bars:            20,
		Framerate:       60,
		Stereo:          false,
		NoiseReduction:  0.65,
		Monstercat:      0,
		Waves:           0,
		LowCutoff:       50,
		HighCutoff:      17000,
		Input:           CavaInputPipeWire,
		Source:          "auto",
		Style:           CavaStyleBars,
		Direction:       CavaDirectionNormal,
		Color:           mustColor("accent"),
		ButtonBgColor:   mustColor("bg-surface-elevated"),
		BarWidth:        6,
		BarGap:          1,
		InternalPadding: Size{Value: 0.5, Unit: SizeMultiplier},
		BorderShow:      false,
		BorderColor:     mustColor("border-accent"),
		LeftClick:       ClickAction{},
		RightClick:      ClickAction{},
		MiddleClick:     ClickAction{},
		ScrollUp:        ClickAction{},
		ScrollDown:      ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c CavaConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// CavaStyle is ported from crates/wayle-config/src/schemas/modules/cava/types.rs.
//
// Visualization rendering style.
type CavaStyle string

// CavaStyle values.
const (
	// Rectangular frequency bars.
	CavaStyleBars CavaStyle = "bars"
	// Smooth curve connecting bar peaks.
	CavaStyleWave CavaStyle = "wave"
	// Bars with floating peak indicators that decay over time.
	CavaStylePeaks CavaStyle = "peaks"
)

var _ = registerEnum(CavaStyleBars, CavaStyleWave, CavaStylePeaks)

// CavaDirection is ported from crates/wayle-config/src/schemas/modules/cava/types.rs.
//
// Bar growth direction relative to the bar's attached screen edge.
type CavaDirection string

// CavaDirection values.
const (
	// Bars grow away from the attached edge.
	CavaDirectionNormal CavaDirection = "normal"
	// Bars grow toward the attached edge.
	CavaDirectionReverse CavaDirection = "reverse"
	// Bars grow symmetrically from center.
	CavaDirectionMirror CavaDirection = "mirror"
)

var _ = registerEnum(CavaDirectionNormal, CavaDirectionReverse, CavaDirectionMirror)

// CavaInput is ported from crates/wayle-config/src/schemas/modules/cava/types.rs.
//
// Audio capture backend.
type CavaInput string

// CavaInput values.
const (
	// PipeWire multimedia server.
	CavaInputPipeWire CavaInput = "pipe-wire"
	// PulseAudio sound server.
	CavaInputPulse CavaInput = "pulse"
	// Advanced Linux Sound Architecture.
	CavaInputAlsa CavaInput = "alsa"
	// JACK Audio Connection Kit.
	CavaInputJack CavaInput = "jack"
	// Named pipe (FIFO) input.
	CavaInputFifo CavaInput = "fifo"
	// PortAudio cross-platform library.
	CavaInputPortAudio CavaInput = "port-audio"
	// sndio audio subsystem (BSD).
	CavaInputSndio CavaInput = "sndio"
	// Open Sound System (legacy).
	CavaInputOss CavaInput = "oss"
	// Shared memory input.
	CavaInputShmem CavaInput = "shmem"
	// Windows audio capture (WASAPI).
	CavaInputWinscap CavaInput = "winscap"
)

var _ = registerEnum(CavaInputPipeWire, CavaInputPulse, CavaInputAlsa, CavaInputJack, CavaInputFifo, CavaInputPortAudio, CavaInputSndio, CavaInputOss, CavaInputShmem, CavaInputWinscap)

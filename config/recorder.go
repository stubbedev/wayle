package config

// RecorderConfig is ported from crates/wayle-config/src/schemas/modules/recorder/mod.rs.
//
// Native screen recorder backed by a GStreamer pipeline.
//
// Click the bar button to start/stop; the dropdown exposes the recording
// options below. Controllable from the CLI / RPC socket:
// `wayle recorder start|stop|toggle|pause|status`.
type RecorderConfig struct {
	// Icon when idle (not recording).
	IconIdle string `cfg:"icon-idle"`
	// Icon while recording.
	IconRecording string `cfg:"icon-recording"`
	// Icon while recording is paused.
	IconPaused string `cfg:"icon-paused"`
	// Format string for the label.
	//
	// ## Placeholders
	//
	// - `{{ state }}` - Recorder state text (Idle, Recording, Paused)
	// - `{{ elapsed }}` - Elapsed recording time (e.g., "01:23", "--" when idle)
	Format string `cfg:"format"`
	// Capture the microphone in the recording.
	Microphone bool `cfg:"microphone"`
	// Microphone PipeWire/PulseAudio source name. Empty uses the default source.
	MicrophoneDevice string `cfg:"microphone-device"`
	// Capture desktop (system) audio in the recording.
	SystemAudio bool `cfg:"system-audio"`
	// Capture framerate in frames per second.
	Framerate uint32 `cfg:"framerate"`
	// Overlay a webcam picture-in-picture frame into the recording.
	WebcamEnabled bool `cfg:"webcam-enabled"`
	// Webcam V4L2 device path. Empty auto-selects the first camera.
	WebcamDevice string `cfg:"webcam-device"`
	// Webcam frame horizontal position, as a percentage of the free
	// horizontal space (0 = flush left, 100 = flush right). Stored relative so
	// it stays correct across monitors of different resolutions.
	WebcamX Percentage `cfg:"webcam-x"`
	// Webcam frame vertical position, as a percentage of the free vertical
	// space (0 = flush top, 100 = flush bottom). Stored relative so it stays
	// correct across monitors of different resolutions.
	WebcamY Percentage `cfg:"webcam-y"`
	// Webcam frame width as a percentage of the recording width.
	WebcamSize Percentage `cfg:"webcam-size"`
	// Output directory for recordings. Empty uses the XDG Videos directory.
	OutputDirectory string `cfg:"output-directory"`
	// Container format / codec preset.
	OutputFormat RecorderFormat `cfg:"output-format"`
	// Draw the mouse cursor in the recording.
	ShowCursor bool `cfg:"show-cursor"`
	// Delay between choosing the capture source and the recording actually
	// starting, in milliseconds. Gives on-screen UI (the start toast) time to
	// clear so it isn't captured in the video.
	StartDelayMs uint32 `cfg:"start-delay-ms"`
	// Display border around button.
	BorderShow bool `cfg:"border-show"`
	// Border color token.
	BorderColor ColorValue `cfg:"border-color"`
	// Display module icon.
	IconShow bool `cfg:"icon-show"`
	// Icon foreground color. Auto selects based on variant for contrast.
	IconColor ColorValue `cfg:"icon-color"`
	// Icon container background color token.
	IconBgColor ColorValue `cfg:"icon-bg-color"`
	// Display label.
	LabelShow bool `cfg:"label-show"`
	// Label text color token.
	LabelColor ColorValue `cfg:"label-color"`
	// Max label characters before truncation with ellipsis. Set to 0 to disable.
	LabelMaxLength uint32 `cfg:"label-max-length"`
	// Button background color token.
	ButtonBgColor ColorValue `cfg:"button-bg-color"`
	// Action on left click. Default toggles recording.
	LeftClick ClickAction `cfg:"left-click"`
	// Action on right click. Default opens the recorder dropdown.
	RightClick ClickAction `cfg:"right-click"`
	// Action on middle click.
	MiddleClick ClickAction `cfg:"middle-click"`
	// Action on scroll up.
	ScrollUp ClickAction `cfg:"scroll-up"`
	// Action on scroll down.
	ScrollDown ClickAction `cfg:"scroll-down"`
}

// DefaultsRecorder returns the schema defaults.
func DefaultsRecorder() RecorderConfig {
	return RecorderConfig{
		IconIdle:         "ld-video-symbolic",
		IconRecording:    "ld-circle-dot-symbolic",
		IconPaused:       "ld-circle-pause-symbolic",
		Format:           "{{ elapsed }}",
		Microphone:       false,
		MicrophoneDevice: "",
		SystemAudio:      true,
		Framerate:        60,
		WebcamEnabled:    false,
		WebcamDevice:     "",
		WebcamX:          100,
		WebcamY:          100,
		WebcamSize:       20,
		OutputDirectory:  "",
		OutputFormat:     RecorderFormatMkv,
		ShowCursor:       true,
		StartDelayMs:     1400,
		BorderShow:       false,
		BorderColor:      mustColor("red"),
		IconShow:         true,
		IconColor:        mustColor("auto"),
		IconBgColor:      mustColor("red"),
		LabelShow:        true,
		LabelColor:       mustColor("red"),
		LabelMaxLength:   0,
		ButtonBgColor:    mustColor("bg-surface-elevated"),
		LeftClick:        ParseClickAction("wayle recorder toggle"),
		RightClick:       ParseClickAction("dropdown:recorder"),
		MiddleClick:      ClickAction{},
		ScrollUp:         ClickAction{},
		ScrollDown:       ClickAction{},
	}
}

// Clicks returns the five input bindings.
func (c RecorderConfig) Clicks() ClickConfig {
	return ClickConfig{c.LeftClick, c.RightClick, c.MiddleClick, c.ScrollUp, c.ScrollDown}
}

// RecorderFormat is ported from crates/wayle-config/src/schemas/modules/recorder/types.rs.
//
// Container format / codec preset for recordings.
type RecorderFormat string

// RecorderFormat values.
const (
	// H.264 in an MP4 container.
	RecorderFormatMp4 RecorderFormat = "mp4"
	// H.264 in a Matroska container (resilient to crashes).
	RecorderFormatMkv RecorderFormat = "mkv"
	// VP9 in a WebM container.
	RecorderFormatWebm RecorderFormat = "webm"
)

var _ = registerEnum(RecorderFormatMp4, RecorderFormatMkv, RecorderFormatWebm)

package recorder

import (
	"fmt"
	"math/bits"
	"strings"
)

// Encoding constants (pipeline.rs).
const (
	audioBitrate    = 128_000
	x264Quality     = 18
	vp9CQLevel      = 20
	hwH264QP        = 20
	edgeMarginPct   = 5
	fallbackScreenW = 1920
	fallbackScreenH = 1080
	webcamMaxFPS    = 30
	webcamMinW      = 80
	webcamMinH      = 60
)

// Format is the container/codec preset (options.rs's OutputFormat).
type Format int

// Formats.
const (
	FormatMP4 Format = iota
	FormatMKV
	FormatWebM
)

// Extension is the file extension.
func (f Format) Extension() string {
	switch f {
	case FormatMKV:
		return "mkv"
	case FormatWebM:
		return "webm"
	}
	return "mp4"
}

// Webcam is the picture-in-picture overlay (WebcamOptions).
type Webcam struct {
	// Device is the V4L2 node; "" auto-selects.
	Device string
	// X and Y place the frame in the free space, 0-100.
	X, Y uint32
	// Size is the frame width as a percentage of the recording, 1-100.
	Size uint32
}

// Options is one recording's settings (RecordOptions).
type Options struct {
	OutputPath string
	Format     Format
	Framerate  uint32
	ShowCursor bool
	// Microphone captures MicrophoneDevice ("" the default source);
	// SystemAudio the default sink's monitor.
	Microphone       bool
	MicrophoneDevice string
	SystemAudio      bool
	// Webcam is the overlay, nil for none.
	Webcam *Webcam
}

// Cast is the negotiated screen: the PipeWire remote fd, the node, and
// the size (zero when unknown).
type Cast struct {
	FD            uintptr
	Node          uint32
	Width, Height int32
}

// Built is a pipeline description and whether its video encoder is
// hardware (pipeline.rs's Built).
type Built struct {
	Description string
	Hardware    bool
}

// BuildPipeline is pipeline.rs's build_with: the portal's pipewiresrc,
// optionally composited with a letterboxed webcam, encoded and muxed,
// with system audio and the microphone mixed into one track. A
// hardware H.264 encoder is used when allowHardware and hasFactory
// reports its element; threads is the encoder thread count.
func BuildPipeline(opts Options, cast Cast, allowHardware bool, threads int, hasFactory func(string) bool) Built {
	fps := max(opts.Framerate, 1)
	screenW, screenH := cast.Width, cast.Height
	if screenW <= 0 || screenH <= 0 {
		screenW, screenH = fallbackScreenW, fallbackScreenH
	}
	path := quote(opts.OutputPath)
	threads = max(threads, 1)
	encoder, hardware := videoEncoder(opts.Format, fps, threads, allowHardware, hasFactory)
	parser := videoParser(opts.Format)
	audioEncoder := fmt.Sprintf("opusenc bitrate=%d", audioBitrate)
	mux := muxer(opts.Format)
	source := fmt.Sprintf("pipewiresrc fd=%d path=%d do-timestamp=true", cast.FD, cast.Node)

	var b strings.Builder
	if cam := opts.Webcam; cam != nil {
		camW := max(int32(float64(screenW)*float64(min(max(cam.Size, 1), 100))/100), webcamMinW)
		camH := max(camW*9/16, webcamMinH)
		x, y := webcamXY(cam.X, cam.Y, screenW, screenH, camW, camH)
		device := ""
		if cam.Device != "" {
			device = " device=" + quote(cam.Device)
		}
		fmt.Fprintf(&b, "compositor name=comp background=black "+
			"sink_1::xpos=%d sink_1::ypos=%d sink_1::width=%d sink_1::height=%d "+
			"! videoconvert n-threads=0 ! queue leaky=downstream ! %s ! queue ! %s%s name=mux ! filesink location=%s "+
			"%s ! videorate ! video/x-raw,framerate=%d/1 ! videoconvert n-threads=0 ! queue ! comp.sink_0 "+
			"v4l2src%s do-timestamp=true ! queue leaky=downstream max-size-buffers=4 ! videorate max-rate=%d ! videoconvert n-threads=0 ! videoscale add-borders=true ! video/x-raw,width=%d,height=%d ! queue leaky=downstream max-size-buffers=4 ! comp.sink_1 ",
			x, y, camW, camH, encoder, parser, mux, path, source, fps, device, min(fps, webcamMaxFPS), camW, camH)
	} else {
		fmt.Fprintf(&b, "%s ! videorate ! video/x-raw,framerate=%d/1 "+
			"! videoconvert n-threads=0 ! queue leaky=downstream ! %s ! queue ! %s%s name=mux ! filesink location=%s ",
			source, fps, encoder, parser, mux, path)
	}

	var audio []string
	if opts.SystemAudio {
		audio = append(audio, "pulsesrc device=@DEFAULT_MONITOR@")
	}
	if opts.Microphone {
		if opts.MicrophoneDevice == "" {
			audio = append(audio, "pulsesrc")
		} else {
			audio = append(audio, "pulsesrc device="+quote(opts.MicrophoneDevice))
		}
	}
	if len(audio) > 1 {
		fmt.Fprintf(&b, "audiomixer name=amix ! audioconvert ! audioresample ! %s ! queue ! mux. ", audioEncoder)
		for _, src := range audio {
			fmt.Fprintf(&b, "%s ! queue ! audioconvert ! audioresample ! amix. ", src)
		}
	} else {
		for _, src := range audio {
			fmt.Fprintf(&b, "%s ! queue ! audioconvert ! audioresample ! %s ! queue ! mux. ", src, audioEncoder)
		}
	}
	return Built{Description: strings.TrimRight(b.String(), " "), Hardware: hardware}
}

// quote is pipeline.rs's quote: a double-quoted gst-launch value.
func quote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

// videoEncoder is video_encoder: constant-quality H.264 (VA-API, then
// NVENC, then x264) or software VP9, a keyframe every two seconds.
func videoEncoder(format Format, fps uint32, threads int, allowHardware bool, hasFactory func(string) bool) (string, bool) {
	keyint := max(fps*2, 1)
	if format == FormatWebM {
		tiles := min(log2Floor(threads), 6)
		return fmt.Sprintf("vp9enc threads=%d tile-columns=%d deadline=1 cpu-used=4 lag-in-frames=0 end-usage=cq cq-level=%d target-bitrate=25000 keyframe-max-dist=%d",
			threads, tiles, vp9CQLevel, keyint), false
	}
	if allowHardware {
		if hasFactory("vah264enc") {
			return fmt.Sprintf("vah264enc rate-control=cqp qpi=%d qpp=%d qpb=%d key-int-max=%d", hwH264QP, hwH264QP, hwH264QP, keyint), true
		}
		if hasFactory("nvh264enc") {
			return fmt.Sprintf("nvh264enc preset=hq rc-mode=constqp qp-const=%d gop-size=%d", hwH264QP, keyint), true
		}
	}
	return fmt.Sprintf("x264enc threads=%d speed-preset=veryfast tune=zerolatency pass=qual quantizer=%d key-int-max=%d",
		threads, x264Quality, keyint), false
}

// videoParser is video_parser.
func videoParser(format Format) string {
	if format == FormatWebM {
		return ""
	}
	return "h264parse ! "
}

// muxer is muxer.
func muxer(format Format) string {
	switch format {
	case FormatMKV:
		return "matroskamux"
	case FormatWebM:
		return "webmmux"
	}
	return "mp4mux"
}

// log2Floor is log2_floor (0 for n <= 1).
func log2Floor(n int) int {
	if n <= 1 {
		return 0
	}
	return bits.Len(uint(n)) - 1
}

// WebcamMargin is webcam_margin: the uniform inset, a percentage of
// the shorter side. The dropdown's position preview uses it too.
func WebcamMargin(w, h int32) int32 { return min(w, h) * edgeMarginPct / 100 }

// webcamXY is webcam_xy: the frame's top-left inside the inset travel.
func webcamXY(xPct, yPct uint32, screenW, screenH, camW, camH int32) (int32, int32) {
	margin := WebcamMargin(screenW, screenH)
	travelW := max(screenW-camW-2*margin, 0)
	travelH := max(screenH-camH-2*margin, 0)
	return margin + travelW*int32(min(xPct, 100))/100, margin + travelH*int32(min(yPct, 100))/100
}

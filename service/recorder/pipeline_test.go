package recorder

import (
	"strings"
	"testing"
)

func noFactories(string) bool { return false }

func TestBuildPipelineScreenOnly(t *testing.T) {
	got := BuildPipeline(Options{OutputPath: "/v/a b.mkv", Format: FormatMKV, Framerate: 60},
		Cast{FD: 7, Node: 42, Width: 2560, Height: 1440}, true, 8, noFactories)
	want := `pipewiresrc fd=7 path=42 do-timestamp=true ! videorate ! video/x-raw,framerate=60/1 ` +
		`! videoconvert n-threads=0 ! queue leaky=downstream ! x264enc threads=8 speed-preset=veryfast tune=zerolatency pass=qual quantizer=18 key-int-max=120 ` +
		`! queue ! h264parse ! matroskamux name=mux ! filesink location="/v/a b.mkv"`
	if got.Description != want || got.Hardware {
		t.Errorf("description =\n%s\nwant\n%s", got.Description, want)
	}
}

func TestBuildPipelinePrefersHardware(t *testing.T) {
	has := func(name string) bool { return name == "nvh264enc" }
	got := BuildPipeline(Options{OutputPath: "/v/x.mp4", Format: FormatMP4, Framerate: 30}, Cast{}, true, 4, has)
	if !got.Hardware || !strings.Contains(got.Description, "nvh264enc preset=hq rc-mode=constqp qp-const=20 gop-size=60") ||
		!strings.Contains(got.Description, "mp4mux") {
		t.Errorf("nvenc: %+v", got)
	}
	vaapi := func(name string) bool { return name == "vah264enc" || name == "nvh264enc" }
	if got := BuildPipeline(Options{Format: FormatMP4}, Cast{}, true, 4, vaapi); !strings.Contains(got.Description, "vah264enc") {
		t.Error("VA-API is not preferred over NVENC")
	}
	// The software retry ignores the hardware encoder.
	if got := BuildPipeline(Options{Format: FormatMP4}, Cast{}, false, 4, vaapi); got.Hardware || !strings.Contains(got.Description, "x264enc") {
		t.Errorf("software path: %+v", got)
	}
	// VP9 is always software, without a parser.
	got = BuildPipeline(Options{Format: FormatWebM, Framerate: 30}, Cast{}, true, 16, vaapi)
	if got.Hardware || !strings.Contains(got.Description, "vp9enc threads=16 tile-columns=4") ||
		strings.Contains(got.Description, "h264parse") || !strings.Contains(got.Description, "webmmux") {
		t.Errorf("webm: %+v", got)
	}
}

func TestBuildPipelineAudio(t *testing.T) {
	one := BuildPipeline(Options{Format: FormatMKV, Microphone: true}, Cast{}, false, 1, noFactories).Description
	if !strings.HasSuffix(one, `pulsesrc ! queue ! audioconvert ! audioresample ! opusenc bitrate=128000 ! queue ! mux.`) {
		t.Errorf("default mic: %s", one)
	}
	named := BuildPipeline(Options{Format: FormatMKV, Microphone: true, MicrophoneDevice: `m"ic`}, Cast{}, false, 1, noFactories).Description
	if !strings.Contains(named, `pulsesrc device="m\"ic" ! queue`) {
		t.Errorf("named mic: %s", named)
	}
	both := BuildPipeline(Options{Format: FormatMKV, Microphone: true, SystemAudio: true}, Cast{}, false, 1, noFactories).Description
	if !strings.Contains(both, "audiomixer name=amix ! audioconvert ! audioresample ! opusenc bitrate=128000 ! queue ! mux. "+
		"pulsesrc device=@DEFAULT_MONITOR@ ! queue ! audioconvert ! audioresample ! amix. "+
		"pulsesrc ! queue ! audioconvert ! audioresample ! amix.") {
		t.Errorf("mixed: %s", both)
	}
	none := BuildPipeline(Options{Format: FormatMKV}, Cast{}, false, 1, noFactories).Description
	if strings.Contains(none, "pulsesrc") {
		t.Errorf("no audio: %s", none)
	}
}

func TestBuildPipelineWebcam(t *testing.T) {
	got := BuildPipeline(Options{Format: FormatMKV, Framerate: 60, Webcam: &Webcam{Device: "/dev/video2", X: 100, Y: 100, Size: 20}},
		Cast{Width: 1920, Height: 1080}, false, 1, noFactories).Description
	// 20% of 1920 is 384x216; the 54px inset leaves 1428x756 of travel.
	for _, want := range []string{
		"compositor name=comp background=black sink_1::xpos=1482 sink_1::ypos=810 sink_1::width=384 sink_1::height=216",
		`v4l2src device="/dev/video2" do-timestamp=true`,
		"videorate max-rate=30",
		"video/x-raw,width=384,height=216",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	// A tiny size floors at 80x60 and an unknown screen falls back to
	// 1080p; an empty device auto-selects.
	small := BuildPipeline(Options{Format: FormatMKV, Webcam: &Webcam{Size: 0}}, Cast{}, false, 1, noFactories).Description
	if !strings.Contains(small, "sink_1::width=80 sink_1::height=60") || !strings.Contains(small, "v4l2src do-timestamp=true") {
		t.Errorf("small webcam: %s", small)
	}
}

func TestWebcamXYAndMargin(t *testing.T) {
	if m := WebcamMargin(1920, 1080); m != 54 {
		t.Errorf("margin = %d", m)
	}
	if x, y := webcamXY(0, 0, 1920, 1080, 384, 216); x != 54 || y != 54 {
		t.Errorf("top-left = %d,%d", x, y)
	}
	if x, y := webcamXY(250, 50, 1920, 1080, 384, 216); x != 1482 || y != 432 {
		t.Errorf("clamped = %d,%d", x, y)
	}
	// A frame larger than the screen has no travel.
	if x, _ := webcamXY(100, 0, 100, 100, 500, 50); x != 5 {
		t.Errorf("no travel x = %d", x)
	}
}

func TestQuoteAndHelpers(t *testing.T) {
	if got := quote(`a\b"c`); got != `"a\\b\"c"` {
		t.Errorf("quote = %s", got)
	}
	for n, want := range map[int]int{0: 0, 1: 0, 2: 1, 7: 2, 8: 3, 64: 6} {
		if got := log2Floor(n); got != want {
			t.Errorf("log2Floor(%d) = %d", n, got)
		}
	}
	for f, ext := range map[Format]string{FormatMP4: "mp4", FormatMKV: "mkv", FormatWebM: "webm"} {
		if f.Extension() != ext {
			t.Errorf("%v extension = %q", f, f.Extension())
		}
	}
}

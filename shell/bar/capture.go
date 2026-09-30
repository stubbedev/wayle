package bar

import (
	"image"
	"log"
	"slices"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/app"
	"github.com/stubbedev/gelm/render"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/hyprland"
	"github.com/stubbedev/wayle/shell/colorpicker"
	"github.com/stubbedev/wayle/shell/regionoverlay"
	"github.com/stubbedev/wayle/shell/screenshot"
	"github.com/stubbedev/wayle/shell/sharepicker"
	"github.com/stubbedev/wayle/styling"
)

// overlayLabelPx is the region overlay's size-label face size; the
// color picker shapes its readout from the same face.
const overlayLabelPx = 14

// captureServices is the shell's capture side: the region overlay, the
// color picker, the screenshot host behind com.wayle.Screenshot1, and
// the share picker behind com.wayle.SharePicker1
// (bootstrap's screenshot and share picker service starts).
type captureServices struct {
	host    *screenshot.Host
	release []func()
}

// startCapture builds the overlays and the host on the application and
// serves the host on the session bus. A bus failure leaves the
// in-process builtin working.
func startCapture(application *app.Application, outputs func() []*app.Output, cfg *config.Config, palette *styling.Palette, hypr *hyprland.Connection, sans render.Font, labelPx float64) *captureServices {
	var font render.Font
	if face, err := app.FontWeighted(cfg.General.FontMono, overlayLabelPx, 700, false); err == nil {
		font = app.FontFallback(face)
	} else {
		log.Printf("screenshot: overlay font %q: %v", cfg.General.FontMono, err)
	}
	region := regionoverlay.New(application, outputs, regionoverlay.Style{Accent: palette.Primary, Font: font})
	monitors := func() []regionoverlay.Monitor {
		outs := outputs()
		mons := make([]regionoverlay.Monitor, len(outs))
		for i, o := range outs {
			mons[i] = regionoverlay.MonitorOf(o)
		}
		return mons
	}
	s := &captureServices{host: &screenshot.Host{
		Config:   cfg.Screenshot,
		Monitors: monitors,
		Region:   region,
		Picker:   colorpicker.New(application, outputs, font, colorpicker.HistoryPath()),
		Focus:    screenshot.CompositorFocus{Hyprland: hypr},
		Copy:     clipboardCopier(application),
	}}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("screenshot: session bus: %v", err)
		return s
	}
	s.release = append(s.release, func() { _ = conn.Close() })
	if release, err := screenshot.NewDaemon(s.host).Export(conn); err == nil {
		s.release = append(s.release, release)
	} else {
		log.Printf("screenshot: daemon: %v", err)
	}
	picker := sharepicker.New(application, cfg.SharePicker, sharepicker.Style{Font: sans, LabelPx: labelPx, Palette: palette}, region)
	if release, err := sharepicker.NewDaemon(picker).Export(conn); err == nil {
		s.release = append(s.release, release)
	} else {
		log.Printf("share picker: daemon: %v", err)
	}
	return s
}

// trigger is the `wayle screenshot ...` builtin: a fire-and-forget
// capture whose reply nobody awaits, like the Rust trigger's dropped
// oneshot.
func (s *captureServices) trigger(mode, target string) {
	go func() {
		if _, err := s.host.Capture(mode, target); err != nil {
			log.Printf("screenshot: %v", err)
		}
	}()
}

// close drops the bus name and connection.
func (s *captureServices) close() {
	for _, release := range slices.Backward(s.release) {
		release()
	}
}

// clipboardCopier copies an image through the application's clipboard
// (RunWith installs it). The Wayland calls run on the loop.
func clipboardCopier(application *app.Application) func(image.Image) error {
	return func(img image.Image) error {
		errc := make(chan error, 1)
		application.Invoke(func() {
			clip := application.Clipboard()
			if clip == nil {
				errc <- app.ErrClipboardUnavailable
				return
			}
			errc <- clip.WriteImage(img)
		})
		return <-errc
	}
}

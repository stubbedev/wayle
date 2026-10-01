package bar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/wayle/config"
)

func TestBarsAffected(t *testing.T) {
	base := config.Defaults()
	same := config.Defaults()
	if barsAffected(base, same) {
		t.Error("identical configs rebuild the bars")
	}
	capture := config.Defaults()
	capture.Recorder.Microphone = !base.Recorder.Microphone
	capture.Recorder.MicrophoneDevice = "mic0"
	capture.Recorder.WebcamX, capture.Recorder.WebcamY = 33, 66
	capture.Recorder.WebcamEnabled = !base.Recorder.WebcamEnabled
	if barsAffected(base, capture) {
		t.Error("recorder capture settings rebuild the bars")
	}
	icon := config.Defaults()
	icon.Recorder.IconIdle = "other"
	if !barsAffected(base, icon) {
		t.Error("a recorder icon change does not rebuild the bars")
	}
	loc := config.Defaults()
	loc.Bar.Location = config.LocationBottom
	if !barsAffected(base, loc) {
		t.Error("a bar change does not rebuild the bars")
	}
	if !barsAffected(nil, base) {
		t.Error("no previous config must rebuild")
	}
	// The scrubbed fields are scrubbed on copies, not the live configs.
	if capture.Recorder.MicrophoneDevice != "mic0" {
		t.Error("barsAffected mutated its argument")
	}
}

func TestConfigSetterPersists(t *testing.T) {
	dir := t.TempDir()
	svc := config.Load(dir, config.DiscardDiagnostics)
	defer svc.Close()
	set := configSetter(svc)
	set("modules.recorder.microphone-device", "mic0")
	if got := svc.Config().Recorder.MicrophoneDevice; got != "mic0" {
		t.Errorf("live value = %q", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "runtime.toml"))
	if err != nil || !strings.Contains(string(body), `microphone-device = "mic0"`) {
		t.Errorf("runtime.toml = %q (%v)", body, err)
	}
	// A bad path is logged and leaves everything as it was.
	set("modules.recorder.nope", true)
	if got := svc.Config().Recorder.MicrophoneDevice; got != "mic0" {
		t.Errorf("after a bad set = %q", got)
	}
	var nilCtx ModuleContext
	nilCtx.setConfig("modules.recorder.microphone", true) // no service: a no-op
}

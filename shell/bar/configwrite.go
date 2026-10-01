package bar

import (
	"log"
	"reflect"

	"github.com/stubbedev/wayle/config"
)

// configSetter is ModuleContext.SetConfig over a config service: the
// runtime override, then runtime.toml.
func configSetter(svc *config.Service) func(path string, value any) {
	return func(path string, value any) {
		if err := svc.SetByPath(path, value); err != nil {
			log.Printf("config: set %s: %v", path, err)
			return
		}
		if err := svc.Save(); err != nil {
			log.Printf("config: save: %v", err)
		}
	}
}

// setConfig writes through ctx.SetConfig when there is a service.
func (c ModuleContext) setConfig(path string, value any) {
	if c.SetConfig != nil {
		c.SetConfig(path, value)
	}
}

// barsAffected reports whether a reload changes anything the bars
// render. The recorder's capture settings, which its dropdown writes,
// only shape the next recording: rebuilding the bars for them would
// close the dropdown being edited.
func barsAffected(old, next *config.Config) bool {
	if old == nil || next == nil {
		return true
	}
	a, b := *old, *next
	for _, c := range []*config.Config{&a, &b} {
		r := &c.Recorder
		r.Microphone, r.MicrophoneDevice, r.SystemAudio = false, "", false
		r.WebcamEnabled, r.WebcamDevice = false, ""
		r.WebcamX, r.WebcamY = 0, 0
	}
	return !reflect.DeepEqual(a, b)
}

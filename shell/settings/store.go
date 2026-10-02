// Package settings is wayle-settings: the GUI over the config, every
// change a runtime override persisted to runtime.toml as it lands.
// Pages are data (page.go): rows name config paths, and the editor for
// each comes from the field's type (config.Field) unless the page asks
// for another.
package settings

import (
	"log"
	"reflect"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// store is the config the window edits. Every write is a runtime
// override saved at once, as the Rust app's PersistenceWatcher saves on
// every change.
type store struct{ svc *config.Service }

// value is the effective value at path, encoded; nil for an unset
// optional.
func (s store) value(path string) any {
	v, err := s.svc.GetByPath(path)
	if m, ok := config.Field(path); err != nil && ok && m.Optional {
		return nil // unset: the encoding leaves it out
	}
	if err != nil {
		log.Printf("settings: %s: %v", path, err)
	}
	return v
}

// set writes a runtime override; a value the field rejects is logged
// and changes nothing.
func (s store) set(path string, v any) error {
	if err := s.svc.SetByPath(path, v); err != nil {
		log.Printf("settings: set %s: %v", path, err)
		return err
	}
	s.save()
	return nil
}

// reset drops the runtime override at path.
func (s store) reset(path string) {
	if _, err := s.svc.ResetByPath(path); err != nil {
		log.Printf("settings: reset %s: %v", path, err)
		return
	}
	s.save()
}

// resetAll drops every runtime override (perform_reset_all).
func (s store) resetAll() {
	if err := s.svc.ResetAllRuntime(); err != nil {
		log.Printf("settings: reset-all partially failed: %v", err)
	}
}

func (s store) save() {
	if err := s.svc.Save(); err != nil {
		log.Printf("settings: cannot persist config changes: %v", err)
	}
}

// sourceInfo is what a row shows about where its value comes from
// (row/methods.rs): the badge, its color class and tooltip, and
// whether the reset button is live.
type sourceInfo struct {
	label, tooltip, class string
	badge, reset          bool
}

// sourceOf reads the row state for path. A runtime override is
// "custom" without a config value under it and "override" with one;
// a config value equal to the default shows no badge.
func sourceOf(s store, path string) sourceInfo {
	t := i18n.Settings()
	key, class := "", ""
	_, inConfig := s.svc.ConfigValue(path)
	switch s.svc.Source(path) {
	case config.SourceDefault:
		return sourceInfo{}
	case config.SourceConfig:
		key, class = "settings-source-config", "info"
		cv, _ := s.svc.ConfigValue(path)
		if dv, err := config.DefaultValue(path); err == nil && reflect.DeepEqual(cv, dv) {
			return sourceInfo{}
		}
	case config.SourceRuntime:
		key, class = "settings-source-custom", "success"
		if inConfig {
			key, class = "settings-source-override", "warning"
		}
	}
	return sourceInfo{
		label: t.Get(key), tooltip: t.Attr(key, "description"), class: class, badge: true,
		reset: s.svc.Source(path) == config.SourceRuntime,
	}
}

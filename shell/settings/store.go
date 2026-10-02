// Package settings is wayle-settings: the GUI over the config, every
// change a runtime override persisted to runtime.toml as it lands.
// Pages are data (page.go): rows name config paths, and the editor for
// each comes from the field's type (config.Field) unless the page asks
// for another.
package settings

import (
	"fmt"
	"log"
	"maps"
	"reflect"
	"strings"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
)

// store is the config the window edits. Every write is a runtime
// override saved at once, as the Rust app's PersistenceWatcher saves on
// every change. A path inside a value struct (animations.osd.enter)
// edits that field of its leaf: the leaf is what the layers hold, so
// the write is the whole leaf with the field changed (Rust's field
// projections, with_field_source and with_field_reset).
type store struct{ svc *config.Service }

// value is the effective value at path, plain; nil for an unset
// optional.
func (s store) value(path string) any {
	leaf, sub, ok := config.LeafPath(path)
	if !ok {
		log.Printf("settings: %s names no config field", path)
		return nil
	}
	v, err := s.svc.GetByPath(leaf)
	if err != nil {
		if m, ok := config.Field(path); ok && m.Optional {
			return nil // unset: the encoding leaves it out
		}
		log.Printf("settings: %s: %v", path, err)
		return nil
	}
	v = config.Plain(v)
	if sub != "" {
		v, _ = subValue(v, sub)
	}
	return v
}

// subValue is the field at sub inside a plain value struct.
func subValue(v any, sub string) (any, bool) {
	for seg := range strings.SplitSeq(sub, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[seg]; !ok {
			return nil, false
		}
	}
	return v, true
}

// withSub is v (a plain value struct, or none) with the field at sub
// set, or removed for a nil value, copied, never shared.
func withSub(v any, sub string, field any) map[string]any {
	src, _ := v.(map[string]any)
	out := make(map[string]any, len(src)+1)
	maps.Copy(out, src)
	head, rest, nested := strings.Cut(sub, ".")
	switch {
	case nested:
		out[head] = withSub(out[head], rest, field)
	case field == nil:
		delete(out, head)
	default:
		out[head] = field
	}
	return out
}

// set writes a runtime override; a value the field rejects is logged
// and changes nothing.
func (s store) set(path string, v any) error {
	leaf, sub, ok := config.LeafPath(path)
	if !ok {
		return fmt.Errorf("settings: %s names no config field", path)
	}
	if sub != "" {
		v = withSub(s.value(leaf), sub, v)
	}
	if err := s.svc.SetByPath(leaf, v); err != nil {
		log.Printf("settings: set %s: %v", path, err)
		return err
	}
	s.save()
	return nil
}

// reset drops the runtime override at path. For a field of a value
// struct it is with_field_reset: the runtime value takes the
// baseline's field, the baseline being the config's value, else the
// default; without a runtime value there is nothing to reset.
func (s store) reset(path string) {
	leaf, sub, ok := config.LeafPath(path)
	if !ok {
		return
	}
	if sub == "" {
		if _, err := s.svc.ResetByPath(path); err != nil {
			log.Printf("settings: reset %s: %v", path, err)
			return
		}
		s.save()
		return
	}
	baseline, inConfig := s.svc.ConfigValue(leaf)
	if !inConfig {
		d, _ := config.DefaultValue(leaf)
		baseline = config.Plain(d)
	}
	current, inRuntime := s.svc.RuntimeValue(leaf)
	if !inRuntime {
		return // the field already shows the baseline
	}
	field, _ := subValue(baseline, sub)
	if err := s.svc.SetByPath(leaf, withSub(current, sub, field)); err != nil {
		log.Printf("settings: reset %s: %v", path, err)
		return
	}
	s.save()
}

// unset clears an optional field (a None). A leaf's runtime override
// drops, which is where Rust's runtime None lands once runtime.toml
// (which cannot hold one) is read back; a value struct's field leaves
// the struct.
func (s store) unset(path string) {
	if _, sub, _ := config.LeafPath(path); sub != "" {
		_ = s.set(path, nil)
		return
	}
	s.reset(path)
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
// a config value equal to the default shows no badge. A value struct's
// field shows its struct's state while it is set, nothing while unset.
func sourceOf(s store, path string) sourceInfo {
	leaf, sub, ok := config.LeafPath(path)
	if !ok {
		return sourceInfo{}
	}
	if sub != "" {
		// with_field_source: an unset field is the default's; a set one
		// badges as its struct does.
		if _, set := subValue(s.value(leaf), sub); !set {
			return sourceInfo{}
		}
		path = leaf
	}
	t := i18n.Settings()
	key, class := "", ""
	_, inConfig := s.svc.ConfigValue(path)
	switch s.svc.Source(path) {
	case config.SourceDefault:
		return sourceInfo{}
	case config.SourceConfig:
		key, class = "settings-source-config", "info"
		cv, _ := s.svc.ConfigValue(path)
		if dv, err := config.DefaultValue(path); err == nil && reflect.DeepEqual(cv, config.Plain(dv)) {
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

// slot is where an editor reads and writes its value: a config path,
// or one item of a list editor (written back with the whole list).
type slot struct {
	get func() any
	set func(any) error
	// unset clears an optional value (store.unset, or the item's key).
	unset func()
}

// pathSlot is the value at a config path.
func pathSlot(s store, path string) slot {
	return slot{get: func() any { return s.value(path) }, set: func(v any) error { return s.set(path, v) }, unset: func() { s.unset(path) }}
}

// text is the slot's value as a string ("" for anything else).
func (s slot) text() string {
	v, _ := s.get().(string)
	return v
}

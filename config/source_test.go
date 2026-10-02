package config

import (
	"reflect"
	"testing"
)

func TestSourceFollowsTheLayers(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.toml", "[bar]\nlocation = \"bottom\"\n[osd]\nduration = 1500\n")
	writeFile(t, dir, "runtime.toml", "[bar]\nlocation = \"left\"\n")
	svc := Load(dir, DiscardDiagnostics)
	for path, want := range map[string]ValueSource{
		"bar.location": SourceRuntime,
		"osd.duration": SourceConfig,
		"osd.enabled":  SourceDefault,
		"bar":          SourceRuntime, // a container takes its highest leaf
		"nope.nothing": SourceDefault,
	} {
		if got := svc.Source(path); got != want {
			t.Errorf("Source(%s) = %v, want %v", path, got, want)
		}
	}
	if v, ok := svc.ConfigValue("bar.location"); !ok || v != "bottom" {
		t.Errorf("ConfigValue(bar.location) = %v %v, want the file's bottom under the override", v, ok)
	}
	if v, ok := svc.RuntimeValue("bar.location"); !ok || v != "left" {
		t.Errorf("RuntimeValue(bar.location) = %v %v", v, ok)
	}
	if _, ok := svc.ConfigValue("osd.enabled"); ok {
		t.Error("ConfigValue of a field the file does not set")
	}
	if _, ok := svc.RuntimeValue("osd.duration"); ok {
		t.Error("RuntimeValue of a field with no override")
	}
	if _, err := svc.ResetByPath("bar.location"); err != nil {
		t.Fatal(err)
	}
	if got := svc.Source("bar.location"); got != SourceConfig {
		t.Errorf("after reset Source = %v, want config", got)
	}
	if err := svc.SetByPath("osd.enabled", false); err != nil {
		t.Fatal(err)
	}
	if got := svc.Source("osd.enabled"); got != SourceRuntime {
		t.Errorf("after set Source = %v, want runtime", got)
	}
}

func TestDefaultValue(t *testing.T) {
	if v, err := DefaultValue("osd.enabled"); err != nil || v != true {
		t.Errorf("DefaultValue(osd.enabled) = %v %v", v, err)
	}
	want, _ := Load(t.TempDir(), DiscardDiagnostics).GetByPath("bar.location")
	if v, _ := DefaultValue("bar.location"); !reflect.DeepEqual(v, want) {
		t.Errorf("DefaultValue(bar.location) = %v, want %v", v, want)
	}
	if _, err := DefaultValue("osd.nope"); err == nil {
		t.Error("a default for no field")
	}
}

func TestSourceAcceptsAliases(t *testing.T) {
	canonical, ok := canonicalPath("lock.lock-on-start")
	if !ok || canonical != "lock.lock-on-start" {
		t.Errorf("canonical = %q %v", canonical, ok)
	}
	if _, ok := canonicalPath("lock.nope"); ok {
		t.Error("an unknown field resolved")
	}
}

func TestPlainUnwrapsTables(t *testing.T) {
	s := Load(t.TempDir(), DiscardDiagnostics)
	if err := s.SetByPath("animations.osd", map[string]any{"enter": "fade"}); err != nil {
		t.Fatal(err)
	}
	v, _ := s.GetByPath("animations.osd")
	m, ok := Plain(v).(map[string]any)
	if !ok || m["enter"] != "fade" {
		t.Errorf("Plain = %#v, want the table as a map", Plain(v))
	}
	if Plain(int64(3)) != int64(3) {
		t.Error("a scalar changed")
	}
}

func TestEncodeIsWhatGetByPathReads(t *testing.T) {
	svc := Load(t.TempDir(), DiscardDiagnostics)
	defer svc.Close()
	got, err := svc.GetByPath("bar.layout")
	if err != nil {
		t.Fatal(err)
	}
	if enc := Encode(svc.Config().Bar.Layout); !reflect.DeepEqual(enc, Plain(got)) {
		t.Errorf("Encode = %#v\nGetByPath = %#v", enc, Plain(got))
	}
	// And SetByPath takes it back.
	layouts := svc.Config().Bar.Layout
	layouts[0].Left = append(layouts[0].Left, BarItem{Module: "clock"})
	if err := svc.SetByPath("bar.layout", Encode(layouts)); err != nil {
		t.Fatal(err)
	}
	if l := svc.Config().Bar.Layout[0].Left; l[len(l)-1].Module != "clock" {
		t.Errorf("left zone %+v", l)
	}
}

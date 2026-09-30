package launcher

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
)

func memHistory(t *testing.T) *History {
	t.Helper()
	h, err := OpenHistoryAt(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func TestRecordAccumulatesUses(t *testing.T) {
	h := memHistory(t)
	_ = h.RecordAt("run", "htop", 1000, 25)
	_ = h.RecordAt("run", "htop", 2000, 25)
	w, err := h.FrecencyAt("run", 2000)
	if err != nil || math.Abs(w["htop"]-2.0) > 1e-9 {
		t.Errorf("weights = %v %v", w, err)
	}
}

func TestFrecencyDecaysWithAge(t *testing.T) {
	h := memHistory(t)
	_ = h.RecordAt("drun", "old.desktop", 0, 25)
	_ = h.RecordAt("drun", "new.desktop", 100*day, 25)
	w, _ := h.FrecencyAt("drun", 100*day)
	if w["new.desktop"] <= w["old.desktop"] || math.Abs(w["old.desktop"]-0.1) > 1e-9 {
		t.Errorf("weights = %v", w)
	}
}

func TestPruneKeepsMostRecent(t *testing.T) {
	h := memHistory(t)
	for i, name := range []string{"a", "b", "c", "d"} {
		_ = h.RecordAt("run", name, int64(i), 2)
	}
	if got, _ := h.Recent("run"); !reflect.DeepEqual(got, []string{"d", "c"}) {
		t.Errorf("recent = %v", got)
	}
}

func TestRemoveDeletesEntry(t *testing.T) {
	h := memHistory(t)
	_ = h.RecordAt("run", "htop", 0, 25)
	_ = h.Remove("run", "htop")
	if got, _ := h.Recent("run"); len(got) != 0 {
		t.Errorf("recent = %v", got)
	}
}

func TestModesAreIsolated(t *testing.T) {
	h := memHistory(t)
	_ = h.RecordAt("run", "htop", 0, 25)
	_ = h.RecordAt("drun", "firefox.desktop", 0, 25)
	if got, _ := h.Recent("run"); !reflect.DeepEqual(got, []string{"htop"}) {
		t.Errorf("run = %v", got)
	}
	if got, _ := h.Recent("drun"); !reflect.DeepEqual(got, []string{"firefox.desktop"}) {
		t.Errorf("drun = %v", got)
	}
}

func TestHistoryPersistsInTheRustFileLayout(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	h, err := OpenHistory()
	if err != nil {
		t.Fatal(err)
	}
	_ = h.RecordAt("ssh", "host", 5, 25)
	_ = h.Close()
	dir, _ := DataDir()
	h2, err := OpenHistoryAt(filepath.Join(dir, "launcher.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h2.Close() }()
	if got, _ := h2.Recent("ssh"); !reflect.DeepEqual(got, []string{"host"}) {
		t.Errorf("reopened = %v", got)
	}
}

func TestAnUnopenableHistoryIsAnError(t *testing.T) {
	if _, err := OpenHistoryAt(filepath.Join(t.TempDir(), "missing-dir", "x.db")); err == nil {
		t.Error("a database in a missing directory must fail to open")
	}
}

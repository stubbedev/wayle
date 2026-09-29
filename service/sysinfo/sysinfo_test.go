package sysinfo

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeProc writes a minimal procfs tree and repoints ProcRoot at it.
func fakeProc(t *testing.T, stat, meminfo string) {
	t.Helper()
	dir := t.TempDir()
	if stat != "" {
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if meminfo != "" {
		if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte(meminfo), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := ProcRoot
	ProcRoot = dir
	t.Cleanup(func() { ProcRoot = old })
}

func TestCpuUsageBetweenSamples(t *testing.T) {
	// idle=1000 total=2000 then idle=1100 total=3000: 90% busy.
	prev := CpuSample{Idle: 1000, Total: 2000}
	next := CpuSample{Idle: 1100, Total: 3000}
	if got := next.Usage(prev); got < 89.9 || got > 90.1 {
		t.Errorf("usage = %f, want 90", got)
	}
	// No elapsed time reads as no usage, not a divide by zero.
	if got := prev.Usage(prev); got != 0 {
		t.Errorf("same sample = %f, want 0", got)
	}
}

func TestReadCpuSample(t *testing.T) {
	fakeProc(t, "cpu  100 0 200 1000 50 0 0 0 0 0\ncpu0 100 0 200 1000 50 0 0 0 0 0\n", "")
	sample, err := ReadCpuSample()
	if err != nil {
		t.Fatalf("ReadCpuSample: %v", err)
	}
	// The aggregate line skips iowait from idle (field index 3 only).
	if sample.Idle != 1000 || sample.Total != 1350 {
		t.Errorf("sample = %+v, want idle 1000 total 1350", sample)
	}
}

func TestReadCpuSampleErrors(t *testing.T) {
	fakeProc(t, "", "")
	if _, err := ReadCpuSample(); err == nil {
		t.Error("missing stat file: want an error")
	}
	fakeProc(t, "intr 123\n", "")
	if _, err := ReadCpuSample(); err == nil {
		t.Error("no cpu line: want an error")
	}
	fakeProc(t, "cpu  1 2\n", "")
	if _, err := ReadCpuSample(); err == nil {
		t.Error("short cpu line: want an error")
	}
}

func TestReadMemory(t *testing.T) {
	fakeProc(t, "", "MemTotal:       16000000 kB\nMemAvailable:    8000000 kB\nSwapTotal:       4000000 kB\nSwapFree:        1000000 kB\n")
	mem, err := ReadMemory()
	if err != nil {
		t.Fatalf("ReadMemory: %v", err)
	}
	if mem.Total != 16000000*1024 || mem.Available != 8000000*1024 {
		t.Errorf("mem = %+v", mem)
	}
	if mem.UsagePercent() < 49.9 || mem.UsagePercent() > 50.1 {
		t.Errorf("usage = %f, want 50", mem.UsagePercent())
	}
	if mem.SwapPercent() < 74.9 || mem.SwapPercent() > 75.1 {
		t.Errorf("swap = %f, want 75", mem.SwapPercent())
	}
	// sysinfo's used = total - available.
	if mem.Used() != mem.Total-mem.Available {
		t.Errorf("used = %d", mem.Used())
	}
}

func TestReadMemoryErrors(t *testing.T) {
	fakeProc(t, "", "")
	if _, err := ReadMemory(); err == nil {
		t.Error("missing meminfo: want an error")
	}
	fakeProc(t, "", "MemTotal: 100 kB\n")
	if _, err := ReadMemory(); err == nil {
		t.Error("missing keys: want an error")
	}
	// A zero-total memory reads as 0%, not NaN.
	if got := (Memory{}).UsagePercent(); got != 0 {
		t.Errorf("empty memory usage = %f, want 0", got)
	}
}

func TestGiB(t *testing.T) {
	const gib = 1024 * 1024 * 1024
	if got := GiB(gib / 2); got != "0.5" {
		t.Errorf("GiB = %q, want 0.5", got)
	}
	if got := GiB(3 * gib); got != "3.0" {
		t.Errorf("GiB = %q, want 3.0", got)
	}
}

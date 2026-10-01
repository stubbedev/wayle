// Package sysinfo reads the kernel's own counters: /proc/stat for CPU
// time, /proc/meminfo for memory, and statfs for storage. The pure-Go
// counterpart of crates/wayle-sysinfo's read path.
package sysinfo

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var (
	errCPUShortLine = errors.New("sysinfo: /proc/stat cpu line too short")
	errNoCpuLine    = errors.New("sysinfo: /proc/stat has no cpu line")
)

// ProcRoot is the procfs mount; tests repoint it at a fake tree.
var ProcRoot = "/proc"

// CpuSample is one snapshot of the aggregate CPU time counters.
type CpuSample struct {
	Idle  uint64
	Total uint64
}

// ReadCpuSample parses the "cpu " aggregate line of /proc/stat.
func ReadCpuSample() (CpuSample, error) {
	file, err := os.Open(filepath.Join(ProcRoot, "stat"))
	if err != nil {
		return CpuSample{}, fmt.Errorf("sysinfo: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		if len(fields) < 4 {
			return CpuSample{}, errCPUShortLine
		}
		var total, idle uint64
		for i, field := range fields {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return CpuSample{}, fmt.Errorf("sysinfo: /proc/stat field %d: %w", i, err)
			}
			total += value
			if i == 3 { // idle; iowait (i==4) stays busy like sysinfo's default
				idle = value
			}
		}
		return CpuSample{Idle: idle, Total: total}, nil
	}
	if err := scanner.Err(); err != nil {
		return CpuSample{}, fmt.Errorf("sysinfo: read /proc/stat: %w", err)
	}
	return CpuSample{}, errNoCpuLine
}

// Usage returns the percent of non-idle time between two samples.
func (s CpuSample) Usage(prev CpuSample) float64 {
	if s.Total <= prev.Total {
		return 0
	}
	dTotal := s.Total - prev.Total
	dIdle := s.Idle - prev.Idle
	if dIdle >= dTotal {
		return 0
	}
	return float64(dTotal-dIdle) / float64(dTotal) * 100
}

// Memory is one /proc/meminfo snapshot, in bytes.
type Memory struct {
	Total     uint64
	Available uint64
	SwapTotal uint64
	SwapFree  uint64
}

// ReadMemory parses the meminfo totals.
func ReadMemory() (Memory, error) {
	file, err := os.Open(filepath.Join(ProcRoot, "meminfo"))
	if err != nil {
		return Memory{}, fmt.Errorf("sysinfo: %w", err)
	}
	defer file.Close()
	want := map[string]*uint64{
		"MemTotal":     new(uint64),
		"MemAvailable": new(uint64),
		"SwapTotal":    new(uint64),
		"SwapFree":     new(uint64),
	}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		target, ok := want[name]
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return Memory{}, fmt.Errorf("sysinfo: meminfo %s: %w", name, err)
		}
		*target = kb * 1024
		seen[name] = true
	}
	if err := scanner.Err(); err != nil {
		return Memory{}, fmt.Errorf("sysinfo: read /proc/meminfo: %w", err)
	}
	for name := range want {
		if !seen[name] {
			return Memory{}, fmt.Errorf("sysinfo: meminfo misses %s", name)
		}
	}
	return Memory{
		Total:     *want["MemTotal"],
		Available: *want["MemAvailable"],
		SwapTotal: *want["SwapTotal"],
		SwapFree:  *want["SwapFree"],
	}, nil
}

// Used returns total minus available (sysinfo's definition).
func (m Memory) Used() uint64 { return m.Total - m.Available }

// UsagePercent maps used onto 0..100.
func (m Memory) UsagePercent() float64 {
	if m.Total == 0 {
		return 0
	}
	return float64(m.Used()) / float64(m.Total) * 100
}

// SwapPercent maps swap usage onto 0..100.
func (m Memory) SwapPercent() float64 {
	if m.SwapTotal == 0 {
		return 0
	}
	return float64(m.SwapTotal-m.SwapFree) / float64(m.SwapTotal) * 100
}

// GiB renders a byte count the way the Rust module does: one decimal.
func GiB(bytes uint64) string {
	const gib = 1024.0 * 1024.0 * 1024.0
	return strconv.FormatFloat(float64(bytes)/gib, 'f', 1, 64)
}

// ReadStoragePercent reports the used percent of the mount points
// together (storage helpers.rs aggregate_storage): used and total sum
// across them through statfs. Paths that cannot be read are skipped;
// none readable is an error.
func ReadStoragePercent(paths []string) (float64, error) {
	var used, total uint64
	var errs []error
	for _, path := range paths {
		var stat unix.Statfs_t
		if err := unix.Statfs(path, &stat); err != nil {
			errs = append(errs, fmt.Errorf("sysinfo: statfs %s: %w", path, err))
			continue
		}
		size := stat.Blocks * uint64(stat.Bsize)
		total += size
		used += size - stat.Bfree*uint64(stat.Bsize)
	}
	if len(errs) == len(paths) {
		return 0, errors.Join(append(errs, errors.New("sysinfo: no readable mount point"))...)
	}
	if total == 0 {
		return 0, nil
	}
	return float64(used) / float64(total) * 100, nil
}

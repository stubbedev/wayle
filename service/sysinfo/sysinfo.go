// Package sysinfo reads the kernel's own counters: /proc/stat for CPU
// time, /proc/meminfo for memory, and statfs for storage. The pure-Go
// counterpart of crates/wayle-sysinfo's read path.
package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcRoot and SysRoot are the procfs and sysfs mounts; tests repoint
// them at fake trees.
var (
	ProcRoot = "/proc"
	SysRoot  = "/sys"
)

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

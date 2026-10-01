package sysinfo

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CPUTimes is one /proc/stat cpu line reduced the way the sysinfo
// crate does: work is user, nice, system, irq, and softirq (with guest
// time taken out of user and nice, where the kernel already counts
// it); total adds idle, iowait, steal, and the guest time back.
type CPUTimes struct {
	Work, Total uint64
}

// CPUStat is the aggregate line and one per core, in core order.
type CPUStat struct {
	Global CPUTimes
	Cores  []CPUTimes
}

// ReadCPUStat parses /proc/stat's cpu lines.
func ReadCPUStat() (CPUStat, error) {
	file, err := os.Open(filepath.Join(ProcRoot, "stat"))
	if err != nil {
		return CPUStat{}, fmt.Errorf("sysinfo: %w", err)
	}
	defer file.Close()
	var stat CPUStat
	found := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		times, err := parseCPUTimes(fields[1:])
		if err != nil {
			return CPUStat{}, err
		}
		if fields[0] == "cpu" {
			stat.Global, found = times, true
		} else {
			stat.Cores = append(stat.Cores, times)
		}
	}
	if err := scanner.Err(); err != nil {
		return CPUStat{}, fmt.Errorf("sysinfo: read /proc/stat: %w", err)
	}
	if !found {
		return CPUStat{}, errors.New("sysinfo: /proc/stat has no cpu line")
	}
	return stat, nil
}

func parseCPUTimes(fields []string) (CPUTimes, error) {
	var v [10]uint64
	for i := range min(len(fields), 10) {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return CPUTimes{}, fmt.Errorf("sysinfo: /proc/stat field %d: %w", i, err)
		}
		v[i] = n
	}
	user, nice, system, idle, iowait, irq, softirq, steal, guest, guestNice := v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9]
	user = satSub(user, guest)
	nice = satSub(nice, guestNice)
	work := user + nice + system + irq + softirq
	return CPUTimes{Work: work, Total: work + idle + iowait + guest + guestNice + steal}, nil
}

func satSub(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return 0
}

// Usage is sysinfo's CpuUsage percent between two readings: the work
// delta over the total delta, capped at 100. Against a zero reading
// (the first refresh) it is the average since boot.
func Usage(prev, cur CPUTimes) float32 {
	work := float32(0)
	if cur.Work > prev.Work {
		work = float32(cur.Work - prev.Work)
	}
	total := float32(1)
	if cur.Total > prev.Total {
		total = float32(cur.Total - prev.Total)
	}
	return min(work/total*100, 100)
}

// CoreFrequencyMHz is get_cpu_frequency: the core's cpufreq
// scaling_cur_freq (kHz), else the first MHz line of /proc/cpuinfo,
// else 0.
func CoreFrequencyMHz(core int) uint64 {
	path := filepath.Join(SysRoot, "devices/system/cpu", "cpu"+strconv.Itoa(core), "cpufreq/scaling_cur_freq")
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // a sysfs path under SysRoot
		line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
		if khz, err := strconv.ParseUint(line, 10, 64); err == nil {
			return khz / 1000
		}
	}
	data, err := os.ReadFile(filepath.Join(ProcRoot, "cpuinfo"))
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu MHz\t") && !strings.HasPrefix(line, "BogoMIPS") &&
			!strings.HasPrefix(line, "clock\t") && !strings.HasPrefix(line, "bogomips per cpu") {
			continue
		}
		i := strings.LastIndexByte(line, ':')
		value := strings.TrimSpace(strings.ReplaceAll(line[i+1:], "MHz", ""))
		if mhz, err := strconv.ParseFloat(value, 64); err == nil {
			return uint64(mhz)
		}
		return 0
	}
	return 0
}

// Core is one core's usage and frequency.
type Core struct {
	UsagePercent float32
	FrequencyMHz uint64
}

// CPU is polling/cpu.rs's CpuData.
type CPU struct {
	UsagePercent       float32
	AvgFrequencyMHz    uint64
	MaxFrequencyMHz    uint64
	BusiestCoreFreqMHz uint64
	// TemperatureC is the matched sensor's reading; ok is false
	// without one.
	TemperatureC   float32
	HasTemperature bool
	Cores          []Core
}

// CPUReader keeps the previous counters between polls, as the Rust
// poll loop keeps its System.
type CPUReader struct {
	prev CPUStat
	// Sensor is the temp-sensor key: "auto" or a label substring.
	Sensor string
}

// Read samples the counters, frequencies, and temperature.
func (r *CPUReader) Read() (CPU, error) {
	stat, err := ReadCPUStat()
	if err != nil {
		return CPU{}, err
	}
	out := CPU{UsagePercent: Usage(r.prev.Global, stat.Global)}
	var sum uint64
	busiest := -1
	for i, cur := range stat.Cores {
		var prev CPUTimes
		if i < len(r.prev.Cores) {
			prev = r.prev.Cores[i]
		}
		core := Core{UsagePercent: Usage(prev, cur), FrequencyMHz: CoreFrequencyMHz(i)}
		out.Cores = append(out.Cores, core)
		sum += core.FrequencyMHz
		out.MaxFrequencyMHz = max(out.MaxFrequencyMHz, core.FrequencyMHz)
		// max_by keeps the last of equal maxima.
		if busiest < 0 || core.UsagePercent >= out.Cores[busiest].UsagePercent {
			busiest = i
		}
	}
	if n := len(out.Cores); n > 0 {
		out.AvgFrequencyMHz = sum / uint64(n)
		out.BusiestCoreFreqMHz = out.Cores[busiest].FrequencyMHz
	}
	r.prev = stat
	out.TemperatureC, out.HasTemperature = CPUTemperature(r.Sensor)
	return out, nil
}

// cpuTempPatterns are CPU_TEMP_PATTERNS, tried in order for "auto".
var cpuTempPatterns = []string{"tctl", "tdie", "tccd", "k10temp", "coretemp", "package id", "cpu"}

// Sensor is one hwmon temperature input, labeled the way the sysinfo
// crate labels its components.
type Sensor struct {
	Label        string
	TemperatureC float32
}

// Sensors lists the hwmon temperature inputs (sysinfo's Components):
// a sensor's label is its tempN_label, else "<chip> <model>" or
// "<chip> tempN". Without any hwmon sensor, thermal_zone0 stands in.
// The order is by hwmon directory, then by input number.
func Sensors() []Sensor {
	var out []Sensor
	dirs, _ := filepath.Glob(filepath.Join(SysRoot, "class/hwmon/hwmon*"))
	sort.Strings(dirs)
	for _, dir := range dirs {
		chip := readLine(filepath.Join(dir, "name"))
		model, hasModel := readLineOK(filepath.Join(dir, "device/model"))
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		type entry struct {
			id int
			s  Sensor
		}
		var entries []entry
		for _, input := range inputs {
			id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(input), "temp"), "_input"))
			if err != nil {
				continue
			}
			temp, ok := readMilliCelsius(input)
			if !ok {
				continue
			}
			label := readLine(filepath.Join(dir, "temp"+strconv.Itoa(id)+"_label"))
			if label == "" {
				if hasModel {
					label = chip + " " + model
				} else {
					label = chip + " temp" + strconv.Itoa(id)
				}
			}
			entries = append(entries, entry{id, Sensor{Label: label, TemperatureC: temp}})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
		for _, e := range entries {
			if !containsLabel(out, e.s.Label) {
				out = append(out, e.s)
			}
		}
	}
	if len(out) == 0 {
		zone := filepath.Join(SysRoot, "class/thermal/thermal_zone0")
		if temp, ok := readMilliCelsius(filepath.Join(zone, "temp")); ok {
			out = append(out, Sensor{Label: "", TemperatureC: temp})
		}
	}
	return out
}

func containsLabel(sensors []Sensor, label string) bool {
	for _, s := range sensors {
		if s.Label == label {
			return true
		}
	}
	return false
}

// CPUTemperature is find_cpu_temperature: a named sensor by label
// substring (case-insensitive), or for "auto" the first sensor matching
// each pattern in turn.
func CPUTemperature(sensor string) (float32, bool) {
	sensors := Sensors()
	find := func(pattern string) (float32, bool) {
		for _, s := range sensors {
			if strings.Contains(strings.ToLower(s.Label), pattern) {
				return s.TemperatureC, true
			}
		}
		return 0, false
	}
	if sensor != "auto" {
		return find(strings.ToLower(sensor))
	}
	for _, p := range cpuTempPatterns {
		if t, ok := find(p); ok {
			return t, true
		}
	}
	return 0, false
}

func readMilliCelsius(path string) (float32, bool) {
	n, err := strconv.ParseInt(readLine(path), 10, 32)
	if err != nil {
		return 0, false
	}
	return float32(n) / 1000, true
}

func readLine(path string) string {
	s, _ := readLineOK(path)
	return s
}

func readLineOK(path string) (string, bool) {
	data, err := os.ReadFile(path) //nolint:gosec // a sysfs/procfs path under the roots
	if err != nil {
		return "", false
	}
	return strings.TrimRightFunc(string(data), func(r rune) bool { return r == '\n' || r == ' ' || r == '\t' || r == '\r' }), true
}

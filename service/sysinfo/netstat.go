package sysinfo

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SysClassNet is the interface directory; tests repoint it.
var SysClassNet = "/sys/class/net"

// NetTotals is one interface's lifetime byte counters.
type NetTotals struct {
	Rx uint64
	Tx uint64
}

// ReadNetTotals reads rx/tx byte totals per interface.
func ReadNetTotals() (map[string]NetTotals, error) {
	entries, err := os.ReadDir(SysClassNet)
	if err != nil {
		return nil, fmt.Errorf("sysinfo: %w", err)
	}
	out := make(map[string]NetTotals, len(entries))
	for _, entry := range entries {
		totals, err := readInterfaceTotals(entry.Name())
		if err != nil {
			continue
		}
		out[entry.Name()] = totals
	}
	return out, nil
}

func readInterfaceTotals(name string) (NetTotals, error) {
	rx, err := readCounter(name, "rx_bytes")
	if err != nil {
		return NetTotals{}, err
	}
	tx, err := readCounter(name, "tx_bytes")
	if err != nil {
		return NetTotals{}, err
	}
	return NetTotals{Rx: rx, Tx: tx}, nil
}

func readCounter(iface, counter string) (uint64, error) {
	data, err := os.ReadFile(filepath.Join(SysClassNet, iface, "statistics", counter)) //nolint:gosec // the path is SysClassNet + validated directory entries
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sysinfo: %s/%s: %w", iface, counter, err)
	}
	return value, nil
}

// SelectInterface is netstat's helpers.rs select_interface: "auto"
// picks the busiest non-loopback interface, anything else matches by
// exact name (ok=false when absent).
func SelectInterface(totals map[string]NetTotals, iface string) (string, bool) {
	if iface != "auto" {
		if _, ok := totals[iface]; ok {
			return iface, true
		}
		return "", false
	}
	best, bestLoad := "", uint64(0)
	for name, totals := range totals {
		if strings.HasPrefix(name, "lo") {
			continue
		}
		if load := totals.Rx + totals.Tx; load > bestLoad {
			best, bestLoad = name, load
		}
	}
	return best, best != ""
}

var errNetRate = errors.New("sysinfo: interface counters went backwards")

// Rate is one interface's bytes-per-second over the sample window.
type Rate struct {
	Interface string
	RxPerSec  uint64
	TxPerSec  uint64
}

// NetRate computes the per-second rate of one interface between two
// total snapshots (elapsed in seconds).
func NetRate(name string, prev, now NetTotals, elapsed float64) (Rate, error) {
	if elapsed <= 0 {
		return Rate{}, errNetRate
	}
	if now.Rx < prev.Rx || now.Tx < prev.Tx {
		// A counter reset (rebind, suspend) reads as no traffic for
		// this window.
		return Rate{Interface: name}, nil
	}
	return Rate{
		Interface: name,
		RxPerSec:  uint64(float64(now.Rx-prev.Rx) / elapsed),
		TxPerSec:  uint64(float64(now.Tx-prev.Tx) / elapsed),
	}, nil
}

// AutoBytes renders the bytesize crate's Display: 1000-based units,
// one decimal, plain bytes below a kilobyte.
func AutoBytes(bytes uint64) string {
	const unit = 1000
	if bytes < unit {
		return strconv.FormatUint(bytes, 10) + " B"
	}
	value := float64(bytes)
	suffix := ""
	for _, suffix = range []string{"KB", "MB", "GB", "TB", "PB", "EB"} {
		value /= unit
		if value < unit {
			break
		}
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix
}

// ReadNetDevTotals is an alternative reader over /proc/net/dev used by
// the tests to drive counter snapshots; production reads sysfs.
func ReadNetDevTotals(path string) (map[string]NetTotals, error) {
	file, err := os.Open(path) //nolint:gosec // the path is the caller's procfs location, injected for tests
	if err != nil {
		return nil, fmt.Errorf("sysinfo: %w", err)
	}
	defer file.Close()
	out := map[string]NetTotals{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		name, rest, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		out[strings.TrimSpace(name)] = NetTotals{Rx: rx, Tx: tx}
	}
	return out, scanner.Err()
}

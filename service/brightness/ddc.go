package brightness

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// DDC/CI over i2c-dev, the Go counterpart of backend/ddc.rs and the
// ddc / ddc-i2c crates' raw VCP get/set it is built on. External
// monitors are driven through the VESA DDC/CI protocol on the
// monitor's I²C bus: the i2c-dev kernel module and read/write access
// to /dev/i2c-* (usually the i2c group) are required.
//
// All DDC I/O is blocking and slow (tens of milliseconds per
// transaction, and a scan probes every bus), so none of it runs on
// the UI loop.

const (
	// vcpLuminance is the VCP feature code for monitor brightness.
	vcpLuminance = 0x10

	// ddcAddress is the DDC/CI command I²C slave address; the wire
	// uses its 8-bit forms 0x6E (write) and 0x6F (read).
	ddcAddress = 0x37
	// ddcSubAddress prefixes every host-to-display packet.
	ddcSubAddress = 0x51
	// i2cSlave is the i2c-dev ioctl that selects the slave address.
	i2cSlave = 0x0703

	// ddcAttempts is the total tries per write; DDC/CI transactions
	// are routinely corrupted or NAK'd.
	ddcAttempts = 3
	// ddcRetryDelay is the pause between write attempts.
	ddcRetryDelay = 40 * time.Millisecond

	// getVCPResponseDelay is the wait between a Get VCP Feature
	// request and reading its reply; commandDelay and failedDelay are
	// the quiet time the display needs before the next command after
	// a success or a failure (the ddc crate's Delay).
	getVCPResponseDelay = 40 * time.Millisecond
	commandDelay        = 50 * time.Millisecond
	failedDelay         = 40 * time.Millisecond

	// ddcNamePrefix names synthesized DDC devices; it carries "ddc"
	// and "i2c" so the dropdown's friendly-name lookup reads them as
	// external monitors.
	ddcNamePrefix = "ddci2c-"
	// i2cNodePrefix is the per-bus node name under DevDir.
	i2cNodePrefix = "i2c-"
)

// DevDir is where Linux exposes i2c bus nodes; a var so tests can
// repoint it.
var DevDir = "/dev"

// i2cBus is one opened bus with the DDC/CI slave selected: plain
// write(2)/read(2) transfers to that address.
type i2cBus interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Close() error
}

// openBus opens a /dev/i2c-N node for DDC/CI; a var so tests can
// substitute fake monitors.
var openBus = func(path string) (i2cBus, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0) //nolint:gosec // the path is DevDir plus a parsed i2c-N entry
	if err != nil {
		return nil, err
	}
	if err := unix.IoctlSetInt(int(file.Fd()), i2cSlave, ddcAddress); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("select DDC/CI slave: %w", err)
	}
	return file, nil
}

// checksum is the DDC/CI XOR checksum.
func checksum(seed byte, data []byte) byte {
	sum := seed
	for _, b := range data {
		sum ^= b
	}
	return sum
}

// encodeCommand frames a command: sub-address, 0x80|length, the
// payload, and a checksum seeded with the destination address 0x6E.
func encodeCommand(payload []byte) []byte {
	packet := make([]byte, 0, len(payload)+3)
	packet = append(packet, ddcSubAddress, 0x80|byte(len(payload)))
	packet = append(packet, payload...)
	return append(packet, checksum(ddcAddress<<1, packet))
}

var (
	errDDCLength   = errors.New("invalid DDC/CI reply length")
	errDDCChecksum = errors.New("invalid DDC/CI reply checksum")
	errDDCOpcode   = errors.New("invalid DDC/CI reply opcode")
)

// decodeReply validates a display-to-host frame (source address,
// 0x80|length, payload, checksum) and returns the payload. The
// checksum is seeded with the reply address 0x6F and the host
// sub-address, as ddc-i2c computes it.
func decodeReply(frame []byte) ([]byte, error) {
	if len(frame) < 2 {
		return nil, errDDCLength
	}
	if frame[1]&0x80 == 0 {
		return nil, errors.New("expected DDC/CI length bit")
	}
	n := int(frame[1] & 0x7f)
	if len(frame) < n+3 {
		return nil, errDDCLength
	}
	if frame[2+n] != checksum((ddcAddress<<1|1)^ddcSubAddress, frame[1:2+n]) {
		return nil, errDDCChecksum
	}
	return frame[2 : 2+n], nil
}

// vcpValue is a Get VCP Feature reply's value and maximum.
type vcpValue struct {
	current, maximum uint16
}

// decodeVCP parses a Get VCP Feature reply payload: opcode 0x02,
// result code, the feature code, type, then max and current.
func decodeVCP(payload []byte) (vcpValue, error) {
	if len(payload) != 8 {
		return vcpValue{}, errDDCLength
	}
	if payload[0] != 0x02 {
		return vcpValue{}, errDDCOpcode
	}
	switch payload[1] {
	case 0x00:
	case 0x01:
		return vcpValue{}, errors.New("unsupported VCP code")
	default:
		return vcpValue{}, fmt.Errorf("unrecognized VCP error code 0x%02x", payload[1])
	}
	return vcpValue{
		maximum: uint16(payload[4])<<8 | uint16(payload[5]),
		current: uint16(payload[6])<<8 | uint16(payload[7]),
	}, nil
}

// ddcHandle serializes transactions on one bus and keeps the quiet
// time the display needs between commands.
type ddcHandle struct {
	bus  i2cBus
	next time.Time
	// sleep is time.Sleep; tests stub it.
	sleep func(time.Duration)
}

// ddcSleep is the pause primitive handles use; tests stub it.
var ddcSleep = time.Sleep

func newHandle(bus i2cBus) *ddcHandle { return &ddcHandle{bus: bus, sleep: ddcSleep} }

func (h *ddcHandle) wait() {
	if d := time.Until(h.next); d > 0 {
		h.sleep(d)
	}
}

func (h *ddcHandle) settle(err error) {
	if err != nil {
		h.next = time.Now().Add(failedDelay)
		return
	}
	h.next = time.Now().Add(commandDelay)
}

// getVCP reads one feature.
func (h *ddcHandle) getVCP(code byte) (value vcpValue, err error) {
	h.wait()
	defer func() { h.settle(err) }()
	if _, err := h.bus.Write(encodeCommand([]byte{0x01, code})); err != nil {
		return vcpValue{}, err
	}
	h.sleep(getVCPResponseDelay)
	frame := make([]byte, 8+3)
	n, err := h.bus.Read(frame)
	if err != nil {
		return vcpValue{}, err
	}
	payload, err := decodeReply(frame[:n])
	if err != nil {
		return vcpValue{}, err
	}
	return decodeVCP(payload)
}

// setVCP writes one feature; Set VCP Feature has no reply.
func (h *ddcHandle) setVCP(code byte, value uint16) (err error) {
	h.wait()
	defer func() { h.settle(err) }()
	_, err = h.bus.Write(encodeCommand([]byte{0x03, code, byte(value >> 8), byte(value)}))
	return err
}

// withRetry runs a transaction up to ddcAttempts times, pausing
// between tries, and returns the last error on exhaustion.
func withRetry(sleep func(time.Duration), transaction func() error) error {
	var last error
	for attempt := 1; attempt <= ddcAttempts; attempt++ {
		if last = transaction(); last == nil {
			return nil
		}
		if attempt < ddcAttempts {
			sleep(ddcRetryDelay)
		}
	}
	return last
}

// ddcDisplay is one managed monitor: its handle, the VCP maximum
// cached at scan time, and the last level read or written (DDC has no
// kernel notification to pick changes up from).
type ddcDisplay struct {
	// io serializes transactions on the bus and guards the handle;
	// it is held across the slow I/O, so the manager's map lock never
	// is.
	io      sync.Mutex
	handle  *ddcHandle
	closed  bool
	max     uint32
	current uint32 // guarded by the manager's mu
}

// close releases the handle once no transaction is using it.
func (d *ddcDisplay) close() {
	d.io.Lock()
	defer d.io.Unlock()
	if !d.closed {
		d.closed = true
		_ = d.handle.bus.Close()
	}
}

// ddcManager owns the open handles (DdcManager). Its lock guards the
// map and the cached levels only.
type ddcManager struct {
	mu       sync.Mutex
	displays map[string]*ddcDisplay
}

func newDDCManager() *ddcManager { return &ddcManager{displays: map[string]*ddcDisplay{}} }

// scannedDisplay pairs a probe result with its handle.
type scannedDisplay struct {
	name    string
	display *ddcDisplay
}

// scan probes every /dev/i2c-* bus in bus order and keeps the ones
// that answer the luminance query. The probe is single-attempt: most
// buses (sensors, HDMI audio) never answer, and retrying each would
// make the scan needlessly slow.
func scan() []scannedDisplay {
	var found []scannedDisplay
	for _, bus := range i2cBuses() {
		path := filepath.Join(DevDir, i2cNodePrefix+strconv.FormatUint(uint64(bus), 10))
		dev, err := openBus(path)
		if err != nil {
			continue
		}
		handle := newHandle(dev)
		value, err := handle.getVCP(vcpLuminance)
		if err != nil {
			_ = dev.Close()
			continue
		}
		found = append(found, scannedDisplay{
			name:    ddcNamePrefix + strconv.FormatUint(uint64(bus), 10),
			display: &ddcDisplay{handle: handle, max: uint32(value.maximum), current: uint32(value.current)},
		})
	}
	return found
}

// i2cBuses lists the bus numbers of every /dev/i2c-N node, sorted;
// empty when i2c-dev is not loaded.
func i2cBuses() []uint32 {
	entries, err := os.ReadDir(DevDir)
	if err != nil {
		log.Printf("brightness: cannot read %s to find i2c buses: %v", DevDir, err)
		return nil
	}
	var buses []uint32
	for _, entry := range entries {
		rest, ok := strings.CutPrefix(entry.Name(), i2cNodePrefix)
		if !ok {
			continue
		}
		if n, err := strconv.ParseUint(rest, 10, 32); err == nil {
			buses = append(buses, uint32(n))
		}
	}
	slices.Sort(buses)
	return buses
}

// refresh re-scans and reconciles, returning the names added and
// removed. Handles of still-present monitors are replaced by fresh
// ones; the old handles close once any in-flight write finishes.
// Blocking and slow.
func (m *ddcManager) refresh() (added, removed []string) {
	scanned := scan()
	next := make(map[string]*ddcDisplay, len(scanned))
	m.mu.Lock()
	for _, s := range scanned {
		if _, ok := m.displays[s.name]; !ok {
			added = append(added, s.name)
		}
		next[s.name] = s.display
	}
	old := m.displays
	for name := range old {
		if _, ok := next[name]; !ok {
			removed = append(removed, name)
		}
	}
	m.displays = next
	m.mu.Unlock()
	for _, d := range old {
		d.close()
	}
	slices.Sort(removed)
	return added, removed
}

// display looks one managed monitor up.
func (m *ddcManager) display(name string) (*ddcDisplay, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.displays[name]
	return d, ok
}

// devices snapshots the managed monitors in name order.
func (m *ddcManager) devices() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Device, 0, len(m.displays))
	for name, d := range m.displays {
		out = append(out, Device{Name: name, Type: TypeDDC, Brightness: d.current, Max: d.max})
	}
	slices.SortFunc(out, func(a, b Device) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// ErrDDCUnavailable is a write to a name that is not (or no longer) a
// managed monitor.
var ErrDDCUnavailable = errors.New("brightness: DDC monitor unavailable")

// setRaw writes a raw luminance, clamped to the monitor's maximum,
// with retries, and caches the level on success. Blocking.
func (m *ddcManager) setRaw(name string, value uint32) error {
	d, ok := m.display(name)
	if !ok {
		return fmt.Errorf("%w: %s", ErrDDCUnavailable, name)
	}
	value = min(value, d.max, 0xffff)
	d.io.Lock()
	var err error
	if d.closed {
		err = ErrDDCUnavailable
	} else {
		err = withRetry(d.handle.sleep, func() error { return d.handle.setVCP(vcpLuminance, uint16(value)) })
	}
	d.io.Unlock()
	if err != nil {
		return fmt.Errorf("brightness: DDC write to %s: %w", name, err)
	}
	m.mu.Lock()
	d.current = value
	m.mu.Unlock()
	return nil
}

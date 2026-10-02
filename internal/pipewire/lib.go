// Package pipewire is the PipeWire video producer wayle's ScreenCast
// portal streams through (wayle-portal screencast/pipewire.rs): an
// output stream on its own pw_thread_loop, fed one captured frame per
// cycle the consumer drives, stamped with the header, transform and
// damage metas. libpipewire is bound through purego, so the binary
// stays CGO_ENABLED=0.
package pipewire

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/stubbedev/wayle/internal/clib"
)

var libCandidates = clib.SystemCandidates("libpipewire-0.3.so.0")

// libpw holds the bound entry points.
type libpw struct {
	init             func(argc, argv unsafe.Pointer)
	loopNew          func(props unsafe.Pointer) *cPwLoop
	loopDestroy      func(loop *cPwLoop)
	propertiesNew    func(s string) uintptr
	streamNewSimple  func(loop *cPwLoop, name string, props uintptr, events unsafe.Pointer, data uintptr) uintptr
	streamConnect    func(stream uintptr, direction uint32, target uint32, flags uint32, params unsafe.Pointer, n uint32) int32
	streamDisconnect func(stream uintptr) int32
	streamDestroy    func(stream uintptr)
	streamNodeID     func(stream uintptr) uint32
	streamUpdate     func(stream uintptr, params unsafe.Pointer, n uint32) int32
	streamDequeue    func(stream uintptr) unsafe.Pointer
	streamQueue      func(stream uintptr, buffer unsafe.Pointer) int32
	stateAsString    func(state int32) string

	// The event trampolines: one process-wide C function each, routing
	// by the stream's data handle to its Producer.
	onState, onParam, onProcess uintptr
}

var (
	libOnce sync.Once
	lib     *libpw
	errLib  error
)

// load binds libpipewire and initializes it, once per process.
func load() (*libpw, error) {
	libOnce.Do(func() { lib, errLib = bind() })
	return lib, errLib
}

func bind() (*libpw, error) {
	h, err := clib.Open(libCandidates)
	if err != nil {
		return nil, fmt.Errorf("load libpipewire: %w", err)
	}
	l := &libpw{}
	for _, f := range []struct {
		fn   any
		name string
	}{
		{&l.init, "pw_init"},
		{&l.loopNew, "pw_loop_new"},
		{&l.loopDestroy, "pw_loop_destroy"},
		{&l.propertiesNew, "pw_properties_new_string"},
		{&l.streamNewSimple, "pw_stream_new_simple"},
		{&l.streamConnect, "pw_stream_connect"},
		{&l.streamDisconnect, "pw_stream_disconnect"},
		{&l.streamDestroy, "pw_stream_destroy"},
		{&l.streamNodeID, "pw_stream_get_node_id"},
		{&l.streamUpdate, "pw_stream_update_params"},
		{&l.streamDequeue, "pw_stream_dequeue_buffer"},
		{&l.streamQueue, "pw_stream_queue_buffer"},
		{&l.stateAsString, "pw_stream_state_as_string"},
	} {
		sym, err := purego.Dlsym(h, f.name)
		if err != nil {
			return nil, fmt.Errorf("libpipewire: %s: %w", f.name, err)
		}
		purego.RegisterFunc(f.fn, sym)
	}
	l.onState = purego.NewCallback(stateTrampoline)
	l.onParam = purego.NewCallback(paramTrampoline)
	l.onProcess = purego.NewCallback(processTrampoline)
	l.init(nil, nil)
	return l, nil
}

// cStreamEvents mirrors struct pw_stream_events (version 2) on LP64.
type cStreamEvents struct {
	version      uint32
	_            uint32
	destroy      uintptr
	stateChanged uintptr
	controlInfo  uintptr
	ioChanged    uintptr
	paramChanged uintptr
	addBuffer    uintptr
	removeBuffer uintptr
	process      uintptr
	drained      uintptr
	command      uintptr
	triggerDone  uintptr
}

// cPwBuffer mirrors struct pw_buffer.
type cPwBuffer struct {
	buffer   *cSpaBuffer
	userData unsafe.Pointer
	size     uint64
}

// cSpaBuffer, cSpaMeta, cSpaData and cSpaChunk mirror spa/buffer.
type cSpaBuffer struct {
	nMetas, nDatas uint32
	metas          *cSpaMeta
	datas          *cSpaData
}

type cSpaMeta struct {
	typ, size uint32
	data      unsafe.Pointer
}

type cSpaData struct {
	_       [2]uint32 // type, flags
	_       int64     // fd
	_       uint32    // mapoffset
	maxsize uint32
	data    unsafe.Pointer
	chunk   *cSpaChunk
}

type cSpaChunk struct {
	offset, size uint32
	stride       int32
	_            int32 // flags
}

// cMetaHeader mirrors struct spa_meta_header.
type cMetaHeader struct {
	flags, offset uint32
	pts           int64
	dtsOffset     int64
	seq           uint64
}

// cMetaRegion mirrors struct spa_meta_region.
type cMetaRegion struct {
	x, y          int32
	width, height uint32
}

// Stream states (enum pw_stream_state).
const (
	stateError  int32 = -1
	statePaused int32 = 2
)

const (
	directionOutput = 1
	idAny           = 0xffffffff
	flagMapBuffers  = 1 << 2
)

// cPwLoop mirrors struct pw_loop; control is a spa_loop_control
// interface, whose methods pw_loop_enter / iterate / leave (static
// inlines in C) call through.
type cPwLoop struct {
	_       [2]unsafe.Pointer // system, loop
	control *cSpaInterface
	_       unsafe.Pointer // utils
	_       *byte          // name
}

// cSpaInterface mirrors struct spa_interface.
type cSpaInterface struct {
	_       *byte // type
	_       uint32
	methods *cLoopControlMethods
	data    unsafe.Pointer
}

// cLoopControlMethods mirrors the head of struct
// spa_loop_control_methods.
type cLoopControlMethods struct {
	_       uint32  // version
	_       uintptr // get_fd
	_       uintptr // add_hook
	enter   uintptr
	leave   uintptr
	iterate uintptr
}

func (l *cPwLoop) enter() { purego.SyscallN(l.control.methods.enter, uintptr(l.control.data)) }
func (l *cPwLoop) leave() { purego.SyscallN(l.control.methods.leave, uintptr(l.control.data)) }

// iterate dispatches what is ready, waiting at most timeoutMs.
func (l *cPwLoop) iterate(timeoutMs int) {
	purego.SyscallN(l.control.methods.iterate, uintptr(l.control.data), uintptr(timeoutMs))
}

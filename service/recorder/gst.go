package recorder

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/stubbedev/wayle/internal/clib"
)

// The libgstreamer binding: dlopen through purego, so the binary stays
// CGO_ENABLED=0 and the recording runs in-process as the Rust engine's
// does (gst::parse::launch, real PAUSED/PLAYING, EOS on stop).

// GStreamer enum values (gstelement.h, gstmessage.h).
const (
	gstStateNull    int32 = 1
	gstStatePaused  int32 = 3
	gstStatePlaying int32 = 4

	gstChangeFailure   int32 = 0
	gstChangeSuccess   int32 = 1
	gstChangeAsync     int32 = 2
	gstChangeNoPreroll int32 = 3

	gstMessageEOS   int32 = 1 << 0
	gstMessageError int32 = 1 << 1
)

// Engine timeouts (lib.rs).
const (
	eosTimeout     = 5 * time.Second
	startupTimeout = 10 * time.Second
	monitorPoll    = 100 * time.Millisecond
)

// gerror mirrors GError on LP64: the domain quark, the code, the
// message.
type gerror struct {
	domain  uint32
	code    int32
	message *byte
}

// libgst holds the bound entry points.
type libgst struct {
	initCheck        func(argc, argv unsafe.Pointer, err *unsafe.Pointer) bool
	parseLaunch      func(desc string, err *unsafe.Pointer) unsafe.Pointer
	setState         func(el unsafe.Pointer, state int32) int32
	getState         func(el unsafe.Pointer, state, pending *int32, timeout uint64) int32
	sendEvent        func(el, ev unsafe.Pointer) bool
	newEOS           func() unsafe.Pointer
	getBus           func(el unsafe.Pointer) unsafe.Pointer
	timedPopFiltered func(bus unsafe.Pointer, timeout uint64, types int32) unsafe.Pointer
	parseError       func(msg unsafe.Pointer, err *unsafe.Pointer, debug *unsafe.Pointer)
	messageStructure func(msg unsafe.Pointer) unsafe.Pointer
	structureName    func(s unsafe.Pointer) string
	miniObjectUnref  func(obj unsafe.Pointer)
	objectUnref      func(obj unsafe.Pointer)
	factoryFind      func(name string) unsafe.Pointer
	errorFree        func(err unsafe.Pointer)
	free             func(p unsafe.Pointer)
}

var (
	gstOnce sync.Once
	gst     *libgst
	errGst  error
)

// loadGst binds libgstreamer and initializes it once per process.
func loadGst() (*libgst, error) {
	gstOnce.Do(func() { gst, errGst = bindGst() })
	return gst, errGst
}

func bindGst() (*libgst, error) {
	lib, err := clib.Open(clib.SystemCandidates("libgstreamer-1.0.so.0"))
	if err != nil {
		return nil, fmt.Errorf("recorder: load libgstreamer: %w", err)
	}
	glib, err := clib.Open(clib.SystemCandidates("libglib-2.0.so.0"))
	if err != nil {
		return nil, fmt.Errorf("recorder: load libglib: %w", err)
	}
	g := &libgst{}
	purego.RegisterLibFunc(&g.initCheck, lib, "gst_init_check")
	purego.RegisterLibFunc(&g.parseLaunch, lib, "gst_parse_launch")
	purego.RegisterLibFunc(&g.setState, lib, "gst_element_set_state")
	purego.RegisterLibFunc(&g.getState, lib, "gst_element_get_state")
	purego.RegisterLibFunc(&g.sendEvent, lib, "gst_element_send_event")
	purego.RegisterLibFunc(&g.newEOS, lib, "gst_event_new_eos")
	purego.RegisterLibFunc(&g.getBus, lib, "gst_element_get_bus")
	purego.RegisterLibFunc(&g.timedPopFiltered, lib, "gst_bus_timed_pop_filtered")
	purego.RegisterLibFunc(&g.parseError, lib, "gst_message_parse_error")
	purego.RegisterLibFunc(&g.messageStructure, lib, "gst_message_get_structure")
	purego.RegisterLibFunc(&g.structureName, lib, "gst_structure_get_name")
	purego.RegisterLibFunc(&g.miniObjectUnref, lib, "gst_mini_object_unref")
	purego.RegisterLibFunc(&g.objectUnref, lib, "gst_object_unref")
	purego.RegisterLibFunc(&g.factoryFind, lib, "gst_element_factory_find")
	purego.RegisterLibFunc(&g.errorFree, glib, "g_error_free")
	purego.RegisterLibFunc(&g.free, glib, "g_free")
	var gerr unsafe.Pointer
	if !g.initCheck(nil, nil, &gerr) {
		return nil, fmt.Errorf("recorder: gstreamer init failed: %s", g.takeError(gerr))
	}
	return g, nil
}

// takeError reads and frees a GError.
func (g *libgst) takeError(p unsafe.Pointer) string {
	if p == nil {
		return "unknown error"
	}
	msg := clib.GoString((*gerror)(p).message)
	g.errorFree(p)
	return msg
}

// hasFactory is has_factory: whether an element factory is registered.
func (g *libgst) hasFactory(name string) bool {
	f := g.factoryFind(name)
	if f == nil {
		return false
	}
	g.objectUnref(f)
	return true
}

// pipeline is one launched pipeline and its bus.
type pipeline struct {
	g   *libgst
	el  unsafe.Pointer
	bus unsafe.Pointer
}

// launchPipeline is launch_pipeline: parse, play, and confirm the
// pipeline reached PLAYING, else tear it down with the bus's reason.
func (g *libgst) launchPipeline(desc string) (*pipeline, error) {
	var gerr unsafe.Pointer
	el := g.parseLaunch(desc, &gerr)
	if gerr != nil {
		// Any parse error fails, as gst::parse::launch does: a
		// missing element would otherwise leave a partial pipeline.
		if el != nil {
			g.setState(el, gstStateNull)
			g.objectUnref(el)
		}
		return nil, errors.New(g.takeError(gerr))
	}
	if el == nil {
		return nil, errors.New("pipeline description produced nothing")
	}
	p := &pipeline{g: g, el: el, bus: g.getBus(el)}
	if g.setState(el, gstStatePlaying) == gstChangeFailure {
		reason := p.busError("state change to playing failed")
		p.release()
		return nil, errors.New(reason)
	}
	var state, pending int32
	switch g.getState(el, &state, &pending, uint64(startupTimeout)) {
	case gstChangeSuccess, gstChangeNoPreroll:
		return p, nil
	case gstChangeAsync:
		reason := p.busError("pipeline did not start within timeout")
		p.release()
		return nil, errors.New(reason)
	}
	reason := p.busError("state change to playing failed")
	p.release()
	return nil, errors.New(reason)
}

// busError is bus_error: the first error posted on the bus, else
// fallback.
func (p *pipeline) busError(fallback string) string {
	msg := p.g.timedPopFiltered(p.bus, 0, gstMessageError)
	if msg == nil {
		return fallback
	}
	defer p.g.miniObjectUnref(msg)
	return p.errorText(msg)
}

// errorText parses an error message.
func (p *pipeline) errorText(msg unsafe.Pointer) string {
	var gerr, debug unsafe.Pointer
	p.g.parseError(msg, &gerr, &debug)
	if debug != nil {
		p.g.free(debug)
	}
	return p.g.takeError(gerr)
}

// setPaused is set_paused.
func (p *pipeline) setPaused(paused bool) error {
	state := gstStatePlaying
	if paused {
		state = gstStatePaused
	}
	if p.g.setState(p.el, state) == gstChangeFailure {
		return errors.New(p.busError("pipeline state change failed"))
	}
	return nil
}

// stop is Recorder::stop: EOS so the muxer writes its trailer, wait
// for it (or an error) up to eosTimeout, then NULL and release.
func (p *pipeline) stop() {
	if !p.g.sendEvent(p.el, p.g.newEOS()) {
		log.Printf("recorder: failed to send EOS to the recording pipeline")
	}
	if msg := p.g.timedPopFiltered(p.bus, uint64(eosTimeout), gstMessageEOS|gstMessageError); msg != nil {
		p.g.miniObjectUnref(msg)
	}
	p.release()
}

// release sets NULL and drops the references.
func (p *pipeline) release() {
	p.g.setState(p.el, gstStateNull)
	if p.bus != nil {
		p.g.objectUnref(p.bus)
		p.bus = nil
	}
	p.g.objectUnref(p.el)
	p.el = nil
}

// watch is spawn_monitor: the first unexpected error or EOS while
// running is reported on term, until stop closes. Polling keeps the
// bus single-threaded with stop's own pop: watch returns before stop
// pops.
func (p *pipeline) watch(stop <-chan struct{}, term func(reason string)) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			msg := p.g.timedPopFiltered(p.bus, uint64(monitorPoll), gstMessageEOS|gstMessageError)
			if msg == nil {
				continue
			}
			reason := "capture source ended unexpectedly"
			if s := p.g.messageStructure(msg); s != nil && p.g.structureName(s) == "GstMessageError" {
				reason = p.errorText(msg)
			}
			p.g.miniObjectUnref(msg)
			select {
			case <-stop:
			default:
				term(reason)
			}
			return
		}
	}()
	return done
}

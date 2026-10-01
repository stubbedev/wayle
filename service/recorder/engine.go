package recorder

import (
	"context"
	"fmt"
	"log"
	"runtime"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/internal/portal"
	"github.com/stubbedev/wayle/internal/xdg"
)

// GstEngine is the production Engine: the ScreenCast portal on the
// session bus and an in-process GStreamer pipeline.
type GstEngine struct {
	// Conn is the session bus the portal is reached on.
	Conn *dbus.Conn
}

// portalSession is a portal.Stream as a Session.
type portalSession struct{ s *portal.Stream }

func (p portalSession) Cast() Cast {
	return Cast{FD: p.s.Remote.Fd(), Node: p.s.Node, Width: p.s.Width, Height: p.s.Height}
}

func (p portalSession) Close() { p.s.Close() }

// OpenSession implements Engine: one monitor through the portal, the
// restore token in wayle's state dir.
func (e GstEngine) OpenSession(ctx context.Context, showCursor bool) (Session, error) {
	dir, _ := xdg.StateDir()
	s, err := portal.OpenScreenCast(ctx, e.Conn, showCursor, dir)
	if err != nil {
		return nil, fmt.Errorf("screencast portal failed: %w", err)
	}
	return portalSession{s}, nil
}

// Start implements Engine (Recorder::start): the hardware-preferring
// pipeline, retried once on the software encoder when a detected
// hardware encoder fails to reach PLAYING.
func (e GstEngine) Start(session Session, opts Options, term func(reason string)) (Recording, error) {
	g, err := loadGst()
	if err != nil {
		return nil, err
	}
	cast := session.Cast()
	built := BuildPipeline(opts, cast, true, runtime.NumCPU(), g.hasFactory)
	p, err := g.launchPipeline(built.Description)
	if err != nil && built.Hardware {
		log.Printf("recorder: hardware encoder failed (%v); retrying with software", err)
		p, err = g.launchPipeline(BuildPipeline(opts, cast, false, runtime.NumCPU(), g.hasFactory).Description)
	}
	if err != nil {
		return nil, fmt.Errorf("recording failed to start: %w", err)
	}
	stop := make(chan struct{})
	return &gstRecording{p: p, stop: stop, watched: p.watch(stop, term)}, nil
}

// gstRecording is one running pipeline.
type gstRecording struct {
	p       *pipeline
	stop    chan struct{}
	watched <-chan struct{}
}

func (r *gstRecording) SetPaused(paused bool) error { return r.p.setPaused(paused) }

// Stop ends the watch first (the teardown is expected), then
// finalizes.
func (r *gstRecording) Stop() {
	close(r.stop)
	<-r.watched
	r.p.stop()
}

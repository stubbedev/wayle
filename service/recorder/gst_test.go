package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The binding runs real pipelines; the .#go shell provides libgstreamer
// and the base and good plugins.
func testGst(t *testing.T) *libgst {
	t.Helper()
	g, err := loadGst()
	if err != nil {
		t.Fatalf("libgstreamer: %v (run inside nix develop .#go)", err)
	}
	return g
}

func TestGstHasFactory(t *testing.T) {
	g := testGst(t)
	if !g.hasFactory("videotestsrc") {
		t.Error("videotestsrc not found")
	}
	if g.hasFactory("wayle-no-such-element") {
		t.Error("a missing element was found")
	}
}

// A recording plays, pauses, resumes, and on stop sends EOS so the
// muxer finalizes a non-empty file; the watch reports nothing for a
// requested stop.
func TestGstRecordPauseStopFinalizes(t *testing.T) {
	g := testGst(t)
	out := filepath.Join(t.TempDir(), "out.mkv")
	p, err := g.launchPipeline(`videotestsrc is-live=true ! video/x-raw,width=32,height=24,framerate=30/1 ! matroskamux ! filesink location=` + quote(out))
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	reported := make(chan string, 1)
	watched := p.watch(stop, func(reason string) { reported <- reason })
	time.Sleep(150 * time.Millisecond)
	if err := p.setPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := p.setPaused(false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	close(stop)
	<-watched
	p.stop()
	info, err := os.Stat(out)
	if err != nil || info.Size() == 0 {
		t.Fatalf("output: %v (%v)", info, err)
	}
	select {
	case r := <-reported:
		t.Errorf("a requested stop was reported as %q", r)
	default:
	}
}

// A source that ends on its own is reported as unexpected.
func TestGstWatchReportsAnUnexpectedEnd(t *testing.T) {
	g := testGst(t)
	p, err := g.launchPipeline(`videotestsrc num-buffers=3 ! fakesink sync=false`)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	reported := make(chan string, 1)
	watched := p.watch(stop, func(reason string) { reported <- reason })
	select {
	case r := <-reported:
		if r != "capture source ended unexpectedly" {
			t.Errorf("reason = %q", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the EOS was never reported")
	}
	<-watched
	close(stop)
	p.release()
}

func TestGstLaunchFailures(t *testing.T) {
	g := testGst(t)
	if _, err := g.launchPipeline(`wayle-no-such-element ! fakesink`); err == nil || !strings.Contains(err.Error(), "wayle-no-such-element") {
		t.Errorf("unknown element: %v", err)
	}
	// An unwritable path fails the state change with the sink's error.
	_, err := g.launchPipeline(`videotestsrc ! fakesink name=a videotestsrc ! filesink location=/nonexistent/wayle/x.raw`)
	if err == nil {
		t.Fatal("unwritable output: want an error")
	}
	if !strings.Contains(err.Error(), "nonexistent") && !strings.Contains(strings.ToLower(err.Error()), "could not open") {
		t.Errorf("error = %v, want the sink's reason", err)
	}
}

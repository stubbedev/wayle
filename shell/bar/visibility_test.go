package bar

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
)

type fakeLayer struct{ closed bool }

func (f *fakeLayer) Close() { f.closed = true }

func TestBarSetClosesHiddenAndReopensShown(t *testing.T) {
	opened := map[string]int{}
	layers := map[string]*fakeLayer{}
	var failOpen bool
	set := newBarSet(func(o *app.Output, _ config.BarLayout) (barLayer, error) {
		if failOpen {
			return nil, errors.New("no surface")
		}
		opened[o.Name]++
		layers[o.Name] = &fakeLayer{}
		return layers[o.Name], nil
	})
	set.plug(&app.Output{Name: "DP-1"}, config.BarLayout{}, true)
	set.plug(&app.Output{Name: "DP-2"}, config.BarLayout{}, true)
	dp1, dp2 := layers["DP-1"], layers["DP-2"]
	clear(opened)
	if got := set.connectors(); len(got) != 2 || got[0] != "DP-1" || got[1] != "DP-2" {
		t.Fatalf("connectors = %v", got)
	}

	set.apply(map[string]bool{"DP-1": true})
	if !dp1.closed || dp2.closed || len(opened) != 0 {
		t.Fatalf("hide DP-1: dp1 closed %v, dp2 closed %v, opened %v", dp1.closed, dp2.closed, opened)
	}
	set.apply(map[string]bool{"DP-1": true}) // already hidden: nothing happens
	set.apply(map[string]bool{})
	if opened["DP-1"] != 1 || opened["DP-2"] != 0 {
		t.Fatalf("show all: opened %v", opened)
	}

	// A bar that cannot reopen stays hidden and is retried next time.
	set.apply(map[string]bool{"DP-2": true})
	failOpen = true
	set.apply(map[string]bool{})
	failOpen = false
	set.apply(map[string]bool{})
	if opened["DP-2"] != 1 {
		t.Fatalf("retry reopen: opened %v", opened)
	}
}

// A hotplugged output gets its bar when its layout shows one; an
// unplugged one loses it and leaves the set; a known output plugged
// again is not reopened.
func TestBarSetFollowsHotplug(t *testing.T) {
	var layers []*fakeLayer
	set := newBarSet(func(*app.Output, config.BarLayout) (barLayer, error) {
		l := &fakeLayer{}
		layers = append(layers, l)
		return l, nil
	})
	dp1 := &app.Output{Name: "DP-1"}
	set.plug(dp1, config.BarLayout{}, true)
	if len(layers) != 1 || !slices.Equal(set.connectors(), []string{"DP-1"}) {
		t.Fatalf("plug DP-1: layers %d connectors %v", len(layers), set.connectors())
	}
	set.plug(dp1, config.BarLayout{}, true)
	if len(layers) != 1 {
		t.Fatal("a known output was opened twice")
	}
	// A layout without a bar registers the output but opens nothing.
	hdmi := &app.Output{Name: "HDMI-A-1"}
	set.plug(hdmi, config.BarLayout{}, false)
	if len(layers) != 1 || len(set.order) != 2 {
		t.Fatalf("plug hidden HDMI: layers %d order %v", len(layers), set.order)
	}

	set.unplug(dp1)
	if !layers[0].closed || len(set.connectors()) != 0 || len(set.order) != 1 {
		t.Fatalf("unplug DP-1: closed %v connectors %v order %v", layers[0].closed, set.connectors(), set.order)
	}
	// An output that is not registered unplugs as a no-op.
	set.unplug(&app.Output{Name: "DP-9"})
	if len(set.order) != 1 {
		t.Fatalf("unknown unplug changed the set: %v", set.order)
	}

	// A renamed output moves to its new name.
	hdmi.Name = "HDMI-A-2"
	set.plug(hdmi, config.BarLayout{}, true)
	if !slices.Equal(set.order, []string{"HDMI-A-2"}) || len(layers) != 2 {
		t.Fatalf("rename: order %v layers %d", set.order, len(layers))
	}
}

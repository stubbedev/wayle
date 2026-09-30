package bar

import (
	"errors"
	"testing"

	"github.com/stubbedev/gelm/app"

	"github.com/stubbedev/wayle/config"
)

type fakeLayer struct{ closed bool }

func (f *fakeLayer) Close() { f.closed = true }

func TestBarSetClosesHiddenAndReopensShown(t *testing.T) {
	opened := map[string]int{}
	var failOpen bool
	set := newBarSet(func(o *app.Output, _ config.BarLayout) (barLayer, error) {
		if failOpen {
			return nil, errors.New("no surface")
		}
		opened[o.Name]++
		return &fakeLayer{}, nil
	})
	dp1, dp2 := &fakeLayer{}, &fakeLayer{}
	set.add(&app.Output{Name: "DP-1"}, config.BarLayout{}, dp1)
	set.add(&app.Output{Name: "DP-2"}, config.BarLayout{}, dp2)
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

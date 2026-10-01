package bar

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stubbedev/wayle/config"
)

// TestRefresherOrdersAndCoalesces pins the reader every dropdown goes
// through: requests during a read coalesce into one more read, results
// apply in the order their reads started (a read that began before a
// newer one never lands last), requestThen runs after the read that
// starts after it, and a closed dropdown applies nothing more.
// Regression: the battery dropdown's write re-read and its follower
// raced, so a stale profile could overwrite the daemon's newer one.
func TestRefresherOrdersAndCoalesces(t *testing.T) {
	ctx := newTestContext(t, config.Defaults())
	life, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	reads := 0
	gates := make(chan chan struct{}, 8)
	var applied []int
	r := newRefresher(ctx, life, func(context.Context) int {
		mu.Lock()
		reads++
		n := reads
		mu.Unlock()
		gate := make(chan struct{})
		gates <- gate
		<-gate
		return n
	}, func(n int) { applied = append(applied, n) })

	r.request()
	first := <-gates // read 1 runs
	var thenSaw []int
	r.request()
	r.request()
	r.requestThen(func() { thenSaw = slices.Clone(applied) })
	close(first)
	second := <-gates // one coalesced read 2
	close(second)
	waitHeadless(t, "both reads applied", func() bool { return len(applied) == 2 })
	if !slices.Equal(applied, []int{1, 2}) {
		t.Errorf("applied = %v, want 1 then 2", applied)
	}
	mu.Lock()
	if reads != 2 {
		t.Errorf("reads = %d, want 3 requests during a read coalesced into 1", reads)
	}
	mu.Unlock()
	if !slices.Equal(onHeadlessLoop(func() []int { return thenSaw }), []int{1, 2}) {
		t.Errorf("then ran after %v, want after the read that started after it", thenSaw)
	}

	// Closed: a read in flight applies nothing.
	r.request()
	third := <-gates
	cancel()
	close(third)
	r.request()
	select {
	case g := <-gates:
		close(g)
	default:
	}
	if onHeadlessLoop(func() int { return len(applied) }) != 2 {
		t.Errorf("a closed dropdown applied %v", applied)
	}
}

package network

import (
	"context"
	"log"
	"slices"
	"sync/atomic"
	"time"

	"github.com/stubbedev/wayle/internal/feed"
)

// This file brings tunnels back after a suspend (vpn/resume.rs).
//
// NetworkManager tears a VPN down when the machine sleeps (unless its
// profile is persistent, as the openconnect profiles wayle makes are)
// and brings none back on wake: a VPN is never autoconnected. So the
// tunnels up on the way down are recorded and re-activated once the
// network is back, through restore: the agent hands NM whatever it has
// cached, and a session that needs signing into again is left for the
// next connect, since nobody is there to approve a second factor.
//
// A NetworkManager restart is the same story: the tunnels it was
// running when it went away come back once it returns, by the same
// path. Only tunnels that were up are restored; one the user turned off
// stays off, and nothing is recorded across a reboot, so this can never
// become a VPN that dials itself unattended.

const (
	login1Path  = "/org/freedesktop/login1"
	login1Iface = "org.freedesktop.login1.Manager"
)

// resumeTiming is resume.rs's constants, injectable for tests.
type resumeTiming struct {
	// networkWait is how long to wait for the network after a wake;
	// wifi reassociating plus DHCP take tens of seconds.
	networkWait time.Duration
	// attempts per tunnel: the first right after the network is back
	// can be refused ("could not find source connection").
	attempts int
	// retryDelay is the pause between attempts.
	retryDelay time.Duration
	// nmStopWindow is how long before NM goes away a tunnel may have
	// dropped and still count as taken down by NM stopping.
	nmStopWindow time.Duration
	// agentSettle gives the secret agent time to re-register with an
	// NM that just came back; both follow the same bus signal.
	agentSettle time.Duration
}

// defaultResumeTiming is the Rust service's timing.
var defaultResumeTiming = resumeTiming{
	networkWait:  90 * time.Second,
	attempts:     5,
	retryDelay:   3 * time.Second,
	nmStopWindow: 20 * time.Second,
	agentSettle:  2 * time.Second,
}

// restoreRun is one restore in flight: its tunnels, whether it has
// finished, and its cancel.
type restoreRun struct {
	tunnels []string
	done    atomic.Bool
	stop    context.CancelFunc
}

func (r *restoreRun) cancel() {
	if r != nil {
		r.stop()
	}
}

// sleepResume follows logind's PrepareForSleep.
type sleepResume struct {
	vpn     *VPNService
	wasUp   []string
	pending *restoreRun
}

func (r *sleepResume) cancel() { r.pending.cancel() }

func (r *sleepResume) prepareForSleep(ctx context.Context, sleeping bool) {
	if sleeping {
		// A restore still waiting on the network when the machine
		// sleeps again belongs to the previous wake: cancel it, and
		// carry over the tunnels it never got to (a lid bounced on wake
		// sleeps again within the second).
		var unfinished []string
		if r.pending != nil {
			r.pending.cancel()
			if !r.pending.done.Load() {
				unfinished = r.pending.tunnels
			}
			r.pending = nil
		}
		r.wasUp = carriedOver(unfinished, r.vpn.upNow(ctx))
		return
	}
	tunnels := r.wasUp
	r.wasUp = nil
	if len(tunnels) == 0 {
		return
	}
	r.pending = r.vpn.startRestore(ctx, tunnels, 0)
}

// restartResume follows NetworkManager's bus name.
type restartResume struct {
	vpn     *VPNService
	owed    []string
	pending *restoreRun
}

func (r *restartResume) cancel() { r.pending.cancel() }

func (r *restartResume) ownerChanged(ctx context.Context, wentAway, cameBack bool) {
	s := r.vpn
	if wentAway {
		r.pending.cancel()
		r.pending = nil
		now := time.Now()
		s.mu.Lock()
		rows := make([]nmStopRow, 0, len(s.entries))
		for _, e := range s.entries {
			rows = append(rows, nmStopRow{uuid: e.uuid, state: e.state, wentDownAt: e.wentDownAt, turnedOff: e.turnedOff})
		}
		r.owed = owedAfterNMStop(rows, now, s.timing.nmStopWindow)
		// Nothing runs a tunnel without NM, and the objects whose
		// signals would have said so went away with it.
		for _, e := range s.entries {
			if e.state.isUp() {
				e.state = VPNDisconnected
			}
		}
		s.watched = nil
		s.mu.Unlock()
		feed.Notify(&s.changes)
	}
	if cameBack {
		tunnels := r.owed
		r.owed = nil
		if len(tunnels) == 0 {
			return
		}
		r.pending = s.startRestore(ctx, tunnels, s.timing.agentSettle)
	}
}

// nmStopRow is one VPN as NetworkManager went away.
type nmStopRow struct {
	uuid       string
	state      VPNState
	wentDownAt time.Time
	turnedOff  bool
}

// owedAfterNMStop is which tunnels to bring back once NM returns: one
// still up went away with NM, and one that went down within the stop
// window was taken down by NM stopping, unless someone turned it off,
// however recently.
func owedAfterNMStop(rows []nmStopRow, now time.Time, window time.Duration) []string {
	var owed []string
	for _, row := range rows {
		droppedJustNow := !row.wentDownAt.IsZero() && now.Sub(row.wentDownAt) <= window
		if row.state.isUp() || droppedJustNow && !row.turnedOff {
			owed = append(owed, row.uuid)
		}
	}
	return owed
}

// upNow is the known VPNs NM is running right now, read straight off
// NM rather than off the rows, which follow a signal behind: NM starts
// tearing tunnels down on the same sleep signal.
func (s *VPNService) upNow(ctx context.Context) []string {
	active, err := s.nm.activeByUUID(ctx)
	if err != nil {
		return nil
	}
	states := make(map[string]ActiveState, len(active))
	for uuid, path := range active {
		states[uuid] = s.nm.activeState(ctx, path)
	}
	s.mu.Lock()
	known := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		known = append(known, e.uuid)
	}
	s.mu.Unlock()
	return toRestore(states, known)
}

// toRestore is which active connections to bring back: the listed VPNs
// among them that were up or on their way (deactivating counts: NM may
// already be taking the tunnel down because of the sleep). Anything
// else NM runs, the wifi or the wired link, is NM's own to bring back.
// The result follows the rows' order.
func toRestore(active map[string]ActiveState, known []string) []string {
	var out []string
	for _, uuid := range known {
		switch active[uuid] {
		case ActiveActivating, ActiveActivated, ActiveDeactivating:
			out = append(out, uuid)
		}
	}
	return out
}

// carriedOver is what to restore on the next wake: the tunnels an
// interrupted restore still owed, then the ones up now, each once.
func carriedOver(unfinished, up []string) []string {
	tunnels := slices.Clone(unfinished)
	for _, uuid := range up {
		if !slices.Contains(tunnels, uuid) {
			tunnels = append(tunnels, uuid)
		}
	}
	return tunnels
}

// startRestore runs a restore of tunnels after delay, cancelled by the
// returned run or ctx.
func (s *VPNService) startRestore(ctx context.Context, tunnels []string, delay time.Duration) *restoreRun {
	runCtx, stop := context.WithCancel(ctx)
	run := &restoreRun{tunnels: tunnels, stop: stop}
	go func() {
		defer run.done.Store(true)
		if delay > 0 && !sleepCtx(runCtx, delay) {
			return
		}
		s.restoreAll(runCtx, tunnels)
	}()
	return run
}

// restoreAll waits for the network, then brings each tunnel back.
func (s *VPNService) restoreAll(ctx context.Context, tunnels []string) {
	if !s.networkBack(ctx) {
		return
	}
	for _, uuid := range tunnels {
		row, ok := s.Get(uuid)
		if !ok {
			// Deleted while the machine slept: nothing to restore.
			continue
		}
		err := s.restoreOne(ctx, uuid)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("network: %s: cannot restore VPN after resume: %v", row.Name, err)
		} else {
			log.Printf("network: %s: VPN restored after resume", row.Name)
		}
	}
}

// restoreOne brings one tunnel back, retrying while NM settles. A
// cancelled restore gives up quietly.
func (s *VPNService) restoreOne(ctx context.Context, uuid string) error {
	for attempt := 1; ; attempt++ {
		err := s.restore(ctx, uuid)
		if err == nil || attempt >= s.timing.attempts {
			return err
		}
		if !sleepCtx(ctx, s.timing.retryDelay) {
			return nil
		}
	}
}

// networkBack reports whether NM has a usable network within the
// wait. Subscribed before the first read, so a change landing between
// the two is not missed.
func (s *VPNService) networkBack(ctx context.Context) bool {
	waitCtx, cancel := context.WithTimeout(ctx, s.timing.networkWait)
	defer cancel()
	changes := s.stateChanges.SubscribeContext(waitCtx)
	state := s.nm.managerState(waitCtx)
	for !isNetworkBack(state) {
		if _, ok := <-changes; !ok {
			if ctx.Err() == nil {
				log.Printf("network: the network did not come back after resume; not restoring VPNs")
			}
			return false
		}
		s.mu.Lock()
		state = s.managerState
		s.mu.Unlock()
	}
	return true
}

// isNetworkBack is whether an NM state has a default route a tunnel can
// ride. ConnectedSite counts (the connectivity check has not passed,
// which a gateway reachable over the local route does not care about);
// ConnectedLocal does not: an activation now is refused outright.
func isNetworkBack(state State) bool {
	return state == StateConnectedSite || state == StateConnectedGlobal
}

// sleepCtx waits d, reporting false when ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

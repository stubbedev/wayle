package bar

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/i18n"
	"github.com/stubbedev/wayle/internal/xdg"
	"github.com/stubbedev/wayle/service/hyprsunset"
)

// Hyprsunset cadences (watchers.rs).
const (
	hyprsunsetStatePoll    = time.Second
	hyprsunsetScheduleTick = time.Minute
	hyprsunsetLocationEach = 6 * time.Hour
)

// hyprsunsetLabel is helpers.rs's build_label: On/Off status with the
// live temp and gamma while running, "--" placeholders when off.
func hyprsunsetLabel(format string, enabled bool, temp, gamma, configTemp, configGamma int) string {
	status, tempText, gammaText := i18n.T("bar-hyprsunset-off"), "--", "--"
	if enabled {
		status, tempText, gammaText = i18n.T("bar-hyprsunset-on"), strconv.Itoa(temp), strconv.Itoa(gamma)
	}
	out := replaceTemplateVar(format, "status", status)
	out = replaceTemplateVar(out, "temp", tempText)
	out = replaceTemplateVar(out, "gamma", gammaText)
	out = replaceTemplateVar(out, "config_temp", strconv.Itoa(configTemp))
	out = replaceTemplateVar(out, "config_gamma", strconv.Itoa(configGamma))
	return out
}

// sunsetEnv is everything the module touches outside itself; the shell
// wires the real filter, socket, clock, state dir, and GeoClue, tests
// fake them.
type sunsetEnv struct {
	start    func(temp, gamma int) error
	stop     func() error
	query    func() (hyprsunset.State, bool)
	now      func() time.Time
	stateDir string
	// locate resolves the schedule location; nil disables the lookup.
	locate func(ctx context.Context) (hyprsunset.Location, error)
}

// liveSunsetEnv is the real environment.
func liveSunsetEnv() sunsetEnv {
	filter := hyprsunset.NewFilter()
	dir, _ := xdg.StateDir()
	return sunsetEnv{
		start: filter.Start,
		stop:  filter.Stop,
		query: func() (hyprsunset.State, bool) {
			sock, ok := hyprsunset.SocketPath()
			if !ok {
				return hyprsunset.State{}, false
			}
			return hyprsunset.QueryState(sock)
		},
		now:      time.Now,
		stateDir: dir,
		locate: func(ctx context.Context) (hyprsunset.Location, error) {
			conn, err := dbus.ConnectSystemBus()
			if err != nil {
				return hyprsunset.Location{}, err
			}
			defer func() { _ = conn.Close() }()
			return hyprsunset.QueryLocation(ctx, conn)
		},
	}
}

// hyprsunsetModule is the night-light toggle with the solar
// auto-schedule (hyprsunset/mod.rs).
type hyprsunsetModule struct {
	ctx   ModuleContext
	env   sunsetEnv
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget

	enabled bool
	temp    int
	gamma   int
	// autoPhase is the last phase the schedule applied (zero while the
	// schedule is off); manualOverride holds a manual toggle until the
	// next sunrise/sunset.
	autoPhase      hyprsunset.Phase
	manualOverride bool
	// geo is the GeoClue location, preferred over the configured one.
	geo    *hyprsunset.Location
	cancel context.CancelFunc
}

func newHyprsunset(ctx ModuleContext) (Module, error) {
	return newHyprsunsetWith(ctx, liveSunsetEnv()), nil
}

// newHyprsunsetWith builds the module over env: it replays a remembered
// manual toggle, then (with the loop) polls the filter, ticks the
// schedule, and resolves the location.
func newHyprsunsetWith(ctx ModuleContext, env sunsetEnv) *hyprsunsetModule {
	cfg := ctx.Config.Hyprsunset
	m := &hyprsunsetModule{ctx: ctx, env: env, temp: int(cfg.Temperature), gamma: int(cfg.Gamma)}
	m.label = widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)
	m.icon = moduleIcon(ctx, cfg.IconOffView())
	m.root = assembleModule(ctx, m.icon, m.label)

	// The filter dies with the shell, so a manual toggle is replayed
	// rather than lost to the schedule.
	if restored, ok := hyprsunset.LoadOverride(env.stateDir, env.now()); ok {
		m.autoPhase, m.manualOverride = restored.Phase, true
		if restored.Enabled {
			m.startFilter()
		}
	}
	m.render()
	if ctx.App != nil {
		m.run()
	}
	return m
}

// run starts the watchers: the 1s state poll, the 60s schedule tick,
// and the GeoClue lookup (at start and every six hours while the
// schedule is on).
func (m *hyprsunsetModule) run() {
	runCtx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go func() {
		poll := time.NewTicker(hyprsunsetStatePoll)
		tick := time.NewTicker(hyprsunsetScheduleTick)
		defer poll.Stop()
		defer tick.Stop()
		m.ctx.Invoke(m.evaluate)
		for {
			select {
			case <-runCtx.Done():
				return
			case <-poll.C:
				st, ok := m.env.query()
				m.ctx.Invoke(func() { m.applyState(st, ok) })
			case <-tick.C:
				m.ctx.Invoke(m.evaluate)
			}
		}
	}()
	if m.ctx.Config.Hyprsunset.AutoSchedule && m.env.locate != nil {
		go func() {
			for {
				lookup, done := context.WithTimeout(runCtx, 30*time.Second)
				loc, err := m.env.locate(lookup)
				done()
				if err == nil {
					m.ctx.Invoke(func() { m.setLocation(loc) })
				}
				select {
				case <-runCtx.Done():
					return
				case <-time.After(hyprsunsetLocationEach):
				}
			}
		}()
	}
}

// Stop ends the watchers.
func (m *hyprsunsetModule) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// render is update_display: the state icon and the label.
func (m *hyprsunsetModule) render() {
	cfg := m.ctx.Config.Hyprsunset
	text := ""
	if cfg.LabelShow {
		text = hyprsunsetLabel(cfg.Format, m.enabled, m.temp, m.gamma, int(cfg.Temperature), int(cfg.Gamma))
	}
	m.label.SetText(text)
	if m.icon != nil {
		icon := cfg.IconOffView()
		if m.enabled {
			icon = cfg.IconOnView()
		}
		m.icon.SetThemeName(icon.Name)
	}
}

// applyState is StateChanged: a running filter reports its values, a
// silent socket means off (with the configured values shown).
func (m *hyprsunsetModule) applyState(st hyprsunset.State, running bool) {
	cfg := m.ctx.Config.Hyprsunset
	temp, gamma := int(cfg.Temperature), int(cfg.Gamma)
	if running {
		temp, gamma = st.Temp, st.Gamma
	}
	if m.enabled == running && m.temp == temp && m.gamma == gamma {
		return
	}
	m.enabled, m.temp, m.gamma = running, temp, gamma
	m.render()
}

// currentPhase is the phase here and now: the GeoClue location when
// resolved, else the configured coordinates.
func (m *hyprsunsetModule) currentPhase() hyprsunset.Phase {
	cfg := m.ctx.Config.Hyprsunset
	lat, lng := cfg.Latitude, cfg.Longitude
	if m.geo != nil {
		lat, lng = m.geo.Latitude, m.geo.Longitude
	}
	return hyprsunset.PhaseAt(m.env.now(), lat, lng)
}

// setLocation is LocationResolved: a new fix re-evaluates at once.
func (m *hyprsunsetModule) setLocation(loc hyprsunset.Location) {
	if m.geo != nil && *m.geo == loc {
		return
	}
	m.geo = &loc
	m.evaluate()
}

// evaluate is evaluate_schedule: night turns the filter on, day off; a
// manual toggle holds until the phase changes, which clears it and the
// remembered record.
func (m *hyprsunsetModule) evaluate() {
	if !m.ctx.Config.Hyprsunset.AutoSchedule {
		m.autoPhase, m.manualOverride = 0, false
		return
	}
	phase := m.currentPhase()
	if m.autoPhase != phase {
		m.autoPhase = phase
		m.manualOverride = false
		hyprsunset.ClearOverride(m.env.stateDir)
	}
	if m.manualOverride {
		return
	}
	if wantOn := phase == hyprsunset.PhaseNight; wantOn != m.enabled {
		if wantOn {
			m.startFilter()
		} else {
			m.stopFilter()
		}
	}
}

// RunAction handles the module's own :toggle; the rest fall through.
func (m *hyprsunsetModule) RunAction(action config.ClickAction) {
	if action.Kind == config.ClickShell && action.Command == ":toggle" {
		m.toggle()
		return
	}
	runClickAction(m.ctx, action)
}

// toggle is the left-click :toggle: under the schedule it sets the
// override (seeding the phase when the schedule has not ticked yet),
// records the new state for restarts, and flips the filter.
func (m *hyprsunsetModule) toggle() {
	if m.ctx.Config.Hyprsunset.AutoSchedule {
		m.manualOverride = true
		if m.autoPhase == 0 {
			m.autoPhase = m.currentPhase()
		}
	}
	hyprsunset.SaveOverride(m.env.stateDir, hyprsunset.Override{Phase: m.autoPhase, Enabled: !m.enabled}, m.env.now())
	if m.enabled {
		m.stopFilter()
		return
	}
	m.startFilter()
}

// startFilter spawns the filter at the configured values.
func (m *hyprsunsetModule) startFilter() {
	cfg := m.ctx.Config.Hyprsunset
	if err := m.env.start(int(cfg.Temperature), int(cfg.Gamma)); err != nil {
		log.Printf("hyprsunset: start: %v", err)
		return
	}
	m.enabled, m.temp, m.gamma = true, int(cfg.Temperature), int(cfg.Gamma)
	m.render()
}

// stopFilter terminates the filter.
func (m *hyprsunsetModule) stopFilter() {
	if err := m.env.stop(); err != nil {
		log.Printf("hyprsunset: stop: %v", err)
	}
	cfg := m.ctx.Config.Hyprsunset
	m.enabled, m.temp, m.gamma = false, int(cfg.Temperature), int(cfg.Gamma)
	m.render()
}

func (m *hyprsunsetModule) Root() widget.Widget { return m.root }

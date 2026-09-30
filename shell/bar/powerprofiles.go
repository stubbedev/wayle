package bar

import (
	"context"
	"errors"
	"log"

	"github.com/stubbedev/gelm/render"
	"github.com/stubbedev/gelm/widget"

	"github.com/stubbedev/wayle/config"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/styling"
)

// powerProfilesLabel is helpers.rs's format_label: the only variable
// is the active profile's name.
func powerProfilesLabel(format, profile string) string {
	return replaceTemplateVar(format, "profile", profile)
}

// powerProfiles is the module: the active power profile, cycling on
// the `:cycle` binding.
type powerProfilesModule struct {
	ctx   ModuleContext
	src   powerprofiles.Source
	label *widget.Label
	icon  *widget.Icon
	root  widget.Widget
	snap  powerprofiles.Snapshot
	stop  func()
}

func newPowerProfiles(ctx ModuleContext) (Module, error) {
	if ctx.PowerProfiles == nil {
		return nil, errors.New("power-profiles: no power-profiles-daemon available")
	}
	m := &powerProfilesModule{ctx: ctx, src: ctx.PowerProfiles, label: widget.NewLabel(ctx.Font, ctx.Style.labelPx, "", ctx.Style.fg)}
	m.icon = moduleIcon(ctx, ctx.Config.PowerProfiles.Icons[config.ProfileBalanced])
	m.root = assembleModule(ctx, m.icon, m.label)
	if err := m.refresh(); err != nil {
		return nil, err
	}
	ticks, stop, err := ctx.PowerProfiles.Subscribe(context.Background())
	if err != nil {
		return nil, err
	}
	m.stop = stop
	go func() {
		for range ticks {
			m.ctx.Invoke(func() { _ = m.refresh() })
		}
	}()
	return m, nil
}

// refresh re-reads the daemon and restyles label and icon for the
// active profile.
func (m *powerProfilesModule) refresh() error {
	cfg := m.ctx.Config.PowerProfiles
	snap, err := m.src.Read(context.Background())
	if err != nil {
		return err
	}
	m.snap = snap
	text := ""
	if cfg.LabelShow && snap.Available {
		text = powerProfilesLabel(cfg.Format, snap.Active)
	}
	m.label.SetText(text)
	m.label.SetColor(m.profileColor(snap.Active))
	if setter := m.icon; setter != nil {
		setter.SetTint(m.profileColor(snap.Active))
		setter.SetThemeName(cfg.Icons[snap.Active].Name)
	}
	return nil
}

// profileColor resolves the active profile's configured color, the
// bar fg otherwise.
func (m *powerProfilesModule) profileColor(profile string) render.Color {
	color, ok := m.ctx.Config.PowerProfiles.Colors[profile]
	if !ok {
		return m.ctx.Style.fg
	}
	if resolved, ok := styling.ResolveColor(color, m.ctx.Style.palette); ok {
		return resolved
	}
	return m.ctx.Style.fg
}

// RunAction handles the module's own bindings: `:cycle` moves to the
// next profile in the daemon's available set; the rest fall through
// to the shared executor.
func (m *powerProfilesModule) RunAction(action config.ClickAction) {
	if action.Kind == config.ClickShell && action.Command == ":cycle" {
		m.cycle()
		return
	}
	runClickAction(m.ctx, action)
}

// cycle writes the next profile; the daemon's PropertiesChanged tick
// redraws. Synchronous: the click handler runs on the loop and the
// tick repaints.
func (m *powerProfilesModule) cycle() {
	next := powerprofiles.NextProfile(m.snap.Active, m.snap.Profiles)
	if err := m.src.SetActive(context.Background(), next); err != nil {
		log.Printf("power-profiles: cycle: %v", err)
	}
}

func (m *powerProfilesModule) Root() widget.Widget { return m.root }

// Stop releases the subscription.
func (m *powerProfilesModule) Stop() {
	if m.stop != nil {
		m.stop()
	}
}

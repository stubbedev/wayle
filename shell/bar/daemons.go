package bar

import (
	"log"

	"github.com/godbus/dbus/v5"

	"github.com/stubbedev/wayle/service/mpris"
	"github.com/stubbedev/wayle/service/powerprofiles"
	"github.com/stubbedev/wayle/service/pulse"
	"github.com/stubbedev/wayle/service/sni"
)

// serveCLIDaemons exports the session-bus interfaces the `wayle` CLI
// drives (the Rust services' with_daemon registrations in
// wayle-shell/src/bootstrap). Each is optional, as in Rust: a service
// that cannot start is logged and its CLI reports it as not running.
// The returned release drops every name and the connection.
func serveCLIDaemons(ctx ModuleContext, tray *sni.Host) func() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("cli daemons: session bus: %v", err)
		return func() {}
	}
	var releases []func()
	serve := func(what string, release func(), err error) {
		if err != nil {
			log.Printf("%s: daemon: %v", what, err)
			return
		}
		releases = append(releases, release)
	}
	if ctx.PowerProfiles != nil {
		release, err := powerprofiles.ServeDaemon(conn, ctx.PowerProfiles)
		serve("power-profiles", release, err)
	}
	if mixer, ok := ctx.Pulse.(pulse.Mixer); ok {
		release, err := pulse.ServeDaemon(conn, mixer)
		serve("audio", release, err)
	}
	if media, err := mpris.NewController(conn, ctx.Config.Media.PlayersIgnored, ctx.Config.Media.PlayerPriority); err == nil {
		releases = append(releases, media.Close)
		release, err := mpris.ServeDaemon(conn, media)
		serve("media", release, err)
	} else {
		log.Printf("media: controller: %v", err)
	}
	if tray != nil {
		release, err := sni.ServeDaemon(conn, ctx.SNI, tray.Actions(), tray.IsWatcher())
		serve("systray", release, err)
	}
	return func() {
		for _, release := range releases {
			release()
		}
		_ = conn.Close()
	}
}

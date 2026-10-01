# Go rewrite

The `go-rewrite` branch carries wayle's port from Rust/GTK4 to Go on top
of [gelm](https://github.com/stubbedev/gelm), the pure-Go Wayland widget
kit (issues #18, #19). Direction of travel: the toolkit already exists;
this branch is the consumer side — port order bar → OSD/toasts →
launcher → lock screen → settings.

## Layout

| Go package | Ports | Notes |
| --- | --- | --- |
| `config` | wayle-config | One cfg-tag reflection layer derives loading, the runtime layer, `wayle config`, and the JSON Schema; schema_test holds every def to schema/wayle-config.schema.json. Same discovery, imports, YAML, and failure behavior as Rust: a bad value is a per-field diagnostic that keeps that field's default. |
| `strftime` | chrono strftime formatting | Format strings are validated at load; an unsupported specifier is an error, not garbage at runtime. |
| `styling` | wayle-styling | theme_css, the palette color math, the matugen/pywal/wallust providers, the user stylesheet (`internal/scss` compiles its SCSS subset), and the embedded compiled Rust stylesheet bundle (`just go-css` refreshes it). |
| `shell/*` | wayle-shell | bar, osd, popups, lock, credential (the lock/greeter prompt), screenshot, regionoverlay, colorpicker, sharepicker, wallpaper. |
| `greeter`, `cmd/wayle-greeter` | wayle-greeter | WIP: compiles and passes lint; the greetd screen is unfinished. |
| `cmd/wayle` | wayle bin | The full clap tree on `internal/cli` (help, errors, and completions byte-for-byte with clap). |
| `cmd/wayle-lock` | wayle-lock | One-token `wayle lock`. |

## Running

```sh
nix develop .#go -c just go-check   # fmt + vet + golangci-lint + test (pinned toolchain)
go run ./cmd/wayle shell            # the bar, from ~/.config/wayle/config.toml
```

The `.#go` devShell is the Go counterpart of the Rust devShell: `go_1_27`
pinned to the go.mod version (no toolchain downloads), the gelm lint stack,
fontconfig with the config's default fonts wired over the host fontconfig
(FONTCONFIG_FILE), sway for a headless compositor session, and dbus for the
private test buses. gelm is pure Go, so the shell sets `CGO_ENABLED=0` and
needs no native libraries (PAM goes through purego).

Lint enforcement is gelm's `.golangci.yml` verbatim (gci section
repointed), wired into the justfile as `go-lint` / `go-check`.

## Status

- [x] config: every root section of the Rust schema at its keys and
      defaults (oracle-tested), YAML and imports, the runtime layer
      (runtime.toml), hot reload (bars rebuild, OSD and popups take
      their sections), `wayle config get/set/reset/schema/default`
- [x] i18n: the Fluent runtime, locale negotiation, both embedded
      domains; bar labels, dropdowns, and the OSD brightness label
- [x] styling: theme_css with Rust goldens, providers, the user
      stylesheet, the compiled Rust stylesheet; the bar is the Rust
      BarButton tree styled by that bundle (the inset/border
      workarounds are gone); threshold colors through the bar-button
      variables
- [x] bar: every default-layout module plus dashboard, user-session,
      screenshot, media (stateful MPRIS service, icon modes, the player
      dropdown with transport controls), systray (full watcher,
      DBusMenu, IconPixmap), hyprsunset (solar auto-schedule over
      GeoClue, persisted override), volume icon-muted, popup
      hover-pause
- [x] workspaces: hyprland, sway, niri, mango
- [x] services: upower, brightness (sysfs, logind, DDC/CI, uevent
      hotplug), pulse (native protocol in pure Go, com.wayle.Audio1),
      mpris (com.wayle.Media1 on the bar's service), network (secret
      agent, native VPNs, openconnect sign-ins incl. GlobalProtect SAML,
      wifi controls), bluetooth (full BlueZ model, pairing agent),
      sysinfo, weather, mail (inotify), clipboard (data-control
      history), wallpaper (cycling, com.wayle.Wallpaper1, extractor),
      launcher engine and modes, auth (PAM via purego, greetd),
      shellipc (com.wayle.Shell1: bar visibility, Lock, VPN callback)
- [x] surfaces: lock screen (ext-session-lock, logind triggers,
      LockedHint recovery), screenshot + freeze-frame region overlay,
      color picker, share picker (com.wayle.SharePicker1), wallpaper
      layers with transitions
- [x] CLI: audio, idle, launcher (rofi flags and the rofi symlink, the
      socket lifetime contract), lock, media, notify, panel, power,
      recorder, screenshot, systray, toast, vpn, wallpaper, widget,
      shell; daily log files

## Remaining for feature parity

### Parked work (local branches)

- [ ] `collect/launcher` (tip commit): the launcher views
      (`shell/launcher`, `internal/pango`); unfinished, 113 unused
      symbols.
- [ ] `collect/icons`: `wayle icons` with a usvg/skia-path port for
      icon transforms; unfinished, fails lint.

### Bar

- [x] dashboard dropdown: quick actions, volume, now playing, battery,
      network, system rings, user session
- [x] calendar, mail, brightness, battery (with power profiles and
      the charge limit), audio (default devices, app volumes, device
      pickers), and notification dropdowns, section by section
- [x] recorder: the Rust engine (ScreenCast portal, in-process
      GStreamer through purego) and its dropdown
- [x] the dropdowns still thin stand-ins: treeman and network
- [x] notification history persistence: the Rust notifications.db,
      schema and zvariant hint JSON, read and written both ways; the
      raw hints ride on each notification (the sound, category and
      position hints are decoded by Rust and consumed nowhere)
- [x] notification popup cards: close and action buttons, the default
      action, the urgency bar, popup layer/margins/shadow keys (cards
      styled by the shell stylesheet; layers through shell/layering with
      tearing mode)
- [ ] notification popup enter/exit animations (the Rust WayleRevealer
      with the notifications animation surface)
- [x] format strings render through internal/jinja (minijinja-checked)
- [x] the custom module: icon-map/icon-names, color-map, class-format,
      tooltips, on-action, restart policies
- [x] cpu/ram/storage: frequencies, temperatures, and byte columns
- [ ] systray: nested submenu popups (gelm has no popup-in-popup),
      live accelerators, popovers that grow with their content
- [ ] cava: the wave style and the stereo split (error at creation),
      the `stereo`/`waves`/`input` keys
- [x] network dialogs: secret prompt, wifi password, wifi list, the VPN
      rows and the VPN editor (typed and raw fields, WireGuard key
      generation and wg-quick import through the portal, inline delete
      confirm; a profile whose plugin is gone opens raw and keeps its type)
- [x] bluetooth: the pairing notifier without a placed module (started
      with the bar, as every Rust bar warms its bluetooth dropdown)
- [x] brightness dropdown: friendly names for DDC monitors (the Rust
      friendly_device_name; neither reads EDID model names)
- [x] weather: the three providers, retrying poll service, condition
      icons, the full dropdown; `[dropdowns.*]` sizes for every dropdown

### Config

- [ ] `wayle config docs` (the VitePress page generator)
- [ ] hot reload beyond the bar/OSD/popups: wallpaper, lock,
      launcher, and the other watchers the Rust services run

### Shell surfaces

- [ ] launcher views (`collect/launcher`)
- [ ] greeter screen (WIP in `greeter/`)
- [ ] portal dialogs: file chooser, color picker, print, share picker
      (`wayle portal share-picker` should call `sharepicker.NewClient`)
- [ ] the share-preview gbm dmabuf allocator and PipeWire producer
- [ ] enter/exit animations for every transient surface (gelm has no
      public animation API or revealer)

### Binaries

- [ ] wayle-settings (the settings GUI; the largest single item)
- [ ] xdg-desktop-portal-wayle (wayle-portal backend)

### Cross-cutting

- [x] a live compositor session smoke test (headless sway: start, three
      config reloads incl. a side bar and a bad value, no fatal exits)
- [ ] packaging (nix package, systemd unit, portal files) still points
      at the Rust binaries; the Go binary dlopens libgstreamer (and its
      plugins via GST_PLUGIN_SYSTEM_PATH_1_0) for the recorder, so the
      package must wrap both, as nix/package.nix does for Rust
- [ ] drop the go.mod replace once gelm tags a release
- [x] module lifetime: each bar is a mount generation; `follow` and
      `ModuleContext.Life` end subscriptions and tickers with it

## gelm gaps and the shortcuts they force

Each entry is a workaround wayle carries because gelm lacks the
capability. Remedy it in gelm, then delete the workaround in wayle.

| gelm lacks | wayle workaround (where) | remedy in gelm |
| --- | --- | --- |
| a public animation API and a revealer (tweens live in `internal/anim`) | no enter/exit animations anywhere in the Go shell | export the tween/easing API and add a Revealer (slide/crossfade) widget |
| popups inside popups | tray submenus slide in place | nested popup surfaces |
| an fd/file watcher on the event loop | inotify goroutines + Invoke (internal/fswatch) | optional: `app.WatchFD` |
| `LayerConfig.Output = nil` (documented as "compositor chooses") panics in the Wayland binding | callers always pass an output | send a null output |

Closed in gelm since the inventory: popovers driven by the application loop (focused Entry input, loop-driven repaint, layer get_popup, rect-anchored placement, clicks and the wheel inside them), GTK-style give-way layout (a column shrinks its expanding Scroll; Scroll.VerticalOnly and SetMaxContentHeight), the CSS engine (var(), calc(),
color-mix(), :not(), structural selectors, the box model, per-side
padding and borders, rounded rings, gradients, box shadows), raw-pixel
icons, ext-session-lock, the capture protocols and dmabuf import,
data-control, Button.BgExplicit, menu row icons and MenuStack.

## Decisions

- **Audio speaks the PulseAudio native protocol in pure Go.**
  `service/pulse/native` is a focused client (protocol 32, no shared
  memory, servers from 24), so it serves PulseAudio and pipewire-pulse
  alike. It is used instead of github.com/jfreymuth/pulse, whose reader
  drops decode errors, reads past frame boundaries, and can panic on an
  empty blob. It is tested against `service/pulse/pulsetest`, a fake
  server speaking real frames. Like the Rust backend, it does not
  reconnect.
- **cava is a cavacore.c translation fed by the native client**: a
  record stream on the default sink's monitor (s16le mono, 44.1 kHz,
  512-frame fragments) that follows default-device changes.
- **One implementation of each shared piece**: `internal/dbuscli`
  (dbus.rs format_error, method vs property), `internal/dbustest`
  (private buses), `internal/dbusx` (zbus-style exports),
  `service/shellipc` (the only com.wayle.Shell1), `internal/desktopentry`
  (gio's key file and app list).

## Known deviations from the Rust shell

- Workspace modules re-query the compositor on each relevant event
  instead of folding events into a local state; the rendered result is
  the same.
- The `replace` directive in `go.mod` points at the local gelm checkout;
  the require line tracks the latest pushed gelm commit wayle needs.
- Audio setters return server errors instead of firing and forgetting;
  the default device is looked up by name on every read.
- hyprsunset's solar math uses the correct longitude sign; Rust's
  solar.rs mirrors every schedule about Greenwich (fix there:
  `jStar = n + lW/360`).
- The clipboard history keeps recording after a restore (Rust stops for
  the session: its skip-own-offer slot is never cleared).
- Bluetooth refusals answer Rejected/Canceled rather than a generic
  Failed, and passkey entry sends a passkey (Rust sent it as a PIN).
- openconnect keeps cookies set on a redirect (Rust drops them, which
  breaks Juniper/Pulse gateways).
- A config reload rebuilds a custom module and restarts its command;
  the Rust module keeps a watch command running when only its look
  changed. The last output carries over, so the label does not blank.
- The weather dropdown's refresh and retry refetch with the current
  settings; Rust re-sets the location as a city, so coordinates were
  geocoded as a place name on retry.
- Screenshot frames decode by their real pixel format (24- and 10-bit
  included) and honor y-invert; Rust assumed XRGB.

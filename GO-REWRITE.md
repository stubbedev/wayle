# Go rewrite

The `go-rewrite` branch carries wayle's port from Rust/GTK4 to Go on top
of [gelm](https://github.com/stubbedev/gelm), the pure-Go Wayland widget
kit (issues #18, #19). Direction of travel: the toolkit already exists;
this branch is the consumer side — port order bar → OSD/toasts →
launcher → lock screen → settings.

## Layout

| Go package | Ports | Notes |
| --- | --- | --- |
| `config` | wayle-config (bar/general/clock subset) | Same files, discovery order, keys, defaults, and failure behavior: a bad value is a load error and the shell falls back to defaults. |
| `strftime` | chrono strftime formatting | Format strings are validated at load; an unsupported specifier is an error, not garbage at runtime. |
| `shell/bar` | wayle-shell bar | Anchor/exclusive-zone/namespace mapping matches `bar/methods.rs`; `FindLayout` ports `find_layout` + `merge_parent` (extends, cycles, `*` fallback). |
| `cmd/wayle` | wayle bin | `wayle shell` runs the bar; unported subcommands say so and exit 1. |

## Running

```sh
nix develop .#go -c just go-check   # fmt + vet + golangci-lint + test (pinned toolchain)
go run ./cmd/wayle shell            # the bar, from ~/.config/wayle/config.toml
```

The `.#go` devShell is the Go counterpart of the Rust devShell: `go_1_27`
pinned to the go.mod version (no toolchain downloads), the gelm lint stack,
fontconfig with the config's default fonts wired over the host fontconfig
(FONTCONFIG_FILE), and sway for a headless compositor session. gelm is pure
Go, so the shell sets `CGO_ENABLED=0` and needs no native libraries.

Lint enforcement is gelm's `.golangci.yml` verbatim (gci section
repointed), wired into the justfile as `go-lint` / `go-check`.

## Status

- [x] config: paths, discovery, bar/general/clock/cava/separator/
      hyprland-workspaces subsets, defaults, load errors
- [x] styling: palette + full token table, ColorValue/Size/rounding resolution
- [x] bar: layer surfaces per output, layout resolution, chrome (classes,
      opacity mix, borders, insets, groups), clock, cava, separator,
      hyprland-workspaces, battery, brightness, volume, media,
      keyboard-layout, microphone, bluetooth modules - the default
      layout's module set is fully covered
- [x] service/hyprland: command + event sockets, dispatch, clients,
      workspace rules, the v2 window events
- [x] workspaces: hyprland, sway, niri, and mango modules at full
      schema on one shared widget layer (filtering, placeholders and
      rules, relative numbering, app icons, urgent pulse, all five
      bindings, the _workspaces.scss cascade); service/sway (i3 IPC
      with GET_TREE), service/niri (niri-ipc JSON lines), service/mango
      (dispatch + watch frames)
- [x] service/upower, service/brightness (sysfs+logind+inotify),
      service/pulse (pactl sink+source), service/mpris (MPRIS2),
      service/network (NetworkManager), service/bluetooth (BlueZ),
      service/sysinfo (proc/stat, meminfo, statfs, net devices),
      service/weather (geocoding + forecast, fake-server tested)
- [x] module click/scroll bindings: the full ClickAction grammar
      (dropdown:, brightness:delta/toggle, shell), per-module config,
      and the native brightness/audio executors
- [x] cmd/wayle: shell, idle, notify, recorder, toast

## Remaining for feature parity

Everything below exists in the Rust shell and is absent or partial in
Go. Checked against the crates, binaries, CLI tree, and config schema
at the end of September 2026.

### Bar

- [ ] modules: `dashboard` (the dropdown panel exists, the bar module
      does not), `screenshot`, `user-session`
- [ ] hyprsunset: auto-schedule (geoclue location + solar times) and
      persistence
- [ ] button variants: block-prefix and icon-square structures
- [ ] systray: DBusMenu rendering, IconPixmap icons (see gelm gaps)
- [ ] media: MPRIS transport controls in the module
- [ ] status-token severity colors: dashboard thresholds, recorder
      classes, the popup urgency bar
- [ ] volume: icon-muted
- [ ] notification popups: hover-pause
- [ ] bar shadow preset (and its layer margin)
- [ ] cava: the wave style and the stereo split (error at creation)
- [ ] mail: inotify-driven refresh (Go polls every 15s)
- [ ] styling: theme providers, the generated theme CSS, and the user's
      custom stylesheet; the Go bar resolves the default token palette
      only (see gelm gaps: CSS engine)

### Config

- [ ] YAML configs (discovered, not parsed: a config.yaml falls back to
      defaults with a load error)
- [ ] the runtime override layer and hot reload
- [ ] unparsed sections: `[styling]`, `[wallpaper]`, `[lock]`,
      `[launcher]`, `[animations]`, `[dropdowns]`, `[greeter]`,
      `[share_picker]`
- [ ] i18n: the wayle-i18n Fluent strings (Go renders none)

### Shell surfaces (crates/wayle-shell/src/shell)

- [ ] launcher (engine actor, match model, views)
- [ ] lock screen (logind integration)
- [ ] power menu
- [ ] screenshot capture and the region overlay
- [ ] share picker (screencast source picker + dmabuf preview producer)
- [ ] wallpaper, with the color extractor that feeds theming
- [ ] portal dialogs: file chooser, color picker, print
- [ ] enter/exit animations for the OSD, popups, dropdowns, and every
      transient surface (`[animations]`, surface_anim.rs) - the Go
      surfaces appear and vanish instantly (see gelm gaps)

### Binaries

- [ ] wayle-lock (standalone lock)
- [ ] wayle-greeter (greetd display manager)
- [ ] wayle-settings (the settings GUI; the largest single item)
- [ ] xdg-desktop-portal-wayle (wayle-portal backend)

### CLI (wayle/src/cli)

- [ ] audio, media, config (get/set/reset/schema/docs/default), icons
      (install/import/export/sync/list/remove/open/setup/sources),
      launcher, lock, panel, power, screenshot, systray, wallpaper,
      widget, vpn

### Services

- [ ] native VPN: NetworkManager secret agent and the GlobalProtect
      sign-in
- [ ] wayle-auth (PAM; the lock screen and greeter need it)
- [ ] launcher engine, wallpaper, clipboard (wayle-clipboard)
- [ ] audio through libpulse/PipeWire proper (Go uses pactl, see
      Decisions)
- [ ] bluetooth pairing and agents (Go reads adapter power and the
      connected devices only)
- [ ] brightness: external DDC/CI monitors

### Cross-cutting

- [ ] a live compositor session smoke test: everything so far is tested
      against fake sockets and headless widget trees
- [ ] packaging (nix package, systemd unit, portal files) still points
      at the Rust binaries
- [ ] drop the go.mod replace once gelm tags a release

## gelm gaps and the shortcuts they force

Each entry is a workaround wayle carries because gelm lacks the
capability. Remedy it in gelm, then delete the workaround in wayle.

| gelm lacks | wayle workaround (where) | remedy in gelm |
| --- | --- | --- |
| CSS beyond the basic subset: no `var()`, `color-mix()`, `:not()`, `opacity`, `background-image` gradients, inset `box-shadow`, transitions | the _workspaces.scss/bar cascade is re-derived in Go (`cwsResolveColors`, `computeStyle`, `barStylesheet`); user stylesheets and theme providers cannot be supported | grow internal/style to the property/selector set wayle's generated CSS uses, so wayle can load the Rust SCSS output and user CSS directly |
| per-side padding (Box padding is uniform) | `inset` widget (shell/bar/inset.go) | per-side padding on Box and in the CSS engine (`padding: t r b l`) |
| per-side borders (the CSS border strokes the whole outline) | `borderPainter` strips (shell/bar/border.go), reused by the workspace container | per-side `border-*-width` in the CSS engine and the painters |
| a rounded outline primitive (only `BorderRect`, square) | Button's transparent-fill border strokes square corners | `Canvas.RoundedBorder` (ring) and use it in Button/Entry |
| a public animation API and a revealer (tweens live in `internal/anim`) | no enter/exit animations anywhere in the Go shell | export the tween/easing API and add a Revealer (slide/crossfade) widget |
| `render.Icon` from raw pixels (only LoadSVG/LoadPNG) | systray IconPixmap (ARGB32) cannot render | `render.IconFromImage` / `IconFromARGB32` |
| ext-session-lock-v1 | no lock screen or greeter possible | session-lock surfaces in app |
| screencopy / ext-image-copy-capture, linux-dmabuf | no screenshot, region overlay freeze, color picker, or share preview | the capture protocols (and dmabuf export for the screencast producer) |
| wlr/ext data-control | no clipboard manager | data-control devices in app |
| an fd/file watcher on the event loop | mail polls every 15s (can also be solved in wayle with x/sys inotify + Invoke) | optional: `app.WatchFD` for loop-integrated readiness |
| a drawn workspace button fits Button only with `BgExplicit` (now in gelm) | `cwsButton` paints itself (shell/bar/cwswidgets.go) | done in gelm (53cb59f); wayle can move to Button + BgExplicit, keeping its underline/opacity/min-size painting |

## Decisions

- **cava stays, capture via `pw-record`.** No maintained pure-Go
  PipeWire/PulseAudio monitor client exists (purego-pipewire is dormant,
  tubo is playback-only, go-audio-capture is hw: mic-only), so the
  analyzer is a faithful cavacore.c translation on gonum's FFT and the
  PCM comes from a `pw-record` subprocess. The wave style and stereo
  split are not ported and error at module creation.

## Known deviations from the Rust shell

- Workspace modules re-query the compositor on each relevant event
  instead of folding events into a local state (the Rust services'
  EventStreamState); the rendered result is the same, and events the
  Rust module ignores cost no query.

- The `replace` directive in `go.mod` points at the local gelm checkout;
  the require line tracks the latest pushed gelm commit wayle needs.
- Bar visuals resolve the default token palette in Go; theme providers
  and custom stylesheets land with the styling port (see gelm gaps).
- Audio goes through `pactl` (queries, `pactl subscribe`, setters)
  instead of libpulse FFI.
- YAML configs are discovered but not parsed yet; a `config.yaml` falls
  back to defaults with a load error rather than being misread.

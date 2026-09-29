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
      hyprland-workspaces, battery, brightness, volume, media modules
- [x] service/hyprland: command + event sockets, dispatch
- [x] service/upower, service/brightness (sysfs+logind+inotify),
      service/pulse (pactl), service/mpris (MPRIS2 over godbus)
- [ ] config: YAML configs, runtime layer, hot reload, the rest of the schema
- [ ] bar: remaining modules (network, systray, keyboard layout, custom),
      button component styling, module click/scroll actions
- [ ] services: UPower, PipeWire, NetworkManager, ... (the zbus crates)
- [ ] OSD, launcher, lock screen, settings (per #19 M5 order)

## Decisions

- **cava stays, capture via `pw-record`.** No maintained pure-Go
  PipeWire/PulseAudio monitor client exists (purego-pipewire is dormant,
  tubo is playback-only, go-audio-capture is hw: mic-only), so the
  analyzer is a faithful cavacore.c translation on gonum's FFT and the
  PCM comes from a `pw-record` subprocess. The wave style and stereo
  split are not ported and error at module creation.

## Known deviations from the Rust shell

- The `replace` directive in `go.mod` points at the local gelm checkout;
  it carries the public `app.Connect`/`Font`/layer-enum aliases wayle
  needs. Drop it once gelm tags a release with them.
- Bar visuals: gelm dark palette stands in for the `bg-surface` token
  system, `padding-ends` is parsed but not yet applied per-side, and
  groups flatten into the section row. All land with the styling port.
- YAML configs are discovered but not parsed yet; a `config.yaml` falls
  back to defaults with a load error rather than being misread.

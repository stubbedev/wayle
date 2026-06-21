<p align="center">
  <img src="assets/wayle.svg" width="200" alt="Wayle">
</p>

# Wayle

<p align="center">
  <a href="https://github.com/stubbedev/wayle/actions"><img src="https://img.shields.io/github/actions/workflow/status/stubbedev/wayle/ci.yml?branch=master&style=for-the-badge" alt="CI"></a>
  <a href="https://github.com/stubbedev/wayle/blob/master/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue?style=for-the-badge" alt="License"></a>
</p>

A Wayland desktop shell with the bar, notifications, OSD, wallpaper, and device controls built in. Written in Rust with GTK4 and Relm4.

This is a standalone fork of [wayle-rs/wayle](https://github.com/wayle-rs/wayle) with additional features (YAML config, pixel sizing, per-state icon/color cycling, per-workspace icons, custom toasts, a JSON-RPC widget socket, and animations).

Configure it in `config.toml` or `config.yaml`, through the `wayle-settings` GUI, or with the `wayle config` CLI.

## Features

- **Bar** with per-monitor layouts, groups, and CSS classes.
- **Built-in modules** — clock, battery, bluetooth, network, audio/volume, microphone, brightness, power, power profiles, system tray, storage, CPU/RAM, media player, audio visualizer (cava), notifications, weather, world clock, mail, screen recorder, idle inhibit, and Hyprland / Niri / Mango workspaces.
- **Custom modules** — back any bar widget with a shell command (poll or watch), with [icon and color cycling by state](docs/guide/custom-modules.md) (`icon-map` / `color-map` keyed on the output's `alt`).
- **Per-workspace icons** — give individual workspaces their own icon, shown even in label mode.
- **Notifications, OSD, and custom toasts** — `wayle toast "…"` shows an on-screen toast (icon + label, or a progress bar) reusing the OSD styling.
- **Animations** — configurable enter/exit transitions (fade / slide) for the OSD, toasts, and notification cards via `[animations]`.
- **Pixel or scale sizing** — every size accepts a scale multiplier or absolute pixels (`"24px"`), HiDPI-correct.
- **TOML or YAML config** with imports, live reload, and a published [JSON schema](schema/wayle-config.schema.json) for editor autocomplete.
- **Scriptable** — a JSON-RPC unix socket and CLI push live updates to any widget by id (`wayle widget update <id> …`).
- **Wallpaper and dynamic theming** — Matugen, Pywal, and Wallust palette extraction.

<p align="center">
  <img src="assets/wayle-preview.png" alt="Wayle desktop shell">
</p>

<p align="center">
  <img src="assets/wayle-settings-preview.png" alt="Wayle settings GUI">
</p>

## Documentation

Guides and reference live in [`docs/`](docs/):

- [Getting started](docs/guide/getting-started.md) - Installation instructions
- [Editing config](docs/guide/editing-config.md) - File layout, live reload, imports, CLI editing
- [Bars and layouts](docs/guide/bars-and-layouts.md) - Per monitor layouts, groups, classes
- [Themes](docs/guide/themes.md) - Color tokens, theme files
- [Custom icons](docs/guide/custom-icons.md) - Installing icons, icon sources
- [Custom modules](docs/guide/custom-modules.md) - Shell-backed bar modules
- [CLI](docs/guide/cli.md) - Every subcommand
- [Config reference](docs/config/) - Full config documentation

## Install

Build from source (no prebuilt packages are published for this fork).

<details>
<summary><b>Arch (from source)</b></summary>

Install Rust via [rustup](https://rustup.rs), then the system libraries:

```sh
sudo pacman -S --needed git gtk4 gtk4-layer-shell gtksourceview5 \
  libpulse fftw libpipewire systemd-libs gst-plugins-base clang base-devel
```

The screen recorder also needs GStreamer plugins at runtime:

```sh
sudo pacman -S --needed gst-plugins-base gst-plugins-good gst-plugins-bad \
  gst-plugins-ugly gst-libav
```

Runtime daemons for the battery, bluetooth, network, power, and audio modules (skip any you don't need):

```sh
sudo pacman -S --needed bluez bluez-utils networkmanager upower \
  power-profiles-daemon pipewire wireplumber pipewire-pulse
sudo systemctl enable --now bluetooth NetworkManager upower power-profiles-daemon
```

</details>

<details>
<summary><b>Debian / Ubuntu</b></summary>

Ubuntu 24.04 LTS does not package `libgtk4-layer-shell-dev`. Use Ubuntu 25.04+ or Debian 13 (trixie).

Install Rust via [rustup](https://rustup.rs), then the system libraries:

```sh
sudo apt install git pkg-config cmake libgtk-4-dev libgtk4-layer-shell-dev \
  libgtksourceview-5-dev libpulse-dev libfftw3-dev libpipewire-0.3-dev \
  libudev-dev libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
  clang build-essential
```

The screen recorder also needs GStreamer plugins at runtime:

```sh
sudo apt install gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav
```

Runtime daemons:

```sh
sudo apt install dbus-user-session bluez network-manager \
  upower power-profiles-daemon pipewire-pulse wireplumber
sudo systemctl enable --now bluetooth NetworkManager upower power-profiles-daemon
```

</details>

<details>
<summary><b>Fedora</b></summary>

Requires Fedora 42 or later.

Install Rust via [rustup](https://rustup.rs), then the system libraries:

```sh
sudo dnf install git cmake pkgconf-pkg-config gtk4-devel gtk4-layer-shell-devel \
  gtksourceview5-devel pulseaudio-libs-devel fftw-devel pipewire-devel \
  systemd-devel gstreamer1-devel gstreamer1-plugins-base-devel clang gcc
```

The screen recorder also needs GStreamer plugins at runtime:

```sh
sudo dnf install gstreamer1-plugins-base gstreamer1-plugins-good \
  gstreamer1-plugins-bad-free gstreamer1-plugins-ugly-free gstreamer1-libav
```

Fedora Workstation already ships the runtime daemons. Minimal and Server installs need:

```sh
sudo dnf install pipewire-pulseaudio wireplumber NetworkManager bluez upower \
  power-profiles-daemon
sudo systemctl enable --now bluetooth NetworkManager upower power-profiles-daemon
```

</details>

### Build and launch:

```sh
git clone https://github.com/stubbedev/wayle
cd wayle
cargo install --path wayle
cargo install --path crates/wayle-settings
wayle icons setup
wayle panel start
```

On a different distro? See [docs/guide/getting-started.md](docs/guide/getting-started.md) for the library-version reference.

## Configuration

The config file is at `~/.config/wayle/config.toml`. Changes reload on save:

```toml
[bar]
location = "top"
scale = 1.25

[[bar.layout]]
monitor = "*"
left = ["dashboard"]
center = ["clock"]
right = ["volume", "network", "bluetooth", "battery"]

[modules.clock]
format = "%H:%M"
```

Every field is documented in the [config reference](docs/config/).

## Requirements

A Wayland compositor that implements the `wlr-layer-shell` protocol. Compositor-specific modules (such as workspaces) currently target Hyprland, Niri, and Mango; Sway support is planned.

## Credits

Logo by [@M70v](https://www.instagram.com/m70v.art/).

## License

MIT

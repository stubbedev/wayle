# CLI

Every command and subcommand takes `--help`.

Panel lifecycle and visibility:

```sh
wayle panel start
wayle panel stop
wayle panel restart
wayle panel status
wayle panel settings
wayle panel inspect

# Per-monitor visibility (omit the connector to affect all monitors)
wayle panel hide DP-1
wayle panel show DP-1
wayle panel toggle
```

Read and edit config values from the command line:

```sh
wayle config get bar.scale
wayle config set bar.scale 1.25
wayle config reset bar.scale

# Emit the JSON schema, default TOML, or markdown docs
wayle config schema --stdout
wayle config default --stdout
wayle config docs --out docs/config
```

Audio controls. Volume takes an absolute level (`0-100`) or a relative
adjustment (`+5`, `-10`):

```sh
wayle audio output-volume +5
wayle audio output-mute
wayle audio input-volume 50
wayle audio input-mute
wayle audio sinks
wayle audio sources
wayle audio status
```

Media player control. Most subcommands take an optional player id (a number or
partial name match); omit it to target the active player:

```sh
wayle media list
wayle media play-pause
wayle media next
wayle media previous
wayle media shuffle on
wayle media loop track
wayle media active spotify
wayle media info
```

Idle inhibition:

```sh
wayle idle on            # use the default duration
wayle idle on 30         # inhibit for 30 minutes
wayle idle on --indefinite
wayle idle off
wayle idle toggle
wayle idle duration +15  # +N / -N / N adjusts the upper limit
wayle idle remaining -5  # +N / -N / N adjusts the active timer
wayle idle status
```

Power profiles:

```sh
wayle power status
wayle power set balanced  # power-saver, balanced, performance
wayle power cycle
wayle power list
```

Notifications:

```sh
wayle notify list
wayle notify dismiss 42
wayle notify dismiss-all
wayle notify dnd          # toggle Do Not Disturb
wayle notify status
```

System tray:

```sh
wayle systray list
wayle systray activate <id>
wayle systray status
```

Wallpapers. Set a single image, or cycle through a directory:

```sh
wayle wallpaper set ~/Pictures/wall.png --fit fill --monitor DP-1
wayle wallpaper cycle ~/Pictures/walls --interval 300 --mode sequential
wayle wallpaper stop
wayle wallpaper next
wayle wallpaper previous
wayle wallpaper info --monitor DP-1
wayle wallpaper theming-monitor DP-1
```

`--fit` accepts `fill`, `fit`, `center`, `tile`, `stretch`; `--mode` accepts
`sequential` or `shuffle`.

Screen recorder:

```sh
wayle recorder start
wayle recorder stop
wayle recorder toggle
wayle recorder pause
wayle recorder resume
wayle recorder status
```

Screenshots:

```sh
wayle screenshot region        # drag-select a region
wayle screenshot output DP-1   # whole output (focused output if omitted)
wayle screenshot window        # the active window
```

Icons. Install bundled assets, pull from a CDN source, import local SVGs, or
sync everything referenced in your config:

```sh
wayle icons setup
wayle icons sources
wayle icons install tabler home settings bell
wayle icons import ~/Downloads/my-icon.svg my-icon
wayle icons list --source tb --interactive
wayle icons remove tb-home-symbolic
wayle icons export ~/exported-icons
wayle icons open
wayle icons sync --dry-run
```

Push a runtime update to a custom module by its `id`. The payload is parsed
exactly like the module's own command output, so it drives the label, icon, and
colors (see [custom modules](/guide/custom-modules)):

```sh
wayle widget update gpu '{"text":"72°C","alt":"hot","percentage":90}'
```

Show a custom on-screen toast. It reuses the OSD styling and position; add
`--percentage` for a progress bar, `--icon` for a leading icon, `--duration`
to override the OSD dismiss time, `--preset` to base it on a configured preset,
and `--class` for a custom CSS class:

```sh
wayle toast "Build finished"
wayle toast "Volume" --icon audio-volume-high-symbolic --percentage 65
wayle toast "Syncing…" --duration 5000
```

Run the desktop shell in the foreground:

```sh
wayle shell
```

Shell completions for any clap-supported shell (bash, fish, zsh, …):

```sh
wayle completions fish > ~/.config/fish/completions/wayle.fish
```

> `wayle share-picker` exists for the xdg-desktop-portal screencast flow and is
> invoked by the portal, not by hand.

# CLI

Every subcommand takes `--help`.

Panel lifecycle:

```sh
wayle panel start
wayle panel restart
wayle panel settings
```

Read and edit config values from the command line:

```sh
wayle config get bar.scale
wayle config set bar.scale 1.25
wayle config reset bar.scale
```

Audio, media, and idle controls:

```sh
wayle audio output-volume +5
wayle media play-pause
wayle idle toggle
```

Push a runtime update to a custom module by its `id`. The payload is parsed
exactly like the module's own command output, so it drives the label, icon, and
colors (see [custom modules](/guide/custom-modules)):

```sh
wayle widget update gpu '{"text":"72°C","alt":"hot","percentage":90}'
```

Show a custom on-screen toast. It reuses the OSD styling and position; add
`--percentage` for a progress bar, `--icon` for a leading icon, and `--duration`
to override the OSD dismiss time:

```sh
wayle toast "Build finished"
wayle toast "Volume" --icon audio-volume-high-symbolic --percentage 65
wayle toast "Syncing…" --duration 5000
```

Shell completions for bash, fish, and zsh:

```sh
wayle completions fish > ~/.config/fish/completions/wayle.fish
```

---
title: wallpaper
outline: [2, 3]
---

# wallpaper

<div v-pre>

Wallpaper rendering, cycling, and per-monitor overrides.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `wallpaper` | string | `""` | A single image file to use as the wallpaper on all monitors. Leave empty to use cycling and/or per-monitor overrides instead. |
| `fit-mode` | [`FitMode`](/config/types#fit-mode) | `"fill"` | How the wallpaper is scaled to the screen. Per-monitor entries in `[[wallpaper.monitors]]` override this. |

## Cycling

| Field | Type | Default | Description |
|---|---|---|---|
| `cycling-directory` | string | `""` | Directory of images to cycle through. Set it to enable cycling; leave empty to disable. Takes precedence over the single `wallpaper` image. |
| `cycling-mode` | [`CyclingMode`](/config/types#cycling-mode) | `"sequential"` | Wallpaper cycling order. |
| `cycling-interval-mins` | [`CyclingInterval`](/config/types#cycling-interval) | `15` | Time between wallpaper changes in minutes. |
| `cycling-same-image` | bool | `false` | Show the same cycling wallpaper on all monitors. Only affects shuffle mode since sequential already displays the same image. |

## Per-monitor overrides

| Field | Type | Default | Description |
|---|---|---|---|
| `monitors` | array of [`MonitorWallpaperConfig`](/config/types#monitor-wallpaper-config) | `[]` | Per-monitor wallpaper and fit mode settings. Each entry targets a monitor by connector name. See [`MonitorWallpaperConfig`] for the available fields. |

::: details More about `monitors`

#### Example

```toml
[[wallpaper.monitors]]
name = "DP-1"
wallpaper = "/home/me/pictures/wall-primary.png"
fit-mode = "fill"

[[wallpaper.monitors]]
name = "HDMI-1"
wallpaper = "/home/me/pictures/wall-secondary.png"
fit-mode = "fit"
```

:::

## Default configuration

```toml
[wallpaper]
wallpaper = ""
fit-mode = "fill"
cycling-directory = ""
cycling-mode = "sequential"
cycling-interval-mins = 15
cycling-same-image = false
monitors = []
```


</div>

---
title: osd
outline: [2, 3]
---

# osd

<div v-pre>

On-screen display overlay for transient events like volume and brightness.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Show OSD overlays for volume, brightness, and keyboard toggles. |
| `position` | [`OsdPosition`](/config/types#osd-position) | `"bottom"` | Screen anchor position. |
| `text-align` | [`OsdTextAlign`](/config/types#osd-text-align) | `"center"` | Horizontal alignment of toast and toggle overlay content. Sliders (volume/brightness) keep their own label+value layout. |
| `duration` | u32 | `2500` | Auto-dismiss delay in milliseconds. |
| `monitor` | [`OsdMonitor`](/config/types#osd-monitor) | `"primary"` | Target monitor: "primary" or a connector name like "DP-1". |
| `margin` | [`Size`](/config/types#size) | `150` | Margin from screen edges. Accepts a scale multiplier or pixels (e.g. `"150px"`). |
| `border` | bool | `true` | Show a border around the OSD. |
| `layer` | [`Layer`](/config/types#layer) | `"overlay"` | Layer-shell layer the OSD is placed on. |

::: details More about `layer`

When `general.tearing-mode` is enabled, `overlay` is demoted to `top`
to allow fullscreen tearing.

:::

## Default configuration

```toml
[osd]
enabled = true
position = "bottom"
text-align = "center"
duration = 2500
monitor = "primary"
margin = 150.0
border = true
layer = "overlay"
```


</div>

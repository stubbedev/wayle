---
title: toasts
outline: [2, 3]
---

# toasts

<div v-pre>

Toast overlays shown via `wayle toast`.

Toasts are independent of the OSD: they have their own screen position,
monitor, layer, duration, alignment, and a list of reusable presets.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Show toast overlays pushed via `wayle toast`. |
| `position` | [`OsdPosition`](/config/types#osd-position) | `"bottom"` | Screen anchor position. |
| `text-align` | [`OsdTextAlign`](/config/types#osd-text-align) | `"center"` | Horizontal alignment of toast content. |
| `duration` | u32 | `2500` | Auto-dismiss delay in milliseconds. |
| `monitor` | [`OsdMonitor`](/config/types#osd-monitor) | `"primary"` | Target monitor: "primary" or a connector name like "DP-1". |
| `margin` | [`Size`](/config/types#size) | `150` | Margin from screen edges. Accepts a scale multiplier or pixels (e.g. `"150px"`). |
| `border` | bool | `true` | Show a border around the toast. |
| `layer` | [`Layer`](/config/types#layer) | `"overlay"` | Layer-shell layer toasts are placed on. |
| `presets` | array of [`ToastPreset`](/config/types#toast-preset) | `[]` | Reusable toast presets, each triggerable with `wayle toast --preset <id>`. |

## Default configuration

```toml
[toasts]
enabled = true
position = "bottom"
text-align = "center"
duration = 2500
monitor = "primary"
margin = 150.0
border = true
layer = "overlay"
presets = []
```


</div>

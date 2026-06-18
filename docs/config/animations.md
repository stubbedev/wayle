---
title: animations
outline: [2, 3]
---

# animations

<div v-pre>

Enter/exit and change animations for transient surfaces.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable enter/exit animations (OSD, toasts, notifications) and icon-change crossfades. When disabled, surfaces appear instantly. |
| `duration` | u32 | `200` | Animation duration in milliseconds. |
| `transition` | [`AnimationType`](/config/types#animation-type) | `"fade"` | Transition style used for enter/exit of the OSD, toasts, and notification cards. Base fallback for every surface and direction. |
| `enter` | [`AnimationType`](/config/types#animation-type) or null | `null` | Global enter transition. Unset → `transition`. |
| `exit` | [`AnimationType`](/config/types#animation-type) or null | `null` | Global exit transition. Unset → `transition`. |
| `enter-duration` | u32 or null | `null` | Global enter duration in ms. Unset → `duration`. |
| `exit-duration` | u32 or null | `null` | Global exit duration in ms. Unset → `duration`. |
| `interaction-duration` | u32 | `150` | Duration in ms for in-place dropdown transitions: hover highlights and the page/stack crossfades inside dropdowns. `enabled = false` removes them entirely. |
| `ui-duration` | u32 | `250` | Base duration in ms for general UI micro-transitions (hover, focus, and color fades) driven by the CSS `--duration-*` token family. Fast, normal, and slow speeds are derived from this. `enabled = false` zeroes them for an instant UI. |
| `indicators` | bool | `true` | Run looping status indicators: spinners, network/bluetooth scan animations, the recording pulse, and the clock blink. Disable (or set `enabled = false`) for a fully static UI. |
| `notifications` | [`SurfaceAnimation`](/config/types#surface-animation) | `{...}` | Per-surface override for notification popup cards. |
| `osd` | [`SurfaceAnimation`](/config/types#surface-animation) | `{...}` | Per-surface override for the OSD (volume/brightness/toggle). |
| `toast` | [`SurfaceAnimation`](/config/types#surface-animation) | `{...}` | Per-surface override for toasts (`wayle toast`). |
| `dropdown` | [`SurfaceAnimation`](/config/types#surface-animation) | `{...}` | Per-surface override for bar widget dropdown foldouts. |

## Default configuration

```toml
[animations]
enabled = true
duration = 200
transition = "fade"
interaction-duration = 150
ui-duration = 250
indicators = true
```


</div>

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
| `transition` | [`AnimationType`](/config/types#animation-type) | `"fade"` | Transition style used for enter/exit of the OSD, toasts, and notification cards. |

## Default configuration

```toml
[animations]
enabled = true
duration = 200
transition = "fade"
```


</div>

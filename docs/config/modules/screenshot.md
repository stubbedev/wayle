---
title: screenshot
outline: [2, 3]
---

# screenshot

<div v-pre>

Screenshot capture button.

Click the bar button to capture a region, output, or window. Controllable
from the CLI / RPC socket: `wayle screenshot region|output|window`.

Add it to your layout with `screenshot`:

```toml
[[bar.layout]]
monitor = "*"
right = ["screenshot"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `icon` | string | `"ld-camera-symbolic"` | Bar button icon. |
| `output-directory` | string | `""` | Output directory for screenshots. Empty uses the XDG Pictures directory. |
| `filename-format` | string | `"Screenshot_%Y-%m-%d_%H-%M-%S.png"` | Saved file name, formatted with `chrono`/`strftime` specifiers. |
| `copy-to-clipboard` | bool | `true` | Copy the captured image to the clipboard. |
| `notify` | bool | `true` | Show a desktop notification after capturing. |
| `border-show` | bool | `false` | Display border around button. |
| `icon-show` | bool | `true` | Display module icon. |
| `label` | string | `""` | Static label text shown beside the icon. |
| `label-show` | bool | `false` | Display label. |
| `label-max-length` | u32 | `0` | Max label characters before truncation with ellipsis. Set to 0 to disable. |

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"accent"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"accent"` | Icon container background color token. |
| `label-color` | [`ColorValue`](/config/types#color-value) | `"accent"` | Label text color token. |
| `button-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Button background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `left-click` | [`ClickAction`](/config/types#click-action) | `"wayle screenshot region"` | Action on left click. Default captures a region. |
| `right-click` | [`ClickAction`](/config/types#click-action) | `"wayle screenshot output"` | Action on right click. Default captures the focused output. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `"wayle screenshot window"` | Action on middle click. Default captures the active window. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |

## Default configuration

```toml
[modules.screenshot]
icon = "ld-camera-symbolic"
output-directory = ""
filename-format = "Screenshot_%Y-%m-%d_%H-%M-%S.png"
copy-to-clipboard = true
notify = true
border-show = false
border-color = "accent"
icon-show = true
icon-color = "auto"
icon-bg-color = "accent"
label = ""
label-show = false
label-color = "accent"
label-max-length = 0
button-bg-color = "bg-surface-elevated"
left-click = "wayle screenshot region"
right-click = "wayle screenshot output"
middle-click = "wayle screenshot window"
scroll-up = ""
scroll-down = ""
```


</div>

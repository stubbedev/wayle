---
title: share-picker
outline: [2, 3]
---

# share-picker

<div v-pre>

Screen-share picker shown by xdg-desktop-portal when an app requests a
window, output, or region to capture.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `default-page` | [`SharePickerPage`](/config/types#share-picker-page) | `"windows"` | Page selected when the picker opens. |
| `hide-token-restore` | bool | `false` | Hide the "allow a restore token" checkbox. |
| `width` | [`Size`](/config/types#size) | `1` | Picker window width: a multiplier of the default 1000px (`1.0` = default) or absolute pixels (e.g. `"1200px"`). |
| `height` | [`Size`](/config/types#size) | `1` | Picker window height: a multiplier of the default 500px (`1.0` = default) or absolute pixels. |
| `resize-size` | u32 | `640` | Downscale every captured frame to at most this height in pixels. |
| `widget-size` | [`Size`](/config/types#size) | `1` | Height of each card's preview image: a multiplier of the default 150px (`1.0` = default) or absolute pixels. |
| `windows-spacing` | [`Size`](/config/types#size) | `1` | Spacing between window cards: a multiplier of the default 12px (`1.0` = default) or absolute pixels. |
| `windows-min-per-row` | u32 | `3` | Minimum window cards per row. |
| `windows-max-per-row` | u32 | `4` | Maximum window cards per row. |
| `outputs-spacing` | [`Size`](/config/types#size) | `1` | Spacing between output cards (applied per side): a multiplier of the default 6px (`1.0` = default) or absolute pixels. |
| `outputs-show-label` | bool | `false` | Show the output name label under each output card. |
| `outputs-respect-scaling` | bool | `true` | Scale output cards by their fractional scale. |

## Default configuration

```toml
[share-picker]
default-page = "windows"
hide-token-restore = false
width = 1.0
height = 1.0
resize-size = 640
widget-size = 1.0
windows-spacing = 1.0
windows-min-per-row = 3
windows-max-per-row = 4
outputs-spacing = 1.0
outputs-show-label = false
outputs-respect-scaling = true
```


</div>

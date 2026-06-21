---
title: recorder
outline: [2, 3]
---

# recorder

<div v-pre>

Native screen recorder backed by a GStreamer pipeline.

Click the bar button to start/stop; the dropdown exposes the recording
options below. Controllable from the CLI / RPC socket:
`wayle recorder start|stop|toggle|pause|status`.

Add it to your layout with `recorder`:

```toml
[[bar.layout]]
monitor = "*"
right = ["recorder"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `icon-idle` | string | `"ld-video-symbolic"` | Icon when idle (not recording). |
| `icon-recording` | string | `"ld-circle-dot-symbolic"` | Icon while recording. |
| `icon-paused` | string | `"ld-circle-pause-symbolic"` | Icon while recording is paused. |
| `format` | string | `"{{ elapsed }}"` | Format string for the label. |
| `microphone` | bool | `false` | Capture the microphone in the recording. |
| `microphone-device` | string | `""` | Microphone PipeWire/PulseAudio source name. Empty uses the default source. |
| `system-audio` | bool | `true` | Capture desktop (system) audio in the recording. |
| `separate-audio-tracks` | bool | `true` | Keep microphone and system audio as separate, individually editable tracks instead of mixing them into one. |
| `framerate` | u32 | `30` | Capture framerate in frames per second. |
| `webcam-enabled` | bool | `false` | Overlay a webcam picture-in-picture frame into the recording. |
| `webcam-device` | string | `""` | Webcam V4L2 device path. Empty auto-selects the first camera. |
| `webcam-position` | [`WebcamPosition`](/config/types#webcam-position) | `"bottom-right"` | Corner the webcam frame is anchored to. |
| `webcam-size` | [`Percentage`](/config/types#percentage) | `20` | Webcam frame width as a percentage of the recording width. |
| `output-directory` | string | `""` | Output directory for recordings. Empty uses the XDG Videos directory. |
| `output-format` | [`RecorderFormat`](/config/types#recorder-format) | `"mp4"` | Container format / codec preset. |
| `show-cursor` | bool | `true` | Draw the mouse cursor in the recording. |
| `border-show` | bool | `false` | Display border around button. |
| `icon-show` | bool | `true` | Display module icon. |
| `label-show` | bool | `true` | Display label. |
| `label-max-length` | u32 | `0` | Max label characters before truncation with ellipsis. Set to 0 to disable. |

::: details More about `format`

#### Placeholders

- `{{ state }}` - Recorder state text (Idle, Recording, Paused)
- `{{ elapsed }}` - Elapsed recording time (e.g., "01:23", "--" when idle)

:::

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"red"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"red"` | Icon container background color token. |
| `label-color` | [`ColorValue`](/config/types#color-value) | `"red"` | Label text color token. |
| `button-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Button background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `left-click` | [`ClickAction`](/config/types#click-action) | `"wayle recorder toggle"` | Action on left click. Default toggles recording. |
| `right-click` | [`ClickAction`](/config/types#click-action) | `"dropdown:recorder"` | Action on right click. Default opens the recorder dropdown. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |

## Default configuration

```toml
[modules.recorder]
icon-idle = "ld-video-symbolic"
icon-recording = "ld-circle-dot-symbolic"
icon-paused = "ld-circle-pause-symbolic"
format = "{{ elapsed }}"
microphone = false
microphone-device = ""
system-audio = true
separate-audio-tracks = true
framerate = 30
webcam-enabled = false
webcam-device = ""
webcam-position = "bottom-right"
webcam-size = 20
output-directory = ""
output-format = "mp4"
show-cursor = true
border-show = false
border-color = "red"
icon-show = true
icon-color = "auto"
icon-bg-color = "red"
label-show = true
label-color = "red"
label-max-length = 0
button-bg-color = "bg-surface-elevated"
left-click = "wayle recorder toggle"
right-click = "dropdown:recorder"
middle-click = ""
scroll-up = ""
scroll-down = ""
```


</div>

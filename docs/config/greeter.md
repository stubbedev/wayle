---
title: greeter
outline: [2, 3]
---

# greeter

<div v-pre>

Greeter (display manager): the pre-login screen `wayle-greeter` renders as
a greetd greeter.

The greeter reads the system config (`/etc/wayle/config.toml`), so these
settings take effect there — copy or symlink your user config if you want
the login screen to follow it.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `background-mode` | [`LockBackground`](/config/types#lock-background) | `"color"` | How the background is drawn: solid color, an image, or the wallpaper. |
| `background-image` | string | `""` | Background image path (used when `background-mode = "image"`). |
| `background-color` | [`HexColor`](/config/types#hex-color) | `"#000000"` | Background fill color (used when `background-mode = "color"`). |
| `show-clock` | bool | `true` | Show a clock above the login form. |
| `clock-format` | string | `"%H:%M"` | `strftime` format for the greeter time. |
| `date-format` | string | `"%A, %B %-d"` | `strftime` format for the greeter date. |
| `show-user-list` | bool | `true` | Show clickable avatars for the machine's login users. |
| `show-power-buttons` | bool | `true` | Show the shutdown/reboot buttons at the bottom of the screen. |
| `cursor-theme` | string | `""` | Xcursor theme used on the login screen (empty = system default). |
| `cursor-size` | u32 | `24` | Logical cursor size on the login screen. Scaled automatically per display, so HiDPI outputs get a matching high-resolution cursor. |

## Default configuration

```toml
[greeter]
background-mode = "color"
background-image = ""
background-color = "#000000"
show-clock = true
clock-format = "%H:%M"
date-format = "%A, %B %-d"
show-user-list = true
show-power-buttons = true
cursor-theme = ""
cursor-size = 24
```


</div>

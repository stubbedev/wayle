---
title: lock
outline: [2, 3]
---

# lock

<div v-pre>

Lock screen: a secure session lock rendered by Wayle via `ext-session-lock-v1`.

When `enabled`, Wayle locks the session in response to the logind `Lock`
signal (`loginctl lock-session`), the `wayle lock` CLI, or the shell IPC
`lock()` method. The lock surface grabs all input and survives client
crashes (the compositor shows a solid color), unlike a layer-shell overlay.

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Let Wayle handle session locking. When off, lock requests are ignored and an external locker (e.g. hyprlock) stays responsible. |
| `lock-on-start` | bool | `false` | Lock the session as soon as the shell starts. For autologin setups (e.g. greetd with no password at boot) where the lock screen is the access gate: the session comes up locked and requires the password before use. Off by default — a normal login has already authenticated. |
| `background-mode` | [`LockBackground`](/config/types#lock-background) | `"color"` | How the background is drawn: solid color, an image, or the wallpaper. |
| `background-image` | string | `""` | Background image path (used when `background-mode = "image"`). |
| `background-color` | [`HexColor`](/config/types#hex-color) | `"#000000"` | Background fill color (used when `background-mode = "color"`). |
| `blur` | u32 | `0` | Gaussian blur radius applied to image/wallpaper backgrounds (0 = none). |
| `show-clock` | bool | `true` | Show a clock on the lock screen. |
| `clock-format` | string | `"%H:%M"` | `strftime` format for the lock-screen time. |
| `date-format` | string | `"%A, %B %-d"` | `strftime` format for the lock-screen date. |
| `grace-period-ms` | u32 | `0` | Grace window after locking during which the screen unlocks without a password (milliseconds, `0` = always require the password). |
| `max-attempts` | u32 | `0` | Maximum failed password attempts before further input is blocked (`0` = unlimited). The screen stays locked regardless. |
| `show-failed-attempts` | bool | `true` | Show the failed-attempt count on the lock screen. |
| `blank-timeout-ms` | u32 | `0` | Black out the lock screen after this idle time (milliseconds, `0` = never). This is a visual blank that hides the clock/prompt and dismisses on any key; true display power-off (DPMS) is left to your idle daemon. |
| `pam-service` | string | `"system-auth"` | PAM service name used to authenticate the unlock. Distro-dependent: `system-auth` (Arch/Fedora), `login`, or a custom `/etc/pam.d` entry. |

## Default configuration

```toml
[lock]
enabled = true
lock-on-start = false
background-mode = "color"
background-image = ""
background-color = "#000000"
blur = 0
show-clock = true
clock-format = "%H:%M"
date-format = "%A, %B %-d"
grace-period-ms = 0
max-attempts = 0
show-failed-attempts = true
blank-timeout-ms = 0
pam-service = "system-auth"
```


</div>

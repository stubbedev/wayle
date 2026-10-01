---
title: power
outline: [2, 3]
---

# power

<div v-pre>

Shutdown, reboot, and logout menu.

Add it to your layout with `power`:

```toml
[[bar.layout]]
monitor = "*"
right = ["power"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `icon-name` | string | `"ld-power-symbolic"` | Icon name to display. |
| `border-show` | bool | `false` | Display border around button. |
| `lock-command` | string | `"loginctl lock-session"` | Command run by the power menu's Lock button. |
| `logout-command` | string | `"loginctl terminate-session $XDG_SESSION_ID"` | Command run by the power menu's Log out button. |
| `suspend-command` | string | `"systemctl suspend"` | Command run by the power menu's Suspend button. |
| `reboot-command` | string | `"systemctl reboot"` | Command run by the power menu's Reboot button. |
| `shutdown-command` | string | `"systemctl poweroff"` | Command run by the power menu's Shut down button. |
| `show-lock` | bool | `true` | Show the Lock button in the power menu. |
| `show-logout` | bool | `true` | Show the Log out button in the power menu. |
| `show-suspend` | bool | `true` | Show the Suspend button in the power menu. |
| `show-reboot` | bool | `true` | Show the Reboot button in the power menu. |
| `show-shutdown` | bool | `true` | Show the Shut down button in the power menu. |

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"red"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"red"` | Icon container background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `right-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on right click. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |
| `left-click` | [`ClickAction`](/config/types#click-action) | `":menu"` | Action on left click. Default opens wayle's native power menu (`:menu`). |

## Default configuration

```toml
[modules.power]
icon-name = "ld-power-symbolic"
border-show = false
border-color = "red"
icon-color = "auto"
icon-bg-color = "red"
right-click = ""
middle-click = ""
scroll-up = ""
scroll-down = ""
left-click = ":menu"
lock-command = "loginctl lock-session"
logout-command = "loginctl terminate-session $XDG_SESSION_ID"
suspend-command = "systemctl suspend"
reboot-command = "systemctl reboot"
shutdown-command = "systemctl poweroff"
show-lock = true
show-logout = true
show-suspend = true
show-reboot = true
show-shutdown = true
```


</div>

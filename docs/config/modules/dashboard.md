---
title: dashboard
outline: [2, 3]
---

# dashboard

<div v-pre>

Quick-access button with a distro icon; opens the dashboard dropdown.

Add it to your layout with `dashboard`:

```toml
[[bar.layout]]
monitor = "*"
right = ["dashboard"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `icon-override` | string | `""` | Override the auto-detected distro icon. |
| `border-show` | bool | `false` | Display border around button. |
| `usage-warning` | f32 | `60` | CPU/RAM/disk usage percent at which the dashboard rings turn warning. |
| `usage-error` | f32 | `85` | CPU/RAM/disk usage percent at which the dashboard rings turn error. |
| `temp-warning` | f32 | `65` | CPU temperature (°C) at which the dashboard temp ring turns warning. |
| `temp-error` | f32 | `85` | CPU temperature (°C) at which the dashboard temp ring turns error. |
| `battery-warning` | f32 | `30` | Battery percent at or below which the dashboard battery shows warning. |
| `battery-critical` | f32 | `15` | Battery percent at or below which the dashboard battery shows critical. |
| `user-session` | [`UserSessionConfig`](/config/types#user-session-config) | `{...}` | User session configuration |

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"yellow"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"yellow"` | Icon container background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `right-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on right click. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |
| `left-click` | [`ClickAction`](/config/types#click-action) | `"dropdown:dashboard"` | Action on left click. |

## Dropdown

| Field | Type | Default | Description |
|---|---|---|---|
| `dropdown-lock-command` | string | `"loginctl lock-session"` | Shell command for the lock button in the dashboard dropdown. |
| `dropdown-logout-command` | string | `"loginctl terminate-session $XDG_SESSION_ID"` | Shell command for the logout button in the dashboard dropdown. |
| `dropdown-reboot-command` | string | `"systemctl reboot"` | Shell command for the reboot button in the dashboard dropdown. |
| `dropdown-poweroff-command` | string | `"systemctl poweroff"` | Shell command for the power-off button in the dashboard dropdown. |

## Default configuration

```toml
[modules.dashboard]
icon-override = ""
border-show = false
border-color = "yellow"
icon-color = "auto"
icon-bg-color = "yellow"
right-click = ""
middle-click = ""
scroll-up = ""
scroll-down = ""
left-click = "dropdown:dashboard"
dropdown-lock-command = "loginctl lock-session"
dropdown-logout-command = "loginctl terminate-session $XDG_SESSION_ID"
dropdown-reboot-command = "systemctl reboot"
dropdown-poweroff-command = "systemctl poweroff"
usage-warning = 60.0
usage-error = 85.0
temp-warning = 65.0
temp-error = 85.0
battery-warning = 30.0
battery-critical = 15.0

[modules.dashboard.user-session]
actions = [
    "lock",
    "log-out",
    "reboot",
    "power-off",
]
```


</div>

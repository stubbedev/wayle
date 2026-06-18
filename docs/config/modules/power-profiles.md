---
title: power-profiles
outline: [2, 3]
---

# power-profiles

<div v-pre>

Power profile indicator and switcher (power-profiles-daemon).

Shows the active profile with a per-profile icon and color, and cycles
through the available profiles on click. Backed by the same
power-profiles-daemon D-Bus interface as `powerprofilesctl`.

Add it to your layout with `power-profiles`:

```toml
[[bar.layout]]
monitor = "*"
right = ["power-profiles"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `format` | string | `"{{ profile }}"` | Format string for the label. |
| `icon-power-saver` | string | `"ld-leaf-symbolic"` | Icon shown while the power-saver profile is active. |
| `icon-balanced` | string | `"ld-scale-symbolic"` | Icon shown while the balanced profile is active. |
| `icon-performance` | string | `"ld-rocket-symbolic"` | Icon shown while the performance profile is active. |
| `border-show` | bool | `false` | Display border around button. |
| `icon-show` | bool | `true` | Display module icon. |
| `label-show` | bool | `false` | Display label. |
| `label-max-length` | u32 | `0` | Max label characters before truncation with ellipsis. Set to 0 to disable. |

::: details More about `format`

#### Placeholders

- `{{ profile }}` - Active profile name (power-saver, balanced, performance)

#### Examples

- `"{{ profile }}"` - "balanced"

:::

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `color-power-saver` | [`ColorValue`](/config/types#color-value) | `"green"` | Icon/label color while the power-saver profile is active. |
| `color-balanced` | [`ColorValue`](/config/types#color-value) | `"blue"` | Icon/label color while the balanced profile is active. |
| `color-performance` | [`ColorValue`](/config/types#color-value) | `"red"` | Icon/label color while the performance profile is active. |
| `border-color` | [`ColorValue`](/config/types#color-value) | `"blue"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Icon container background color token. |
| `label-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Label text color token. |
| `button-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Button background color token. |

::: details More about `icon-color`

Overridden per active profile by the `color-*` fields.

:::

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `left-click` | [`ClickAction`](/config/types#click-action) | `":cycle"` | Action on left click. Default cycles to the next power profile. |
| `right-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on right click. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |

## Default configuration

```toml
[modules.power-profiles]
format = "{{ profile }}"
icon-power-saver = "ld-leaf-symbolic"
icon-balanced = "ld-scale-symbolic"
icon-performance = "ld-rocket-symbolic"
color-power-saver = "green"
color-balanced = "blue"
color-performance = "red"
border-show = false
border-color = "blue"
icon-show = true
icon-color = "auto"
icon-bg-color = "bg-surface-elevated"
label-show = false
label-color = "auto"
label-max-length = 0
button-bg-color = "bg-surface-elevated"
left-click = ":cycle"
right-click = ""
middle-click = ""
scroll-up = ""
scroll-down = ""
```


</div>

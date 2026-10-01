---
title: brightness
outline: [2, 3]
---

# brightness

<div v-pre>

Backlight control bar module.

Add it to your layout with `brightness`:

```toml
[[bar.layout]]
monitor = "*"
right = ["brightness"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `level-icons` | array of string | `[...]` | Icons for brightness levels from low to maximum. |
| `border-show` | bool | `false` | Display border around button. |
| `icon-show` | bool | `true` | Display module icon. |
| `label-show` | bool | `true` | Display percentage label. |
| `format` | string | `"{{ percent }}%"` | Format string for the label. |
| `label-max-length` | u32 | `0` | Max label characters before truncation with ellipsis. Set to 0 to disable. |
| `min-brightness` | u32 | `1` | Lower bound (percent) for the native brightness-adjust action. |
| `enable-external` | bool | `true` | Discover and control external monitors over DDC/CI (I²C). |
| `thresholds` | array of [`ThresholdEntry`](/config/types#threshold-entry) | `[]` | Dynamic color thresholds based on brightness percentage. |

::: details More about `level-icons`

The percentage is divided evenly among icons. With 3 icons:
0-33% uses icons\[0\], 34-66% uses icons\[1\], 67-100% uses icons\[2\].

:::

::: details More about `format`

#### Placeholders

- `{{ percent }}` - Brightness (0-100)

#### Examples

- `"{{ percent }}%"` - "65%"

:::

::: details More about `min-brightness`

Scrolling or clicking down never drops below this, so a dimmer cannot
reach a fully dark screen by accident. Use the brightness-toggle action
to intentionally blackout to 0%.

:::

::: details More about `enable-external`

External monitors appear as extra sliders in the brightness panel.
Requires the `i2c-dev` kernel module and read/write access to
`/dev/i2c-*` (usually membership in the `i2c` group). Disable to skip
the slow DDC scan if you only have an internal panel.

:::

::: details More about `thresholds`

Entries are checked in order; the last matching entry wins for each
color slot. Use `below` for low-brightness warnings.

#### Example

```toml
[[modules.brightness.thresholds]]
below = 20
icon-color = "status-warning"
label-color = "status-warning"
```

:::

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"yellow"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"yellow"` | Icon container background color token. |
| `label-color` | [`ColorValue`](/config/types#color-value) | `"yellow"` | Label text color token. |
| `button-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Button background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `left-click` | [`ClickAction`](/config/types#click-action) | `"dropdown:brightness"` | Action on left click. Default opens the brightness dropdown. |
| `right-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on right click. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `"brightness:5"` | Action on scroll up. Default raises brightness by 5%. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `"brightness:-5"` | Action on scroll down. Default lowers brightness by 5%, floored at `min-brightness`. |

## Default configuration

```toml
[modules.brightness]
level-icons = [
    "ld-sun-dim-symbolic",
    "ld-sun-medium-symbolic",
    "ld-sun-symbolic",
]
border-show = false
border-color = "yellow"
icon-show = true
icon-color = "auto"
icon-bg-color = "yellow"
label-show = true
label-color = "yellow"
format = "{{ percent }}%"
label-max-length = 0
min-brightness = 1
enable-external = true
button-bg-color = "bg-surface-elevated"
left-click = "dropdown:brightness"
right-click = ""
middle-click = ""
scroll-up = "brightness:5"
scroll-down = "brightness:-5"
thresholds = []
```


</div>

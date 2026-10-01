---
title: treeman
outline: [2, 3]
---

# treeman

<div v-pre>

treeman worktree health across all registered repos, in a dropdown.

Add it to your layout with `treeman`:

```toml
[[bar.layout]]
monitor = "*"
right = ["treeman"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `format` | string | `"{{ total }}"` | Format string for the label. |
| `icon-name` | string | `"ld-layers-symbolic"` | Module icon (shown when every worktree is resting-ready). |
| `icon-preparing` | string | `"tb-loader-2-symbolic"` | Icon shown while any worktree is being prepared. |
| `icon-tearing-down` | string | `"ld-trash-2-symbolic"` | Icon shown while any worktree is being torn down. |
| `icon-failed` | string | `"tb-alert-triangle-symbolic"` | Icon shown when any worktree's last finalize errored. |
| `hide-if-empty` | bool | `false` | Collapse the module entirely when there are no active worktrees. |
| `border-show` | bool | `false` | Display border around button. |
| `icon-show` | bool | `true` | Display module icon. |
| `label-show` | bool | `true` | Display count label. |
| `label-max-length` | u32 | `0` | Max label characters before truncation with ellipsis. Set to 0 to disable. |

::: details More about `format`

#### Placeholders

- `{{ total }}` - Total active worktrees
- `{{ stable }}` - Worktrees in the ready bucket
- `{{ up }}` - Worktrees preparing
- `{{ down }}` - Worktrees tearing down
- `{{ failed }}` - Worktrees whose last finalize errored

#### Examples

- `"{{ total }}"` - "7"
- `"{{ total }} ({{ failed }}!)"` - "7 (1!)"

:::

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"border-accent"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"accent"` | Icon container background color token. |
| `label-color` | [`ColorValue`](/config/types#color-value) | `"accent"` | Label text color token. |
| `button-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Button background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `left-click` | [`ClickAction`](/config/types#click-action) | `"dropdown:treeman"` | Action on left click. |
| `right-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on right click. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |

## Default configuration

```toml
[modules.treeman]
format = "{{ total }}"
icon-name = "ld-layers-symbolic"
icon-preparing = "tb-loader-2-symbolic"
icon-tearing-down = "ld-trash-2-symbolic"
icon-failed = "tb-alert-triangle-symbolic"
hide-if-empty = false
border-show = false
border-color = "border-accent"
icon-show = true
icon-color = "auto"
icon-bg-color = "accent"
label-show = true
label-color = "accent"
label-max-length = 0
button-bg-color = "bg-surface-elevated"
left-click = "dropdown:treeman"
right-click = ""
middle-click = ""
scroll-up = ""
scroll-down = ""
```


</div>

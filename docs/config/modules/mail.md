---
title: mail
outline: [2, 3]
---

# mail

<div v-pre>

Unread mail count, backed by a notmuch query.

Runs `notmuch count <query>` and re-queries whenever the maildir changes
(event-driven via an inotify watch on the notmuch database path). Hidden
while the count is zero when `hide-when-zero` is set.

Add it to your layout with `mail`:

```toml
[[bar.layout]]
monitor = "*"
right = ["mail"]
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `format` | string | `"{{ count }}"` | Format string for the label. |
| `query` | string | `"tag:unread"` | notmuch search query whose match count is shown. |
| `accounts` | array of [`MailAccount`](/config/types#mail-account) | `[]` | Per-account unread breakdown shown in the mail dropdown. Each account has its own notmuch query and a provider (for its icon). When set, the bar count/label is the sum across accounts and `query` is ignored. |
| `hide-when-zero` | bool | `true` | Hide the module entirely while the count is zero. |
| `notify` | bool | `false` | Fire a desktop notification (via `notify-send`) when the unread count rises — i.e. new mail arrives. One notification per newly-arrived message (capped per burst), showing its sender and subject. With `accounts` configured, each notification uses that account's provider icon; otherwise the module icon is used. |
| `notify-summary` | string | `"{{ sender }}"` | Notification summary when new mail arrives. |
| `notify-body` | string | `"{{ subject }}"` | Notification body when new mail arrives. Same placeholders as `notify-summary`. |
| `icon-name` | string | `"ld-mail-symbolic"` | Module icon. |
| `border-show` | bool | `false` | Display border around button. |
| `icon-show` | bool | `true` | Display module icon. |
| `label-show` | bool | `true` | Display label. |
| `label-max-length` | u32 | `0` | Max label characters before truncation with ellipsis. Set to 0 to disable. |

::: details More about `format`

#### Placeholders

- `{{ count }}` - Number of messages matching the query

#### Examples

- `"{{ count }}"` - "3"

:::

::: details More about `query`

Any query `notmuch count` accepts, e.g. `tag:unread`,
`tag:unread and tag:inbox`, `folder:work and tag:unread`.

Ignored when `accounts` is non-empty — the bar count is then the sum of
the per-account queries.

:::

::: details More about `notify-summary`

#### Placeholders

- `{{ sender }}` - Message sender (name or address)
- `{{ subject }}` - Message subject
- `{{ count }}` - Total messages matching the query
- `{{ new }}` - How many arrived since the last count

:::

## Colors

| Field | Type | Default | Description |
|---|---|---|---|
| `border-color` | [`ColorValue`](/config/types#color-value) | `"blue"` | Border color token. |
| `icon-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Icon foreground color. Auto selects based on variant for contrast. |
| `icon-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Icon container background color token. |
| `label-color` | [`ColorValue`](/config/types#color-value) | `"auto"` | Label text color token. |
| `button-bg-color` | [`ColorValue`](/config/types#color-value) | `"bg-surface-elevated"` | Button background color token. |

## Click actions

| Field | Type | Default | Description |
|---|---|---|---|
| `left-click` | [`ClickAction`](/config/types#click-action) | `"dropdown:mail"` | Action on left click. Defaults to opening the per-account dropdown; set to empty for no action, or a shell command (e.g. your mail client). |
| `right-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on right click. |
| `middle-click` | [`ClickAction`](/config/types#click-action) | `""` | Action on middle click. |
| `scroll-up` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll up. |
| `scroll-down` | [`ClickAction`](/config/types#click-action) | `""` | Action on scroll down. |

## Default configuration

```toml
[modules.mail]
format = "{{ count }}"
query = "tag:unread"
accounts = []
hide-when-zero = true
notify = false
notify-summary = "{{ sender }}"
notify-body = "{{ subject }}"
icon-name = "ld-mail-symbolic"
border-show = false
border-color = "blue"
icon-show = true
icon-color = "auto"
icon-bg-color = "bg-surface-elevated"
label-show = true
label-color = "auto"
label-max-length = 0
button-bg-color = "bg-surface-elevated"
left-click = "dropdown:mail"
right-click = ""
middle-click = ""
scroll-up = ""
scroll-down = ""
```


</div>

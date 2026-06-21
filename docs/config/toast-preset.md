---
title: toast-preset
outline: [2, 3]
---

# toast-preset

<div v-pre>

A reusable toast preset, triggerable by id with `wayle toast --preset <id>`.

A preset captures a toast's text, icon, optional progress bar, duration, and
CSS class so it can be fired by name. Any field can still be overridden per
invocation on the command line (or over the widget socket).

## Example

```toml
[[toasts.presets]]
id = "saved"
label = "Saved"
icon = "ld-check-symbolic"
duration-ms = 1500
class = "success"

# Fire it: wayle toast --preset saved
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `id` | string | required | Unique identifier. Trigger with `wayle toast --preset <id>`. |
| `label` | string or null | `null` | Toast text. An explicit label on the command line overrides this. |
| `icon` | string or null | `null` | Symbolic icon name shown beside the text. |
| `percentage` | number or null | `null` | Progress percentage (0-100). When set, renders a progress bar instead of a plain icon + label toast. |
| `duration-ms` | u32 or null | `null` | Auto-dismiss duration in milliseconds. Unset falls back to the toast config duration. |
| `class` | string or null | `null` | Extra CSS class applied to the toast for custom styling. |

## Default configuration

Required fields (must be set in your config): `id`.


</div>

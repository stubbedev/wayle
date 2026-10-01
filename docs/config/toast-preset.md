---
title: toast-preset
outline: [2, 3]
---

# toast-preset

<div v-pre>

A reusable toast preset, triggerable by id with `wayle toast --preset <id>`.

A preset captures a toast's text and icon so it can be fired by name. The
label/icon can still be overridden per invocation, and runtime-only fields
(`--percentage`, `--duration`, `--class`) are supplied at invoke time, not
stored on the preset. Duration always follows the OSD config.

## Example

```toml
[[osd.presets]]
id = "saved"
label = "Saved"
icon = "ld-check-symbolic"

# Fire it: wayle toast --preset saved
# With a progress bar: wayle toast --preset saved --percentage 80
```

## General

| Field | Type | Default | Description |
|---|---|---|---|
| `id` | string | required | Unique identifier. Trigger with `wayle toast --preset <id>`. |
| `label` | unknown | `null` | Toast text. An explicit label on the command line overrides this. |
| `icon` | unknown | `null` | Symbolic icon name shown beside the text. |

## Default configuration

Required fields (must be set in your config): `id`.


</div>

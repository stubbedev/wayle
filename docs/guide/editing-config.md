---
title: Editing config
---

# Editing config

Wayle reads configuration from `~/.config/wayle/config.toml` or `~/.config/wayle/config.yaml`. All fields have defaults, so the config file may be empty; declare only the fields that should override a default.

The `wayle config` CLI and the settings GUI write to `~/.config/wayle/runtime.toml` rather than `config.toml`. For each field, Wayle uses the first value defined among these sources:

1. `runtime.toml` - overrides written by `wayle config` or the settings GUI.
2. `config.toml` - values declared by hand.
3. The built-in default.

A minimal override:

```toml
[bar]
scale = 1.25

[modules.clock]
format = "%H:%M"
```

Every supported key is documented in the [config reference](/config/).

## TOML or YAML

Both formats are accepted and map to the same fields. When more than one exists, Wayle prefers `config.yaml`, then `config.yml`, then `config.toml`. A fresh install creates `config.toml`; to switch to YAML, just write a `config.yaml`. The minimal override above is equivalent to:

```yaml
bar:
  scale: 1.25
modules:
  clock:
    format: "%H:%M"
```

`imports` work in either format, and an imported file may itself be TOML or YAML — a bare import name (no extension) resolves to an existing `.yaml`, `.yml`, or `.toml` sibling. The GUI- and CLI-written `runtime.toml` overlay is always TOML.

## Pixel sizes

Any size field — bar insets and padding, the module gap, button icon/label sizes and paddings, and per-module icon, label, gap, and padding sizes — accepts either a scale multiplier (a bare number, multiplied by the bar scale) or an absolute pixel length written as a string:

```toml
[bar]
button-icon-size = "20px"   # absolute pixels, ignores bar scale
button-gap = 1.5            # scale multiplier (default form)
```

Pixel values are logical pixels: GTK scales them for HiDPI displays automatically, so `"20px"` renders at the correct physical size on a 2× monitor while ignoring the configurable `bar.scale` multiplier.

## Imports

`config.toml` may declare a top-level `imports` array to load additional TOML files. Files referenced through `imports` may themselves declare `imports`, forming a chain:

```toml
imports = ["themes/nord.toml", "modules/clock.toml"]

[bar]
scale = 1.25
```

Paths are resolved relative to the importing file's directory; the `.toml` extension may be omitted. Imports are merged in declaration order, then the importing file is overlaid on top. Tables merge key by key; scalars and arrays in the overlay replace the corresponding value in the base. The merged result becomes the `config.toml` layer described above.

`runtime.toml` does not resolve imports. Circular chains are rejected at load; the previous valid configuration remains active and the error is recorded in the log.

## Editor setup

On startup, Wayle writes a JSON Schema for the configuration to `~/.config/wayle/schema.json`. Any TOML language server with JSON Schema support can use this file for completion, hover documentation, and validation. The schema is generated from the installed binary and matches the version of Wayle on disk.

[Tombi](https://tombi-toml.github.io/tombi/) is one such server. The Tombi extension is available in the VS Code marketplace; the `tombi` LSP binary runs under Neovim, Helix, and Zed. Configure the server to associate `~/.config/wayle/schema.json` with `config.toml`.

The same schema is published in the repository at [`schema/wayle-config.schema.json`](https://github.com/stubbedev/wayle/blob/master/schema/wayle-config.schema.json) and works for YAML too. Add a modeline at the top of `config.yaml` so any `yaml-language-server` gives completion and validation:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/stubbedev/wayle/master/schema/wayle-config.schema.json
```

You can regenerate the schema for the exact version you run with `wayle config schema --stdout`.

## Live reload

Wayle watches the configuration directory. Changes to `config.toml` trigger an in-process reload; a shell restart is not required. Invalid configuration is rejected, the previous valid state is retained, and parse or validation errors are recorded in the log.

## Editing from the CLI

The `wayle config` subcommand reads and writes individual fields by dotted path:

```bash
wayle config get bar.scale
wayle config set modules.clock.format "%H:%M"
wayle config reset modules.clock.format
```

`set` writes to `~/.config/wayle/runtime.toml`; `config.toml` is never modified by the CLI or GUI Settings dialog. `reset` removes the runtime override for the given path, reverting the field to the value declared in `config.toml` or to the built-in default.

## Printing the default configuration

`wayle config default --stdout` prints every key with its default value to standard output. Without `--stdout`, the command writes `config.toml.example` to the configuration directory; `config.toml` is not modified. `wayle config schema --stdout` prints the JSON Schema in the same manner.

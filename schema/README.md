# Wayle config JSON Schema

`wayle-config.schema.json` is the JSON Schema for the complete Wayle
configuration. It is generated from the Rust config types and kept current by
CI (the `Config schema` job fails if this file drifts from the generated
output).

Regenerate it after changing any config type:

```sh
cargo run -p wayle -- config schema --stdout | jq -S . > schema/wayle-config.schema.json
```

## Editor hints

The same schema works for both YAML and TOML configs — it gives autocomplete,
inline docs, and validation in editors that speak the Language Server Protocol.

### YAML (`config.yaml`)

Add a `yaml-language-server` modeline at the top of the file:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/wayle-rs/wayle/master/schema/wayle-config.schema.json
```

Or point at a local checkout:

```yaml
# yaml-language-server: $schema=./schema/wayle-config.schema.json
```

### TOML (`config.toml`)

Wayle already writes a `tombi.toml` into the config directory that associates
`config.toml` with the generated `schema.json` (see `wayle config schema`).
Editors using Taplo can instead add a directive at the top of the file:

```toml
#:schema https://raw.githubusercontent.com/wayle-rs/wayle/master/schema/wayle-config.schema.json
```

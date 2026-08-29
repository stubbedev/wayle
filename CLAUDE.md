# CLAUDE.md

## Build and test

Cargo needs the nix dev shell for system libs (wayland, gtk4, gstreamer):

```sh
nix develop -c cargo check --workspace
just test          # cargo nextest run --workspace --no-fail-fast
just lint          # fmt + clippy -D warnings
just check         # lint + test, run before every release
```

## Tests define behavior

Every feature and every bug fix lands with tests, and each behavior gets both
assertions:

- **positive** — the behavior holds when it should (the session stays open, the
  flag is parsed, the row is emitted).
- **negative** — it does *not* hold when it should not, and the failure mode is
  the defined one (the socket closes only on exit, a bad value errors instead of
  silently defaulting, the ignored flag is ignored).

A fix without a test that fails before it is not a fix. Prove the test catches
the regression by reverting the change and watching it fail.

Unit tests live in a `mod tests` next to the code they cover; cross-process or
cross-crate contracts go in `<crate>/tests/*.rs` (see
`wayle/tests/launcher_session.rs` for the launcher socket lifetime, and
`crates/wayle-config/tests/deprecated_alias.rs`).

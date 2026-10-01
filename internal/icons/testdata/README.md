# Icon transform oracle

`symbolic/` holds SVGs and what the Rust `wayle-icons` transform
(`crates/wayle-icons/src/transform.rs`, on usvg 0.46) makes of each:

- `<name>.svg.golden`: `to_symbolic`'s output, or `NONE` when it gives
  up;
- `<name>.svg.parse`: `OK`, or `ERR: ` and the error
  `usvg::Tree::from_str` rejects the file with.

`TestToSymbolicMatchesRust` holds the Go port to both, byte for byte.

The inputs are a sample of the four icon sets wayle installs from
(Simple Icons `si-`, CC0; Lucide `ld-`, ISC; Tabler outline `tb-` and
filled `tbf-`, MIT; Material Symbols outlined `md-`, Apache-2.0), of
Adwaita's symbolic icons (`adwaita-`, CC-BY-SA-3.0/LGPL-3.0), and
hand-written `edge-` cases for what the sets rarely exercise: broken
XML with and without a recoverable `d`, CSS, `use`/`symbol`, nested
`svg`, every join and cap, dashes, units, clips, masks, filters,
markers (orientations, units, view boxes, paint order), and so on;
and `edge-xml-` cases for the XML layer (roxmltree): every error it
reports, entities (markup, loops, attribute normalization),
namespaces, and the text `<style>` is read from.

To regenerate after changing the Rust transform, or to check the port
against a larger corpus (the full npm packages of the four sets agree
too, 14800 files at the time of writing):

```sh
cp Cargo.lock internal/icons/testdata/symgold/
nix develop -c cargo run --release --offline \
  --manifest-path internal/icons/testdata/symgold/Cargo.toml -- \
  internal/icons/testdata/symbolic
# a larger corpus: generate its goldens the same way, then
SYMBOLIC_CORPUS=/path/to/corpus nix develop .#go -c go test ./internal/icons/ -run TestToSymbolicMatchesRust
```

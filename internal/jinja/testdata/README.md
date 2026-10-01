# minijinja oracle corpus

`corpus.jsonl` holds one case per line: a template `t` and its context
`c`. `corpus.golden.jsonl` holds what minijinja 2.15.1 — the version
and feature set (`builtins`, `serde`, default features off) the Rust
shell builds with — renders for each: `{"ok": "<output>"}`, or
`{"err": true}` where it fails. `TestMatchesMinijinja` replays the
corpus through this package and compares.

To add cases, append to `corpus.jsonl` and regenerate the golden file
with this program (a standalone Cargo package; `cargo build --offline`
works from the workspace's vendored registry, with the repo's
`rust-toolchain.toml` and `Cargo.lock` copied next to it):

`Cargo.toml`:

```toml
[package]
name = "mjoracle"
version = "0.1.0"
edition = "2021"

[dependencies]
minijinja = { version = "=2.15.1", default-features = false, features = ["builtins", "serde"] }
serde_json = "1"
```

`src/main.rs`:

```rust
use std::io::{self, BufRead, Write};

fn main() {
    let stdin = io::stdin();
    let mut out = io::stdout();
    for line in stdin.lock().lines() {
        let line = line.unwrap();
        if line.trim().is_empty() { continue; }
        let case: serde_json::Value = serde_json::from_str(&line).unwrap();
        let tpl = case["t"].as_str().unwrap();
        let ctx = case["c"].clone();
        let env = minijinja::Environment::new();
        let res = env.render_str(tpl, ctx);
        let v = match res {
            Ok(s) => serde_json::json!({"ok": s}),
            Err(_) => serde_json::json!({"err": true}),
        };
        writeln!(out, "{}", v).unwrap();
    }
}
```

Then: `target/debug/mjoracle < corpus.jsonl > corpus.golden.jsonl`.

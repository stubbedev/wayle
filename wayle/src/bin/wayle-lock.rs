//! Thin `wayle-lock` binary: a single-token entry point for `wayle lock`.
//!
//! Behaves exactly like `wayle lock` (it calls the same dispatch), but as its
//! own executable. Single-token-exec contexts — a systemd `ExecStart=`, an idle
//! daemon or compositor `spawn` that does not split on whitespace — can lock the
//! session without a wrapper script or a `sh -c` indirection.

use std::process;

use tokio::runtime::Runtime;
use wayle::cli;

fn main() {
    let Ok(runtime) = Runtime::new() else {
        eprintln!("Failed to create tokio runtime");
        process::exit(1);
    };

    let code = runtime.block_on(async {
        match cli::lock::execute().await {
            Ok(()) => 0,
            Err(err) => {
                eprintln!("{err}");
                1
            }
        }
    });

    process::exit(code);
}

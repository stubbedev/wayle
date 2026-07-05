//! Enforces link order for gtk4-layer-shell.
//!
//! The FTL concatenation this build script used to do lives in
//! wayle-shell-core (which owns `locales/`) now. The link-order pragma is
//! kept here as well as there: it must hold for the final binary link, and
//! duplicating the flag is harmless.

fn main() {
    println!("cargo:rerun-if-changed=build.rs");

    // wayle-idle-inhibit can pull in libwayland-client early due to linker behavior.
    // Which then prevents the gtk4 layer shell from interposing since it's gotta be
    // first in the link/load order used for symbol resolution.
    // Easier to just enforce the linking order in our shell, so here we are...
    println!("cargo:rustc-link-lib=gtk4-layer-shell");
    println!("cargo:rustc-link-lib=wayland-client");
}

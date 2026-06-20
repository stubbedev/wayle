//! `wayle portal` — the xdg-desktop-portal backend entry point.
//!
//! Runs the long-lived `org.freedesktop.impl.portal.*` D-Bus service. The
//! frontend (`xdg-desktop-portal`) D-Bus-activates this via the installed
//! `org.freedesktop.impl.portal.desktop.wayle.service` file; it can also be run
//! by hand for testing. Blocks until the process is terminated.

/// Runs the portal backend, returning the process exit code.
pub async fn execute() -> i32 {
    match wayle_portal::run().await {
        Ok(()) => 0,
        Err(err) => {
            eprintln!("portal backend failed: {err}");
            1
        }
    }
}

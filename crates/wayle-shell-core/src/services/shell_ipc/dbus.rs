//! D-Bus interface adapter for shell IPC.

use zbus::{fdo, interface};

use super::{bar::BarVisibility, state::ShellIpcState};

/// D-Bus daemon that dispatches shell commands to domain handlers.
pub struct ShellIpcDaemon {
    bar: BarVisibility,
    state: ShellIpcState,
}

impl ShellIpcDaemon {
    pub fn new(state: ShellIpcState) -> Self {
        Self {
            bar: BarVisibility::new(state.clone()),
            state,
        }
    }
}

#[interface(name = "com.wayle.Shell1")]
impl ShellIpcDaemon {
    /// Hides the bar on a monitor. Empty string hides all bars.
    pub async fn bar_hide(&self, monitor: &str) {
        self.bar.hide(monitor);
    }

    /// Shows the bar on a monitor. Empty string shows all bars.
    pub async fn bar_show(&self, monitor: &str) {
        self.bar.show(monitor);
    }

    /// Toggles bar visibility on a monitor. Empty string toggles all.
    pub async fn bar_toggle(&self, monitor: &str) -> fdo::Result<()> {
        self.bar.toggle(monitor)
    }

    /// Locks the session via Wayle's lock screen.
    pub async fn lock(&self) -> fdo::Result<()> {
        if super::lock_trigger()() {
            Ok(())
        } else {
            Err(fdo::Error::Failed(
                "lock screen not ready (shell UI not initialized)".to_string(),
            ))
        }
    }

    /// Hands a `globalprotectcallback:` URI from the browser to a waiting
    /// GlobalProtect SAML sign-in.
    ///
    /// The desktop entry wayle installs for that URI scheme runs
    /// `wayle vpn sso-callback`, which calls this.
    pub async fn vpn_sso_callback(&self, uri: &str) -> fdo::Result<()> {
        if wayle_network::vpn::deliver_sso_callback(uri) {
            Ok(())
        } else {
            // Not an error the browser can act on, but the CLI reports it so a
            // stale callback does not look like a sign-in that worked.
            Err(fdo::Error::Failed(
                "no VPN browser sign-in is waiting for a callback".to_string(),
            ))
        }
    }

    /// Currently hidden monitor connectors.
    #[zbus(property)]
    pub async fn bar_hidden(&self) -> Vec<String> {
        let mut result: Vec<String> = self.state.hidden_bars.get().into_iter().collect();
        result.sort();
        result
    }

    /// All active monitor connectors.
    #[zbus(property)]
    pub async fn connectors(&self) -> Vec<String> {
        self.state.connectors.get()
    }
}

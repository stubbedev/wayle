//! VPN command: the browser's answer to a GlobalProtect SAML sign-in.
//!
//! Not something to run by hand. wayle registers a desktop entry for the
//! `globalprotectcallback:` URI scheme pointing at this, so when the identity
//! provider finishes and redirects the browser to that scheme, the payload
//! reaches the shell — and the sign-in waiting inside it — rather than a
//! "no application can open this address" dialog.

use wayle_ipc::shell_ipc::ShellIpcProxy;
use zbus::Connection;

use crate::cli::CliAction;

/// Forwards a `globalprotectcallback:` URI to the running shell.
///
/// # Errors
///
/// Returns an error if the session bus is unavailable, the shell is not
/// running, or no sign-in was waiting for a callback.
pub async fn sso_callback(uri: &str) -> CliAction {
    let connection = Connection::session()
        .await
        .map_err(|err| format!("D-Bus session unavailable: {err}"))?;

    let proxy = ShellIpcProxy::new(&connection)
        .await
        .map_err(|err| format!("cannot create shell IPC proxy: {err}"))?;

    proxy
        .vpn_sso_callback(uri)
        .await
        .map_err(|err| format!("VPN sign-in callback failed: {err}"))?;

    println!("Sign-in completed; you can close the browser tab.");
    Ok(())
}

/// `wayle vpn` subcommands.
#[derive(Debug, clap::Subcommand)]
pub enum VpnCommands {
    /// Hand a browser sign-in callback to the running shell
    ///
    /// Registered as the handler for the `globalprotectcallback:` URI scheme;
    /// not normally run by hand.
    SsoCallback {
        /// The `globalprotectcallback:` URI the browser was sent to.
        uri: String,
    },
}

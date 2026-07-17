//! PAM-backed [`AuthConversation`] for in-session authentication (lock screen).
//!
//! The conversation runs on the worker thread [`crate::spawn`] creates, so the
//! GTK loop never blocks on PAM. Secret replies are zeroed as soon as they have
//! been handed to PAM, so the plaintext does not linger in our buffers longer
//! than the single verification attempt. The password is never logged.

use std::{
    cell::RefCell,
    ffi::{CStr, CString},
    io::Write,
    process::{Command, Stdio},
};

use pam::Converse;
use tracing::warn;
use zeroize::{Zeroize, Zeroizing};

use crate::{AuthConversation, AuthPrompt};

/// Unlock the gnome-keyring login collection with the just-verified password.
///
/// Under greetd autologin no password is entered at boot, so the PAM login
/// hook (`pam_gnome_keyring`) starts the daemon but leaves the login keyring
/// LOCKED — the first password the user ever types is at this lock screen. We
/// hand that password to the already-running daemon so the keyring unlocks
/// here instead, matching what a normal password login would have done.
///
/// Deliberately a short-lived subprocess, not `pam`'s `open_session()`: that
/// path calls `env::set_var` on the live process (multithreaded here → UB, and
/// it also sets `PATH` to a literal `"$PATH:…"`). The subprocess keeps all of
/// that isolated. Best-effort: any failure is logged and ignored, never
/// blocking or denying the unlock the user already authenticated.
fn unlock_login_keyring(password: &str) {
    // gnome-keyring-daemon finds the running daemon via GNOME_KEYRING_CONTROL;
    // the greetd PAM session exports it, but fall back to the well-known path
    // under XDG_RUNTIME_DIR so an unset var still works.
    let control = std::env::var_os("GNOME_KEYRING_CONTROL").unwrap_or_else(|| {
        let rt = std::env::var("XDG_RUNTIME_DIR").unwrap_or_default();
        format!("{rt}/keyring").into()
    });

    let mut child = match Command::new("gnome-keyring-daemon")
        .arg("--unlock")
        .env("GNOME_KEYRING_CONTROL", control)
        .stdin(Stdio::piped())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
    {
        Ok(child) => child,
        // gnome-keyring not installed / not on PATH: nothing to unlock.
        Err(err) => {
            warn!(error = %err, "keyring: could not spawn gnome-keyring-daemon --unlock");
            return;
        }
    };

    if let Some(mut stdin) = child.stdin.take() {
        // No trailing newline: the keyring password is the exact byte string.
        if let Err(err) = stdin.write_all(password.as_bytes()) {
            warn!(error = %err, "keyring: could not write password to unlock pipe");
        }
        // Drop closes stdin → EOF, so the daemon stops reading.
    }
    let _ = child.wait();
}

/// Resolves the login name of the session user from the environment.
///
/// A locked graphical session always has `USER` (or `LOGNAME`) set; both are
/// checked so the unlock still authenticates if one is missing.
#[must_use]
pub fn current_username() -> String {
    std::env::var("USER")
        .ok()
        .filter(|user| !user.is_empty())
        .or_else(|| {
            std::env::var("LOGNAME")
                .ok()
                .filter(|user| !user.is_empty())
        })
        .unwrap_or_default()
}

/// PAM authentication against a configured service (e.g. `system-auth`).
pub struct PamAuth {
    /// PAM service name to authenticate against.
    pub service: String,
}

impl PamAuth {
    /// Creates a PAM backend for `service`.
    #[must_use]
    pub fn new(service: impl Into<String>) -> Self {
        Self {
            service: service.into(),
        }
    }
}

impl AuthConversation for PamAuth {
    fn run(
        &mut self,
        username: Option<String>,
        ask: &mut dyn FnMut(AuthPrompt) -> Option<String>,
    ) -> Result<(), String> {
        // The PAM transaction is started with no user, so PAM requests the
        // username via the echoed prompt; we answer it from `username` rather
        // than bouncing it to the UI (the session user is already known).
        let username = username.unwrap_or_else(current_username);
        // Stash the last secret we hand PAM so a successful auth can also
        // unlock the login keyring (see unlock_login_keyring). Zeroized on drop.
        let captured: RefCell<Option<Zeroizing<String>>> = RefCell::new(None);
        let converse = PamConverse {
            username,
            ask,
            captured: &captured,
        };

        let mut authenticator = pam::Authenticator::with_handler(&self.service, converse)
            .map_err(|err| {
                warn!(service = %self.service, error = %err, "auth: could not start PAM transaction");
                format!("could not start PAM transaction: {err}")
            })?;

        authenticator.authenticate().map_err(|err| {
            warn!(service = %self.service, error = %err, "auth: PAM authentication failed");
            format!("authentication failed: {err}")
        })?;

        // Authenticated: unlock the gnome-keyring login collection with the
        // same password (best-effort; never fails the unlock). Needed because
        // greetd autologin never entered a password at login.
        if let Some(password) = captured.borrow().as_deref() {
            unlock_login_keyring(password);
        }
        Ok(())
    }
}

/// Bridges PAM's [`Converse`] callbacks to an [`AuthConversation`] `ask`
/// closure. Holds the conversation only for the duration of a single
/// [`PamAuth::run`].
struct PamConverse<'a> {
    username: String,
    ask: &'a mut dyn FnMut(AuthPrompt) -> Option<String>,
    /// Last secret handed to PAM, kept so a successful auth can reuse it to
    /// unlock the login keyring. Zeroized when the `RefCell` in `run` drops.
    captured: &'a RefCell<Option<Zeroizing<String>>>,
}

impl Converse for PamConverse<'_> {
    fn prompt_echo(&mut self, _msg: &CStr) -> Result<CString, ()> {
        // Echoed prompts are the username request; answer from the known user.
        CString::new(self.username.clone()).map_err(|_| ())
    }

    fn prompt_blind(&mut self, msg: &CStr) -> Result<CString, ()> {
        let label = msg.to_string_lossy().into_owned();
        let mut response = (self.ask)(AuthPrompt::Secret(label)).ok_or(())?;
        let secret = CString::new(response.as_str()).map_err(|_| ());
        // Keep a copy for the post-auth keyring unlock (Zeroizing clears it on
        // drop). Only the most recent secret is retained.
        *self.captured.borrow_mut() = Some(Zeroizing::new(response.clone()));
        response.zeroize();
        secret
    }

    fn info(&mut self, msg: &CStr) {
        let _ = (self.ask)(AuthPrompt::Info(msg.to_string_lossy().into_owned()));
    }

    fn error(&mut self, msg: &CStr) {
        let _ = (self.ask)(AuthPrompt::Error(msg.to_string_lossy().into_owned()));
    }

    fn username(&self) -> &str {
        &self.username
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn current_username_is_nonempty_in_normal_env() {
        // CI/dev shells always set USER or LOGNAME. Guard the assertion so a
        // truly minimal sandbox can't flake the suite.
        if std::env::var("USER").is_ok() || std::env::var("LOGNAME").is_ok() {
            assert!(!current_username().is_empty());
        }
    }
}

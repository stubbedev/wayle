//! PAM-backed [`AuthConversation`] for in-session authentication (lock screen).
//!
//! The conversation runs on the worker thread [`crate::spawn`] creates, so the
//! GTK loop never blocks on PAM. Secret replies are zeroed as soon as they have
//! been handed to PAM, so the plaintext does not linger in our buffers longer
//! than the single verification attempt. The password is never logged.

use std::ffi::{CStr, CString};

use pam::Converse;
use tracing::warn;
use zeroize::Zeroize;

use crate::{AuthConversation, AuthPrompt};

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
        let converse = PamConverse { username, ask };

        let mut authenticator = pam::Authenticator::with_handler(&self.service, converse)
            .map_err(|err| {
                warn!(service = %self.service, error = %err, "auth: could not start PAM transaction");
                format!("could not start PAM transaction: {err}")
            })?;

        authenticator.authenticate().map_err(|err| {
            warn!(service = %self.service, error = %err, "auth: PAM authentication failed");
            format!("authentication failed: {err}")
        })
    }
}

/// Bridges PAM's [`Converse`] callbacks to an [`AuthConversation`] `ask`
/// closure. Holds the conversation only for the duration of a single
/// [`PamAuth::run`].
struct PamConverse<'a> {
    username: String,
    ask: &'a mut dyn FnMut(AuthPrompt) -> Option<String>,
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

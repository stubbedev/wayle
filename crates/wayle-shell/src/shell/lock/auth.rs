//! PAM-backed password verification for the lock screen.
//!
//! Authentication is intentionally isolated here and always runs on a blocking
//! thread (see [`authenticate`] callers) so the GTK main loop never stalls on a
//! PAM conversation. The password is taken by value and zeroed (via
//! [`zeroize`]) before this returns, so the plaintext does not linger in memory
//! longer than the single verification attempt. The password is never logged.

use tracing::warn;
use zeroize::Zeroize;

/// Resolves the login name of the session user from the environment.
///
/// A locked graphical session always has `USER` (or `LOGNAME`) set; both are
/// checked so the unlock still authenticates if one is missing.
pub(crate) fn current_username() -> String {
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

/// Verifies `password` for `user` against the given PAM `service`.
///
/// Returns `true` only when PAM reports successful authentication. The
/// `password` buffer is zeroed before this function returns regardless of the
/// outcome. Any PAM error is treated as a failed attempt (the screen stays
/// locked); the error is logged without the password.
#[must_use]
pub(crate) fn authenticate(service: &str, user: &str, mut password: String) -> bool {
    let result = match pam::Authenticator::with_password(service) {
        Ok(mut authenticator) => {
            authenticator
                .get_handler()
                .set_credentials(user, password.as_str());
            match authenticator.authenticate() {
                Ok(()) => true,
                Err(err) => {
                    warn!(%service, %user, error = %err, "lock: PAM authentication failed");
                    false
                }
            }
        }
        Err(err) => {
            warn!(%service, error = %err, "lock: could not start PAM transaction");
            false
        }
    };

    password.zeroize();
    result
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

//! Types for the NetworkManager secret agent — what NM asks for, and what the
//! user answers with.

use std::collections::HashMap;

/// One credential NM is missing, and how to ask for it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SecretField {
    /// The NM settings key this fills, e.g. `password`, `psk`, `pin`.
    pub key: String,
    /// English label for the field. The UI translates by key where it has a
    /// string and falls back to this otherwise, so an unknown plugin's own
    /// key still produces a usable prompt rather than a blank one.
    pub label: String,
    /// Whether the input should be masked.
    pub secret: bool,
}

/// A credential prompt NM is waiting on.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SecretRequest {
    /// UUID of the connection profile the secrets are for.
    pub uuid: String,
    /// The profile's display name, for the prompt header.
    pub name: String,
    /// The settings block NM wants filled — `vpn`, `802-11-wireless-security`,
    /// `802-1x`, and so on.
    pub setting: String,
    /// A message from the VPN service itself, when there is one. This is what
    /// carries a 2FA challenge's own wording ("Enter the code from your
    /// authenticator"), which no generic label can replace.
    pub message: Option<String>,
    /// What to ask for.
    pub fields: Vec<SecretField>,
}

/// The answer to a [`SecretRequest`]: values by key, or `None` for a cancel.
pub type SecretReply = Option<HashMap<String, String>>;

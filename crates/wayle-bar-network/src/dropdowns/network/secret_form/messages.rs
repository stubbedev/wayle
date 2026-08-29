use std::collections::HashMap;

use wayle_network::types::agent::SecretField;

#[derive(Debug)]
pub enum SecretFormInput {
    /// NetworkManager is waiting on credentials for this profile.
    Show {
        name: String,
        /// The VPN service's own wording, when it sent any — a 2FA challenge
        /// says what it wants far better than a generic label can.
        message: Option<String>,
        fields: Vec<SecretField>,
    },
    /// The request was withdrawn by NM, or answered elsewhere.
    Hide,
    SubmitClicked,
    CancelClicked,
}

#[derive(Debug)]
pub enum SecretFormOutput {
    /// Values by settings key, ready to hand back to NetworkManager.
    Submit(HashMap<String, String>),
    Cancel,
}

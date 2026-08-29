//! Turning "NM wants secrets for setting X, hinting at keys Y" into a prompt.
//!
//! The hints are the whole point of the VPN-hints capability: a plugin says
//! exactly which keys it is missing, so the prompt asks for those rather than
//! guessing that every VPN wants a username and a password.

use crate::types::agent::SecretField;

/// Keys no human can answer.
///
/// OpenConnect's secrets are a session cookie, the gateway it was issued for,
/// and that gateway's certificate hash — the *output* of an authentication, not
/// its input. Prompting for them would put an un-fillable text box on screen.
/// They are produced natively instead; see [`crate::vpn`].
const MACHINE_ONLY: &[&str] = &[
    "cookie",
    "gwcert",
    "gateway",
    "resolve",
    "xmlconfig",
    "certsigs",
    "lasthost",
];

/// The conventional secret key of a setting, used when NM hints at nothing.
fn default_key(setting: &str) -> Option<&'static str> {
    Some(match setting {
        "802-11-wireless-security" => "psk",
        "802-1x" | "vpn" | "pppoe" => "password",
        "gsm" | "cdma" => "password",
        "wireguard" => "private-key",
        _ => return None,
    })
}

/// Label and masking for a key.
fn describe(key: &str) -> (&'static str, bool) {
    match key {
        "psk" | "password" | "passwd" | "leap-password" | "secret" => ("Password", true),
        "wep-key0" | "wep-key1" | "wep-key2" | "wep-key3" => ("WEP key", true),
        "private-key-password" => ("Private key password", true),
        "private-key" => ("Private key", true),
        "pin" => ("PIN", true),
        "user" | "username" | "user-name" => ("Username", false),
        "usergroup" => ("Group", false),
        "domain" => ("Domain", false),
        // An unknown key from some plugin's own vocabulary. Masked, because a
        // secret shown in the clear is the worse of the two wrong guesses.
        _ => ("Password", true),
    }
}

/// What to ask the user for, or `None` when there is nothing a person could
/// usefully type.
pub(super) fn for_request(
    setting: &str,
    hints: &[String],
    _connection_type: &str,
) -> Option<Vec<SecretField>> {
    let keys: Vec<String> = if hints.is_empty() {
        vec![default_key(setting)?.to_owned()]
    } else {
        hints
            .iter()
            .filter(|hint| !MACHINE_ONLY.contains(&hint.as_str()))
            .cloned()
            .collect()
    };

    if keys.is_empty() {
        return None;
    }

    Some(
        keys.into_iter()
            .map(|key| {
                let (label, secret) = describe(&key);
                SecretField {
                    key,
                    label: String::from(label),
                    secret,
                }
            })
            .collect(),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hints(values: &[&str]) -> Vec<String> {
        values.iter().map(|value| String::from(*value)).collect()
    }

    #[test]
    fn hints_become_one_field_each() {
        let fields = for_request("vpn", &hints(&["user", "password"]), "vpn").expect("fields");
        assert_eq!(fields.len(), 2);
        assert_eq!(fields[0].key, "user");
        assert!(!fields[0].secret, "a username is not masked");
        assert!(fields[1].secret, "a password is");
    }

    #[test]
    fn a_hintless_wifi_request_asks_for_the_psk() {
        let fields =
            for_request("802-11-wireless-security", &[], "802-11-wireless").expect("fields");
        assert_eq!(fields.len(), 1);
        assert_eq!(fields[0].key, "psk");
    }

    #[test]
    fn an_openconnect_request_is_not_a_prompt() {
        // cookie/gateway/gwcert are produced by authenticating, not typed.
        assert!(
            for_request("vpn", &hints(&["cookie", "gateway", "gwcert"]), "vpn").is_none(),
            "asking a user to type a session cookie is not a prompt"
        );
    }

    #[test]
    fn a_mixed_request_keeps_only_the_answerable_keys() {
        let fields = for_request("vpn", &hints(&["password", "gwcert"]), "vpn").expect("fields");
        assert_eq!(fields.len(), 1);
        assert_eq!(fields[0].key, "password");
    }

    #[test]
    fn an_unknown_setting_with_no_hints_has_nothing_to_ask() {
        assert!(for_request("bluetooth", &[], "bluetooth").is_none());
    }

    #[test]
    fn an_unknown_key_is_masked_rather_than_shown() {
        let fields = for_request("vpn", &hints(&["totp-token"]), "vpn").expect("fields");
        assert!(fields[0].secret);
    }
}

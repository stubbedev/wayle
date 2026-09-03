//! Reading a `wg-quick` configuration file into form values.
//!
//! A WireGuard tunnel arrives as a `.conf` file, and without this the nine
//! fields in it get retyped by hand — including two 44-character base64 keys,
//! where a typo produces a tunnel that comes up and carries nothing.
//!
//! Only the keys wayle's WireGuard form asks for are read. `wg-quick`'s own
//! extras — `MTU`, `Table`, `PreUp` and friends — have nowhere to go in a
//! NetworkManager profile built from this form, and inventing somewhere for
//! them would be inventing a second config format.

use std::collections::HashMap;

/// The interface name to use when the file does not imply one.
const DEFAULT_INTERFACE: &str = "wg0";

/// Parses a `wg-quick` file into the values the WireGuard form holds.
///
/// `file_name` names the interface, the way `wg-quick up wg0` does: a
/// `wg0.conf` is the `wg0` interface. Pass an empty name when there is no file
/// to take it from.
///
/// Returns `None` when the text is not a `wg-quick` config at all — an
/// `[Interface]` section with a private key is the least it can be.
#[must_use]
pub fn parse(text: &str, file_name: &str) -> Option<HashMap<String, String>> {
    let mut interface: HashMap<String, String> = HashMap::new();
    // Only the first peer: the form models one, and picking silently among
    // several would be picking wrong half the time.
    let mut peer: HashMap<String, String> = HashMap::new();
    let mut section = Section::None;
    let mut seen_peer = false;

    for line in text.lines() {
        let line = strip_comment(line);
        if line.is_empty() {
            continue;
        }

        if let Some(name) = section_name(line) {
            section = match name.to_ascii_lowercase().as_str() {
                "interface" => Section::Interface,
                "peer" if !seen_peer => {
                    seen_peer = true;
                    Section::Peer
                }
                // A second peer is skipped rather than merged into the first.
                "peer" => Section::Other,
                _ => Section::Other,
            };
            continue;
        }

        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        let key = key.trim().to_ascii_lowercase();
        let value = value.trim();
        if value.is_empty() {
            continue;
        }

        match section {
            Section::Interface => interface.insert(key, String::from(value)),
            Section::Peer => peer.insert(key, String::from(value)),
            Section::None | Section::Other => None,
        };
    }

    interface.get("privatekey")?;

    let mut values = HashMap::new();
    values.insert(String::from("interface"), interface_name(file_name));
    take(&mut values, "private-key", interface.get("privatekey"));
    take(&mut values, "address", interface.get("address"));
    take(&mut values, "dns", interface.get("dns"));
    take(&mut values, "peer-public-key", peer.get("publickey"));
    take(&mut values, "peer-preshared-key", peer.get("presharedkey"));
    take(&mut values, "peer-allowed-ips", peer.get("allowedips"));
    take(&mut values, "peer-endpoint", peer.get("endpoint"));
    take(
        &mut values,
        "peer-keepalive",
        peer.get("persistentkeepalive"),
    );
    Some(values)
}

#[derive(Clone, Copy)]
enum Section {
    None,
    Interface,
    Peer,
    Other,
}

fn take(values: &mut HashMap<String, String>, key: &str, value: Option<&String>) {
    if let Some(value) = value {
        values.insert(String::from(key), value.clone());
    }
}

/// `wg0.conf` names the `wg0` interface, and a name the kernel would refuse is
/// not used at all.
fn interface_name(file_name: &str) -> String {
    let stem = file_name
        .rsplit('/')
        .next()
        .unwrap_or(file_name)
        .strip_suffix(".conf")
        .unwrap_or_else(|| file_name.rsplit('/').next().unwrap_or(file_name));

    let usable = !stem.is_empty()
        && stem.len() <= 15
        && stem
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_');

    if usable {
        String::from(stem)
    } else {
        String::from(DEFAULT_INTERFACE)
    }
}

fn strip_comment(line: &str) -> &str {
    let line = line.split('#').next().unwrap_or("");
    line.trim()
}

fn section_name(line: &str) -> Option<&str> {
    line.strip_prefix('[')?.strip_suffix(']')
}

#[cfg(test)]
mod tests {
    use super::*;

    const CONF: &str = "\
[Interface]
# The tunnel's own end.
PrivateKey = 6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=
Address = 10.0.0.2/24, fd00::2/64
DNS = 10.0.0.1
MTU = 1420

[Peer]
PublicKey = Kx3AZBHm3vDJXPGRAJfvTvUEHY1c2Jw4qYE9nR6qEXY=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = vpn.example.com:51820
PersistentKeepalive = 25
";

    fn parsed() -> HashMap<String, String> {
        parse(CONF, "wg0.conf").expect("a wg-quick config")
    }

    #[test]
    fn every_field_the_form_asks_for_comes_out_of_the_file() {
        let values = parsed();

        assert_eq!(values.get("interface").map(String::as_str), Some("wg0"));
        assert_eq!(
            values.get("private-key").map(String::as_str),
            Some("6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=")
        );
        assert_eq!(
            values.get("address").map(String::as_str),
            Some("10.0.0.2/24, fd00::2/64")
        );
        assert_eq!(values.get("dns").map(String::as_str), Some("10.0.0.1"));
        assert_eq!(
            values.get("peer-public-key").map(String::as_str),
            Some("Kx3AZBHm3vDJXPGRAJfvTvUEHY1c2Jw4qYE9nR6qEXY=")
        );
        assert_eq!(
            values.get("peer-allowed-ips").map(String::as_str),
            Some("0.0.0.0/0, ::/0")
        );
        assert_eq!(
            values.get("peer-endpoint").map(String::as_str),
            Some("vpn.example.com:51820")
        );
        assert_eq!(values.get("peer-keepalive").map(String::as_str), Some("25"));
    }

    #[test]
    fn what_the_form_has_no_home_for_is_left_behind() {
        // MTU is in the file and nowhere in the profile builder; carrying it
        // into the form would be offering a field that goes nowhere.
        assert!(!parsed().contains_key("mtu"));
    }

    #[test]
    fn a_key_in_one_section_does_not_leak_into_the_other() {
        // Both sections have a key whose name ends in "Key"; reading the file
        // flat would put the peer's public key in the interface's slot.
        let values = parsed();

        assert_ne!(values.get("private-key"), values.get("peer-public-key"));
    }

    #[test]
    fn a_preshared_key_is_read_when_there_is_one() {
        let with_psk = CONF.replace(
            "AllowedIPs",
            "PresharedKey = 1oi/mVxLBOM4kRvxTOnQ8Nau6Fmy0OQ4pFm+Xn9zR0M=\nAllowedIPs",
        );

        let values = parse(&with_psk, "wg0.conf").expect("a wg-quick config");

        assert_eq!(
            values.get("peer-preshared-key").map(String::as_str),
            Some("1oi/mVxLBOM4kRvxTOnQ8Nau6Fmy0OQ4pFm+Xn9zR0M=")
        );
        assert!(
            !parsed().contains_key("peer-preshared-key"),
            "and stays absent when the file has none"
        );
    }

    #[test]
    fn only_the_first_peer_is_taken() {
        let two_peers = format!(
            "{CONF}\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n\
             Endpoint = other.example.com:51821\n"
        );

        let values = parse(&two_peers, "wg0.conf").expect("a wg-quick config");

        assert_eq!(
            values.get("peer-endpoint").map(String::as_str),
            Some("vpn.example.com:51820"),
            "the form models one peer; the first is the one it gets"
        );
    }

    #[test]
    fn comments_and_blank_lines_are_not_settings() {
        let noisy = "[Interface]\n\n  # PrivateKey = not-this-one\n\
             PrivateKey = 6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=\n\
             Address = 10.0.0.2/24 # the tunnel address\n";

        let values = parse(noisy, "wg0.conf").expect("a wg-quick config");

        assert_eq!(
            values.get("private-key").map(String::as_str),
            Some("6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=")
        );
        assert_eq!(
            values.get("address").map(String::as_str),
            Some("10.0.0.2/24"),
            "a trailing comment is not part of the value"
        );
    }

    #[test]
    fn the_file_name_names_the_interface() {
        let named = |file_name: &str| {
            parse(CONF, file_name)
                .and_then(|values| values.get("interface").cloned())
                .unwrap_or_default()
        };

        assert_eq!(named("/home/alice/Downloads/work.conf"), "work");
        assert_eq!(named("wg0"), "wg0");
        assert_eq!(named(""), DEFAULT_INTERFACE, "nothing to take a name from");
        assert_eq!(
            named("a name the kernel would refuse.conf"),
            DEFAULT_INTERFACE
        );
        assert_eq!(
            named("sixteen-chars-xx.conf"),
            DEFAULT_INTERFACE,
            "an interface name is at most 15 characters"
        );
    }

    #[test]
    fn something_that_is_not_a_wg_quick_file_is_refused() {
        assert_eq!(parse("", "wg0.conf"), None);
        assert_eq!(parse("hello world", "wg0.conf"), None);
        assert_eq!(
            parse("[Interface]\nAddress = 10.0.0.2/24\n", "wg0.conf"),
            None,
            "no private key means there is no tunnel here"
        );
        assert_eq!(
            parse(
                "[Peer]\nPrivateKey = 6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk=\n",
                "wg0.conf"
            ),
            None,
            "a private key under [Peer] is not the interface's"
        );
    }
}

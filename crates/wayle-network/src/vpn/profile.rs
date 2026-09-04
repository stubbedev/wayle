//! Turning form values into a NetworkManager connection profile, and back.
//!
//! This is the whole of "configuring a VPN": NM's `AddConnection` takes a
//! nested dictionary describing the profile, and every VPN — plugin or native
//! WireGuard — is that same call with a different middle section.

use std::{
    collections::HashMap,
    net::{IpAddr, Ipv4Addr, Ipv6Addr},
};

use zbus::zvariant::{OwnedValue, Value};

use super::kinds::WIREGUARD;

/// A connection dictionary, in NM's `a{sa{sv}}` shape.
pub type ConnectionDict = HashMap<String, HashMap<String, OwnedValue>>;

/// Addresses of one family, as `(address, prefix length)`.
type Addresses = Vec<(String, u32)>;

/// Secret flag `NM_SETTING_SECRET_FLAG_NOT_SAVED`: never store it, always ask
/// the agent. Right for everything openconnect needs, all of which is minted
/// fresh by a sign-in and expires.
const NOT_SAVED: &str = "2";

/// The openconnect secrets that must come from an agent every time.
///
/// Exactly the three keys the plugin's `need_secrets` looks for. The other
/// `-flags` keys wayle used to write — `resolve`, `xmlconfig`, `certsigs`,
/// `lasthost` — were guesses at plugin internals, and flagged nothing that is
/// a secret.
const OPENCONNECT_EPHEMERAL: &[&str] = &["cookie", "gateway", "gwcert"];

fn text(value: &str) -> Option<OwnedValue> {
    OwnedValue::try_from(Value::from(value)).ok()
}

fn dict(values: HashMap<String, String>) -> Option<OwnedValue> {
    OwnedValue::try_from(Value::from(values)).ok()
}

fn insert(section: &mut HashMap<String, OwnedValue>, key: &str, value: Option<OwnedValue>) {
    if let Some(value) = value {
        section.insert(String::from(key), value);
    }
}

/// Builds the profile for a kind from the form's values.
///
/// `uuid` is supplied rather than generated so an edit rewrites the profile it
/// is editing instead of creating a second one.
#[must_use]
pub fn build(
    kind: &str,
    name: &str,
    uuid: &str,
    values: &HashMap<String, String>,
) -> ConnectionDict {
    if kind == WIREGUARD {
        return wireguard(name, uuid, values);
    }
    plugin(kind, name, uuid, values)
}

fn connection_section(
    name: &str,
    uuid: &str,
    connection_type: &str,
    interface: Option<&str>,
) -> HashMap<String, OwnedValue> {
    let mut section = HashMap::new();
    insert(&mut section, "id", text(name));
    insert(&mut section, "uuid", text(uuid));
    insert(&mut section, "type", text(connection_type));
    // Never autoconnect. A VPN that dials itself on every boot is a surprise,
    // and one needing 2FA would raise a prompt nobody asked for.
    insert(
        &mut section,
        "autoconnect",
        OwnedValue::try_from(Value::from(false)).ok(),
    );
    if let Some(interface) = interface.filter(|interface| !interface.is_empty()) {
        insert(&mut section, "interface-name", text(interface));
    }
    section
}

fn ip_section(method: &str) -> HashMap<String, OwnedValue> {
    let mut section = HashMap::new();
    insert(&mut section, "method", text(method));
    section
}

/// A plugin VPN: everything but the `vpn` section is boilerplate.
fn plugin(
    service_type: &str,
    name: &str,
    uuid: &str,
    values: &HashMap<String, String>,
) -> ConnectionDict {
    let mut data: HashMap<String, String> = HashMap::new();
    let mut secrets: HashMap<String, String> = HashMap::new();

    for (key, value) in values {
        if value.is_empty() {
            continue;
        }
        if is_plugin_secret(service_type, key) {
            secrets.insert(key.clone(), value.clone());
            data.insert(format!("{key}-flags"), String::from("0"));
        } else {
            data.insert(key.clone(), value.clone());
        }
    }

    if service_type == "org.freedesktop.NetworkManager.openconnect" {
        for key in OPENCONNECT_EPHEMERAL {
            data.insert(format!("{key}-flags"), String::from(NOT_SAVED));
        }
        // The plugin reads this to decide whether to run the Windows-only
        // host-check trojan. It will not, and saying so avoids a stall.
        data.insert(String::from("enable_csd_trojan"), String::from("no"));
    }
    if service_type == "org.freedesktop.NetworkManager.openvpn" && !values.is_empty() {
        data.entry(String::from("connection-type"))
            .or_insert_with(|| String::from("password"));
    }

    let mut vpn = HashMap::new();
    insert(&mut vpn, "service-type", text(service_type));
    insert(&mut vpn, "data", dict(data));
    if !secrets.is_empty() {
        insert(&mut vpn, "secrets", dict(secrets));
    }

    HashMap::from([
        (
            String::from("connection"),
            connection_section(name, uuid, "vpn", None),
        ),
        (String::from("vpn"), vpn),
        (String::from("ipv4"), ip_section("auto")),
        (String::from("ipv6"), ip_section("auto")),
    ])
}

/// Whether a key is a secret for this plugin, rather than plain configuration.
///
/// Delegates to [`kinds::secret_keys`], which is derived from each plugin's
/// own source. Keeping the answer in one place is what stops the form and the
/// profile builder from disagreeing — a disagreement writes a password into
/// `vpn.data`, where it sits in the profile in the clear.
fn is_plugin_secret(service_type: &str, key: &str) -> bool {
    super::kinds::is_secret(service_type, key)
}

/// A native WireGuard profile: no plugin, the kernel carries it.
fn wireguard(name: &str, uuid: &str, values: &HashMap<String, String>) -> ConnectionDict {
    let get = |key: &str| values.get(key).map(String::as_str).unwrap_or_default();

    let mut tunnel = HashMap::new();
    insert(&mut tunnel, "private-key", text(get("private-key")));
    if let Some(peer) = peer(values) {
        insert(
            &mut tunnel,
            "peers",
            OwnedValue::try_from(Value::from(vec![peer])).ok(),
        );
    }

    let (v4, v6) = split_addresses(get("address"));
    let (dns4, dns6) = split_servers(get("dns"));

    HashMap::from([
        (
            String::from("connection"),
            connection_section(name, uuid, WIREGUARD, Some(get("interface"))),
        ),
        (String::from("wireguard"), tunnel),
        (String::from("ipv4"), ip_config(&v4, &dns4)),
        (String::from("ipv6"), ip_config(&v6, &dns6)),
    ])
}

/// The single peer the form collects. Multi-peer configurations are the
/// free-form editor's problem, not the common case's.
fn peer(values: &HashMap<String, String>) -> Option<HashMap<String, OwnedValue>> {
    let public_key = values
        .get("peer-public-key")
        .filter(|key| !key.is_empty())?;

    let mut peer = HashMap::new();
    insert(&mut peer, "public-key", text(public_key));
    if let Some(endpoint) = values.get("peer-endpoint").filter(|e| !e.is_empty()) {
        insert(&mut peer, "endpoint", text(endpoint));
    }
    if let Some(preshared) = values.get("peer-preshared-key").filter(|k| !k.is_empty()) {
        insert(&mut peer, "preshared-key", text(preshared));
        // Without this NM treats the preshared key as absent.
        insert(
            &mut peer,
            "preshared-key-flags",
            OwnedValue::try_from(Value::from(0_u32)).ok(),
        );
    }
    let allowed: Vec<String> = split_list(
        values
            .get("peer-allowed-ips")
            .map(String::as_str)
            .unwrap_or_default(),
    );
    if !allowed.is_empty() {
        insert(
            &mut peer,
            "allowed-ips",
            OwnedValue::try_from(Value::from(allowed)).ok(),
        );
    }
    if let Some(keepalive) = values
        .get("peer-keepalive")
        .and_then(|raw| raw.trim().parse::<u32>().ok())
    {
        insert(
            &mut peer,
            "persistent-keepalive",
            OwnedValue::try_from(Value::from(keepalive)).ok(),
        );
    }
    Some(peer)
}

/// An IP section carrying explicit addresses, or `disabled` when the tunnel
/// has none of that family.
///
/// `disabled` rather than `auto`: there is no DHCP inside a WireGuard tunnel,
/// so `auto` would leave NM waiting for a lease that never arrives.
fn ip_config(addresses: &[(String, u32)], dns: &[String]) -> HashMap<String, OwnedValue> {
    if addresses.is_empty() {
        return ip_section("disabled");
    }

    let entries: Vec<HashMap<String, OwnedValue>> = addresses
        .iter()
        .map(|(address, prefix)| {
            let mut entry = HashMap::new();
            insert(&mut entry, "address", text(address));
            insert(
                &mut entry,
                "prefix",
                OwnedValue::try_from(Value::from(*prefix)).ok(),
            );
            entry
        })
        .collect();

    let mut section = ip_section("manual");
    insert(
        &mut section,
        "address-data",
        OwnedValue::try_from(Value::from(entries)).ok(),
    );
    if !dns.is_empty() {
        // `dns-data`, not `dns`: the latter is an array of network-byte-order
        // integers, which is a portability trap for no gain.
        insert(
            &mut section,
            "dns-data",
            OwnedValue::try_from(Value::from(dns.to_vec())).ok(),
        );
    }
    section
}

/// Splits a comma- or whitespace-separated list, dropping empties.
fn split_list(raw: &str) -> Vec<String> {
    raw.split([',', ' ', '\t'])
        .map(str::trim)
        .filter(|part| !part.is_empty())
        .map(String::from)
        .collect()
}

/// Splits `10.0.0.2/24, fd00::2/64` by address family, defaulting the prefix
/// to a single host when none is given.
fn split_addresses(raw: &str) -> (Addresses, Addresses) {
    let mut v4 = Vec::new();
    let mut v6 = Vec::new();

    for entry in split_list(raw) {
        let (address, prefix) = match entry.split_once('/') {
            Some((address, prefix)) => (address.to_owned(), prefix.parse::<u32>().ok()),
            None => (entry.clone(), None),
        };
        match address.parse::<IpAddr>() {
            Ok(IpAddr::V4(_)) => v4.push((address, prefix.unwrap_or(32).min(32))),
            Ok(IpAddr::V6(_)) => v6.push((address, prefix.unwrap_or(128).min(128))),
            Err(_) => {}
        }
    }
    (v4, v6)
}

/// Splits DNS servers by address family, so each goes in its own IP section.
fn split_servers(raw: &str) -> (Vec<String>, Vec<String>) {
    let mut v4 = Vec::new();
    let mut v6 = Vec::new();
    for entry in split_list(raw) {
        if entry.parse::<Ipv4Addr>().is_ok() {
            v4.push(entry);
        } else if entry.parse::<Ipv6Addr>().is_ok() {
            v6.push(entry);
        }
    }
    (v4, v6)
}

/// Reads a saved profile back into form values, for the edit form.
#[must_use]
pub fn read_values(settings: &ConnectionDict) -> HashMap<String, String> {
    let mut values = HashMap::new();

    if let Some(vpn) = settings.get("vpn")
        && let Some(data) = vpn
            .get("data")
            .and_then(|value| HashMap::<String, String>::try_from(value.clone()).ok())
    {
        // The `-flags` keys are bookkeeping wayle wrote itself; showing them
        // on the form would invite someone to edit them by hand.
        for (key, value) in data {
            if !key.ends_with("-flags") {
                values.insert(key, value);
            }
        }
        return values;
    }

    if let Some(connection) = settings.get("connection")
        && let Some(interface) = connection
            .get("interface-name")
            .and_then(|value| String::try_from(value.clone()).ok())
    {
        values.insert(String::from("interface"), interface);
    }
    values
}

/// The kind identifier a saved profile belongs to.
#[must_use]
pub fn kind_of(settings: &ConnectionDict) -> Option<String> {
    let connection_type = settings
        .get("connection")?
        .get("type")
        .and_then(|value| String::try_from(value.clone()).ok())?;

    if connection_type == WIREGUARD {
        return Some(String::from(WIREGUARD));
    }
    settings
        .get("vpn")?
        .get("service-type")
        .and_then(|value| String::try_from(value.clone()).ok())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn values(pairs: &[(&str, &str)]) -> HashMap<String, String> {
        pairs
            .iter()
            .map(|(key, value)| (String::from(*key), String::from(*value)))
            .collect()
    }

    fn as_string(section: &HashMap<String, OwnedValue>, key: &str) -> Option<String> {
        String::try_from(section.get(key)?.clone()).ok()
    }

    #[test]
    fn a_wireguard_profile_needs_no_plugin_section_at_all() {
        let profile = build(
            WIREGUARD,
            "Home",
            "uuid-1",
            &values(&[
                ("interface", "wg0"),
                ("private-key", "PRIVATE"),
                ("address", "10.0.0.2/24"),
                ("peer-public-key", "PUBLIC"),
                ("peer-endpoint", "vpn.example.com:51820"),
                ("peer-allowed-ips", "0.0.0.0/0"),
            ]),
        );

        let connection = profile.get("connection").expect("connection section");
        assert_eq!(as_string(connection, "type").as_deref(), Some(WIREGUARD));
        assert_eq!(
            as_string(connection, "interface-name").as_deref(),
            Some("wg0")
        );
        assert!(profile.contains_key("wireguard"));
        // A WireGuard profile that carried a `vpn` section would be rejected
        // by NM as two connection types at once.
        assert!(!profile.contains_key("vpn"));
    }

    #[test]
    fn a_wireguard_tunnel_gets_manual_addressing_not_dhcp() {
        let profile = build(
            WIREGUARD,
            "Home",
            "uuid-1",
            &values(&[("address", "10.0.0.2/24"), ("peer-public-key", "PUBLIC")]),
        );
        let ipv4 = profile.get("ipv4").expect("ipv4");
        assert_eq!(as_string(ipv4, "method").as_deref(), Some("manual"));
        // No DHCP server lives inside a tunnel; `auto` would hang.
        let ipv6 = profile.get("ipv6").expect("ipv6");
        assert_eq!(as_string(ipv6, "method").as_deref(), Some("disabled"));
    }

    #[test]
    fn an_openconnect_profile_marks_its_secrets_as_never_stored() {
        let profile = build(
            "org.freedesktop.NetworkManager.openconnect",
            "Work",
            "uuid-1",
            &values(&[("gateway", "vpn.example.com"), ("protocol", "gp")]),
        );
        let vpn = profile.get("vpn").expect("vpn section");
        let data = HashMap::<String, String>::try_from(vpn.get("data").expect("data").clone())
            .expect("data dict");

        assert_eq!(
            data.get("gateway").map(String::as_str),
            Some("vpn.example.com")
        );
        // A stored cookie would be handed to the plugin after it expired, and
        // the failure looks like a broken VPN rather than a stale session.
        assert_eq!(
            data.get("cookie-flags").map(String::as_str),
            Some(NOT_SAVED)
        );
        assert_eq!(
            data.get("gateway-flags").map(String::as_str),
            Some(NOT_SAVED)
        );
        assert_eq!(
            data.get("gwcert-flags").map(String::as_str),
            Some(NOT_SAVED)
        );
        assert!(!vpn.contains_key("secrets"));

        // Only the keys the plugin actually asks for. The rest were guesses,
        // and they read to anyone inspecting the profile as settings wayle
        // understands.
        let mut flags: Vec<&str> = data
            .keys()
            .filter(|key| key.ends_with("-flags"))
            .map(String::as_str)
            .collect();
        flags.sort_unstable();
        assert_eq!(flags, ["cookie-flags", "gateway-flags", "gwcert-flags"]);
    }

    #[test]
    fn an_openvpn_password_goes_in_secrets_not_in_plain_data() {
        let profile = build(
            "org.freedesktop.NetworkManager.openvpn",
            "Work",
            "uuid-1",
            &values(&[("remote", "vpn.example.com"), ("password", "hunter2")]),
        );
        let vpn = profile.get("vpn").expect("vpn");
        let data = HashMap::<String, String>::try_from(vpn.get("data").expect("data").clone())
            .expect("dict");
        let secrets =
            HashMap::<String, String>::try_from(vpn.get("secrets").expect("secrets").clone())
                .expect("dict");

        assert_eq!(secrets.get("password").map(String::as_str), Some("hunter2"));
        assert!(
            !data.contains_key("password"),
            "a password in vpn.data is stored in the clear and shown by nmcli"
        );
        assert_eq!(data.get("password-flags").map(String::as_str), Some("0"));
    }

    #[test]
    fn no_vpn_is_ever_created_dialling_itself() {
        for kind in [WIREGUARD, "org.freedesktop.NetworkManager.openconnect"] {
            let profile = build(kind, "X", "uuid-1", &values(&[]));
            let connection = profile.get("connection").expect("connection");
            assert_eq!(
                bool::try_from(connection.get("autoconnect").expect("autoconnect").clone()),
                Ok(false),
                "{kind} must not autoconnect"
            );
        }
    }

    #[test]
    fn addresses_are_split_by_family_with_sane_default_prefixes() {
        let (v4, v6) = split_addresses("10.0.0.2/24, fd00::2/64, 192.168.1.5");
        assert_eq!(
            v4,
            vec![
                (String::from("10.0.0.2"), 24),
                (String::from("192.168.1.5"), 32),
            ]
        );
        assert_eq!(v6, vec![(String::from("fd00::2"), 64)]);
    }

    #[test]
    fn a_value_that_is_not_an_address_is_dropped_rather_than_sent_to_nm() {
        let (v4, v6) = split_addresses("not-an-address, 10.0.0.2/24");
        assert_eq!(v4.len(), 1);
        assert!(v6.is_empty());
        let (dns4, dns6) = split_servers("hello, 1.1.1.1");
        assert_eq!(dns4, vec![String::from("1.1.1.1")]);
        assert!(dns6.is_empty());
    }

    #[test]
    fn an_oversized_prefix_is_clamped_to_its_family() {
        let (v4, _) = split_addresses("10.0.0.2/99");
        assert_eq!(v4, vec![(String::from("10.0.0.2"), 32)]);
    }

    #[test]
    fn a_saved_profile_reads_back_without_its_bookkeeping() {
        let profile = build(
            "org.freedesktop.NetworkManager.openconnect",
            "Work",
            "uuid-1",
            &values(&[("gateway", "vpn.example.com"), ("protocol", "gp")]),
        );
        let read = read_values(&profile);
        assert_eq!(
            read.get("gateway").map(String::as_str),
            Some("vpn.example.com")
        );
        assert!(
            read.keys().all(|key| !key.ends_with("-flags")),
            "flag keys are wayle's own bookkeeping: {read:?}"
        );
        assert_eq!(
            kind_of(&profile).as_deref(),
            Some("org.freedesktop.NetworkManager.openconnect")
        );
    }

    #[test]
    fn a_wireguard_profile_reads_back_as_wireguard() {
        let profile = build(
            WIREGUARD,
            "Home",
            "uuid-1",
            &values(&[("interface", "wg0")]),
        );
        assert_eq!(kind_of(&profile).as_deref(), Some(WIREGUARD));
        assert_eq!(
            read_values(&profile).get("interface").map(String::as_str),
            Some("wg0")
        );
    }
}

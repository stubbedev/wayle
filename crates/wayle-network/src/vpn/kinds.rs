//! What kinds of VPN this machine can actually run, and what each one needs
//! to be asked for.
//!
//! WireGuard is always available — NetworkManager carries it natively, the
//! kernel does the work, no plugin involved. Everything else needs a VPN
//! plugin installed, and NM advertises those as `.name` files: one INI per
//! plugin naming its D-Bus service. Reading them means the type picker offers
//! exactly what can work here, rather than a fixed list where two thirds of
//! the entries fail at connect time.

use std::{
    collections::HashMap,
    fs,
    net::{IpAddr, Ipv6Addr},
    path::Path,
};

/// Where NM looks for VPN plugin descriptors. Both are read; a file in the
/// second shadows the same name in the first, which is how a distribution
/// lets `/etc` override its own packaging.
const PLUGIN_DIRECTORIES: &[&str] = &[
    "/usr/lib/NetworkManager/VPN",
    "/usr/lib64/NetworkManager/VPN",
    "/etc/NetworkManager/VPN",
];

/// The `id` of the built-in WireGuard kind. Not a service name: WireGuard is
/// a connection *type*, not a VPN plugin.
pub const WIREGUARD: &str = "wireguard";

/// What a field's value has to look like to be worth sending to
/// NetworkManager.
///
/// NM validates too, and refuses with a raw D-Bus error string that the form
/// then shows verbatim; the address builder is worse still and silently drops
/// what it cannot parse, so a typo in a CIDR became a tunnel with no route and
/// nothing said. Checking here means the complaint names the field.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum VpnFormat {
    /// Anything non-empty.
    #[default]
    Text,
    /// A hostname or address, with no scheme and no path.
    Host,
    /// A hostname or address with a port: `vpn.example.com:51820`.
    HostPort,
    /// One or more IP addresses, comma separated.
    IpList,
    /// One or more addresses, each optionally with a prefix length.
    CidrList,
    /// A base64 X25519 key, as WireGuard writes them.
    Key,
    /// A non-negative whole number.
    Number,
}

impl VpnFormat {
    /// Whether `value` is in this format. An empty value is not this check's
    /// business — that is what `required` is for.
    #[must_use]
    pub fn accepts(self, value: &str) -> bool {
        let value = value.trim();
        if value.is_empty() {
            return true;
        }
        match self {
            Self::Text => true,
            Self::Host => is_host(value),
            Self::HostPort => is_host_port(value),
            Self::IpList => every_item(value, |item| item.parse::<IpAddr>().is_ok()),
            Self::CidrList => every_item(value, is_cidr),
            Self::Key => is_wireguard_key(value),
            Self::Number => value.parse::<u64>().is_ok(),
        }
    }
}

/// Whether every comma-separated item passes `check`. An empty list does not.
fn every_item(raw: &str, check: impl Fn(&str) -> bool) -> bool {
    let mut items = raw
        .split(',')
        .map(str::trim)
        .filter(|item| !item.is_empty())
        .peekable();
    items.peek().is_some() && items.all(check)
}

/// An address, or a name that could resolve to one. Deliberately loose: the
/// point is to catch `https://vpn.example.com/portal`, not to re-implement
/// DNS.
fn is_host(value: &str) -> bool {
    if value.parse::<IpAddr>().is_ok() {
        return true;
    }
    !value.is_empty()
        && !value.contains(['/', ' ', ':', '@'])
        && value.split('.').all(|part| {
            !part.is_empty() && part.chars().all(|c| c.is_ascii_alphanumeric() || c == '-')
        })
}

fn is_host_port(value: &str) -> bool {
    // An IPv6 endpoint is bracketed, which is also how the port stays
    // unambiguous: `[fd00::1]:51820`.
    if let Some(rest) = value.strip_prefix('[') {
        let Some((address, port)) = rest.split_once("]:") else {
            return false;
        };
        return address.parse::<Ipv6Addr>().is_ok() && is_port(port);
    }
    let Some((host, port)) = value.rsplit_once(':') else {
        return false;
    };
    // A leftover colon means an unbracketed IPv6 address, where the last colon
    // is part of the address rather than the separator — `fd00::1:51820` is a
    // whole address with no port at all.
    !host.contains(':') && is_host(host) && is_port(port)
}

fn is_port(value: &str) -> bool {
    value.parse::<u16>().is_ok_and(|port| port != 0)
}

fn is_cidr(value: &str) -> bool {
    let (address, prefix) = match value.split_once('/') {
        Some((address, prefix)) => (address, Some(prefix)),
        None => (value, None),
    };
    let Ok(address) = address.parse::<IpAddr>() else {
        return false;
    };
    let Some(prefix) = prefix else {
        return true;
    };
    let limit = if address.is_ipv4() { 32 } else { 128 };
    prefix.parse::<u32>().is_ok_and(|prefix| prefix <= limit)
}

/// A WireGuard key is 32 bytes of base64: 43 characters and an `=`.
fn is_wireguard_key(value: &str) -> bool {
    value.len() == 44
        && value.ends_with('=')
        && value[..43]
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || c == '+' || c == '/')
}

/// One choice of a field that is a picker rather than a text box.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VpnChoice {
    /// What is stored in the profile.
    pub value: String,
    /// What to call it in the picker.
    pub label: String,
    /// Whether wayle signs into this choice itself.
    ///
    /// The form says so while the choice is being made. The alternative is
    /// what used to happen: the profile saves happily and the first connect
    /// fails with "no native sign-in for openconnect protocol fortinet".
    pub native_sign_in: bool,
}

/// One field on the add/edit form.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VpnField {
    /// Where the value goes. Interpreted per kind by the profile builder.
    pub key: String,
    /// English label. The UI translates the ones it knows.
    pub label: String,
    /// Whether the input is masked and kept out of the profile's plain data.
    pub secret: bool,
    /// Whether saving without it is refused.
    pub required: bool,
    /// Example value, shown as placeholder text.
    pub placeholder: String,
    /// The values this field accepts. Empty means free text.
    pub choices: Vec<VpnChoice>,
    /// What a value has to look like to be worth sending to NM.
    pub format: VpnFormat,
    /// Which group of the form this field belongs to, as a slug the UI turns
    /// into a heading. Empty means it stands on its own.
    ///
    /// WireGuard's nine fields are really three things — the local end, its
    /// addressing, and the peer — and drawn as one flat list none of that is
    /// visible.
    pub section: String,
}

impl VpnField {
    fn new(key: &str, label: &str, placeholder: &str) -> Self {
        Self {
            key: String::from(key),
            label: String::from(label),
            secret: false,
            required: false,
            placeholder: String::from(placeholder),
            choices: Vec::new(),
            format: VpnFormat::Text,
            section: String::new(),
        }
    }

    const fn format(mut self, format: VpnFormat) -> Self {
        self.format = format;
        self
    }

    fn section(mut self, section: &str) -> Self {
        self.section = String::from(section);
        self
    }

    fn choices(mut self, choices: Vec<VpnChoice>) -> Self {
        self.choices = choices;
        self
    }

    const fn required(mut self) -> Self {
        self.required = true;
        self
    }

    const fn secret(mut self) -> Self {
        self.secret = true;
        self
    }
}

/// A kind of VPN that can be created here.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VpnKind {
    /// [`WIREGUARD`], or the plugin's D-Bus service name.
    pub id: String,
    /// What to call it in the picker.
    pub label: String,
    /// The fields to ask for. Empty means there is no typed form and the
    /// free-form editor is the only way in.
    pub fields: Vec<VpnField>,
}

impl VpnKind {
    /// Whether this kind has a purpose-built form, or falls back to the
    /// free-form `key = value` editor.
    #[must_use]
    pub fn is_typed(&self) -> bool {
        !self.fields.is_empty()
    }
}

/// Every VPN kind this machine can run, WireGuard first.
#[must_use]
pub fn available() -> Vec<VpnKind> {
    let mut kinds = vec![wireguard()];
    for (service, plugin_name) in installed_plugins() {
        kinds.push(VpnKind {
            fields: fields_for(&service),
            label: display_label(&service, &plugin_name),
            id: service,
        });
    }
    kinds
}

fn wireguard() -> VpnKind {
    VpnKind {
        id: String::from(WIREGUARD),
        label: String::from("WireGuard"),
        fields: vec![
            VpnField::new("interface", "Interface", "wg0")
                .required()
                .section("interface"),
            VpnField::new("private-key", "Private key", "")
                .required()
                .secret()
                .format(VpnFormat::Key)
                .section("interface"),
            VpnField::new("address", "Addresses", "10.0.0.2/24")
                .required()
                .format(VpnFormat::CidrList)
                .section("addressing"),
            VpnField::new("dns", "DNS", "10.0.0.1")
                .format(VpnFormat::IpList)
                .section("addressing"),
            VpnField::new("peer-public-key", "Peer public key", "")
                .required()
                .format(VpnFormat::Key)
                .section("peer"),
            VpnField::new("peer-endpoint", "Peer endpoint", "vpn.example.com:51820")
                .required()
                .format(VpnFormat::HostPort)
                .section("peer"),
            VpnField::new("peer-allowed-ips", "Allowed IPs", "0.0.0.0/0, ::/0")
                .required()
                .format(VpnFormat::CidrList)
                .section("peer"),
            VpnField::new("peer-preshared-key", "Preshared key", "")
                .secret()
                .format(VpnFormat::Key)
                .section("peer"),
            VpnField::new("peer-keepalive", "Keepalive (seconds)", "25")
                .format(VpnFormat::Number)
                .section("peer"),
        ],
    }
}

/// The typed form for a known plugin, or none for one wayle has no form for.
fn fields_for(service: &str) -> Vec<VpnField> {
    match service {
        "org.freedesktop.NetworkManager.openconnect" => vec![
            VpnField::new("gateway", "Gateway", "vpn.example.com")
                .required()
                .format(VpnFormat::Host),
            VpnField::new("protocol", "Protocol", "gp")
                .required()
                .choices(openconnect_protocols()),
            VpnField::new("wayle-username", "Username", "alice"),
        ],
        "org.freedesktop.NetworkManager.openvpn" => vec![
            VpnField::new("remote", "Server", "vpn.example.com:1194")
                .required()
                .format(VpnFormat::HostPort),
            VpnField::new("username", "Username", "alice"),
            VpnField::new("password", "Password", "").secret(),
            VpnField::new("ca", "CA certificate", "/path/to/ca.crt"),
        ],
        _ => Vec::new(),
    }
}

/// The openconnect protocols, as picker choices.
///
/// A free-text box here meant typing a protocol name from memory and finding
/// out at connect time whether it was one openconnect knows and one wayle can
/// sign into.
fn openconnect_protocols() -> Vec<VpnChoice> {
    crate::vpn::openconnect::PROTOCOLS
        .iter()
        .map(|(value, label)| VpnChoice {
            value: String::from(*value),
            label: String::from(*label),
            native_sign_in: crate::vpn::openconnect::signs_in_natively(value),
        })
        .collect()
}

/// What to call a plugin in the type picker.
///
/// A plugin's `.name` file says what the packager called it, which on a normal
/// machine is literally `openconnect`, `openvpn`, `pptp`. Nobody setting up
/// GlobalProtect knows to pick "openconnect", so the ones wayle recognises get
/// named after what they connect to, and anything else keeps the plugin's own
/// wording — there is no list of every VPN plugin that exists.
fn display_label(service: &str, plugin_name: &str) -> String {
    let known = match service {
        "org.freedesktop.NetworkManager.openconnect" => {
            "OpenConnect (GlobalProtect, AnyConnect, Fortinet, Pulse)"
        }
        "org.freedesktop.NetworkManager.openvpn" => "OpenVPN",
        "org.freedesktop.NetworkManager.vpnc" => "Cisco (vpnc)",
        "org.freedesktop.NetworkManager.strongswan" => "IPsec/IKEv2 (strongSwan)",
        "org.freedesktop.NetworkManager.libreswan" => "IPsec/IKEv2 (libreswan)",
        "org.freedesktop.NetworkManager.l2tp" => "L2TP/IPsec",
        "org.freedesktop.NetworkManager.pptp" => "PPTP",
        "org.freedesktop.NetworkManager.sstp" => "SSTP",
        "org.freedesktop.NetworkManager.fortisslvpn" => "Fortinet SSL VPN",
        "org.freedesktop.NetworkManager.iodine" => "Iodine (DNS tunnel)",
        _ => return String::from(plugin_name),
    };
    String::from(known)
}

/// Reads every installed plugin's `.name` file as `(service, display name)`.
fn installed_plugins() -> Vec<(String, String)> {
    let mut found: HashMap<String, String> = HashMap::new();
    let mut order: Vec<String> = Vec::new();

    for directory in PLUGIN_DIRECTORIES {
        for (service, label) in read_directory(Path::new(directory)) {
            if !found.contains_key(&service) {
                order.push(service.clone());
            }
            found.insert(service, label);
        }
    }

    order
        .into_iter()
        .filter_map(|service| found.remove(&service).map(|label| (service, label)))
        .collect()
}

fn read_directory(directory: &Path) -> Vec<(String, String)> {
    let Ok(entries) = fs::read_dir(directory) else {
        return Vec::new();
    };
    let mut plugins: Vec<(String, String)> = entries
        .filter_map(Result::ok)
        .filter(|entry| entry.path().extension().is_some_and(|ext| ext == "name"))
        .filter_map(|entry| fs::read_to_string(entry.path()).ok())
        .filter_map(|contents| parse_name_file(&contents))
        .collect();
    // Directory order is arbitrary; the picker should not reshuffle between
    // runs.
    plugins.sort_by(|left, right| left.1.cmp(&right.1));
    plugins
}

/// Pulls `service` and `name` out of a plugin descriptor's
/// `[VPN Connection]` section.
fn parse_name_file(contents: &str) -> Option<(String, String)> {
    let mut in_section = false;
    let mut service = None;
    let mut name = None;

    for line in contents.lines() {
        let line = line.trim();
        if line.starts_with('[') {
            in_section = line.eq_ignore_ascii_case("[VPN Connection]");
            continue;
        }
        if !in_section || line.starts_with('#') {
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            continue;
        };
        match key.trim() {
            "service" => service = Some(String::from(value.trim())),
            "name" => name = Some(String::from(value.trim())),
            _ => {}
        }
    }

    let service = service.filter(|service| !service.is_empty())?;
    let label = name
        .filter(|name| !name.is_empty())
        .unwrap_or_else(|| service.clone());
    Some((service, label))
}

#[cfg(test)]
mod tests {
    use super::*;

    const OPENCONNECT: &str = "[VPN Connection]\n\
        name=openconnect\n\
        service=org.freedesktop.NetworkManager.openconnect\n\
        program=/usr/libexec/nm-openconnect-service\n\
        \n\
        [GNOME]\n\
        auth-dialog=/usr/libexec/nm-openconnect-auth-dialog\n";

    #[test]
    fn a_plugin_descriptor_yields_its_service_and_name() {
        assert_eq!(
            parse_name_file(OPENCONNECT),
            Some((
                String::from("org.freedesktop.NetworkManager.openconnect"),
                String::from("openconnect"),
            ))
        );
    }

    #[test]
    fn keys_outside_the_vpn_section_are_not_read() {
        // `auth-dialog` lives under [GNOME]; a naive scan would also pick up a
        // `service=` from another section and offer a plugin that is not one.
        let stray = "[GNOME]\nservice=org.example.NotAVpn\n";
        assert_eq!(parse_name_file(stray), None);
    }

    #[test]
    fn a_descriptor_with_no_service_is_not_a_plugin() {
        assert_eq!(parse_name_file("[VPN Connection]\nname=broken\n"), None);
        assert_eq!(parse_name_file(""), None);
    }

    #[test]
    fn a_descriptor_with_no_name_falls_back_to_its_service() {
        let anonymous = "[VPN Connection]\nservice=org.example.Vpn\n";
        assert_eq!(
            parse_name_file(anonymous),
            Some((
                String::from("org.example.Vpn"),
                String::from("org.example.Vpn")
            ))
        );
    }

    #[test]
    fn wireguard_is_offered_without_any_plugin_installed() {
        let kinds = available();
        assert_eq!(kinds.first().map(|kind| kind.id.as_str()), Some(WIREGUARD));
        assert!(kinds[0].is_typed());
    }

    #[test]
    fn wireguards_fields_are_grouped_into_the_three_things_they_are() {
        let wireguard = wireguard();
        let sections: Vec<&str> = wireguard
            .fields
            .iter()
            .map(|field| field.section.as_str())
            .collect();

        assert_eq!(
            sections,
            [
                "interface",
                "interface",
                "addressing",
                "addressing",
                "peer",
                "peer",
                "peer",
                "peer",
                "peer",
            ],
            "each group is contiguous, or the headings would repeat"
        );
    }

    #[test]
    fn a_kind_with_nothing_to_group_says_so_with_no_section() {
        let fields = fields_for("org.freedesktop.NetworkManager.openconnect");

        assert!(fields.iter().all(|field| field.section.is_empty()));
    }

    #[test]
    fn an_empty_value_is_never_a_format_error() {
        // Emptiness is `required`'s business, not the format's; reporting both
        // for the same box would name it twice.
        for format in [
            VpnFormat::Host,
            VpnFormat::HostPort,
            VpnFormat::IpList,
            VpnFormat::CidrList,
            VpnFormat::Key,
            VpnFormat::Number,
        ] {
            assert!(format.accepts(""), "{format:?} rejected an empty value");
            assert!(format.accepts("   "), "{format:?} rejected blank space");
        }
    }

    #[test]
    fn an_address_list_takes_addresses_with_or_without_a_prefix() {
        assert!(VpnFormat::CidrList.accepts("10.0.0.2/24"));
        assert!(VpnFormat::CidrList.accepts("10.0.0.2"));
        assert!(VpnFormat::CidrList.accepts("0.0.0.0/0, ::/0"));
        assert!(VpnFormat::CidrList.accepts("fd00::2/64"));
    }

    #[test]
    fn an_address_list_refuses_what_would_be_silently_dropped() {
        // Each of these used to reach the profile builder, which kept the
        // entries it could parse and threw the rest away without a word.
        assert!(
            !VpnFormat::CidrList.accepts("10.0.0.256/24"),
            "not an address"
        );
        assert!(
            !VpnFormat::CidrList.accepts("10.0.0.2/33"),
            "prefix past /32"
        );
        assert!(
            !VpnFormat::CidrList.accepts("fd00::2/129"),
            "prefix past /128"
        );
        assert!(
            !VpnFormat::CidrList.accepts("10.0.0.2, nonsense"),
            "one bad entry"
        );
        assert!(!VpnFormat::CidrList.accepts(","), "no entries at all");
    }

    #[test]
    fn a_dns_list_takes_only_bare_addresses() {
        assert!(VpnFormat::IpList.accepts("10.0.0.1, fd00::1"));
        assert!(
            !VpnFormat::IpList.accepts("10.0.0.1/24"),
            "a prefix is not a server"
        );
        assert!(
            !VpnFormat::IpList.accepts("dns.example.com"),
            "a name is not an address"
        );
    }

    #[test]
    fn an_endpoint_needs_a_port() {
        assert!(VpnFormat::HostPort.accepts("vpn.example.com:51820"));
        assert!(VpnFormat::HostPort.accepts("10.0.0.1:51820"));
        assert!(VpnFormat::HostPort.accepts("[fd00::1]:51820"));

        assert!(!VpnFormat::HostPort.accepts("vpn.example.com"), "no port");
        assert!(
            !VpnFormat::HostPort.accepts("vpn.example.com:0"),
            "port zero"
        );
        assert!(
            !VpnFormat::HostPort.accepts("vpn.example.com:99999"),
            "past a u16"
        );
        assert!(
            !VpnFormat::HostPort.accepts("fd00::1:51820"),
            "unbracketed v6 is ambiguous"
        );
    }

    #[test]
    fn a_gateway_is_a_host_and_not_a_url() {
        assert!(VpnFormat::Host.accepts("vpn.example.com"));
        assert!(VpnFormat::Host.accepts("10.0.0.1"));
        assert!(VpnFormat::Host.accepts("fd00::1"));

        assert!(
            !VpnFormat::Host.accepts("https://vpn.example.com"),
            "a scheme is not a host"
        );
        assert!(
            !VpnFormat::Host.accepts("vpn.example.com/portal"),
            "a path is not a host"
        );
        assert!(!VpnFormat::Host.accepts("vpn example com"), "spaces");
    }

    #[test]
    fn a_wireguard_key_is_thirty_two_bytes_of_base64() {
        assert!(VpnFormat::Key.accepts("6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk="));

        assert!(
            !VpnFormat::Key.accepts("6HeTLQTdIcJHFmwCNBjMFR/nGiEBDSQMCsBcgWJZ7Fk"),
            "no padding"
        );
        assert!(!VpnFormat::Key.accepts("not-a-key"), "too short");
        assert!(
            !VpnFormat::Key.accepts("6HeTLQTdIcJHFmwCNBjMFR nGiEBDSQMCsBcgWJZ7Fk="),
            "not base64"
        );
    }

    #[test]
    fn a_keepalive_is_a_number() {
        assert!(VpnFormat::Number.accepts("25"));
        assert!(!VpnFormat::Number.accepts("25s"));
        assert!(!VpnFormat::Number.accepts("-1"));
    }

    #[test]
    fn plain_text_fields_take_anything_that_is_there() {
        assert!(VpnFormat::Text.accepts("wg0"));
        assert!(VpnFormat::Text.accepts("anything at all / really"));
    }

    #[test]
    fn wireguards_fields_carry_the_formats_that_catch_a_typo() {
        let fields = wireguard().fields;
        let format = |key: &str| {
            fields
                .iter()
                .find(|field| field.key == key)
                .map(|field| field.format)
        };

        assert_eq!(format("private-key"), Some(VpnFormat::Key));
        assert_eq!(format("address"), Some(VpnFormat::CidrList));
        assert_eq!(format("dns"), Some(VpnFormat::IpList));
        assert_eq!(format("peer-endpoint"), Some(VpnFormat::HostPort));
        assert_eq!(format("peer-keepalive"), Some(VpnFormat::Number));
        assert_eq!(format("interface"), Some(VpnFormat::Text));
    }

    #[test]
    fn the_protocol_field_is_a_picker_over_what_openconnect_speaks() {
        let fields = fields_for("org.freedesktop.NetworkManager.openconnect");
        let protocol = fields
            .iter()
            .find(|field| field.key == "protocol")
            .expect("openconnect asks for a protocol");

        let values: Vec<&str> = protocol
            .choices
            .iter()
            .map(|choice| choice.value.as_str())
            .collect();
        assert_eq!(
            values,
            ["gp", "anyconnect", "nc", "pulse", "f5", "fortinet", "array"]
        );
    }

    #[test]
    fn a_protocol_wayle_cannot_sign_into_says_so_before_it_is_saved() {
        let choices = openconnect_protocols();
        let native = |value: &str| {
            choices
                .iter()
                .find(|choice| choice.value == value)
                .map(|choice| choice.native_sign_in)
        };

        assert_eq!(native("gp"), Some(true));
        assert_eq!(native("anyconnect"), Some(true));
        assert_eq!(native("fortinet"), Some(false));
        assert_eq!(native("pulse"), Some(false));
    }

    #[test]
    fn a_free_text_field_offers_no_choices() {
        let fields = fields_for("org.freedesktop.NetworkManager.openconnect");
        let gateway = fields
            .iter()
            .find(|field| field.key == "gateway")
            .expect("openconnect asks for a gateway");

        assert!(gateway.choices.is_empty());
    }

    #[test]
    fn a_recognised_plugin_is_named_after_what_it_connects_to() {
        assert_eq!(
            display_label("org.freedesktop.NetworkManager.openconnect", "openconnect"),
            "OpenConnect (GlobalProtect, AnyConnect, Fortinet, Pulse)"
        );
        assert_eq!(
            display_label("org.freedesktop.NetworkManager.openvpn", "openvpn"),
            "OpenVPN"
        );
    }

    #[test]
    fn an_unrecognised_plugin_keeps_its_own_wording() {
        assert_eq!(
            display_label("org.example.SomeVpn", "Some VPN"),
            "Some VPN",
            "there is no list of every VPN plugin that exists"
        );
    }

    #[test]
    fn an_unknown_plugin_gets_the_free_form_editor_rather_than_nothing() {
        assert!(fields_for("org.example.SomeVpn").is_empty());
        let kind = VpnKind {
            id: String::from("org.example.SomeVpn"),
            label: String::from("Some VPN"),
            fields: Vec::new(),
        };
        assert!(!kind.is_typed());
    }
}

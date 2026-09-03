//! What kinds of VPN this machine can actually run, and what each one needs
//! to be asked for.
//!
//! WireGuard is always available — NetworkManager carries it natively, the
//! kernel does the work, no plugin involved. Everything else needs a VPN
//! plugin installed, and NM advertises those as `.name` files: one INI per
//! plugin naming its D-Bus service. Reading them means the type picker offers
//! exactly what can work here, rather than a fixed list where two thirds of
//! the entries fail at connect time.

use std::{collections::HashMap, fs, path::Path};

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
        }
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
            VpnField::new("interface", "Interface", "wg0").required(),
            VpnField::new("private-key", "Private key", "")
                .required()
                .secret(),
            VpnField::new("address", "Addresses", "10.0.0.2/24").required(),
            VpnField::new("dns", "DNS", "10.0.0.1"),
            VpnField::new("peer-public-key", "Peer public key", "").required(),
            VpnField::new("peer-endpoint", "Peer endpoint", "vpn.example.com:51820").required(),
            VpnField::new("peer-allowed-ips", "Allowed IPs", "0.0.0.0/0, ::/0").required(),
            VpnField::new("peer-preshared-key", "Preshared key", "").secret(),
            VpnField::new("peer-keepalive", "Keepalive (seconds)", "25"),
        ],
    }
}

/// The typed form for a known plugin, or none for one wayle has no form for.
fn fields_for(service: &str) -> Vec<VpnField> {
    match service {
        "org.freedesktop.NetworkManager.openconnect" => vec![
            VpnField::new("gateway", "Gateway", "vpn.example.com").required(),
            VpnField::new("protocol", "Protocol", "gp")
                .required()
                .choices(openconnect_protocols()),
            VpnField::new("wayle-username", "Username", "alice"),
        ],
        "org.freedesktop.NetworkManager.openvpn" => vec![
            VpnField::new("remote", "Server", "vpn.example.com:1194").required(),
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

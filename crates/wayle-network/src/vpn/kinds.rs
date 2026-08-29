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
}

impl VpnField {
    fn new(key: &str, label: &str, placeholder: &str) -> Self {
        Self {
            key: String::from(key),
            label: String::from(label),
            secret: false,
            required: false,
            placeholder: String::from(placeholder),
        }
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
    for (service, label) in installed_plugins() {
        kinds.push(VpnKind {
            fields: fields_for(&service),
            label,
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
            VpnField::new("protocol", "Protocol", "gp").required(),
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

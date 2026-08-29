//! VPN entries for the network module (`[[modules.network.vpn]]`).
//!
//! A VPN is described by *where its state comes from*, not by which VPN
//! software it is. Three backends cover essentially every setup:
//!
//! - `networkmanager` — any profile NetworkManager owns (OpenVPN, WireGuard,
//!   OpenConnect, L2TP, PPTP, Fortinet, …). NM is the source of truth for both
//!   state and control, so only `id` is needed.
//! - `systemd` — a unit is the source of truth for *intent*, including the
//!   in-flight "connecting" window that no interface can report yet. Covers
//!   `openconnect-*.service`, `wg-quick@wg0`, `tailscaled`, and anything else
//!   started with `systemctl`.
//! - `link` — the tunnel interface is the source of truth for *reality*. A VPN
//!   process can be alive while its tunnel is down (dropped session, resume
//!   from suspend), which is exactly the case a unit alone cannot see.
//!
//! The `systemd` and `link` backends combine on one entry: set `backend =
//! "systemd"` and also give an `interface`, and the unit supplies `connecting`
//! while the link decides `connected`.

use serde::{Deserialize, Serialize};

/// Where a VPN entry's state comes from, and what a toggle acts on.
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(
    Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize, wayle_derive::EnumVariants,
)]
#[serde(rename_all = "kebab-case")]
pub enum VpnBackend {
    /// A NetworkManager VPN or WireGuard profile, matched by `id`.
    #[default]
    Networkmanager,
    /// A systemd unit, started and stopped over `org.freedesktop.systemd1`.
    Systemd,
    /// A network interface, watched over netlink. Read-only unless `connect` /
    /// `disconnect` commands are configured.
    Link,
}

/// Which systemd bus a unit lives on.
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(
    Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize, wayle_derive::EnumVariants,
)]
#[serde(rename_all = "kebab-case")]
pub enum VpnBus {
    /// System units (`systemctl start`), authorized by polkit.
    #[default]
    System,
    /// User units (`systemctl --user start`).
    User,
}

/// Whether the VPN indicator is part of the network module.
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(
    Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize, wayle_derive::EnumVariants,
)]
#[serde(rename_all = "kebab-case")]
pub enum VpnShow {
    /// Show the VPN state only once a VPN is configured or NM knows one.
    #[default]
    Auto,
    /// Always overlay the VPN state on the network icon.
    Always,
    /// Never show it; the dropdown still lists VPNs.
    Never,
}

/// One VPN in `[[modules.network.vpn]]`.
///
/// ## Examples
///
/// A NetworkManager profile needs nothing but its name:
///
/// ```toml
/// [[modules.network.vpn]]
/// id = "work-openvpn"
/// ```
///
/// openconnect as a systemd system unit, with the tunnel interface as the
/// ground truth for "connected" and a helper doing the interactive 2FA:
///
/// ```toml
/// [[modules.network.vpn]]
/// id = "konform"
/// label = "Konform"
/// backend = "systemd"
/// unit = "openconnect-konform.service"
/// bus = "system"
/// interface = "oc-konform"
/// connect = "vpn-konform-connect"
/// disconnect = "vpn-konform-disconnect"
/// connect-timeout = 90
/// ```
///
/// `wg-quick`, which needs no helper — `StartUnit` is the whole story:
///
/// ```toml
/// [[modules.network.vpn]]
/// id = "wg0"
/// backend = "systemd"
/// unit = "wg-quick@wg0.service"
/// interface = "wg0"
/// ```
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub struct VpnEntry {
    /// Stable identifier. For the `networkmanager` backend this is the
    /// connection's NM id (its name in `nmcli connection show`).
    pub id: String,

    /// Display name in the dropdown. Defaults to `id`.
    #[serde(default)]
    pub label: Option<String>,

    /// Where state comes from and what a toggle acts on.
    #[serde(default)]
    pub backend: VpnBackend,

    /// Unit name for the `systemd` backend, e.g. `wg-quick@wg0.service`.
    #[serde(default)]
    pub unit: Option<String>,

    /// Which bus `unit` lives on.
    #[serde(default)]
    pub bus: VpnBus,

    /// Tunnel interface. On the `link` backend this is the state source; on the
    /// `systemd` backend it is an additional cross-check, so a unit that is
    /// running with a dead tunnel reads as disconnected rather than connected.
    #[serde(default)]
    pub interface: Option<String>,

    /// Command run to connect, instead of the backend's own action. Needed
    /// when connecting is interactive — an openconnect helper doing 2FA and
    /// caching a session cookie, for instance.
    #[serde(default)]
    pub connect: Option<String>,

    /// Command run to disconnect, instead of the backend's own action.
    #[serde(default)]
    pub disconnect: Option<String>,

    /// Seconds to stay in `connecting` before falling back to disconnected.
    #[serde(default = "default_connect_timeout")]
    pub connect_timeout: u64,
}

const fn default_connect_timeout() -> u64 {
    60
}

impl VpnEntry {
    /// Display name: `label` if set, else the id.
    #[must_use]
    pub fn display_name(&self) -> &str {
        self.label.as_deref().unwrap_or(&self.id)
    }
}

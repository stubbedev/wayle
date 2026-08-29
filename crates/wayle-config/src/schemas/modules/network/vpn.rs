//! VPN presentation options for the network module.
//!
//! There is deliberately nothing here describing an individual VPN. Profiles
//! live in NetworkManager and are created, edited and removed from the network
//! widget, so the only thing left to configure is whether the bar icon reports
//! their state.

use serde::{Deserialize, Serialize};

/// Whether the VPN indicator is part of the network module.
#[cfg_attr(feature = "schema", derive(schemars::JsonSchema))]
#[derive(
    Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize, wayle_derive::EnumVariants,
)]
#[serde(rename_all = "kebab-case")]
pub enum VpnShow {
    /// Show the VPN state only once NetworkManager holds a VPN profile.
    #[default]
    Auto,
    /// Always overlay the VPN state on the network icon.
    Always,
    /// Never show it; the dropdown still lists VPNs.
    Never,
}

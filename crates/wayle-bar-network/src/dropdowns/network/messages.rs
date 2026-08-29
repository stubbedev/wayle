use std::sync::Arc;

use wayle_config::ConfigService;
use wayle_network::NetworkService;

use super::{
    available_networks::AvailableNetworksOutput, secret_form::SecretFormOutput,
    vpn_connections::VpnConnectionsOutput, vpn_form::VpnFormOutput,
};

pub struct NetworkDropdownInit {
    pub network: Arc<NetworkService>,
    pub config: Arc<ConfigService>,
}

#[derive(Debug)]
pub enum NetworkDropdownMsg {
    WifiToggled(bool),
    ScanRequested,
    AvailableNetworks(AvailableNetworksOutput),
    SecretForm(SecretFormOutput),
    VpnConnections(VpnConnectionsOutput),
    VpnForm(VpnFormOutput),
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum NetworkDropdownCmd {
    ScaleChanged(f32),
    WifiDeviceChanged,
    WifiEnabledChanged(bool),
    /// NetworkManager raised or withdrew a credential prompt.
    SecretRequestChanged,
    /// A saved profile's settings came back, ready to prefill the edit form.
    VpnSettingsLoaded {
        uuid: String,
        name: String,
        kind: String,
        values: std::collections::HashMap<String, String>,
    },
    /// NetworkManager refused a profile write.
    VpnWriteFailed(String),
    /// A profile write landed. The list updates itself off NM's own signal;
    /// this exists only because a command has to report something.
    VpnWriteSucceeded,
}

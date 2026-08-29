use std::sync::Arc;

use wayle_network::{NetworkService, vpn::VpnState};

pub struct VpnConnectionsInit {
    pub network: Arc<NetworkService>,
}

#[derive(Debug)]
pub enum VpnConnectionsInput {
    /// A row was clicked.
    Toggle(String),
    /// A row's edit button was clicked.
    Edit(String),
    /// The "Add VPN" row was clicked.
    Add,
}

#[derive(Debug)]
pub enum VpnConnectionsOutput {
    /// Open the form on this profile.
    Edit(String),
    /// Open the form empty.
    Add,
}

#[derive(Debug)]
pub enum VpnConnectionsCmd {
    /// NetworkManager gained or lost a VPN profile.
    EntriesChanged,
    /// One entry reported new state, or was renamed.
    Changed {
        uuid: String,
        name: String,
        state: VpnState,
        detail: Option<String>,
    },
}

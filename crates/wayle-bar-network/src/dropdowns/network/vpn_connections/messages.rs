use std::sync::Arc;

use wayle_network::{NetworkService, vpn::VpnState};

pub struct VpnConnectionsInit {
    pub network: Arc<NetworkService>,
}

#[derive(Debug)]
pub enum VpnConnectionsInput {
    /// A row was clicked.
    Toggle(String),
}

#[derive(Debug)]
pub enum VpnConnectionsCmd {
    /// One entry's backend reported new state.
    StateChanged {
        id: String,
        state: VpnState,
        detail: Option<String>,
    },
}

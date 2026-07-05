use std::sync::Arc;

use wayle_network::{NetworkService, types::states::NetworkStatus, wifi::Wifi, wired::Wired};

use crate::shell::bar::dropdowns::network::helpers;

pub struct ActiveConnectionsInit {
    pub network: Arc<NetworkService>,
}

pub struct WifiState {
    pub connected: bool,
    pub connecting: bool,
    pub ssid: Option<String>,
    pub strength: Option<u8>,
    pub icon: &'static str,
    pub frequency: Option<u32>,
    pub ip: Option<String>,
    pub hovered: bool,
}

impl WifiState {
    pub fn from_network(wifi: &Wifi) -> Self {
        let connectivity = wifi.connectivity.get();
        let strength = wifi.strength.get();

        Self {
            connected: connectivity == NetworkStatus::Connected,
            connecting: connectivity == NetworkStatus::Connecting,
            ssid: wifi.ssid.get(),
            strength,
            icon: helpers::signal_strength_icon(strength.unwrap_or(0)),
            frequency: wifi.frequency.get(),
            ip: wifi.ip4_address.get(),
            hovered: false,
        }
    }
}

impl Default for WifiState {
    fn default() -> Self {
        Self {
            connected: false,
            connecting: false,
            ssid: None,
            strength: None,
            icon: helpers::signal_strength_icon(0),
            frequency: None,
            ip: None,
            hovered: false,
        }
    }
}

#[derive(Default)]
pub struct WiredState {
    pub connected: bool,
    pub speed: u32,
    pub ip: Option<String>,
}

impl WiredState {
    pub fn from_network(wired: &Wired) -> Self {
        Self {
            connected: wired.connectivity.get() == NetworkStatus::Connected,
            speed: wired.device.speed.get(),
            ip: wired.ip4_address.get(),
        }
    }
}

#[derive(Default)]
pub struct ConnectionProgress {
    pub ssid: Option<String>,
    pub step: Option<String>,
    pub error: Option<String>,
}

#[derive(Debug)]
pub enum ActiveConnectionsInput {
    DisconnectWifi,
    ForgetWifi,
    DismissError,
    WifiCardHovered(bool),
    SetConnecting(String),
    SetConnectingStep(String),
    ClearConnecting,
    SetConnectionError(String),
    ClearConnectionError,
}

#[derive(Debug)]
#[allow(clippy::enum_variant_names)]
pub enum ActiveConnectionsCmd {
    WifiStateChanged {
        connectivity: NetworkStatus,
        ssid: Option<String>,
        strength: Option<u8>,
        frequency: Option<u32>,
        ip4_address: Option<String>,
    },
    WiredStateChanged {
        connectivity: NetworkStatus,
        speed: u32,
        ip4_address: Option<String>,
    },
    WifiDeviceChanged,
    WiredDeviceChanged,
}

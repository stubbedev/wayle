use wayle_config::schemas::modules::{NetworkConfig, VpnShow};
use wayle_network::{NetworkService, types::connectivity::ConnectionType, vpn::VpnState};

use super::{
    NetworkModule,
    helpers::{WifiContext, WiredContext, wifi_icon, wifi_label, wired_icon, wired_label},
};
use crate::i18n::t;

impl NetworkModule {
    /// Icon + label for the current network state.
    ///
    /// A VPN, when one is being shown, takes over the icon: what the user needs
    /// at a glance is whether their traffic is tunnelled, and the underlying
    /// link is still described by the label. Everything else falls through to
    /// the wifi/wired display unchanged.
    pub fn compute_display(config: &NetworkConfig, network: &NetworkService) -> (String, String) {
        let (link_icon, link_label) = Self::compute_link_display(config, network);
        match Self::vpn_icon(config, network) {
            Some(icon) => (icon, link_label),
            None => (link_icon, link_label),
        }
    }

    /// The VPN overlay icon, or `None` when the VPN state shouldn't be shown.
    ///
    /// `auto` (the default) shows nothing until a VPN is actually configured,
    /// so adding the keys changes nothing on a machine without one.
    fn vpn_icon(config: &NetworkConfig, network: &NetworkService) -> Option<String> {
        let show = config.vpn_show.get();
        if show == VpnShow::Never || (show == VpnShow::Auto && network.vpn.is_empty()) {
            return None;
        }
        Some(match network.vpn.aggregate.get() {
            VpnState::Connected => config.vpn_connected_icon.get().clone(),
            VpnState::Connecting => config.vpn_connecting_icon.get().clone(),
            VpnState::Disconnected | VpnState::Failed => config.vpn_disconnected_icon.get().clone(),
        })
    }

    fn compute_link_display(config: &NetworkConfig, network: &NetworkService) -> (String, String) {
        let primary = network.primary.get();

        match primary {
            ConnectionType::Wifi => {
                if let Some(wifi) = network.wifi.get() {
                    let ssid = wifi.ssid.get();
                    let ctx = WifiContext {
                        enabled: wifi.enabled.get(),
                        connectivity: wifi.connectivity.get(),
                        strength: wifi.strength.get(),
                        ssid: ssid.as_deref(),
                    };
                    (wifi_icon(config, &ctx), wifi_label(&ctx))
                } else {
                    (
                        config.wifi_offline_icon.get().clone(),
                        t!("bar-network-no-wifi"),
                    )
                }
            }
            ConnectionType::Wired => {
                if let Some(wired) = network.wired.get() {
                    let ctx = WiredContext {
                        connectivity: wired.connectivity.get(),
                    };
                    (wired_icon(config, &ctx), wired_label(&ctx))
                } else {
                    (
                        config.wired_disconnected_icon.get().clone(),
                        t!("bar-network-no-ethernet"),
                    )
                }
            }
            ConnectionType::None => (
                config.wifi_offline_icon.get().clone(),
                t!("bar-network-offline"),
            ),

            _ => {
                if let Some(wifi) = network.wifi.get() {
                    let ssid = wifi.ssid.get();
                    let ctx = WifiContext {
                        enabled: wifi.enabled.get(),
                        connectivity: wifi.connectivity.get(),
                        strength: wifi.strength.get(),
                        ssid: ssid.as_deref(),
                    };
                    (wifi_icon(config, &ctx), wifi_label(&ctx))
                } else if let Some(wired) = network.wired.get() {
                    let ctx = WiredContext {
                        connectivity: wired.connectivity.get(),
                    };
                    (wired_icon(config, &ctx), wired_label(&ctx))
                } else {
                    (
                        config.wifi_offline_icon.get().clone(),
                        t!("bar-network-offline"),
                    )
                }
            }
        }
    }
}

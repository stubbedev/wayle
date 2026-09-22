use futures::StreamExt;
use relm4::prelude::*;
use tracing::warn;
use wayle_network::core::access_point::{SecurityType, Ssid};

use crate::{
    i18n::t,
    shell::bar::dropdowns::network::{
        available_networks::{
            AvailableNetworks, ListState, SCAN_TIMEOUT,
            messages::{
                AvailableNetworksCmd, AvailableNetworksInput, AvailableNetworksOutput,
                SelectedNetwork,
            },
            network_item::{NetworkItemInit, NetworkItemOutput},
            watchers,
        },
        helpers,
        password_form::{PasswordFormInput, PasswordFormOutput},
    },
};

impl AvailableNetworks {
    pub fn clear_selection(&mut self) {
        self.selection = None;
    }

    pub fn handle_connection_failure(&mut self, message: String, sender: &ComponentSender<Self>) {
        self.state = ListState::Normal;
        self.clear_selection();
        let _ = sender.output(AvailableNetworksOutput::ConnectionFailed(message));
    }

    pub fn connect_to_selected(
        &mut self,
        password: Option<String>,
        sender: &ComponentSender<Self>,
    ) {
        let Some(selection) = &self.selection else {
            return;
        };

        let Some(wifi) = self.network.wifi.get() else {
            return;
        };

        let ap_path = selection.ap_path.clone();
        let ssid = selection.ssid.clone();
        self.state = ListState::Connecting;
        let _ = sender.output(AvailableNetworksOutput::Connecting(ssid));

        let token = self.connection_watcher.reset();
        watchers::spawn_connection_watcher(sender, &wifi, token);

        sender.command(move |out, _shutdown| async move {
            if let Err(err) = wifi.connect(ap_path, password).await {
                let _ = out.send(AvailableNetworksCmd::ConnectImmediateError(err.to_string()));
            }
        });
    }

    pub fn handle_wifi_availability(&mut self, available: bool, sender: &ComponentSender<Self>) {
        self.wifi_available = available;

        let token = self.ap_watcher.reset();

        if let Some(wifi) = self.network.wifi.get() {
            watchers::spawn(sender, &wifi, token);
        }

        if !available {
            let _ = self.connection_watcher.reset();
            let _ = self.scan_watcher.reset();
            // A different adapter, or none, sees different networks.
            self.ap_cache.clear();
            self.pending_ssid = None;

            if self.state == ListState::Scanning {
                let _ = sender.output(AvailableNetworksOutput::ScanComplete);
            }

            if self.state == ListState::Connecting {
                let _ = sender.output(AvailableNetworksOutput::ClearConnecting);
            }

            self.state = ListState::Normal;
            self.clear_selection();
        }

        self.rebuild_network_list(None);
    }

    pub fn handle_wifi_enabled(&mut self, enabled: bool, sender: &ComponentSender<Self>) {
        if enabled {
            self.rebuild_network_list(None);
            return;
        }

        let _ = self.connection_watcher.reset();
        let _ = self.scan_watcher.reset();

        self.ap_cache.clear();
        self.pending_ssid = None;

        self.network_list.guard().clear();

        if self.state == ListState::Scanning {
            let _ = sender.output(AvailableNetworksOutput::ScanComplete);
        }

        if self.state == ListState::Connecting {
            let _ = sender.output(AvailableNetworksOutput::ClearConnecting);
        }

        self.state = ListState::Normal;
        self.clear_selection();
    }

    /// Scans when the dropdown opens with nothing to show, so an empty list
    /// is never the first thing seen. A list that has entries is left alone:
    /// it is the cache, and the refresh button is there for a fresh one.
    pub fn scan_if_empty(&mut self, sender: &ComponentSender<Self>) {
        let enabled = self
            .network
            .wifi
            .get()
            .is_some_and(|wifi| wifi.enabled.get());

        if self.wifi_available
            && enabled
            && self.ap_cache.is_empty()
            && self.state == ListState::Normal
        {
            self.start_scan(sender);
        }
    }

    pub fn start_scan(&mut self, sender: &ComponentSender<Self>) {
        self.state = ListState::Scanning;

        let _ = sender.output(AvailableNetworksOutput::ScanStarted);

        let network = self.network.clone();
        let token = self.scan_watcher.reset();

        sender.command(move |out, shutdown| async move {
            let Some(wifi) = network.wifi.get() else {
                let _ = out.send(AvailableNetworksCmd::ScanComplete);
                return;
            };

            // NM bumps LastScan when a scan finishes, which is the only real
            // "done" signal: the access-point list changes as results trickle
            // in, long before the last of them has.
            let before = wifi.device.last_scan.get();
            let mut scans = wifi.device.last_scan.watch();

            // A refusal is usually NM saying a scan is already running; that
            // one still ends with a LastScan bump, so wait for it either way
            // and let the timeout cover a refusal that is not.
            if let Err(err) = wifi.device.request_scan().await {
                warn!(error = %err, "wifi scan request refused");
            }

            let finished = async {
                while let Some(at) = scans.next().await {
                    if at != before {
                        break;
                    }
                }
            };

            tokio::select! {
                () = shutdown.wait() => {}
                () = token.cancelled() => {}
                () = finished => {
                    let _ = out.send(AvailableNetworksCmd::ScanComplete);
                }
                () = tokio::time::sleep(SCAN_TIMEOUT) => {
                    let _ = out.send(AvailableNetworksCmd::ScanComplete);
                }
            }
        });
    }

    /// Connects to the network a stale click is waiting on, once a scan has
    /// brought it back with a live access point.
    pub fn connect_pending_if_found(&mut self, sender: &ComponentSender<Self>) {
        let Some(pending) = &self.pending_ssid else {
            return;
        };
        let Some(index) = self
            .ap_cache
            .iter()
            .position(|ap| !ap.stale && &ap.ssid == pending)
        else {
            return;
        };

        self.pending_ssid = None;
        let _ = self.scan_watcher.reset();
        self.state = ListState::Normal;
        let _ = sender.output(AvailableNetworksOutput::ScanComplete);
        let _ = sender.output(AvailableNetworksOutput::ClearConnecting);

        self.select_network(index, sender);
    }

    /// The scan for a stale click finished without the network turning up.
    pub fn finish_pending_search(&mut self, sender: &ComponentSender<Self>) {
        self.pending_ssid = None;
        let _ = sender.output(AvailableNetworksOutput::ScanComplete);
        self.handle_connection_failure(t!("dropdown-network-error-not-found"), sender);
    }

    pub fn rebuild_network_list(&mut self, connected_ssid: Option<&str>) {
        let raw_aps = self.network.wifi.get().map(|wifi| wifi.access_points.get());
        let live = match raw_aps {
            Some(aps) => {
                helpers::sorted_unique_access_points(&aps, connected_ssid, &self.known_ssids)
            }
            None => vec![],
        };

        // NM can drop the whole list for a moment mid-scan; folding that in
        // would flash every row stale until the results land.
        if live.is_empty() && !self.ap_cache.is_empty() && self.state == ListState::Scanning {
            return;
        }

        self.ap_cache =
            helpers::merge_with_cache(live, &self.ap_cache, connected_ssid, &self.known_ssids);

        let mut guard = self.network_list.guard();
        guard.clear();

        for snapshot in &self.ap_cache {
            guard.push_back(NetworkItemInit {
                snapshot: snapshot.clone(),
            });
        }
    }

    pub fn select_network(&mut self, index: usize, sender: &ComponentSender<Self>) {
        let Some(ap) = self.ap_cache.get(index) else {
            return;
        };

        // A click elsewhere supersedes a search still running for a stale row.
        self.pending_ssid = None;

        // Its access point is gone from NM, so there is nothing to connect to
        // yet: scan, and connect from `connect_pending_if_found` if it is back.
        if ap.stale {
            let ssid = ap.ssid.clone();
            self.pending_ssid = Some(ssid.clone());
            let _ = sender.output(AvailableNetworksOutput::Connecting(ssid));
            let _ = sender.output(AvailableNetworksOutput::ConnectionProgress(t!(
                "dropdown-network-step-searching"
            )));
            if self.state != ListState::Scanning {
                self.start_scan(sender);
            }
            return;
        }

        let security_label = translate_security_type(ap.security);
        let signal_icon = helpers::signal_strength_icon(ap.strength);

        self.selection = Some(SelectedNetwork {
            ap_path: ap.object_path.clone(),
            ssid: ap.ssid.clone(),
            security_label: security_label.clone(),
            signal_icon,
        });

        if helpers::requires_password(ap.security) && !ap.known {
            self.state = ListState::PasswordEntry;

            self.password_form.emit(PasswordFormInput::Show {
                ssid: ap.ssid.clone(),
                security_label,
                signal_icon,
                error_message: None,
            });
        } else {
            self.connect_to_selected(None, sender);
        }
    }

    pub fn handle_password_form(
        &mut self,
        form_output: PasswordFormOutput,
        sender: &ComponentSender<Self>,
    ) {
        match form_output {
            PasswordFormOutput::Connect { password } => {
                self.connect_to_selected(Some(password), sender);
            }
            PasswordFormOutput::Cancel => {
                self.state = ListState::Normal;
                self.clear_selection();
            }
        }
    }

    pub fn forget_network(&self, ssid: String, sender: &ComponentSender<Self>) {
        let network = self.network.clone();

        sender.oneshot_command(async move {
            let ssid = Ssid::new(ssid.into_bytes());
            network.settings.delete_connections_for_ssid(&ssid).await;

            AvailableNetworksCmd::AccessPointsChanged
        });
    }
}

pub fn translate_security_type(security: SecurityType) -> String {
    match security {
        SecurityType::None => t!("dropdown-network-security-open"),
        SecurityType::Wep => t!("dropdown-network-security-wep"),
        SecurityType::Wpa => t!("dropdown-network-security-wpa"),
        SecurityType::Wpa2 => t!("dropdown-network-security-wpa2"),
        SecurityType::Wpa3 => t!("dropdown-network-security-wpa3"),
        SecurityType::Enterprise => t!("dropdown-network-security-enterprise"),
    }
}

pub fn forward_network_item_output(item_output: NetworkItemOutput) -> AvailableNetworksInput {
    match item_output {
        NetworkItemOutput::Selected(index) => {
            AvailableNetworksInput::NetworkSelected(index.current_index())
        }

        NetworkItemOutput::ForgetRequested(ssid) => AvailableNetworksInput::ForgetNetwork(ssid),
    }
}

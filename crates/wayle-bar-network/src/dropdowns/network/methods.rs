use relm4::prelude::*;
use tracing::warn;
use wayle_network::vpn::profile;

use super::{
    NetworkDropdown, messages::NetworkDropdownCmd, secret_form::SecretFormInput,
    vpn_form::VpnFormOutput, watchers,
};

impl NetworkDropdown {
    pub fn reset_wifi_watchers(&mut self, sender: &ComponentSender<Self>) {
        let token = self.wifi_watcher.reset();
        watchers::spawn_wifi_watchers(sender, &self.network, token);
    }

    /// Mirrors NetworkManager's pending credential prompt into the form.
    ///
    /// Also called at init: a prompt raised while the dropdown was closed is
    /// still outstanding when it opens, and NM is still blocked on it.
    pub fn show_pending_secret_request(&self) {
        match self.network.secret_request.get() {
            Some(request) => self.secret_form.emit(SecretFormInput::Show {
                name: request.name,
                message: request.message,
                fields: request.fields,
            }),
            None => self.secret_form.emit(SecretFormInput::Hide),
        }
    }

    /// Reads a saved VPN profile so the edit form opens on what is actually
    /// stored, rather than on what the row happens to display.
    pub fn load_vpn_settings(&self, uuid: String, sender: &ComponentSender<Self>) {
        let network = self.network.clone();
        let name = network
            .vpn
            .get(&uuid)
            .map(|vpn| vpn.name.get())
            .unwrap_or_default();

        sender.oneshot_command(async move {
            match network.vpn.settings_of(&uuid).await {
                Ok(settings) => NetworkDropdownCmd::VpnSettingsLoaded {
                    kind: profile::kind_of(&settings).unwrap_or_default(),
                    values: profile::read_values(&settings),
                    uuid,
                    name,
                },
                Err(error) => {
                    warn!(%uuid, %error, "cannot read VPN profile");
                    NetworkDropdownCmd::VpnWriteFailed(error.to_string())
                }
            }
        });
    }

    /// Applies what the VPN form produced.
    ///
    /// Nothing tells the list to refresh: NetworkManager announces the change
    /// and the service's profile watcher rebuilds the rows on its own.
    pub fn apply_vpn_form(&self, output: VpnFormOutput, sender: &ComponentSender<Self>) {
        let network = self.network.clone();

        match output {
            VpnFormOutput::Cancel => {}
            VpnFormOutput::Delete(uuid) => {
                sender.oneshot_command(async move {
                    let failure = network.vpn.remove(&uuid).await.err();
                    report(failure, "cannot delete VPN profile")
                });
            }
            VpnFormOutput::Save {
                uuid,
                kind,
                name,
                values,
            } => {
                sender.oneshot_command(async move {
                    let failure = match uuid {
                        Some(uuid) => network.vpn.update(&uuid, &kind, &name, &values).await,
                        None => network.vpn.add(&kind, &name, &values).await,
                    }
                    .err();
                    report(failure, "cannot save VPN profile")
                });
            }
        }
    }

    pub fn toggle_wifi(&mut self, active: bool, sender: &ComponentSender<Self>) {
        self.wifi_enabled = active;

        let network = self.network.clone();

        sender.command(move |_out, _shutdown| async move {
            if let Some(wifi) = network.wifi.get()
                && let Err(err) = wifi.set_enabled(active).await
            {
                warn!(error = %err, "wifi toggle failed");
            }
        });
    }
}

/// Turns a write failure into something the form can show, and a success into
/// a no-op the command loop can still deliver.
fn report(failure: Option<wayle_network::Error>, context: &'static str) -> NetworkDropdownCmd {
    match failure {
        Some(error) => {
            warn!(%error, context);
            NetworkDropdownCmd::VpnWriteFailed(error.to_string())
        }
        None => NetworkDropdownCmd::VpnWriteSucceeded,
    }
}

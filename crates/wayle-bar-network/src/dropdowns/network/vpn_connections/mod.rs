//! The VPN section of the network dropdown: one row per configured VPN, with
//! its state and a click-to-toggle.
//!
//! Rows come from `[[modules.network.vpn]]`, so the section is invisible on a
//! machine with no VPN configured and needs no scanning, no discovery, and no
//! polling — every row's state is pushed by its backend's watcher.

mod messages;
mod vpn_item;

use std::sync::Arc;

use gtk::prelude::*;
use relm4::{factory::FactoryVecDeque, gtk, prelude::*};
use tracing::warn;
use wayle_network::{NetworkService, vpn::VpnState};
use wayle_widgets::{prelude::*, watch};

pub use self::messages::{VpnConnectionsInit, VpnConnectionsInput};
use self::{
    messages::VpnConnectionsCmd,
    vpn_item::{VpnItem, VpnItemInit, VpnItemInput, VpnItemOutput},
};
use crate::i18n::t;

pub struct VpnConnections {
    network: Arc<NetworkService>,
    rows: FactoryVecDeque<VpnItem>,
    has_entries: bool,
}

#[relm4::component(pub)]
impl Component for VpnConnections {
    type Init = VpnConnectionsInit;
    type Input = VpnConnectionsInput;
    type Output = ();
    type CommandOutput = VpnConnectionsCmd;

    view! {
        #[root]
        gtk::Box {
            set_orientation: gtk::Orientation::Vertical,
            #[watch]
            set_visible: model.has_entries,

            #[name = "section_label"]
            gtk::Label {
                add_css_class: "section-label",
                set_halign: gtk::Align::Start,
                set_label: &t!("dropdown-network-vpn"),
            },

            #[name = "vpn_list_card"]
            #[template]
            Card {
                add_css_class: "network-list",
                set_overflow: gtk::Overflow::Hidden,

                #[local_ref]
                vpn_list_widget -> gtk::Box {
                    set_orientation: gtk::Orientation::Vertical,
                },
            },
        }
    }

    fn init(
        init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let rows = FactoryVecDeque::builder()
            .launch(gtk::Box::default())
            .forward(sender.input_sender(), |output| match output {
                VpnItemOutput::ToggleRequested(id) => VpnConnectionsInput::Toggle(id),
            });

        let mut model = Self {
            network: init.network.clone(),
            rows,
            has_entries: !init.network.vpn.is_empty(),
        };

        {
            let mut guard = model.rows.guard();
            for vpn in &init.network.vpn.entries {
                guard.push_back(VpnItemInit {
                    id: vpn.entry.id.clone(),
                    name: vpn.name().to_owned(),
                    state: vpn.state.get(),
                    detail: vpn.detail.get(),
                });
            }
        }

        // One watcher per entry rather than one on the aggregate: the rows show
        // per-VPN state, which the fold deliberately throws away.
        for vpn in &init.network.vpn.entries {
            let id = vpn.entry.id.clone();
            let state = vpn.state.clone();
            let detail = vpn.detail.clone();
            watch!(sender, [state.watch()], |out| {
                let _ = out.send(VpnConnectionsCmd::StateChanged {
                    id: id.clone(),
                    state: state.get(),
                    detail: detail.get(),
                });
            });
        }

        let vpn_list_widget = model.rows.widget();
        let widgets = view_output!();

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            VpnConnectionsInput::Toggle(id) => {
                let Some(vpn) = self.network.vpn.get(&id).cloned() else {
                    return;
                };
                sender.oneshot_command(async move {
                    if let Err(error) = vpn.toggle().await {
                        warn!(vpn = %vpn.entry.id, %error, "vpn toggle failed");
                    }
                    // The backend watchers report the resulting state; this
                    // command exists only to run the toggle off the UI thread.
                    VpnConnectionsCmd::StateChanged {
                        id: vpn.entry.id.clone(),
                        state: vpn.state.get(),
                        detail: vpn.detail.get(),
                    }
                });
            }
        }
    }

    fn update_cmd(
        &mut self,
        msg: Self::CommandOutput,
        _sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            VpnConnectionsCmd::StateChanged { id, state, detail } => {
                self.apply_state(&id, state, detail);
            }
        }
    }
}

impl VpnConnections {
    /// Pushes new state into the row for `id`.
    ///
    /// Rows are addressed by config id rather than by index so a future
    /// reordering of the config list can't send a state to the wrong row.
    fn apply_state(&mut self, id: &str, state: VpnState, detail: Option<String>) {
        let Some(index) = self.rows.iter().position(|row: &VpnItem| row.id == id) else {
            return;
        };
        self.rows
            .send(index, VpnItemInput::StateChanged { state, detail });
    }
}

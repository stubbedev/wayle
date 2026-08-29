//! The VPN section of the network dropdown: one row per NetworkManager VPN
//! profile, with its state and a click-to-toggle.
//!
//! Rows come from NM, not from config, so the list is whatever NM holds and a
//! VPN added from this very dropdown appears the moment it is saved. Nothing
//! scans and nothing polls: every row's state is pushed by the service's
//! watchers.
//!
//! The section is always present, because the "Add VPN" row is how a machine
//! with no VPN gets its first one.

mod messages;
mod vpn_item;

use std::sync::Arc;

use gtk::prelude::*;
use relm4::{factory::FactoryVecDeque, gtk, prelude::*};
use tracing::warn;
use wayle_network::{NetworkService, vpn::VpnState};
use wayle_widgets::{WatcherToken, prelude::*, watch, watch_cancellable};

pub use self::messages::{VpnConnectionsInit, VpnConnectionsInput, VpnConnectionsOutput};
use self::{
    messages::VpnConnectionsCmd,
    vpn_item::{VpnItem, VpnItemInit, VpnItemInput, VpnItemOutput},
};
use crate::i18n::t;

pub struct VpnConnections {
    network: Arc<NetworkService>,
    rows: FactoryVecDeque<VpnItem>,
    /// Whether any VPN row sits above the "Add VPN" row, which is what decides
    /// if that row draws a separator or opens the card.
    has_entries: bool,
    /// Per-row state watchers. Reset wholesale when the profile list changes,
    /// because the entries they watch are rebuilt rather than mutated.
    row_watchers: WatcherToken,
}

#[relm4::component(pub)]
impl Component for VpnConnections {
    type Init = VpnConnectionsInit;
    type Input = VpnConnectionsInput;
    type Output = VpnConnectionsOutput;
    type CommandOutput = VpnConnectionsCmd;

    view! {
        #[root]
        gtk::Box {
            set_orientation: gtk::Orientation::Vertical,

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

                #[name = "add_row"]
                gtk::Box {
                    add_css_class: "network-item",
                    add_css_class: "vpn-item",
                    add_css_class: "vpn-add",
                    set_cursor_from_name: Some("pointer"),
                    #[watch]
                    set_class_active: ("separated", model.has_entries),

                    #[name = "add_icon"]
                    gtk::Image {
                        add_css_class: "network-item-signal",
                        set_icon_name: Some("ld-plus-symbolic"),
                        set_valign: gtk::Align::Center,
                    },

                    #[name = "add_label"]
                    gtk::Label {
                        add_css_class: "network-item-name",
                        set_halign: gtk::Align::Start,
                        set_hexpand: true,
                        set_label: &t!("dropdown-network-vpn-add"),
                    },
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
                VpnItemOutput::ToggleRequested(uuid) => VpnConnectionsInput::Toggle(uuid),
                VpnItemOutput::EditRequested(uuid) => VpnConnectionsInput::Edit(uuid),
            });

        let mut model = Self {
            network: init.network.clone(),
            rows,
            has_entries: false,
            row_watchers: WatcherToken::new(),
        };

        let entries = init.network.vpn.entries.clone();
        watch!(sender, [entries.watch()], |out| {
            let _ = out.send(VpnConnectionsCmd::EntriesChanged);
        });
        model.rebuild(&sender);

        let vpn_list_widget = model.rows.widget();
        let widgets = view_output!();

        let click = gtk::GestureClick::new();
        let add_sender = sender.input_sender().clone();
        click.connect_released(move |gesture, _, _, _| {
            gesture.set_state(gtk::EventSequenceState::Claimed);
            add_sender.emit(VpnConnectionsInput::Add);
        });
        widgets.add_row.add_controller(click);

        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            VpnConnectionsInput::Toggle(uuid) => {
                let Some(vpn) = self.network.vpn.get(&uuid) else {
                    return;
                };
                sender.oneshot_command(async move {
                    if let Err(error) = vpn.toggle().await {
                        warn!(vpn = %vpn.uuid, %error, "vpn toggle failed");
                    }
                    // The service's watchers report the resulting state; this
                    // command exists only to run the toggle off the UI thread.
                    VpnConnectionsCmd::Changed {
                        uuid: vpn.uuid.clone(),
                        name: vpn.name.get(),
                        state: vpn.state.get(),
                        detail: vpn.detail.get(),
                    }
                });
            }
            VpnConnectionsInput::Edit(uuid) => {
                let _ = sender.output(VpnConnectionsOutput::Edit(uuid));
            }
            VpnConnectionsInput::Add => {
                let _ = sender.output(VpnConnectionsOutput::Add);
            }
        }
    }

    fn update_cmd(
        &mut self,
        msg: Self::CommandOutput,
        sender: ComponentSender<Self>,
        _root: &Self::Root,
    ) {
        match msg {
            VpnConnectionsCmd::EntriesChanged => self.rebuild(&sender),
            VpnConnectionsCmd::Changed {
                uuid,
                name,
                state,
                detail,
            } => self.apply_change(&uuid, name, state, detail),
        }
    }
}

impl VpnConnections {
    /// Rebuilds every row from the service's current entry list, and
    /// re-subscribes to their states.
    fn rebuild(&mut self, sender: &ComponentSender<Self>) {
        let entries = self.network.vpn.entries.get();
        self.has_entries = !entries.is_empty();

        let mut guard = self.rows.guard();
        guard.clear();
        for vpn in &entries {
            guard.push_back(VpnItemInit {
                uuid: vpn.uuid.clone(),
                name: vpn.name.get(),
                state: vpn.state.get(),
                detail: vpn.detail.get(),
            });
        }
        drop(guard);

        // One watcher per entry rather than one on the aggregate: the rows show
        // per-VPN state, which the fold deliberately throws away. The name is
        // watched too, because a rename from the edit form never changes the
        // list it is in.
        let token = self.row_watchers.reset();
        for vpn in entries {
            let state = vpn.state.clone();
            let detail = vpn.detail.clone();
            let name = vpn.name.clone();
            let uuid = vpn.uuid.clone();
            watch_cancellable!(
                sender,
                token.clone(),
                [state.watch(), name.watch()],
                |out| {
                    let _ = out.send(VpnConnectionsCmd::Changed {
                        uuid: uuid.clone(),
                        name: name.get(),
                        state: state.get(),
                        detail: detail.get(),
                    });
                }
            );
        }
    }

    /// Pushes new state into the row for `uuid`.
    ///
    /// Rows are addressed by NM UUID rather than by index so a profile added or
    /// removed mid-flight can't send a state to the wrong row.
    fn apply_change(&mut self, uuid: &str, name: String, state: VpnState, detail: Option<String>) {
        let Some(index) = self.rows.iter().position(|row: &VpnItem| row.uuid == uuid) else {
            return;
        };
        self.rows.send(
            index,
            VpnItemInput::Changed {
                name,
                state,
                detail,
            },
        );
    }
}

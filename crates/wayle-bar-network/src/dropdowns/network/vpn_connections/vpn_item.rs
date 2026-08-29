use gtk::{pango, prelude::*};
use relm4::{gtk, prelude::*};
use wayle_network::vpn::VpnState;
use wayle_widgets::prelude::*;

use crate::i18n::t;

pub struct VpnItemInit {
    /// NM profile UUID, used to address the entry when the row is clicked.
    pub uuid: String,
    pub name: String,
    pub state: VpnState,
    /// Failure reason, shown instead of the state line when there is one.
    pub detail: Option<String>,
}

pub struct VpnItem {
    pub uuid: String,
    name: String,
    state: VpnState,
    detail: Option<String>,
}

#[derive(Debug)]
pub enum VpnItemInput {
    /// NetworkManager reported new state for this profile, or it was renamed.
    Changed {
        name: String,
        state: VpnState,
        detail: Option<String>,
    },
}

#[derive(Debug)]
pub enum VpnItemOutput {
    /// The row was clicked — connect if down, disconnect if up.
    ToggleRequested(String),
    /// The row's edit button was clicked.
    EditRequested(String),
}

/// Icon for a VPN state. Kept out of config: these are the dropdown's own row
/// glyphs, not the bar icon the user themes.
const fn state_icon(state: VpnState) -> &'static str {
    match state {
        VpnState::Connected => "ld-lock-symbolic",
        VpnState::Connecting => "ld-refresh-cw-symbolic",
        VpnState::Disconnected | VpnState::Failed => "ld-unplug-symbolic",
    }
}

fn state_label(state: VpnState) -> String {
    match state {
        VpnState::Connected => t!("dropdown-network-vpn-connected"),
        VpnState::Connecting => t!("dropdown-network-vpn-connecting"),
        VpnState::Disconnected => t!("dropdown-network-vpn-disconnected"),
        VpnState::Failed => t!("dropdown-network-vpn-failed"),
    }
}

#[relm4::factory(pub)]
impl FactoryComponent for VpnItem {
    type Init = VpnItemInit;
    type Input = VpnItemInput;
    type Output = VpnItemOutput;
    type CommandOutput = ();
    type ParentWidget = gtk::Box;

    view! {
        gtk::Box {
            add_css_class: "network-item",
            add_css_class: "vpn-item",
            set_cursor_from_name: Some("pointer"),

            #[name = "state_image"]
            gtk::Image {
                add_css_class: "network-item-signal",
                #[watch]
                set_icon_name: Some(state_icon(self.state)),
                #[watch]
                set_class_active: ("connected", self.state == VpnState::Connected),
                #[watch]
                set_class_active: ("connecting", self.state == VpnState::Connecting),
                set_valign: gtk::Align::Center,
            },

            #[name = "info_column"]
            gtk::Box {
                add_css_class: "network-item-info",
                set_orientation: gtk::Orientation::Vertical,
                set_hexpand: true,

                #[name = "name_label"]
                gtk::Label {
                    add_css_class: "network-item-name",
                    set_halign: gtk::Align::Start,
                    set_ellipsize: pango::EllipsizeMode::End,
                    // A VPN name can be arbitrarily long, and an ellipsized
                    // label still reports its full text as its natural width —
                    // which would widen the popover surface and destroy the
                    // popup. Cap it.
                    set_max_width_chars: 24,
                    #[watch]
                    set_label: &self.name,
                },

                #[name = "state_line"]
                gtk::Label {
                    add_css_class: "network-item-security",
                    set_halign: gtk::Align::Start,
                    set_ellipsize: pango::EllipsizeMode::End,
                    set_max_width_chars: 24,
                    #[watch]
                    set_label: &self.detail.clone().unwrap_or_else(|| state_label(self.state)),
                },
            },

            #[name = "edit_button"]
            #[template]
            GhostIconButton {
                add_css_class: "network-vpn-edit",
                set_icon_name: "ld-settings-symbolic",
                set_valign: gtk::Align::Center,
                set_tooltip_text: Some(&t!("dropdown-network-vpn-edit")),
                connect_clicked[sender, uuid = self.uuid.clone()] => move |_| {
                    sender.output_sender().emit(VpnItemOutput::EditRequested(uuid.clone()));
                },
            },
        }
    }

    fn init_model(init: Self::Init, _index: &Self::Index, _sender: FactorySender<Self>) -> Self {
        Self {
            uuid: init.uuid,
            name: init.name,
            state: init.state,
            detail: init.detail,
        }
    }

    fn update(&mut self, msg: VpnItemInput, _sender: FactorySender<Self>) {
        match msg {
            VpnItemInput::Changed {
                name,
                state,
                detail,
            } => {
                self.name = name;
                self.state = state;
                self.detail = detail;
            }
        }
    }

    fn init_widgets(
        &mut self,
        _index: &Self::Index,
        root: Self::Root,
        _returned_widget: &<Self::ParentWidget as relm4::factory::FactoryView>::ReturnedWidget,
        sender: FactorySender<Self>,
    ) -> Self::Widgets {
        let click = gtk::GestureClick::new();
        let uuid = self.uuid.clone();
        let click_sender = sender.output_sender().clone();
        click.connect_released(move |gesture, _, _, _| {
            gesture.set_state(gtk::EventSequenceState::Claimed);
            click_sender.emit(VpnItemOutput::ToggleRequested(uuid.clone()));
        });
        root.add_controller(click);

        let widgets = view_output!();
        widgets
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_state_has_its_own_icon_except_the_two_that_read_the_same() {
        assert_eq!(state_icon(VpnState::Connected), "ld-lock-symbolic");
        assert_eq!(state_icon(VpnState::Connecting), "ld-refresh-cw-symbolic");
        // A failed attempt reads as "not connected" at a glance; the reason
        // goes in the row's detail line, not the icon.
        assert_eq!(
            state_icon(VpnState::Failed),
            state_icon(VpnState::Disconnected)
        );
    }
}

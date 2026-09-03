//! The credential prompt NetworkManager blocks on.
//!
//! Unlike the wifi password form this one has no fixed shape: NM says which
//! keys it is missing and the fields are built from that, so a VPN asking for
//! a username, a password and a one-time code gets three boxes, and a plugin
//! asking for something wayle has never heard of still gets a usable one.

mod messages;

use std::collections::HashMap;

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_network::types::agent::SecretField;
use wayle_widgets::prelude::*;

pub use self::messages::{SecretFormInput, SecretFormOutput};
use crate::{dropdowns::network::helpers::attach_reveal_toggle, i18n::t};

pub struct SecretForm {
    name: String,
    message: Option<String>,
    visible: bool,
    /// Holds the generated rows. Rebuilt on every `Show`, because the fields
    /// are whatever NM asked for this time.
    container: gtk::Box,
    entries: Vec<(String, gtk::Entry)>,
}

/// The label for a key, translated where wayle knows the key and left as the
/// service's own English otherwise.
///
/// A plugin can ask for anything; an unknown key showing its own name beats
/// showing a blank or a wrong guess.
fn label_for(field: &SecretField) -> String {
    match field.key.as_str() {
        "password" | "passwd" | "psk" | "leap-password" | "secret" => {
            t!("dropdown-network-secret-password")
        }
        "user" | "username" | "user-name" => t!("dropdown-network-secret-username"),
        "pin" => t!("dropdown-network-secret-pin"),
        "usergroup" => t!("dropdown-network-secret-group"),
        "domain" => t!("dropdown-network-secret-domain"),
        "wep-key0" | "wep-key1" | "wep-key2" | "wep-key3" => {
            t!("dropdown-network-secret-wep-key")
        }
        "private-key" => t!("dropdown-network-secret-private-key"),
        "private-key-password" => t!("dropdown-network-secret-private-key-password"),
        _ => field.label.clone(),
    }
}

#[relm4::component(pub)]
impl SimpleComponent for SecretForm {
    type Init = ();
    type Input = SecretFormInput;
    type Output = SecretFormOutput;

    view! {
        #[root]
        gtk::Box {
            add_css_class: "network-password-card",
            add_css_class: "network-secret-card",
            set_orientation: gtk::Orientation::Vertical,
            #[watch]
            set_visible: model.visible,

            #[name = "header"]
            gtk::Box {
                add_css_class: "network-password-header",

                #[name = "header_icon_container"]
                gtk::Box {
                    add_css_class: "network-connection-icon",
                    add_css_class: "vpn",
                    set_hexpand: false,
                    #[name = "header_icon"]
                    gtk::Image {
                        set_icon_name: Some("ld-lock-symbolic"),
                        set_halign: gtk::Align::Center,
                        set_valign: gtk::Align::Center,
                    },
                },

                #[name = "header_info"]
                gtk::Box {
                    add_css_class: "network-password-info",
                    set_orientation: gtk::Orientation::Vertical,
                    set_hexpand: true,

                    #[name = "header_name"]
                    gtk::Label {
                        add_css_class: "network-password-name",
                        set_halign: gtk::Align::Start,
                        set_ellipsize: gtk::pango::EllipsizeMode::End,
                        set_max_width_chars: 24,
                        #[watch]
                        set_label: &model.name,
                    },

                    #[name = "header_subtitle"]
                    gtk::Label {
                        add_css_class: "network-password-security",
                        set_halign: gtk::Align::Start,
                        set_label: &t!("dropdown-network-secret-title"),
                    },
                },

                #[template]
                GhostIconButton {
                    add_css_class: "network-password-close",
                    set_icon_name: "ld-x-symbolic",
                    set_valign: gtk::Align::Start,
                    connect_clicked => SecretFormInput::CancelClicked,
                },
            },

            #[name = "challenge_label"]
            gtk::Label {
                add_css_class: "network-secret-message",
                set_halign: gtk::Align::Start,
                set_wrap: true,
                set_max_width_chars: 32,
                #[watch]
                set_visible: model.message.is_some(),
                #[watch]
                set_label: model.message.as_deref().unwrap_or(""),
            },

            model.container.clone() -> gtk::Box {
                add_css_class: "network-secret-fields",
                set_orientation: gtk::Orientation::Vertical,
            },

            #[name = "action_buttons"]
            gtk::Box {
                add_css_class: "network-password-actions",
                set_halign: gtk::Align::End,

                #[template]
                GhostButton {
                    add_css_class: "network-password-cancel",
                    connect_clicked => SecretFormInput::CancelClicked,
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-network-cancel"),
                    },
                },

                #[template]
                PrimaryButton {
                    add_css_class: "network-password-connect",
                    connect_clicked => SecretFormInput::SubmitClicked,
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-network-secret-submit"),
                    },
                },
            },
        }
    }

    fn init(
        _init: Self::Init,
        _root: Self::Root,
        _sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let model = Self {
            name: String::new(),
            message: None,
            visible: false,
            container: gtk::Box::default(),
            entries: Vec::new(),
        };

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>) {
        match msg {
            SecretFormInput::Show {
                name,
                message,
                fields,
            } => {
                self.name = name;
                self.message = message;
                self.rebuild(&fields, &sender);
                self.visible = true;
                if let Some((_, entry)) = self.entries.first() {
                    entry.grab_focus();
                }
            }
            SecretFormInput::Hide => {
                self.clear();
                self.visible = false;
            }
            SecretFormInput::SubmitClicked => {
                let values: HashMap<String, String> = self
                    .entries
                    .iter()
                    .map(|(key, entry)| (key.clone(), entry.text().to_string()))
                    .collect();
                let _ = sender.output(SecretFormOutput::Submit(values));
                self.clear();
                self.visible = false;
            }
            SecretFormInput::CancelClicked => {
                let _ = sender.output(SecretFormOutput::Cancel);
                self.clear();
                self.visible = false;
            }
        }
    }
}

impl SecretForm {
    /// Replaces the field rows with one per key NM asked for.
    fn rebuild(&mut self, fields: &[SecretField], sender: &ComponentSender<Self>) {
        self.clear();

        for field in fields {
            let label = gtk::Label::builder()
                .label(label_for(field))
                .halign(gtk::Align::Start)
                .css_classes(["network-secret-label"])
                .build();

            let entry = gtk::Entry::builder()
                .css_classes(["network-password-input"])
                .visibility(!field.secret)
                .focusable(true)
                .can_target(true)
                .build();
            if field.secret {
                entry.set_input_purpose(gtk::InputPurpose::Password);
                attach_reveal_toggle(&entry);
            }
            // Enter in any box submits the whole form: a 2FA code is a single
            // short field and reaching for the mouse to confirm it is friction
            // on a timer.
            let submit = sender.input_sender().clone();
            entry.connect_activate(move |_| submit.emit(SecretFormInput::SubmitClicked));

            self.container.append(&label);
            self.container.append(&entry);
            self.entries.push((field.key.clone(), entry));
        }
    }

    /// Drops the rows and, with them, the secrets typed into them.
    fn clear(&mut self) {
        for (_, entry) in self.entries.drain(..) {
            entry.set_text("");
        }
        while let Some(child) = self.container.first_child() {
            self.container.remove(&child);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn field(key: &str) -> SecretField {
        SecretField {
            key: String::from(key),
            label: String::from("Plugin's own wording"),
            secret: true,
        }
    }

    #[test]
    fn an_unknown_key_falls_back_to_the_services_own_label() {
        assert_eq!(label_for(&field("totp-token")), "Plugin's own wording");
    }
}

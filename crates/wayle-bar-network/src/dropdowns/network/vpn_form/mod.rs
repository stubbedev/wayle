//! Creating and editing a VPN, in the same place you join a wifi network.
//!
//! The form is built from whatever the picked kind asks for rather than being
//! hand-drawn per VPN type, which is what lets a plugin wayle has never heard
//! of still be configurable: it gets the free-form `key = value` editor
//! instead of a typed one, and lands in NetworkManager the same way.

mod messages;

use std::collections::HashMap;

use gtk::prelude::*;
use relm4::{gtk, prelude::*};
use wayle_network::vpn::kinds::{self, VpnField, VpnKind};
use wayle_widgets::prelude::*;

pub use self::messages::{VpnFormInput, VpnFormOutput};
use crate::i18n::{t, td};

pub struct VpnForm {
    kinds: Vec<VpnKind>,
    selected: usize,
    /// The profile being edited, or `None` when creating one.
    editing: Option<String>,
    visible: bool,
    error: Option<String>,
    name_entry: gtk::Entry,
    /// Holds the generated field rows, rebuilt whenever the kind changes.
    container: gtk::Box,
    entries: Vec<(String, gtk::Entry)>,
    /// The escape hatch for a kind with no typed form.
    raw: gtk::TextView,
    raw_visible: bool,
}

/// The label for a field: translated where wayle ships a string for the key,
/// and the kind's own English otherwise.
///
/// The fallback is what keeps an unknown plugin's vocabulary usable — there is
/// no list of every key every VPN plugin might want.
fn label_for(field: &VpnField) -> String {
    let id = format!("dropdown-network-vpn-field-{}", field.key);
    if crate::i18n::loader().has(&id) {
        td!(&id)
    } else {
        field.label.clone()
    }
}

/// Reads the free-form editor: one `key = value` per line, `#` for comments.
fn parse_raw(text: &str) -> HashMap<String, String> {
    text.lines()
        .map(str::trim)
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
        .filter_map(|line| line.split_once('='))
        .map(|(key, value)| (key.trim().to_owned(), value.trim().to_owned()))
        .filter(|(key, _)| !key.is_empty())
        .collect()
}

/// Renders values back into the free-form editor, in a stable order.
fn render_raw(values: &HashMap<String, String>) -> String {
    let mut lines: Vec<String> = values
        .iter()
        .map(|(key, value)| format!("{key} = {value}"))
        .collect();
    lines.sort();
    lines.join("\n")
}

/// The first required field left empty, if any.
fn missing_required(kind: &VpnKind, values: &HashMap<String, String>) -> Option<String> {
    kind.fields
        .iter()
        .find(|field| field.required && values.get(&field.key).is_none_or(|value| value.is_empty()))
        .map(label_for)
}

#[relm4::component(pub)]
impl SimpleComponent for VpnForm {
    type Init = ();
    type Input = VpnFormInput;
    type Output = VpnFormOutput;

    view! {
        #[root]
        gtk::Box {
            add_css_class: "network-password-card",
            add_css_class: "network-vpn-form",
            set_orientation: gtk::Orientation::Vertical,
            #[watch]
            set_visible: model.visible,

            #[name = "header"]
            gtk::Box {
                add_css_class: "network-password-header",

                #[name = "header_info"]
                gtk::Box {
                    add_css_class: "network-password-info",
                    set_orientation: gtk::Orientation::Vertical,
                    set_hexpand: true,

                    #[name = "header_title"]
                    gtk::Label {
                        add_css_class: "network-password-name",
                        set_halign: gtk::Align::Start,
                        set_label: &t!("dropdown-network-vpn-new"),
                    },
                },

                #[template]
                GhostIconButton {
                    add_css_class: "network-password-close",
                    set_icon_name: "ld-x-symbolic",
                    set_valign: gtk::Align::Start,
                    connect_clicked => VpnFormInput::CancelClicked,
                },
            },

            #[name = "name_label"]
            gtk::Label {
                add_css_class: "network-secret-label",
                set_halign: gtk::Align::Start,
                set_label: &t!("dropdown-network-vpn-name"),
            },

            model.name_entry.clone() -> gtk::Entry {
                add_css_class: "network-password-input",
            },

            #[name = "kind_label"]
            gtk::Label {
                add_css_class: "network-secret-label",
                set_halign: gtk::Align::Start,
                // Only when creating: changing an existing profile's type is
                // a different profile, not an edit.
                #[watch]
                set_visible: model.editing.is_none(),
                set_label: &t!("dropdown-network-vpn-type"),
            },

            #[name = "kind_picker"]
            gtk::DropDown {
                add_css_class: "network-vpn-kind",
                #[watch]
                set_visible: model.editing.is_none(),
                set_model: Some(&kind_list),
                connect_selected_notify[sender] => move |picker| {
                    sender.input(VpnFormInput::KindSelected(picker.selected()));
                },
            },

            model.container.clone() -> gtk::Box {
                add_css_class: "network-secret-fields",
                set_orientation: gtk::Orientation::Vertical,
            },

            #[name = "raw_hint"]
            gtk::Label {
                add_css_class: "network-secret-label",
                set_halign: gtk::Align::Start,
                set_wrap: true,
                #[watch]
                set_visible: model.raw_visible,
                set_label: &t!("dropdown-network-vpn-raw-hint"),
            },

            model.raw.clone() -> gtk::TextView {
                add_css_class: "network-vpn-raw",
                set_monospace: true,
                #[watch]
                set_visible: model.raw_visible,
            },

            #[name = "error_label"]
            gtk::Label {
                add_css_class: "network-password-error",
                set_halign: gtk::Align::Start,
                set_wrap: true,
                #[watch]
                set_visible: model.error.is_some(),
                #[watch]
                set_label: model.error.as_deref().unwrap_or(""),
            },

            #[name = "action_buttons"]
            gtk::Box {
                add_css_class: "network-password-actions",
                set_halign: gtk::Align::End,

                #[template]
                GhostButton {
                    add_css_class: "network-vpn-delete",
                    // Nothing to forget until there is a saved profile.
                    #[watch]
                    set_visible: model.editing.is_some(),
                    connect_clicked => VpnFormInput::DeleteClicked,
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-network-vpn-delete"),
                    },
                },

                #[template]
                GhostButton {
                    add_css_class: "network-password-cancel",
                    connect_clicked => VpnFormInput::CancelClicked,
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-network-cancel"),
                    },
                },

                #[template]
                PrimaryButton {
                    add_css_class: "network-password-connect",
                    connect_clicked => VpnFormInput::SaveClicked,
                    #[template_child]
                    label {
                        set_label: &t!("dropdown-network-vpn-save"),
                    },
                },
            },
        }
    }

    fn init(
        _init: Self::Init,
        _root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        let kinds = kinds::available();
        let kind_list = gtk::StringList::new(
            &kinds
                .iter()
                .map(|kind| kind.label.as_str())
                .collect::<Vec<_>>(),
        );

        let model = Self {
            kinds,
            selected: 0,
            editing: None,
            visible: false,
            error: None,
            name_entry: gtk::Entry::builder()
                .css_classes(["network-password-input"])
                .build(),
            container: gtk::Box::default(),
            entries: Vec::new(),
            raw: gtk::TextView::builder().monospace(true).build(),
            raw_visible: false,
        };

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>) {
        match msg {
            VpnFormInput::ShowNew => {
                self.editing = None;
                self.selected = 0;
                self.error = None;
                self.name_entry.set_text("");
                self.rebuild(&HashMap::new());
                self.visible = true;
                self.name_entry.grab_focus();
            }
            VpnFormInput::ShowEdit {
                uuid,
                name,
                kind,
                values,
            } => {
                self.editing = Some(uuid);
                self.selected = self
                    .kinds
                    .iter()
                    .position(|candidate| candidate.id == kind)
                    // A profile whose plugin has since been uninstalled still
                    // has to be openable, if only to be deleted.
                    .unwrap_or(0);
                self.error = None;
                self.name_entry.set_text(&name);
                self.rebuild(&values);
                self.visible = true;
            }
            VpnFormInput::KindSelected(index) => {
                let index = index as usize;
                if index < self.kinds.len() && index != self.selected {
                    self.selected = index;
                    self.error = None;
                    self.rebuild(&HashMap::new());
                }
            }
            VpnFormInput::SaveClicked => self.save(&sender),
            VpnFormInput::DeleteClicked => {
                if let Some(uuid) = self.editing.clone() {
                    let _ = sender.output(VpnFormOutput::Delete(uuid));
                    self.visible = false;
                }
            }
            VpnFormInput::CancelClicked => {
                let _ = sender.output(VpnFormOutput::Cancel);
                self.visible = false;
            }
            VpnFormInput::Failed(reason) => {
                // Stay open with the values intact: the point of the message
                // is that the user can correct what NM refused.
                self.error = Some(reason);
                self.visible = true;
            }
        }
    }
}

impl VpnForm {
    fn kind(&self) -> Option<&VpnKind> {
        self.kinds.get(self.selected)
    }

    /// Collects what the user typed, from whichever editor is in use.
    fn values(&self) -> HashMap<String, String> {
        if self.raw_visible {
            let buffer = self.raw.buffer();
            let text = buffer
                .text(&buffer.start_iter(), &buffer.end_iter(), false)
                .to_string();
            return parse_raw(&text);
        }
        self.entries
            .iter()
            .map(|(key, entry)| (key.clone(), entry.text().to_string()))
            .filter(|(_, value)| !value.is_empty())
            .collect()
    }

    fn save(&mut self, sender: &ComponentSender<Self>) {
        let Some(kind) = self.kind().cloned() else {
            return;
        };
        let name = self.name_entry.text().to_string();
        if name.trim().is_empty() {
            self.error = Some(t!("dropdown-network-vpn-name-required"));
            return;
        }

        let values = self.values();
        if let Some(field) = missing_required(&kind, &values) {
            self.error = Some(t!("dropdown-network-vpn-field-required", field = field));
            return;
        }

        self.error = None;
        let _ = sender.output(VpnFormOutput::Save {
            uuid: self.editing.clone(),
            kind: kind.id.clone(),
            name,
            values,
        });
        self.visible = false;
    }

    /// Redraws the field rows for the current kind, prefilled from `values`.
    fn rebuild(&mut self, values: &HashMap<String, String>) {
        self.entries.clear();
        while let Some(child) = self.container.first_child() {
            self.container.remove(&child);
        }

        let Some(kind) = self.kind().cloned() else {
            return;
        };

        // A kind with no typed form falls back to the raw editor rather than
        // to nothing, so no installed plugin is unreachable.
        self.raw_visible = !kind.is_typed();
        if self.raw_visible {
            self.raw.buffer().set_text(&render_raw(values));
            return;
        }

        for field in &kind.fields {
            let label = gtk::Label::builder()
                .label(label_for(field))
                .halign(gtk::Align::Start)
                .css_classes(["network-secret-label"])
                .build();

            let entry = gtk::Entry::builder()
                .css_classes(["network-password-input"])
                .placeholder_text(&field.placeholder)
                .visibility(!field.secret)
                .build();
            if field.secret {
                entry.set_input_purpose(gtk::InputPurpose::Password);
            }
            if let Some(value) = values.get(&field.key) {
                entry.set_text(value);
            }

            self.container.append(&label);
            self.container.append(&entry);
            self.entries.push((field.key.clone(), entry));
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use wayle_network::vpn::kinds::{VpnField, VpnKind};

    use super::*;

    fn a_kind(fields: Vec<VpnField>) -> VpnKind {
        VpnKind {
            id: String::from("test"),
            label: String::from("Test"),
            fields,
        }
    }

    fn required(key: &str) -> VpnField {
        VpnField {
            key: String::from(key),
            label: String::from("Gateway"),
            secret: false,
            required: true,
            placeholder: String::new(),
        }
    }

    #[test]
    fn the_raw_editor_reads_key_equals_value_lines() {
        let parsed = parse_raw("gateway = vpn.example.com\n  protocol=gp  \n");
        assert_eq!(
            parsed.get("gateway").map(String::as_str),
            Some("vpn.example.com")
        );
        assert_eq!(parsed.get("protocol").map(String::as_str), Some("gp"));
    }

    #[test]
    fn blank_lines_comments_and_junk_are_ignored_rather_than_sent_to_nm() {
        let parsed = parse_raw("\n# a comment = not a key\nno-equals-sign\n = orphan\ngateway=x\n");
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed.get("gateway").map(String::as_str), Some("x"));
    }

    #[test]
    fn a_value_containing_an_equals_sign_survives() {
        let parsed = parse_raw("token = abc=def==");
        assert_eq!(parsed.get("token").map(String::as_str), Some("abc=def=="));
    }

    #[test]
    fn the_raw_editor_round_trips_its_own_output() {
        let values: HashMap<String, String> = HashMap::from([
            (String::from("gateway"), String::from("vpn.example.com")),
            (String::from("protocol"), String::from("gp")),
        ]);
        assert_eq!(parse_raw(&render_raw(&values)), values);
    }

    #[test]
    fn a_missing_required_field_is_named_rather_than_saved_empty() {
        let kind = a_kind(vec![required("gateway")]);
        assert_eq!(
            missing_required(&kind, &HashMap::new()).as_deref(),
            Some("Gateway")
        );
        assert_eq!(
            missing_required(
                &kind,
                &HashMap::from([(String::from("gateway"), String::new())])
            )
            .as_deref(),
            Some("Gateway"),
            "an empty string is as missing as an absent key"
        );
    }

    #[test]
    fn a_complete_form_has_nothing_missing() {
        let kind = a_kind(vec![required("gateway")]);
        let values = HashMap::from([(String::from("gateway"), String::from("vpn.example.com"))]);
        assert_eq!(missing_required(&kind, &values), None);
        // Optional fields never block a save.
        assert_eq!(missing_required(&a_kind(Vec::new()), &HashMap::new()), None);
    }

    #[test]
    fn an_unknown_field_key_keeps_the_kinds_own_label() {
        let field = VpnField {
            key: String::from("some-plugin-key"),
            label: String::from("Plugin's own wording"),
            secret: false,
            required: false,
            placeholder: String::new(),
        };
        assert_eq!(label_for(&field), "Plugin's own wording");
    }
}

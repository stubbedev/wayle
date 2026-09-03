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
use crate::{
    dropdowns::network::helpers::attach_reveal_toggle,
    i18n::{t, td},
};

/// How tall the field list is allowed to grow before it scrolls instead.
///
/// WireGuard asks for nine fields, which is eighteen stacked widgets before
/// the header, the picker and the buttons — well past the popover's own
/// height. A popover cannot scroll and cannot be resized once mapped, so the
/// form has to bound itself.
const FIELDS_MAX_HEIGHT: i32 = 260;

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
    /// The profile the delete button is asking about, while the dialog is up.
    confirming_delete: bool,
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

/// The label as shown, with required fields marked.
///
/// `required` gates saving and was previously invisible until the save button
/// named the first field it tripped over; marking it up front is the
/// difference between filling the form once and filling it twice.
fn field_label(field: &VpnField) -> String {
    if field.required {
        format!("{} *", label_for(field))
    } else {
        label_for(field)
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

/// Every required field left empty, in the order they are drawn.
///
/// All of them, not the first: reporting them one save at a time is what makes
/// a nine-field form take nine attempts.
fn missing_required<'a>(kind: &'a VpnKind, values: &HashMap<String, String>) -> Vec<&'a VpnField> {
    kind.fields
        .iter()
        .filter(|field| {
            field.required && values.get(&field.key).is_none_or(|value| value.is_empty())
        })
        .collect()
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

            // Everything the kind decides lives in here, because how much of
            // it there is is up to the kind: WireGuard's nine fields do not
            // fit, and a popover can neither scroll nor be resized once it is
            // mapped, so the form bounds itself instead of running off.
            #[name = "fields_scroll"]
            gtk::ScrolledWindow {
                add_css_class: "network-vpn-form-scroll",
                set_hscrollbar_policy: gtk::PolicyType::Never,
                set_propagate_natural_height: true,
                set_max_content_height: FIELDS_MAX_HEIGHT,
                set_vexpand: true,

                gtk::Box {
                    set_orientation: gtk::Orientation::Vertical,

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
                        // Only when creating: changing an existing profile's
                        // type is a different profile, not an edit.
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
                },
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
            confirming_delete: false,
        };

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: Self::Input, sender: ComponentSender<Self>) {
        match msg {
            VpnFormInput::ShowNew => {
                self.confirming_delete = false;
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
                self.confirming_delete = false;
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
            VpnFormInput::DeleteClicked => self.confirm_delete(&sender),
            VpnFormInput::DeleteConfirmed => {
                self.confirming_delete = false;
                if let Some(uuid) = self.editing.clone() {
                    let _ = sender.output(VpnFormOutput::Delete(uuid));
                    self.visible = false;
                }
            }
            VpnFormInput::DeleteDismissed => self.confirming_delete = false,
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
        let missing = missing_required(&kind, &values);
        self.mark_missing(&missing);
        if let Some(first) = missing.first() {
            self.error = Some(t!(
                "dropdown-network-vpn-field-required",
                field = label_for(first)
            ));
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

    /// Puts the error mark on every required field left empty, and takes it
    /// off the ones that are now filled.
    fn mark_missing(&self, missing: &[&VpnField]) {
        for (key, entry) in &self.entries {
            if missing.iter().any(|field| &field.key == key) {
                entry.add_css_class("error");
            } else {
                entry.remove_css_class("error");
            }
        }
    }

    /// Asks before deleting.
    ///
    /// A VPN profile is not recoverable from anywhere else — a WireGuard
    /// private key exists in NetworkManager and nowhere the user can get it
    /// back from — so a stray click on a button sitting next to Cancel is real
    /// data loss.
    fn confirm_delete(&mut self, sender: &ComponentSender<Self>) {
        if self.editing.is_none() || self.confirming_delete {
            return;
        }

        let name = self.name_entry.text().to_string();
        let dialog = gtk::AlertDialog::builder()
            .modal(true)
            .message(t!("dropdown-network-vpn-delete-confirm", name = name))
            .detail(t!("dropdown-network-vpn-delete-confirm-detail"))
            .buttons([
                t!("dropdown-network-cancel"),
                t!("dropdown-network-vpn-delete"),
            ])
            .cancel_button(0)
            .default_button(0)
            .build();

        self.confirming_delete = true;
        let window = self.name_entry.root().and_downcast::<gtk::Window>();
        let sender = sender.clone();
        dialog.choose(
            window.as_ref(),
            gtk::gio::Cancellable::NONE,
            move |result| {
                sender.input(match result {
                    Ok(1) => VpnFormInput::DeleteConfirmed,
                    _ => VpnFormInput::DeleteDismissed,
                });
            },
        );
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
                .label(field_label(field))
                .halign(gtk::Align::Start)
                .css_classes(if field.required {
                    ["network-secret-label", "required"].as_slice()
                } else {
                    ["network-secret-label"].as_slice()
                })
                .build();

            let entry = gtk::Entry::builder()
                .css_classes(["network-password-input"])
                .placeholder_text(&field.placeholder)
                .visibility(!field.secret)
                .build();
            if field.secret {
                entry.set_input_purpose(gtk::InputPurpose::Password);
                attach_reveal_toggle(&entry);
            }
            if let Some(value) = values.get(&field.key) {
                entry.set_text(value);
            }
            // A field stops being wrong the moment it is edited; leaving the
            // mark on until the next save reads as "still broken".
            entry.connect_changed(|entry| entry.remove_css_class("error"));

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

    fn keys(fields: &[&VpnField]) -> Vec<String> {
        fields.iter().map(|field| field.key.clone()).collect()
    }

    #[test]
    fn a_missing_required_field_is_named_rather_than_saved_empty() {
        let kind = a_kind(vec![required("gateway")]);
        assert_eq!(keys(&missing_required(&kind, &HashMap::new())), ["gateway"]);
        assert_eq!(
            keys(&missing_required(
                &kind,
                &HashMap::from([(String::from("gateway"), String::new())])
            )),
            ["gateway"],
            "an empty string is as missing as an absent key"
        );
    }

    #[test]
    fn every_missing_field_is_reported_at_once_not_one_save_at_a_time() {
        let kind = a_kind(vec![
            required("gateway"),
            VpnField {
                key: String::from("protocol"),
                label: String::from("Protocol"),
                secret: false,
                required: false,
                placeholder: String::new(),
            },
            required("private-key"),
        ]);

        let missing = missing_required(&kind, &HashMap::new());

        assert_eq!(
            keys(&missing),
            ["gateway", "private-key"],
            "optional fields are not missing, and the order is the draw order"
        );
    }

    #[test]
    fn a_complete_form_has_nothing_missing() {
        let kind = a_kind(vec![required("gateway")]);
        let values = HashMap::from([(String::from("gateway"), String::from("vpn.example.com"))]);
        assert!(missing_required(&kind, &values).is_empty());
        // Optional fields never block a save.
        assert!(missing_required(&a_kind(Vec::new()), &HashMap::new()).is_empty());
    }

    #[test]
    fn a_required_field_is_marked_and_an_optional_one_is_not() {
        assert_eq!(field_label(&required("gateway")), "Gateway *");
        assert_eq!(
            field_label(&VpnField {
                key: String::from("protocol"),
                label: String::from("Protocol"),
                secret: false,
                required: false,
                placeholder: String::new(),
            }),
            "Protocol"
        );
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

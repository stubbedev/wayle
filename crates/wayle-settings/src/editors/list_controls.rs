//! Shared add / remove buttons for the list, map, and card-form editors so they
//! all speak one design language: a compact, bordered icon button — a plus for
//! "add a row", a trash for "remove a row" (tinted destructive). The icon alone
//! carries the meaning; the label-key becomes the tooltip. Callers wire
//! `connect_clicked` and own the row mutation.

use relm4::gtk::{self, prelude::*};
use wayle_i18n::t;

/// Builds a bordered icon-only button (tooltip from `label_key`) with the given
/// CSS marker class.
fn list_button(icon: &str, label_key: &str, css: &str) -> gtk::Button {
    let button = gtk::Button::builder()
        .icon_name(icon)
        .tooltip_text(t(label_key))
        .halign(gtk::Align::Start)
        .css_classes(["list-control-button", css])
        .build();
    button.set_cursor_from_name(Some("pointer"));
    button
}

/// Shared "add a row" button: a bordered plus icon. The tooltip names the action.
pub(crate) fn add_button(label_key: &str) -> gtk::Button {
    list_button("ld-plus-symbolic", label_key, "list-control-add")
}

/// Shared "remove a row" button: a bordered trash icon, tinted destructive.
pub(crate) fn remove_button(label_key: &str) -> gtk::Button {
    list_button("ld-trash-2-symbolic", label_key, "list-control-remove")
}

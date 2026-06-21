//! Shared building blocks for the "card form" list editors (mail accounts,
//! toast presets): a `Vec<T>` rendered as a stack of bordered cards. Each card
//! borrows the bar-layout card chrome — a header row carrying the card's
//! identity entry plus a delete button, with a divider below, and a body column
//! of label + control rows.
//!
//! Callers build their controls, hand the identity widget to [`card`], append
//! the remaining rows to the returned `body`, and wire `delete`.

use relm4::gtk::{self, prelude::*};
use wayle_i18n::t;

use super::list_controls::remove_button;

/// Text entry that fills its row, with placeholder text.
pub(crate) fn entry(text: &str, placeholder: &str) -> gtk::Entry {
    gtk::Entry::builder()
        .text(text)
        .placeholder_text(placeholder)
        .hexpand(true)
        .build()
}

/// One body row: a fixed-width label column on the left so every control's left
/// edge lines up, and the control filling the rest of the row.
pub(crate) fn field_row(label_key: &str, control: &gtk::Widget) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .css_classes(["card-form-row"])
        .build();
    let label = gtk::Label::builder()
        .label(t(label_key))
        .halign(gtk::Align::Start)
        .css_classes(["card-form-label"])
        .build();
    control.set_hexpand(true);
    row.append(&label);
    row.append(control);
    row
}

/// Widgets making up a card: the `root` to append to the list, the `body` to
/// append field rows to, the `delete` button to wire to row removal, and (for
/// [`card_titled`]) the header `title` label so the caller can retitle it live.
pub(crate) struct CardWidgets {
    pub(crate) root: gtk::Box,
    pub(crate) body: gtk::Box,
    pub(crate) delete: gtk::Button,
    pub(crate) title: Option<gtk::Label>,
}

/// Builds the card chrome: a bordered surface card split into a header (identity
/// label + entry + delete button) and an empty body column. `label_key` names
/// the identity field; `identity` is its control (filled by the caller).
pub(crate) fn card(label_key: &str, identity: &gtk::Widget) -> CardWidgets {
    let label = gtk::Label::builder()
        .label(t(label_key))
        .halign(gtk::Align::Start)
        .css_classes(["card-form-label"])
        .build();
    identity.set_hexpand(true);
    build_card(&[label.upcast_ref(), identity])
}

/// Card chrome with a plain text title in the header instead of an identity
/// field — for cards that have no single identity (e.g. threshold entries).
/// Header = title (left) + delete (right); body is empty for the caller to fill.
pub(crate) fn card_titled(title: &str) -> CardWidgets {
    let title = gtk::Label::builder()
        .label(title)
        .halign(gtk::Align::Start)
        .hexpand(true)
        .css_classes(["card-form-title"])
        .build();
    CardWidgets {
        title: Some(title.clone()),
        ..build_card(&[title.upcast_ref()])
    }
}

/// Assembles the bordered card from the given header lead widgets (everything
/// before the trailing delete button), plus an empty body column.
fn build_card(header_widgets: &[&gtk::Widget]) -> CardWidgets {
    let root = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .css_classes(["card-form-card"])
        .build();

    let header = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .css_classes(["card-form-header"])
        .build();
    for widget in header_widgets {
        header.append(*widget);
    }
    let delete = remove_button("settings-list-remove");
    delete.set_halign(gtk::Align::End);
    delete.set_valign(gtk::Align::Center);
    header.append(&delete);

    let body = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .css_classes(["card-form-body"])
        .build();

    root.append(&header);
    root.append(&body);

    CardWidgets {
        root,
        body,
        delete,
        title: None,
    }
}

//! Dropdown container widget templates.
#![allow(missing_docs)]

use gtk4::prelude::{Cast, CastNone, ListItemExt, OrientableExt, WidgetExt};
use relm4::{WidgetTemplate, gtk};

/// Builds a factory that renders a [`gtk::StringList`]'s entries as
/// end-ellipsizing, left-aligned labels.
///
/// `GtkDropDown` reuses its `factory` for *both* the open list and the closed
/// button. The default factory's label requests its full natural width, so the
/// button grows and shrinks as the selection changes. An ellipsizing label can
/// shrink instead, letting the button settle at its CSS `min-width` and keep a
/// stable width — long values truncate with `…` rather than resizing the
/// control. Apply with `dropdown.set_factory(Some(&ellipsizing_string_factory()))`.
pub fn ellipsizing_string_factory() -> gtk::SignalListItemFactory {
    let factory = gtk::SignalListItemFactory::new();
    factory.connect_setup(|_, item| {
        let Some(item) = item.downcast_ref::<gtk::ListItem>() else {
            return;
        };
        let label = gtk::Label::builder()
            .ellipsize(gtk::pango::EllipsizeMode::End)
            .xalign(0.0)
            .build();
        item.set_child(Some(&label));
    });
    factory.connect_bind(|_, item| {
        let Some(item) = item.downcast_ref::<gtk::ListItem>() else {
            return;
        };
        let Some(label) = item.child().and_downcast::<gtk::Label>() else {
            return;
        };
        let text = item
            .item()
            .and_downcast::<gtk::StringObject>()
            .map(|obj| obj.string())
            .unwrap_or_default();
        label.set_label(&text);
    });
    factory
}

/// Main dropdown container.
#[relm4::widget_template(pub)]
impl WidgetTemplate for Dropdown {
    view! {
        gtk::Box {
            set_css_classes: &["dropdown"],
            set_orientation: gtk::Orientation::Vertical,
            set_hexpand: false,
        }
    }
}

/// Header with icon, label, and actions container.
#[relm4::widget_template(pub)]
impl WidgetTemplate for DropdownHeader {
    view! {
        gtk::Box {
            set_css_classes: &["dropdown-header"],

            gtk::Box {
                set_css_classes: &["dropdown-title"],
                set_hexpand: true,

                #[name = "icon"]
                gtk::Image {
                    set_visible: false,
                },

                #[name = "label"]
                gtk::Label {},
            },

            #[name = "actions"]
            gtk::Box {
                set_css_classes: &["dropdown-actions"],
            },
        }
    }
}

/// Footer container.
#[relm4::widget_template(pub)]
impl WidgetTemplate for DropdownFooter {
    view! {
        gtk::Box {
            set_css_classes: &["dropdown-footer"],
            set_halign: gtk::Align::Fill,
            set_hexpand: true,
        }
    }
}

/// Content area container.
#[relm4::widget_template(pub)]
impl WidgetTemplate for DropdownContent {
    view! {
        gtk::Box {
            set_css_classes: &["dropdown-content"],
            set_orientation: gtk::Orientation::Vertical,
        }
    }
}

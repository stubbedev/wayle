//! Editor for `DropdownSize` config fields (the dropdowns page): a width and a
//! height control, each an inherit checkbox + a *Scale / Px* mode dropdown + a
//! spin. `Size` is either a scale multiplier or an absolute pixel length, so
//! each control captures both the mode and the value.

use std::rc::Rc;

use relm4::gtk::{self, glib::SignalHandlerId, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::{dropdowns::DropdownSize, styling::Size},
};
use wayle_i18n::t;

use crate::{
    editors::spawn_property_watcher, pages::spec::SettingRowInit, property_handle::PropertyHandle,
    row::RowBehavior,
};

const SCALE_INDEX: u32 = 0;
const PX_INDEX: u32 = 1;
const SPIN_FALLBACK: f64 = 1.0;

struct SizeControl {
    widget: gtk::Widget,
    refresh: Rc<dyn Fn()>,
}

/// Reconfigures the spin for the selected mode: pixels are whole numbers
/// (integer steps, no decimals), scale multipliers keep two decimals.
fn configure_spin_for_mode(spin: &gtk::SpinButton, is_px: bool) {
    if is_px {
        spin.set_digits(0);
        spin.set_increments(1.0, 10.0);
        spin.set_snap_to_ticks(true);
    } else {
        spin.set_digits(2);
        spin.set_increments(0.05, 1.0);
        spin.set_snap_to_ticks(false);
    }
}

/// Syncs the three sub-controls from a `Size` value (with signals blocked by
/// the caller where needed).
fn apply_size(
    value: Option<Size>,
    inherit: &gtk::CheckButton,
    mode: &gtk::DropDown,
    spin: &gtk::SpinButton,
) {
    match value {
        Some(Size::Scale(v)) => {
            inherit.set_active(false);
            mode.set_selected(SCALE_INDEX);
            configure_spin_for_mode(spin, false);
            spin.set_value(f64::from(v));
        }
        Some(Size::Px(v)) => {
            inherit.set_active(false);
            mode.set_selected(PX_INDEX);
            configure_spin_for_mode(spin, true);
            spin.set_value(f64::from(v.round()));
        }
        None => inherit.set_active(true),
    }
    let active = value.is_some();
    mode.set_sensitive(active);
    spin.set_sensitive(active);
}

/// Builds an inherit checkbox + Scale/Px dropdown + spin bound to get/set.
fn size_control(get: Rc<dyn Fn() -> Option<Size>>, set: Rc<dyn Fn(Option<Size>)>) -> SizeControl {
    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .valign(gtk::Align::Center)
        .build();

    let inherit = gtk::CheckButton::with_label(&t("settings-inherit"));
    inherit.set_valign(gtk::Align::Center);
    inherit.add_css_class("size-inherit");

    let modes = gtk::StringList::new(&[&t("settings-size-scale"), &t("settings-size-px")]);
    let mode = gtk::DropDown::new(Some(modes), gtk::Expression::NONE);

    let adjustment = gtk::Adjustment::new(SPIN_FALLBACK, 0.0, 10_000.0, 0.05, 1.0, 0.0);
    let spin = gtk::SpinButton::builder()
        .adjustment(&adjustment)
        .digits(2)
        .valign(gtk::Align::Center)
        .build();

    container.append(&inherit);
    container.append(&mode);
    container.append(&spin);

    apply_size(get(), &inherit, &mode, &spin);

    let commit = {
        let set = Rc::clone(&set);
        let inherit = inherit.clone();
        let mode = mode.clone();
        let spin = spin.clone();
        Rc::new(move || {
            if inherit.is_active() {
                set(None);
                return;
            }
            let raw = spin.value();
            let size = if mode.selected() == PX_INDEX {
                Size::px(raw.round() as f32)
            } else {
                Size::scale(raw as f32)
            };
            set(Some(size));
        })
    };

    let toggle_commit = Rc::clone(&commit);
    let toggle_mode = mode.clone();
    let toggle_spin = spin.clone();
    let inherit_handler = inherit.connect_toggled(move |inherit| {
        let active = !inherit.is_active();
        toggle_mode.set_sensitive(active);
        toggle_spin.set_sensitive(active);
        toggle_commit();
    });
    let mode_commit = Rc::clone(&commit);
    let mode_spin = spin.clone();
    let mode_handler = mode.connect_selected_notify(move |dd| {
        configure_spin_for_mode(&mode_spin, dd.selected() == PX_INDEX);
        mode_commit();
    });
    let spin_commit = Rc::clone(&commit);
    let spin_handler = spin.connect_value_changed(move |_| spin_commit());

    let handlers: Rc<(
        gtk::CheckButton,
        SignalHandlerId,
        gtk::DropDown,
        SignalHandlerId,
        gtk::SpinButton,
        SignalHandlerId,
    )> = Rc::new((
        inherit,
        inherit_handler,
        mode,
        mode_handler,
        spin,
        spin_handler,
    ));

    let refresh = {
        let handlers = Rc::clone(&handlers);
        Rc::new(move || {
            let (inherit, ih, mode, mh, spin, sh) = &*handlers;
            inherit.block_signal(ih);
            mode.block_signal(mh);
            spin.block_signal(sh);
            apply_size(get(), inherit, mode, spin);
            spin.unblock_signal(sh);
            mode.unblock_signal(mh);
            inherit.unblock_signal(ih);
        }) as Rc<dyn Fn()>
    };

    SizeControl {
        widget: container.upcast(),
        refresh,
    }
}

fn field_row(label_key: &str, control: &gtk::Widget) -> gtk::Box {
    let row = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .css_classes(["surface-animation-row"])
        .build();
    let label = gtk::Label::builder()
        .label(t(label_key))
        .halign(gtk::Align::Start)
        .hexpand(true)
        .css_classes(["surface-animation-label"])
        .build();
    row.append(&label);
    row.append(control);
    row
}

/// Full-width row that edits a `DropdownSize` (width + height) property.
pub(crate) fn dropdown_size(property: &ConfigProperty<DropdownSize>) -> SettingRowInit {
    let width = {
        let get = property.clone();
        let set = property.clone();
        size_control(
            Rc::new(move || get.get().width),
            Rc::new(move |value| {
                let mut size = set.get();
                size.width = value;
                set.set(size);
            }),
        )
    };
    let height = {
        let get = property.clone();
        let set = property.clone();
        size_control(
            Rc::new(move || get.get().height),
            Rc::new(move |value| {
                let mut size = set.get();
                size.height = value;
                set.set(size);
            }),
        )
    };

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .hexpand(true)
        .css_classes(["card", "surface-animation"])
        .build();
    container.append(&field_row("settings-dropdown-width", &width.widget));
    container.append(&field_row("settings-dropdown-height", &height.widget));

    let refreshers = [Rc::clone(&width.refresh), Rc::clone(&height.refresh)];
    let keep = (width, height);
    let watcher = spawn_property_watcher(property, move || {
        for refresh in &refreshers {
            refresh();
        }
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, display_size),
        control: container.upcast(),
        keepalive: Box::new((keep, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

fn display_size(size: &DropdownSize) -> String {
    let part = |value: Option<Size>| match value {
        Some(Size::Scale(v)) => format!("{v}x"),
        Some(Size::Px(v)) => format!("{v}px"),
        None => t("settings-inherit"),
    };
    format!("{} × {}", part(size.width), part(size.height))
}

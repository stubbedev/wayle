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
    editors::{optional::override_switch, spawn_property_watcher},
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
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
    inherit: &gtk::Switch,
    mode: &gtk::DropDown,
    spin: &gtk::SpinButton,
) {
    match value {
        Some(Size::Scale(v)) => {
            inherit.set_active(true);
            mode.set_selected(SCALE_INDEX);
            configure_spin_for_mode(spin, false);
            spin.set_value(f64::from(v));
        }
        Some(Size::Px(v)) => {
            inherit.set_active(true);
            mode.set_selected(PX_INDEX);
            configure_spin_for_mode(spin, true);
            spin.set_value(f64::from(v.round()));
        }
        None => inherit.set_active(false),
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

    let inherit = override_switch();

    let modes = gtk::StringList::new(&[&t("settings-size-scale"), &t("settings-size-px")]);
    let mode = gtk::DropDown::new(Some(modes), gtk::Expression::NONE);
    mode.set_cursor_from_name(Some("pointer"));

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
            if !inherit.is_active() {
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
    let inherit_handler = inherit.connect_active_notify(move |inherit| {
        let active = inherit.is_active();
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
        gtk::Switch,
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

/// The two rows that edit a [`DropdownSize`] (width + height), ready to drop
/// into a section's `items` like any other settings row. They build plain
/// [`SettingRowInit`]s (`full_width: false`) so they render and align exactly
/// like the general settings rows — one dropdown per section, two rows each.
pub(crate) fn dropdown_size_rows(property: &ConfigProperty<DropdownSize>) -> Vec<SettingRowInit> {
    vec![
        size_field_row(
            property,
            "settings-dropdown-width",
            |size| size.width,
            |size, value| size.width = value,
        ),
        size_field_row(
            property,
            "settings-dropdown-height",
            |size| size.height,
            |size, value| size.height = value,
        ),
    ]
}

/// One inherit + Scale/Px + spin row for a single `Size` field of the struct.
fn size_field_row(
    property: &ConfigProperty<DropdownSize>,
    i18n_key: &'static str,
    get_field: fn(&DropdownSize) -> Option<Size>,
    set_field: fn(&mut DropdownSize, Option<Size>),
) -> SettingRowInit {
    let get = property.clone();
    let set = property.clone();
    let control = size_control(
        Rc::new(move || get_field(&get.get())),
        Rc::new(move |value| {
            let mut size = set.get();
            set_field(&mut size, value);
            set.set(size);
        }),
    );

    let widget = control.widget.clone();
    let refresh = Rc::clone(&control.refresh);
    let watcher = spawn_property_watcher(property, move || {
        refresh();
        true
    });

    SettingRowInit {
        i18n_key: Some(i18n_key),
        handle: PropertyHandle::new(property, move |size| display_size(get_field(size)))
            .with_field_source(property, get_field)
            .with_field_reset(property, get_field, set_field),
        control: widget,
        keepalive: Box::new((control, watcher)),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

/// Display string for a single optional `Size` field (the value badge tooltip).
fn display_size(value: Option<Size>) -> String {
    match value {
        Some(Size::Scale(v)) => format!("{v}x"),
        Some(Size::Px(v)) => format!("{v}px"),
        None => t("settings-inherit"),
    }
}

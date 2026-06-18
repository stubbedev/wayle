//! Editor for a single `Size` config field: a *Scale / Px* mode dropdown plus a
//! spin. `Size` is either a scale multiplier or an absolute pixel length, so the
//! control captures both the mode and the value. In pixel mode the spin is an
//! integer (floor 0); in scale mode it keeps two decimals.

use std::rc::Rc;

use relm4::gtk::{self, glib::SignalHandlerId, prelude::*};
use wayle_config::{ConfigProperty, schemas::styling::Size};
use wayle_i18n::t;

use crate::{
    editors::spawn_property_watcher, pages::spec::SettingRowInit, property_handle::PropertyHandle,
    row::RowBehavior,
};

const SCALE_INDEX: u32 = 0;
const PX_INDEX: u32 = 1;

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

/// Row that edits a single `Size` property as a Scale/Px dropdown + spin.
pub(crate) fn size(property: &ConfigProperty<Size>) -> SettingRowInit {
    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .valign(gtk::Align::Center)
        .build();

    let modes = gtk::StringList::new(&[&t("settings-size-scale"), &t("settings-size-px")]);
    let mode = gtk::DropDown::new(Some(modes), gtk::Expression::NONE);

    let adjustment = gtk::Adjustment::new(1.0, 0.0, 10_000.0, 0.05, 1.0, 0.0);
    let spin = gtk::SpinButton::builder()
        .adjustment(&adjustment)
        .digits(2)
        .valign(gtk::Align::Center)
        .build();

    container.append(&mode);
    container.append(&spin);

    let apply = {
        let mode = mode.clone();
        let spin = spin.clone();
        Rc::new(move |value: Size| match value {
            Size::Scale(v) => {
                mode.set_selected(SCALE_INDEX);
                configure_spin_for_mode(&spin, false);
                spin.set_value(f64::from(v));
            }
            Size::Px(v) => {
                mode.set_selected(PX_INDEX);
                configure_spin_for_mode(&spin, true);
                spin.set_value(f64::from(v.round()));
            }
        })
    };
    apply(property.get());

    let commit = {
        let set = property.clone();
        let mode = mode.clone();
        let spin = spin.clone();
        Rc::new(move || {
            let raw = spin.value();
            let size = if mode.selected() == PX_INDEX {
                Size::px(raw.round() as f32)
            } else {
                Size::scale(raw as f32)
            };
            set.set(size);
        })
    };

    let mode_commit = Rc::clone(&commit);
    let mode_spin = spin.clone();
    let mode_handler = mode.connect_selected_notify(move |dd| {
        configure_spin_for_mode(&mode_spin, dd.selected() == PX_INDEX);
        mode_commit();
    });
    let spin_commit = Rc::clone(&commit);
    let spin_handler = spin.connect_value_changed(move |_| spin_commit());

    let handlers: Rc<(gtk::DropDown, SignalHandlerId, gtk::SpinButton, SignalHandlerId)> =
        Rc::new((mode, mode_handler, spin, spin_handler));

    let refresh_apply = Rc::clone(&apply);
    let get = property.clone();
    let handlers_for_refresh = Rc::clone(&handlers);
    let watcher = spawn_property_watcher(property, move || {
        let (mode, mh, spin, sh) = &*handlers_for_refresh;
        mode.block_signal(mh);
        spin.block_signal(sh);
        refresh_apply(get.get());
        spin.unblock_signal(sh);
        mode.unblock_signal(mh);
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |size| size.to_string()),
        control: container.upcast(),
        keepalive: Box::new((handlers, apply, watcher)),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

//! Editor for a single `Size` config field: a *Scale / Px* mode dropdown plus a
//! spin. `Size` is either a scale multiplier or an absolute pixel length, so the
//! control captures both the mode and the value. In pixel mode the spin is an
//! integer (floor 0); in scale mode it keeps two decimals.
//!
//! A scale value is a multiplier of the field's base size, expressed in rem
//! (`1rem = 16px`), so switching mode converts the value to its equivalent in
//! the other unit: at base `9.375`rem (=150px), scale `2.0` ⇄ `300`px. The base
//! is the same rem value the shell resolver uses, shared via constants in
//! `wayle-config`.

use std::rc::Rc;

use relm4::gtk::{self, glib::SignalHandlerId, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::styling::{REM_BASE_PX, Size},
};
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

/// Row that edits a single `Size` property whose scale base is 1rem.
pub(crate) fn size(property: &ConfigProperty<Size>) -> SettingRowInit {
    size_with_base(property, 1.0)
}

/// Row that edits a single `Size` property as a Scale/Px dropdown + spin, where
/// a scale value is a multiplier of `base_rem` (in rem). Switching mode converts
/// the value to the equivalent in the other unit (scale↔px) using that base.
pub(crate) fn size_with_base(property: &ConfigProperty<Size>, base_rem: f32) -> SettingRowInit {
    let base_px = base_rem * REM_BASE_PX;
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
        // Convert the current value to the equivalent in the newly-selected
        // unit so the displayed size stays the same across a mode switch.
        let to_px = dd.selected() == PX_INDEX;
        let current = mode_spin.value();
        let converted = if to_px {
            (current * f64::from(base_px)).round()
        } else if base_px > 0.0 {
            current / f64::from(base_px)
        } else {
            current
        };
        configure_spin_for_mode(&mode_spin, to_px);
        mode_spin.set_value(converted);
        mode_commit();
    });
    let spin_commit = Rc::clone(&commit);
    let spin_handler = spin.connect_value_changed(move |_| spin_commit());

    let handlers: Rc<(
        gtk::DropDown,
        SignalHandlerId,
        gtk::SpinButton,
        SignalHandlerId,
    )> = Rc::new((mode, mode_handler, spin, spin_handler));

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

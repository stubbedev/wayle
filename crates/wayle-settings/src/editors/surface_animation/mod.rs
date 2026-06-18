//! Field-by-field editor for a per-surface animation override
//! ([`SurfaceAnimation`]).
//!
//! [`ConfigProperty`] is atomic — it has no sub-property handles — so each
//! inner control reads the whole struct, replaces one field, and writes it
//! back. A single watcher on the property refreshes all four controls when the
//! value changes externally (reset, config reload).

use std::rc::Rc;

use relm4::gtk::{self, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::animations::{AnimationType, SurfaceAnimation},
};
use wayle_i18n::t;

use crate::{
    editors::{
        optional::{OptionalWidget, optional_enum_widget, optional_number_widget},
        spawn_property_watcher,
    },
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// Largest duration (ms) the override spin buttons accept.
const MAX_DURATION_MS: f64 = 100_000.0;
/// Spin value shown when a duration leaves the inherited state.
const DURATION_FALLBACK_MS: u32 = 200;

/// Builds a labeled sub-row (`label … control`) inside the composite.
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

/// Full-width row that edits a [`SurfaceAnimation`] as four inherit-aware
/// controls (enter/exit transitions + enter/exit durations).
pub(crate) fn surface_animation(
    property: &ConfigProperty<SurfaceAnimation>,
    i18n_key: &'static str,
) -> SettingRowInit {
    let enter = optional_enum_widget::<AnimationType>(
        field_getter(property, |surface| surface.enter),
        field_setter(property, |surface, value| surface.enter = value),
    );
    let exit = optional_enum_widget::<AnimationType>(
        field_getter(property, |surface| surface.exit),
        field_setter(property, |surface, value| surface.exit = value),
    );
    let enter_duration = optional_number_widget(
        field_getter(property, |surface| surface.enter_duration),
        field_setter(property, |surface, value| surface.enter_duration = value),
        0.0,
        MAX_DURATION_MS,
        10.0,
        DURATION_FALLBACK_MS,
    );
    let exit_duration = optional_number_widget(
        field_getter(property, |surface| surface.exit_duration),
        field_setter(property, |surface, value| surface.exit_duration = value),
        0.0,
        MAX_DURATION_MS,
        10.0,
        DURATION_FALLBACK_MS,
    );

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .hexpand(true)
        .css_classes(["surface-animation"])
        .build();
    container.append(&field_row("settings-animations-enter", &enter.widget));
    container.append(&field_row("settings-animations-exit", &exit.widget));
    container.append(&field_row(
        "settings-animations-enter-duration",
        &enter_duration.widget,
    ));
    container.append(&field_row(
        "settings-animations-exit-duration",
        &exit_duration.widget,
    ));

    let controls: Rc<[OptionalWidget]> = Rc::from([enter, exit, enter_duration, exit_duration]);
    let watcher_controls = Rc::clone(&controls);
    let watcher = spawn_property_watcher(property, move || {
        for control in watcher_controls.iter() {
            control.refresh();
        }
        true
    });

    SettingRowInit {
        i18n_key: Some(i18n_key),
        handle: PropertyHandle::new(property, display_surface),
        control: container.upcast(),
        keepalive: Box::new((controls, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

/// Read closure for one field of the surface struct.
fn field_getter<T: 'static>(
    property: &ConfigProperty<SurfaceAnimation>,
    project: fn(&SurfaceAnimation) -> Option<T>,
) -> Rc<dyn Fn() -> Option<T>> {
    let property = property.clone();
    Rc::new(move || project(&property.get()))
}

/// Read-modify-write closure for one field of the surface struct.
fn field_setter<T: 'static>(
    property: &ConfigProperty<SurfaceAnimation>,
    assign: fn(&mut SurfaceAnimation, Option<T>),
) -> Rc<dyn Fn(Option<T>)> {
    let property = property.clone();
    Rc::new(move |value| {
        let mut surface = property.get();
        assign(&mut surface, value);
        property.set(surface);
    })
}

/// Compact one-line summary of the override for the source badge tooltip.
fn display_surface(surface: &SurfaceAnimation) -> String {
    let parts = [
        surface.enter.map(|value| format!("enter={value:?}")),
        surface.exit.map(|value| format!("exit={value:?}")),
        surface
            .enter_duration
            .map(|value| format!("enter-dur={value}")),
        surface
            .exit_duration
            .map(|value| format!("exit-dur={value}")),
    ];
    let summary = parts.into_iter().flatten().collect::<Vec<_>>().join(", ");
    if summary.is_empty() {
        t("settings-inherit")
    } else {
        summary
    }
}

//! Per-surface animation override ([`SurfaceAnimation`]) rendered as four
//! standard setting rows — enter/exit transitions and enter/exit durations.
//!
//! [`ConfigProperty`] is atomic — it has no sub-property handles — so each row
//! reads the whole struct, replaces one field, and writes it back. Every row
//! shares the same backing [`ConfigProperty<SurfaceAnimation>`]; the source
//! badge / reset therefore reflect the whole override, and each control runs
//! its own watcher so external changes (reset, config reload) re-sync it.
//!
//! These build plain [`SettingRowInit`]s (`full_width: false`) so they render
//! and align exactly like the general (non-override) animation rows — one
//! surface per section, four rows each.

use std::rc::Rc;

use wayle_config::{
    ConfigProperty,
    schemas::animations::{AnimationType, SurfaceAnimation},
};
use wayle_i18n::t;

use crate::{
    editors::{
        optional::{optional_enum_widget, optional_number_widget},
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

/// The four rows that edit a [`SurfaceAnimation`] override, ready to drop into a
/// section's `items` like any other settings row.
pub(crate) fn surface_animation_rows(
    property: &ConfigProperty<SurfaceAnimation>,
) -> Vec<SettingRowInit> {
    vec![
        enum_field_row(
            property,
            "settings-animations-enter",
            |surface| surface.enter,
            |surface, value| surface.enter = value,
        ),
        enum_field_row(
            property,
            "settings-animations-exit",
            |surface| surface.exit,
            |surface, value| surface.exit = value,
        ),
        number_field_row(
            property,
            "settings-animations-enter-duration",
            |surface| surface.enter_duration,
            |surface, value| surface.enter_duration = value,
        ),
        number_field_row(
            property,
            "settings-animations-exit-duration",
            |surface| surface.exit_duration,
            |surface, value| surface.exit_duration = value,
        ),
    ]
}

/// One *Inherit + variants* dropdown row for a single enum field.
fn enum_field_row(
    property: &ConfigProperty<SurfaceAnimation>,
    i18n_key: &'static str,
    get_field: fn(&SurfaceAnimation) -> Option<AnimationType>,
    set_field: fn(&mut SurfaceAnimation, Option<AnimationType>),
) -> SettingRowInit {
    let widget = optional_enum_widget::<AnimationType>(
        field_getter(property, get_field),
        field_setter(property, set_field),
    );
    let control = widget.widget.clone();
    let refresh = Rc::new(widget);
    let watcher_refresh = Rc::clone(&refresh);
    let watcher = spawn_property_watcher(property, move || {
        watcher_refresh.refresh();
        true
    });

    SettingRowInit {
        i18n_key: Some(i18n_key),
        handle: PropertyHandle::new(property, move |surface| display_enum(get_field(surface)))
            .with_field_source(property, get_field),
        control,
        keepalive: Box::new((refresh, watcher)),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

/// One *Inherit* checkbox + spin row for a single duration field.
fn number_field_row(
    property: &ConfigProperty<SurfaceAnimation>,
    i18n_key: &'static str,
    get_field: fn(&SurfaceAnimation) -> Option<u32>,
    set_field: fn(&mut SurfaceAnimation, Option<u32>),
) -> SettingRowInit {
    let widget = optional_number_widget(
        field_getter(property, get_field),
        field_setter(property, set_field),
        0.0,
        MAX_DURATION_MS,
        10.0,
        DURATION_FALLBACK_MS,
    );
    let control = widget.widget.clone();
    let refresh = Rc::new(widget);
    let watcher_refresh = Rc::clone(&refresh);
    let watcher = spawn_property_watcher(property, move || {
        watcher_refresh.refresh();
        true
    });

    SettingRowInit {
        i18n_key: Some(i18n_key),
        handle: PropertyHandle::new(property, move |surface| {
            display_duration(get_field(surface))
        })
        .with_field_source(property, get_field),
        control,
        keepalive: Box::new((refresh, watcher)),
        full_width: false,
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

/// Display string for an optional enum field (the config-value badge tooltip).
fn display_enum(value: Option<AnimationType>) -> String {
    match value {
        Some(value) => serde_json::to_string(&value)
            .unwrap_or_default()
            .trim_matches('"')
            .to_owned(),
        None => t("settings-inherit"),
    }
}

/// Display string for an optional duration field.
fn display_duration(value: Option<u32>) -> String {
    match value {
        Some(value) => value.to_string(),
        None => t("settings-inherit"),
    }
}

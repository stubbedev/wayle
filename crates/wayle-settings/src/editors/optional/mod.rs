//! "Inherit"-aware controls for `Option<_>` config fields.
//!
//! A `None` value means "inherit / use the fallback". Enum fields render as a
//! dropdown whose first entry is *Inherit*; numeric/color fields render as an
//! override switch ([`override_switch`]) next to a control that is disabled
//! while inherited — the switch is **on** only when a custom value is set.
//!
//! The widget builders ([`optional_enum_widget`], [`optional_number_widget`])
//! are generic over `get`/`set` closures so they can drive either a whole
//! [`ConfigProperty`] (the row builders below) or a single field of a nested
//! struct (see the `surface_animation` editor) via read-modify-write.

use std::rc::Rc;

use relm4::{
    Component, ComponentController,
    gtk::{self, glib::SignalHandlerId, prelude::*},
};
use serde::{
    Deserialize,
    de::value::{Error as SerdeValueError, StrDeserializer},
};
use wayle_config::{ConfigProperty, EnumVariant, EnumVariants, schemas::styling::ColorValue};
use wayle_i18n::t;
use wayle_widgets::prelude::ellipsizing_string_factory;

use crate::{
    editors::{color_value::ColorValueControl, spawn_property_watcher},
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::RowBehavior,
};

/// A built control plus a `refresh` hook that re-syncs it from the backing
/// value (called when the property changes via reset, config reload, etc.).
pub(crate) struct OptionalWidget {
    /// The root GTK widget to mount in a row or composite.
    pub(crate) widget: gtk::Widget,
    refresh: Rc<dyn Fn()>,
}

impl OptionalWidget {
    /// Re-reads the backing value and updates the control without emitting a
    /// change signal.
    pub(crate) fn refresh(&self) {
        (self.refresh)();
    }
}

/// A compact "override the default" switch shared by every optional editor: a
/// lone switch (no label — the value controls beside it light up when it's on,
/// and the tooltip names it). Switch **on** means the field carries a custom
/// value; **off** means it inherits (value `None`). So the calm default state is
/// every switch off, and flipping one on enables its controls.
pub(crate) fn override_switch() -> gtk::Switch {
    let switch = gtk::Switch::builder()
        .valign(gtk::Align::Center)
        .css_classes(["inherit-switch"])
        .tooltip_text(t("settings-override"))
        .build();
    switch.set_cursor_from_name(Some("pointer"));
    switch
}

fn variant_label(variant: &EnumVariant) -> String {
    let resolved = t(variant.fluent_key);
    if resolved == variant.fluent_key {
        variant.value.to_owned()
    } else {
        resolved
    }
}

fn enum_from_value<E: for<'de> Deserialize<'de>>(value: &str) -> Option<E> {
    let de: StrDeserializer<'_, SerdeValueError> = StrDeserializer::new(value);
    E::deserialize(de).ok()
}

/// Dropdown index for the current value: `0` is *Inherit*, otherwise the
/// variant's position offset by one.
fn enum_index<E: EnumVariants + PartialEq + for<'de> Deserialize<'de>>(current: &Option<E>) -> u32 {
    match current {
        None => 0,
        Some(value) => E::variants()
            .iter()
            .position(|variant| enum_from_value::<E>(variant.value).as_ref() == Some(value))
            .map_or(0, |index| index as u32 + 1),
    }
}

/// Builds an *Inherit + variants* dropdown bound to caller-supplied get/set.
pub(crate) fn optional_enum_widget<E>(
    get: Rc<dyn Fn() -> Option<E>>,
    set: Rc<dyn Fn(Option<E>)>,
) -> OptionalWidget
where
    E: EnumVariants + Clone + PartialEq + for<'de> Deserialize<'de> + 'static,
{
    let mut labels = vec![t("settings-inherit")];
    labels.extend(E::variants().iter().map(variant_label));

    let string_list = gtk::StringList::new(&labels.iter().map(String::as_str).collect::<Vec<_>>());
    let dropdown = gtk::DropDown::new(Some(string_list), gtk::Expression::NONE);
    dropdown.set_factory(Some(&ellipsizing_string_factory()));
    dropdown.set_cursor_from_name(Some("pointer"));
    dropdown.set_selected(enum_index(&get()));

    if let Some(popover) = dropdown
        .last_child()
        .and_then(|child| child.downcast::<gtk::Popover>().ok())
    {
        popover.set_halign(gtk::Align::Center);
    }

    let on_change_set = Rc::clone(&set);
    let handler: SignalHandlerId = dropdown.connect_selected_notify(move |dropdown| {
        let selected = dropdown.selected();
        if selected == 0 {
            on_change_set(None);
        } else if let Some(value) = E::variants()
            .get(selected as usize - 1)
            .and_then(|variant| enum_from_value::<E>(variant.value))
        {
            on_change_set(Some(value));
        }
    });
    let handler = Rc::new(handler);

    let refresh_dropdown = dropdown.clone();
    let refresh_handler = Rc::clone(&handler);
    let refresh: Rc<dyn Fn()> = Rc::new(move || {
        let index = enum_index(&get());
        refresh_dropdown.block_signal(&refresh_handler);
        refresh_dropdown.set_selected(index);
        refresh_dropdown.unblock_signal(&refresh_handler);
    });

    OptionalWidget {
        widget: dropdown.upcast(),
        refresh,
    }
}

/// Builds an *Inherit* checkbox + spin button bound to caller-supplied get/set.
/// `fallback` is the value the spin starts at when switching out of inherit.
pub(crate) fn optional_number_widget(
    get: Rc<dyn Fn() -> Option<u32>>,
    set: Rc<dyn Fn(Option<u32>)>,
    min: f64,
    max: f64,
    step: f64,
    fallback: u32,
) -> OptionalWidget {
    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .valign(gtk::Align::Center)
        .build();

    let inherit = override_switch();

    let adjustment = gtk::Adjustment::new(f64::from(fallback), min, max, step, step, 0.0);
    let spin = gtk::SpinButton::builder()
        .adjustment(&adjustment)
        .digits(0)
        .valign(gtk::Align::Center)
        .build();

    let initial = get();
    inherit.set_active(initial.is_some());
    spin.set_sensitive(initial.is_some());
    if let Some(value) = initial {
        spin.set_value(f64::from(value));
    }

    container.append(&inherit);
    container.append(&spin);

    // Switch: turning override on commits the current spin value so the field
    // leaves the inherited state immediately; turning it off clears the value.
    let toggle_set = Rc::clone(&set);
    let toggle_spin = spin.clone();
    let inherit_handler = inherit.connect_active_notify(move |inherit| {
        let active = inherit.is_active();
        toggle_spin.set_sensitive(active);
        if active {
            toggle_set(Some(toggle_spin.value() as u32));
        } else {
            toggle_set(None);
        }
    });
    let inherit_handler = Rc::new(inherit_handler);

    // Spin: only writes while overriding.
    let spin_set = Rc::clone(&set);
    let spin_inherit = inherit.clone();
    let spin_handler = spin.connect_value_changed(move |spin| {
        if spin_inherit.is_active() {
            spin_set(Some(spin.value() as u32));
        }
    });
    let spin_handler = Rc::new(spin_handler);

    let refresh_inherit = inherit.clone();
    let refresh_spin = spin.clone();
    let refresh_inherit_handler = Rc::clone(&inherit_handler);
    let refresh_spin_handler = Rc::clone(&spin_handler);
    let refresh: Rc<dyn Fn()> = Rc::new(move || {
        let value = get();
        refresh_inherit.block_signal(&refresh_inherit_handler);
        refresh_spin.block_signal(&refresh_spin_handler);
        refresh_inherit.set_active(value.is_some());
        refresh_spin.set_sensitive(value.is_some());
        if let Some(value) = value {
            refresh_spin.set_value(f64::from(value));
        }
        refresh_spin.unblock_signal(&refresh_spin_handler);
        refresh_inherit.unblock_signal(&refresh_inherit_handler);
    });

    OptionalWidget {
        widget: container.upcast(),
        refresh,
    }
}

/// Row with an *Inherit + variants* dropdown for an `Option<enum>` property.
pub(crate) fn enum_select_optional<E>(property: &ConfigProperty<Option<E>>) -> SettingRowInit
where
    E: EnumVariants
        + Clone
        + Send
        + Sync
        + PartialEq
        + for<'de> Deserialize<'de>
        + serde::Serialize
        + 'static,
{
    let get_prop = property.clone();
    let set_prop = property.clone();
    let widget = optional_enum_widget::<E>(
        Rc::new(move || get_prop.get()),
        Rc::new(move |value| set_prop.set(value)),
    );

    let control = widget.widget.clone();
    let refresh = Rc::new(widget);
    let watcher_refresh = Rc::clone(&refresh);
    let watcher = spawn_property_watcher(property, move || {
        watcher_refresh.refresh();
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, display_optional_enum::<E>),
        control,
        keepalive: Box::new((refresh, watcher)),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

/// Row with an *Inherit* checkbox + spin for an `Option<u32>` property.
pub(crate) fn number_u32_optional(
    property: &ConfigProperty<Option<u32>>,
    min: u32,
    max: u32,
    step: u32,
    fallback: u32,
) -> SettingRowInit {
    let get_prop = property.clone();
    let set_prop = property.clone();
    let widget = optional_number_widget(
        Rc::new(move || get_prop.get()),
        Rc::new(move |value| set_prop.set(value)),
        f64::from(min),
        f64::from(max),
        f64::from(step.max(1)),
        fallback,
    );

    let control = widget.widget.clone();
    let refresh = Rc::new(widget);
    let watcher_refresh = Rc::clone(&refresh);
    let watcher = spawn_property_watcher(property, move || {
        watcher_refresh.refresh();
        true
    });

    SettingRowInit {
        i18n_key: property.i18n_key(),
        handle: PropertyHandle::new(property, |value| match value {
            Some(value) => value.to_string(),
            None => t("settings-inherit"),
        }),
        control,
        keepalive: Box::new((refresh, watcher)),
        full_width: false,
        dirty_badge: None,
        behavior: RowBehavior::Setting,
        unit: None,
    }
}

fn display_optional_enum<E>(value: &Option<E>) -> String
where
    E: serde::Serialize,
{
    match value {
        Some(value) => serde_json::to_string(value)
            .unwrap_or_default()
            .trim_matches('"')
            .to_owned(),
        None => t("settings-inherit"),
    }
}

/// Builds an *Inherit* checkbox + spin button for an `Option<f64>` field.
pub(crate) fn optional_number_f64_widget(
    get: Rc<dyn Fn() -> Option<f64>>,
    set: Rc<dyn Fn(Option<f64>)>,
    min: f64,
    max: f64,
    step: f64,
    digits: u32,
    fallback: f64,
) -> OptionalWidget {
    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .valign(gtk::Align::Center)
        .build();

    let inherit = override_switch();

    let adjustment = gtk::Adjustment::new(fallback, min, max, step, step, 0.0);
    let spin = gtk::SpinButton::builder()
        .adjustment(&adjustment)
        .digits(digits)
        .valign(gtk::Align::Center)
        .build();

    let initial = get();
    inherit.set_active(initial.is_some());
    spin.set_sensitive(initial.is_some());
    if let Some(value) = initial {
        spin.set_value(value);
    }

    container.append(&inherit);
    container.append(&spin);

    let toggle_set = Rc::clone(&set);
    let toggle_spin = spin.clone();
    let inherit_handler = inherit.connect_active_notify(move |inherit| {
        let active = inherit.is_active();
        toggle_spin.set_sensitive(active);
        if active {
            toggle_set(Some(toggle_spin.value()));
        } else {
            toggle_set(None);
        }
    });
    let inherit_handler = Rc::new(inherit_handler);

    let spin_set = Rc::clone(&set);
    let spin_inherit = inherit.clone();
    let spin_handler = spin.connect_value_changed(move |spin| {
        if spin_inherit.is_active() {
            spin_set(Some(spin.value()));
        }
    });
    let spin_handler = Rc::new(spin_handler);

    let refresh_inherit = inherit.clone();
    let refresh_spin = spin.clone();
    let refresh_inherit_handler = Rc::clone(&inherit_handler);
    let refresh_spin_handler = Rc::clone(&spin_handler);
    let refresh: Rc<dyn Fn()> = Rc::new(move || {
        let value = get();
        refresh_inherit.block_signal(&refresh_inherit_handler);
        refresh_spin.block_signal(&refresh_spin_handler);
        refresh_inherit.set_active(value.is_some());
        refresh_spin.set_sensitive(value.is_some());
        if let Some(value) = value {
            refresh_spin.set_value(value);
        }
        refresh_spin.unblock_signal(&refresh_spin_handler);
        refresh_inherit.unblock_signal(&refresh_inherit_handler);
    });

    OptionalWidget {
        widget: container.upcast(),
        refresh,
    }
}

/// Builds an *Inherit* checkbox + the full ColorValue editor for an
/// `Option<ColorValue>` field. The ColorValue editor is reused unchanged by
/// driving it through a scratch property mirrored back to the field.
pub(crate) fn optional_color_widget(
    get: Rc<dyn Fn() -> Option<ColorValue>>,
    set: Rc<dyn Fn(Option<ColorValue>)>,
) -> OptionalWidget {
    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Horizontal)
        .spacing(8)
        .valign(gtk::Align::Center)
        .build();

    let inherit = override_switch();

    let scratch = ConfigProperty::new(get().unwrap_or(ColorValue::Auto));
    let controller = ColorValueControl::builder()
        .launch(scratch.clone())
        .detach();
    let color_widget = controller.widget().clone();

    let initial = get();
    inherit.set_active(initial.is_some());
    color_widget.set_sensitive(initial.is_some());

    container.append(&inherit);
    container.append(&color_widget);

    let toggle_set = Rc::clone(&set);
    let toggle_scratch = scratch.clone();
    let toggle_widget = color_widget.clone();
    let inherit_handler = inherit.connect_active_notify(move |inherit| {
        let active = inherit.is_active();
        toggle_widget.set_sensitive(active);
        if active {
            toggle_set(Some(toggle_scratch.get()));
        } else {
            toggle_set(None);
        }
    });
    let inherit_handler = Rc::new(inherit_handler);

    // Scratch edits (made through the ColorValue component) flow to the field
    // while overriding.
    let scratch_set = Rc::clone(&set);
    let scratch_inherit = inherit.clone();
    let scratch_for_watch = scratch.clone();
    let scratch_watcher = spawn_property_watcher(&scratch, move || {
        if scratch_inherit.is_active() {
            scratch_set(Some(scratch_for_watch.get()));
        }
        true
    });

    let refresh_inherit = inherit.clone();
    let refresh_widget = color_widget.clone();
    let refresh_scratch = scratch.clone();
    let refresh_inherit_handler = Rc::clone(&inherit_handler);
    // Keep the component + scratch watcher alive for the widget's lifetime.
    let keep = (controller, scratch_watcher);
    let refresh: Rc<dyn Fn()> = Rc::new(move || {
        let _ = &keep;
        let value = get();
        refresh_inherit.block_signal(&refresh_inherit_handler);
        match value {
            Some(color) => {
                refresh_inherit.set_active(true);
                refresh_widget.set_sensitive(true);
                if refresh_scratch.get() != color {
                    refresh_scratch.set(color);
                }
            }
            None => {
                refresh_inherit.set_active(false);
                refresh_widget.set_sensitive(false);
            }
        }
        refresh_inherit.unblock_signal(&refresh_inherit_handler);
    });

    OptionalWidget {
        widget: container.upcast(),
        refresh,
    }
}

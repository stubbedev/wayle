//! Cycling-options group for the wallpaper page.
//!
//! Bundles the cycling sub-settings (mode, interval, same-image) into one
//! full-width row whose body is revealed only when a cycling directory is set —
//! so the knobs stay hidden until cycling is actually in use.

use relm4::{gtk, gtk::prelude::*, prelude::*};
use wayle_config::{
    ConfigProperty,
    schemas::wallpaper::{CyclingInterval, CyclingMode},
};

use crate::{
    editors::{enum_select::enum_select, number::number_newtype, spawn_property_watcher, toggle::toggle},
    pages::spec::SettingRowInit,
    property_handle::PropertyHandle,
    row::{RowBehavior, SettingRow},
};

/// Row labelled "Cycling Options" whose nested mode/interval/same-image rows
/// are revealed when `directory` is non-empty (i.e. cycling is active).
pub(crate) fn cycling_reveal(
    directory: &ConfigProperty<String>,
    mode: &ConfigProperty<CyclingMode>,
    interval: &ConfigProperty<CyclingInterval>,
    same_image: &ConfigProperty<bool>,
) -> SettingRowInit {
    let group = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .build();

    // Build the nested rows with full chrome and stash them in the group.
    let rows: Vec<Controller<SettingRow>> = [
        enum_select(mode),
        number_newtype(
            interval,
            1.0,
            1440.0,
            1.0,
            0,
            |v: &CyclingInterval| v.value() as f64,
            |mins| CyclingInterval::new(mins as u64),
        ),
        toggle(same_image),
    ]
    .into_iter()
    .map(|init| {
        let row = SettingRow::builder().launch(init).detach();
        group.append(row.widget());
        row
    })
    .collect();

    let revealer = gtk::Revealer::builder()
        .transition_type(gtk::RevealerTransitionType::SlideDown)
        .child(&group)
        .reveal_child(!directory.get().is_empty())
        .build();

    // Reveal/collapse as the directory is set or cleared.
    let directory_for_watch = directory.clone();
    let revealer_weak = revealer.downgrade();
    let watcher = spawn_property_watcher(directory, move || {
        let Some(revealer) = revealer_weak.upgrade() else {
            return false;
        };
        revealer.set_reveal_child(!directory_for_watch.get().is_empty());
        true
    });

    SettingRowInit {
        i18n_key: Some("settings-wallpaper-cycling-options"),
        handle: PropertyHandle::new(directory, |value: &String| value.clone()),
        control: revealer.upcast(),
        keepalive: Box::new((rows, watcher)),
        full_width: true,
        dirty_badge: None,
        behavior: RowBehavior::Action,
        unit: None,
    }
}

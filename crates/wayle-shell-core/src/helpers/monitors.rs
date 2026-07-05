//! Monitor enumeration shared between the shell and layer-shell helpers.

use gdk4::{
    gio::prelude::ListModelExt,
    glib::object::Cast,
    prelude::{DisplayExt, MonitorExt},
};
use relm4::gtk::gdk;
use tracing::warn;

/// Monitor connector name (e.g. `DP-1`).
pub type Connector = String;

#[allow(clippy::expect_used)]
pub fn current_monitors() -> Vec<(Connector, gdk::Monitor)> {
    let display = gdk::Display::default().expect("No GDK display found...");
    let monitor_list = display.monitors();

    (0..monitor_list.n_items())
        .filter_map(|i| monitor_list.item(i))
        .filter_map(|obj| obj.downcast::<gdk::Monitor>().ok())
        .filter_map(|monitor| match monitor.connector() {
            Some(connector) => Some((connector.to_string(), monitor)),
            None => {
                warn!(
                    model = monitor.model().map(|m| m.to_string()),
                    "GDK monitor has no connector, skipping"
                );
                None
            }
        })
        .collect()
}

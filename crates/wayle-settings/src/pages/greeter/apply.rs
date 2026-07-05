//! "Apply to login screen" action for the greeter settings page.
//!
//! The greeter reads the *system* config (`/etc/wayle/config.toml` +
//! `runtime.toml`), which a normal user cannot write. Editing the greeter page
//! only changes the user's own config, which the login screen never reads. This
//! button snapshots the page's greeter values and hands them to
//! `wayle-greeter apply-config` through `pkexec`, so any user (after the polkit
//! admin prompt) can push the login-screen background/cursor/etc. to the system.

use std::{fs, process::Command};

use relm4::{gtk, gtk::prelude::*};
use tracing::{info, warn};
use wayle_config::Config;

/// Builds the footer box with the apply button.
pub(super) fn build_footer(config: &Config) -> gtk::Widget {
    let g = &config.greeter;
    // ConfigProperty is cheaply clonable (Arc-backed); capture the handles so
    // the click reads the *current* edited values, not a stale snapshot.
    let background_mode = g.background_mode.clone();
    let background_image = g.background_image.clone();
    let background_color = g.background_color.clone();
    let show_clock = g.show_clock.clone();
    let clock_format = g.clock_format.clone();
    let date_format = g.date_format.clone();
    let show_user_list = g.show_user_list.clone();
    let show_power_buttons = g.show_power_buttons.clone();
    let cursor_theme = g.cursor_theme.clone();
    let cursor_size = g.cursor_size.clone();

    // ponytail: literal labels, not i18n keys — one imperative button doesn't
    // justify touching every locale file; swap to `t()` if the page is localized.
    let button = gtk::Button::with_label("Apply to login screen");
    button.add_css_class("primary");
    button.set_halign(gtk::Align::Start);

    button.connect_clicked(move |button| {
        let mut greeter = toml::Table::new();
        let mut put = |key: &str, value: Result<toml::Value, toml::ser::Error>| match value {
            Ok(value) => {
                greeter.insert(key.to_owned(), value);
            }
            Err(err) => warn!(key, %err, "greeter apply: cannot serialize value"),
        };
        put(
            "background-mode",
            toml::Value::try_from(background_mode.get()),
        );
        put(
            "background-image",
            toml::Value::try_from(background_image.get()),
        );
        put(
            "background-color",
            toml::Value::try_from(background_color.get()),
        );
        put("show-clock", toml::Value::try_from(show_clock.get()));
        put("clock-format", toml::Value::try_from(clock_format.get()));
        put("date-format", toml::Value::try_from(date_format.get()));
        put(
            "show-user-list",
            toml::Value::try_from(show_user_list.get()),
        );
        put(
            "show-power-buttons",
            toml::Value::try_from(show_power_buttons.get()),
        );
        put("cursor-theme", toml::Value::try_from(cursor_theme.get()));
        put("cursor-size", toml::Value::try_from(cursor_size.get()));

        match dispatch(greeter) {
            Ok(()) => info!("greeter apply: launched pkexec wayle-greeter apply-config"),
            Err(err) => {
                warn!(%err, "greeter apply failed");
                button.set_label("Apply failed — see logs");
            }
        }
    });

    let hint = gtk::Label::builder()
        .label("Writes the system login-screen config; asks for admin authentication.")
        .halign(gtk::Align::Start)
        .wrap(true)
        .build();
    hint.add_css_class("settings-section-title");

    let container = gtk::Box::builder()
        .orientation(gtk::Orientation::Vertical)
        .spacing(6)
        .build();
    container.add_css_class("settings-section");
    container.append(&button);
    container.append(&hint);
    container.upcast()
}

/// Stages the greeter table to a temp file and spawns the privileged writer.
fn dispatch(greeter: toml::Table) -> Result<(), String> {
    let mut root = toml::Table::new();
    root.insert("greeter".to_owned(), toml::Value::Table(greeter));
    let body = toml::to_string(&root).map_err(|e| e.to_string())?;

    let path =
        std::env::temp_dir().join(format!("wayle-greeter-apply-{}.toml", std::process::id()));
    fs::write(&path, body).map_err(|e| format!("stage {}: {e}", path.display()))?;

    // Non-blocking: pkexec pops its own auth dialog and runs the writer as root.
    Command::new("pkexec")
        .arg("wayle-greeter")
        .arg("apply-config")
        .arg(&path)
        .spawn()
        .map_err(|e| format!("spawn pkexec: {e}"))?;
    Ok(())
}

//! `org.freedesktop.impl.portal.Background`.
//!
//! `GetAppState` reports nothing (we don't track per-window app state).
//! `NotifyBackground` auto-allows apps to keep running. `EnableAutostart`
//! writes or removes a `~/.config/autostart/<app_id>.desktop` entry, which is
//! the real, headless-doable part of this interface.

use std::{collections::HashMap, path::PathBuf};

use tracing::warn;
use zbus::{
    interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use crate::{dbus_util::owned, response::Response};

/// `EnableAutostart` flag: the app is D-Bus activatable.
const FLAG_DBUS_ACTIVATABLE: u32 = 1;

/// Background portal interface.
pub struct Background;

#[interface(name = "org.freedesktop.impl.portal.Background")]
impl Background {
    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        2
    }

    /// Per-app background state. We don't track windows, so report none.
    async fn get_app_state(&self) -> HashMap<String, OwnedValue> {
        HashMap::new()
    }

    /// Permission to keep running in the background. Auto-allowed.
    async fn notify_background(
        &self,
        _handle: OwnedObjectPath,
        _app_id: String,
        _name: String,
    ) -> (u32, HashMap<String, OwnedValue>) {
        // result: 0 = none, 1 = allow, 2 = forbid.
        let results = owned(1u32)
            .map(|v| HashMap::from([("result".to_owned(), v)]))
            .unwrap_or_default();
        (Response::Success.code(), results)
    }

    /// Adds or removes an autostart entry for the app, returning the resulting
    /// enabled state.
    async fn enable_autostart(
        &self,
        app_id: String,
        enable: bool,
        commandline: Vec<String>,
        flags: u32,
    ) -> bool {
        let Some(path) = autostart_path(&app_id) else {
            warn!("background: no autostart directory (XDG_CONFIG_HOME/HOME unset)");
            return false;
        };

        if !enable {
            let _ = std::fs::remove_file(&path);
            return false;
        }

        let dbus_activatable = flags & FLAG_DBUS_ACTIVATABLE != 0;
        let desktop = autostart_desktop(&app_id, &commandline, dbus_activatable);
        if let Some(dir) = path.parent()
            && let Err(err) = std::fs::create_dir_all(dir)
        {
            warn!(%err, "background: cannot create autostart dir");
            return false;
        }
        match std::fs::write(&path, desktop) {
            Ok(()) => true,
            Err(err) => {
                warn!(%err, "background: cannot write autostart entry");
                false
            }
        }
    }
}

/// The shared connection isn't needed; keep `new` for a uniform call site.
impl Background {
    /// Builds the interface.
    #[must_use]
    pub fn new() -> Self {
        Self
    }
}

/// `$XDG_CONFIG_HOME/autostart/<app_id>.desktop` (or `~/.config/...`).
fn autostart_path(app_id: &str) -> Option<PathBuf> {
    let base = match std::env::var_os("XDG_CONFIG_HOME") {
        Some(dir) if !dir.is_empty() => PathBuf::from(dir),
        _ => PathBuf::from(std::env::var_os("HOME")?).join(".config"),
    };
    Some(base.join("autostart").join(format!("{app_id}.desktop")))
}

/// Builds the autostart `.desktop` contents.
fn autostart_desktop(app_id: &str, commandline: &[String], dbus_activatable: bool) -> String {
    let exec = commandline.join(" ");
    let mut desktop = String::from("[Desktop Entry]\n");
    desktop.push_str("Type=Application\n");
    desktop.push_str(&format!("Name={app_id}\n"));
    desktop.push_str(&format!("Exec={exec}\n"));
    desktop.push_str(&format!("X-Flatpak={app_id}\n"));
    if dbus_activatable {
        desktop.push_str("DBusActivatable=true\n");
    }
    desktop.push_str("X-GNOME-Autostart-enabled=true\n");
    desktop
}

impl Default for Background {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn autostart_path_uses_xdg_config_home() {
        // SAFETY: single-threaded test.
        unsafe {
            std::env::set_var("XDG_CONFIG_HOME", "/tmp/cfg");
        }
        assert_eq!(
            autostart_path("org.app"),
            Some(PathBuf::from("/tmp/cfg/autostart/org.app.desktop"))
        );
    }

    #[test]
    fn desktop_entry_has_exec_and_flags() {
        let d = autostart_desktop("org.app", &["my-app".into(), "--gapless".into()], true);
        assert!(d.contains("Exec=my-app --gapless"));
        assert!(d.contains("DBusActivatable=true"));
        assert!(d.contains("Type=Application"));
    }

    #[test]
    fn desktop_entry_omits_dbus_when_not_activatable() {
        let d = autostart_desktop("org.app", &["app".into()], false);
        assert!(!d.contains("DBusActivatable"));
    }
}

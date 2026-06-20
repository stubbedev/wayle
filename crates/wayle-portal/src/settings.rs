//! `org.freedesktop.impl.portal.Settings` backed by [`wayle_config`].
//!
//! Serves the standard `org.freedesktop.appearance` namespace — `color-scheme`,
//! `accent-color`, `contrast` — from the live Wayle styling config, and emits
//! `SettingChanged` whenever those config values change so toolkits (GTK, Qt,
//! Chromium) re-theme without a restart.

use std::{collections::HashMap, sync::Arc};

use futures::{StreamExt, stream::select_all};
use tracing::{debug, warn};
use wayle_config::{ConfigService, schemas::styling::Appearance};
use zbus::{
    Connection, interface,
    object_server::SignalEmitter,
    zvariant::{OwnedValue, Value},
};

/// The cross-desktop appearance namespace every toolkit reads.
pub const APPEARANCE_NS: &str = "org.freedesktop.appearance";
/// D-Bus object path the portal backend is mounted on.
pub const PORTAL_PATH: &str = "/org/freedesktop/portal/desktop";

const KEY_COLOR_SCHEME: &str = "color-scheme";
const KEY_ACCENT_COLOR: &str = "accent-color";
const KEY_CONTRAST: &str = "contrast";

/// Settings portal interface state.
pub struct Settings {
    config: Arc<ConfigService>,
}

impl Settings {
    /// Builds the interface over a shared config service.
    pub fn new(config: Arc<ConfigService>) -> Self {
        Self { config }
    }

    /// `color-scheme`: 0 = no preference, 1 = dark, 2 = light.
    fn color_scheme(&self) -> u32 {
        match self.config.config().styling.appearance.get() {
            Appearance::Auto => 0,
            Appearance::Dark => 1,
            Appearance::Light => 2,
        }
    }

    /// `accent-color`: sRGB tuple in the `[0, 1]` range, parsed from the
    /// active palette's primary color. Falls back to neutral grey on a
    /// malformed hex string.
    fn accent_color(&self) -> (f64, f64, f64) {
        let hex = self.config.config().styling.palette.primary.get();
        parse_hex_rgb(hex.as_str()).unwrap_or((0.5, 0.5, 0.5))
    }

    /// `contrast`: 0 = normal, 1 = higher contrast.
    fn contrast(&self) -> u32 {
        u32::from(self.config.config().styling.matugen_contrast.get().value() > 0.0)
    }

    /// The full `org.freedesktop.appearance` key map for `ReadAll`.
    fn appearance_map(&self) -> Result<HashMap<String, OwnedValue>, zbus::fdo::Error> {
        let mut map = HashMap::new();
        map.insert(KEY_COLOR_SCHEME.to_owned(), owned(Value::from(self.color_scheme()))?);
        map.insert(KEY_ACCENT_COLOR.to_owned(), owned(accent_value(self.accent_color()))?);
        map.insert(KEY_CONTRAST.to_owned(), owned(Value::from(self.contrast()))?);
        Ok(map)
    }

    /// Reads a single appearance key, or `None` for unknown keys.
    fn read_key(&self, key: &str) -> Option<Result<OwnedValue, zbus::fdo::Error>> {
        match key {
            KEY_COLOR_SCHEME => Some(owned(Value::from(self.color_scheme()))),
            KEY_ACCENT_COLOR => Some(owned(accent_value(self.accent_color()))),
            KEY_CONTRAST => Some(owned(Value::from(self.contrast()))),
            _ => None,
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.Settings")]
impl Settings {
    /// Interface version. Matches the frontend's Settings v2.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        2
    }

    /// Returns every served setting whose namespace matches one of
    /// `namespaces` (empty = all).
    async fn read_all(
        &self,
        namespaces: Vec<String>,
    ) -> zbus::fdo::Result<HashMap<String, HashMap<String, OwnedValue>>> {
        let mut out = HashMap::new();
        if namespace_matches(&namespaces, APPEARANCE_NS) {
            out.insert(APPEARANCE_NS.to_owned(), self.appearance_map()?);
        }
        Ok(out)
    }

    /// Deprecated single-key read (kept for older frontends); delegates to
    /// the same lookup as `ReadOne`.
    async fn read(&self, namespace: String, key: String) -> zbus::fdo::Result<OwnedValue> {
        self.read_one(namespace, key).await
    }

    /// Reads one key from one namespace.
    #[zbus(name = "ReadOne")]
    async fn read_one(&self, namespace: String, key: String) -> zbus::fdo::Result<OwnedValue> {
        if namespace != APPEARANCE_NS {
            return Err(unknown(&namespace, &key));
        }
        match self.read_key(&key) {
            Some(value) => value,
            None => Err(unknown(&namespace, &key)),
        }
    }

    /// Emitted when a served setting changes.
    #[zbus(signal)]
    async fn setting_changed(
        emitter: &SignalEmitter<'_>,
        namespace: &str,
        key: &str,
        value: Value<'_>,
    ) -> zbus::Result<()>;
}

/// Spawns a task that watches the backing config values and emits
/// `SettingChanged` for the appearance keys. Returns once the watchers are
/// armed; the task runs for the life of the connection.
pub fn spawn_watcher(connection: &Connection, config: Arc<ConfigService>) {
    let connection = connection.clone();
    tokio::spawn(async move {
        let Ok(emitter) = SignalEmitter::new(&connection, PORTAL_PATH) else {
            warn!("settings: cannot build signal emitter; live re-theming disabled");
            return;
        };

        let styling = &config.config().styling;
        let scheme = styling
            .appearance
            .watch()
            .map(|_| KEY_COLOR_SCHEME)
            .boxed();
        let accent = styling
            .palette
            .primary
            .watch()
            .map(|_| KEY_ACCENT_COLOR)
            .boxed();
        let contrast = styling
            .matugen_contrast
            .watch()
            .map(|_| KEY_CONTRAST)
            .boxed();
        let mut changes = select_all([scheme, accent, contrast]);

        // The interface struct is cheap to rebuild for each read.
        let settings = Settings::new(config.clone());
        while let Some(key) = changes.next().await {
            let Some(Ok(value)) = settings.read_key(key) else {
                continue;
            };
            if let Err(err) =
                Settings::setting_changed(&emitter, APPEARANCE_NS, key, value.into()).await
            {
                debug!(%err, key, "settings: failed to emit SettingChanged");
            }
        }
    });
}

/// `true` when `wanted` should be served given the requested `namespaces`
/// (empty list means "all namespaces").
fn namespace_matches(namespaces: &[String], wanted: &str) -> bool {
    namespaces.is_empty() || namespaces.iter().any(|ns| wanted.starts_with(ns.as_str()))
}

/// Wraps the accent RGB tuple as a `(ddd)` D-Bus structure.
fn accent_value(rgb: (f64, f64, f64)) -> Value<'static> {
    Value::from((rgb.0, rgb.1, rgb.2))
}

/// Converts a borrowed value into an owned one, mapping failures to a D-Bus
/// error.
fn owned(value: Value<'_>) -> Result<OwnedValue, zbus::fdo::Error> {
    OwnedValue::try_from(value).map_err(|err| zbus::fdo::Error::Failed(err.to_string()))
}

/// Error for an unknown namespace/key pair.
fn unknown(namespace: &str, key: &str) -> zbus::fdo::Error {
    zbus::fdo::Error::Failed(format!("unknown setting {namespace}/{key}"))
}

/// Parses `#rgb` or `#rrggbb` (ignoring any alpha) into sRGB floats in
/// `[0, 1]`. Returns `None` on a malformed string.
fn parse_hex_rgb(hex: &str) -> Option<(f64, f64, f64)> {
    let digits = hex.strip_prefix('#')?;
    let (r, g, b) = match digits.len() {
        3 | 4 => {
            let r = u8::from_str_radix(&digits[0..1].repeat(2), 16).ok()?;
            let g = u8::from_str_radix(&digits[1..2].repeat(2), 16).ok()?;
            let b = u8::from_str_radix(&digits[2..3].repeat(2), 16).ok()?;
            (r, g, b)
        }
        6 | 8 => {
            let r = u8::from_str_radix(&digits[0..2], 16).ok()?;
            let g = u8::from_str_radix(&digits[2..4], 16).ok()?;
            let b = u8::from_str_radix(&digits[4..6], 16).ok()?;
            (r, g, b)
        }
        _ => return None,
    };
    Some((f64::from(r) / 255.0, f64::from(g) / 255.0, f64::from(b) / 255.0))
}

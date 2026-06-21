//! `org.freedesktop.impl.portal.ScreenCast`.
//!
//! `CreateSession` tracks a session; `SelectSources` records the requested
//! options (and decodes a restore token if one is replayed); `Start` shows the
//! Wayle picker (unless a restore token already chose a source), spins up a
//! PipeWire producer per selected source, and returns the node ids the client
//! consumes.
//!
//! The picker is the running shell's `com.wayle.SharePicker1` surface, reached
//! over D-Bus exactly like the legacy `wayle share-picker` stub — but here the
//! selection round-trips entirely within Wayle (we own the capture), so it
//! works on any compositor, not just Hyprland.

#[cfg_attr(not(feature = "pipewire"), allow(dead_code))]
mod capture;
#[cfg(feature = "pipewire")]
mod pipewire;
mod restore;
pub mod source;

use std::collections::HashMap;
#[cfg(feature = "pipewire")]
use std::sync::{Arc, Mutex};

use tracing::{error, warn};
use wayle_ipc::share_picker::SharePickerProxy;
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue, Value},
};

use self::source::{CaptureTarget, PickerSelection, SourceType, parse_picker_reply};
#[cfg(feature = "pipewire")]
use self::pipewire::StreamHandle;
use crate::{
    StreamSizes,
    dbus_util::{Vardict, opt_bool, opt_u32, owned},
    response::Response,
    session,
};

/// Default capture frame rate.
const DEFAULT_FPS: u32 = 30;

/// Per-session ScreenCast configuration accumulated across the method calls.
#[derive(Clone, Default)]
struct SessionConfig {
    /// `cursor_mode` bitmask from `SelectSources`.
    cursor_mode: u32,
    /// Whether multiple sources were requested (we currently stream one).
    multiple: bool,
    /// `persist_mode` from `SelectSources` (0 = no persistence).
    persist_mode: u32,
    /// Target decoded from a replayed restore token, if any.
    restore_target: Option<CaptureTarget>,
}

/// ScreenCast portal interface.
pub struct ScreenCast {
    connection: Connection,
    sessions: session::SessionStore<SessionConfig>,
    /// node id -> stream size, shared with RemoteDesktop for absolute motion.
    /// Only populated when streaming (the `pipewire` feature).
    #[cfg_attr(not(feature = "pipewire"), allow(dead_code))]
    stream_sizes: StreamSizes,
    #[cfg(feature = "pipewire")]
    streams: Arc<Mutex<HashMap<OwnedObjectPath, Vec<StreamHandle>>>>,
}

impl ScreenCast {
    /// Builds the interface over the backend's session-bus connection (used to
    /// reach the picker) and the shared stream-size registry.
    #[must_use]
    pub fn new(connection: Connection, stream_sizes: StreamSizes) -> Self {
        Self {
            connection,
            sessions: session::SessionStore::default(),
            stream_sizes,
            #[cfg(feature = "pipewire")]
            streams: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Shows the picker and parses the user's selection. `None` = cancelled or
    /// the shell UI is unavailable.
    async fn run_picker(&self, allow_token: bool) -> Option<PickerSelection> {
        let proxy = match SharePickerProxy::new(&self.connection).await {
            Ok(proxy) => proxy,
            Err(err) => {
                warn!(%err, "screencast: share picker unavailable (is the shell running?)");
                return None;
            }
        };
        // Empty window list → the picker enumerates toplevels generically and
        // returns a stable ext identifier we can re-resolve when capturing.
        match proxy.pick("", allow_token).await {
            Ok(reply) => parse_picker_reply(&reply),
            Err(err) => {
                warn!(%err, "screencast: picker call failed");
                None
            }
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.ScreenCast")]
impl ScreenCast {
    /// Source types we can capture: monitor | window | virtual(region).
    #[zbus(property, name = "AvailableSourceTypes")]
    fn available_source_types(&self) -> u32 {
        SourceType::Monitor.bit() | SourceType::Window.bit() | SourceType::Virtual.bit()
    }

    /// Cursor modes we support: hidden | embedded.
    #[zbus(property, name = "AvailableCursorModes")]
    fn available_cursor_modes(&self) -> u32 {
        1 | 2
    }

    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        5
    }

    /// Creates a session: mounts the Session object and records default config.
    async fn create_session(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let sessions = self.sessions.clone();
        #[cfg(feature = "pipewire")]
        let streams = self.streams.clone();
        #[cfg(feature = "pipewire")]
        let stream_sizes = self.stream_sizes.clone();
        let key = session_handle.clone();
        let on_close = move || {
            sessions.remove(&key);
            // Dropping the StreamHandles stops their PipeWire loops; also drop
            // their entries from the shared size registry so stale node ids
            // don't accumulate / mislead RemoteDesktop absolute motion.
            #[cfg(feature = "pipewire")]
            if let Ok(mut map) = streams.lock()
                && let Some(handles) = map.remove(&key)
                && let Ok(mut sizes) = stream_sizes.lock()
            {
                for handle in &handles {
                    sizes.remove(&handle.node_id);
                }
            }
        };

        if let Err(err) = session::mount(&self.connection, &session_handle, on_close).await {
            error!(%err, "screencast: cannot mount session object");
            return (Response::Other.code(), HashMap::new());
        }
        self.sessions.insert(session_handle, SessionConfig::default());
        (Response::Success.code(), HashMap::new())
    }

    /// Records the requested capture options; decodes a replayed restore token.
    async fn select_sources(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let cursor_mode = opt_u32(&options, "cursor_mode").unwrap_or(1);
        let multiple = opt_bool(&options, "multiple").unwrap_or(false);
        let persist_mode = opt_u32(&options, "persist_mode").unwrap_or(0);
        let restore_target = options
            .get("restore_data")
            .and_then(|value| restore::decode(value));

        self.sessions.update(&session_handle, |config| {
            config.cursor_mode = cursor_mode;
            config.multiple = multiple;
            config.persist_mode = persist_mode;
            config.restore_target = restore_target;
        });
        (Response::Success.code(), HashMap::new())
    }

    /// Shows the picker (unless restoring), starts the PipeWire stream(s), and
    /// returns the stream node ids.
    #[allow(clippy::cognitive_complexity)]
    async fn start(
        &self,
        handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let config = self.sessions.get(&session_handle).unwrap_or_default();

        // Export a Request so the app can cancel the picker via Close().
        let guard = crate::request::RequestGuard::mount(&self.connection, handle)
            .await
            .ok();
        let cancel = guard.as_ref().map(crate::request::RequestGuard::cancel);

        let selection = match config.restore_target.clone() {
            Some(target) => PickerSelection {
                allow_token: config.persist_mode > 0,
                target,
            },
            None => {
                let picked = match cancel {
                    Some(cancel) => {
                        let picker = self.run_picker(config.persist_mode > 0);
                        tokio::pin!(picker);
                        tokio::select! {
                            selection = &mut picker => selection,
                            () = cancel.cancelled() => None,
                        }
                    }
                    None => self.run_picker(config.persist_mode > 0).await,
                };
                match picked {
                    Some(selection) => selection,
                    None => return (Response::Cancelled.code(), HashMap::new()),
                }
            }
        };

        let stream = match self
            .begin_stream(&session_handle, &selection.target, config.cursor_mode)
            .await
        {
            Ok(stream) => stream,
            Err(err) => {
                error!(%err, "screencast: failed to start stream");
                return (Response::Other.code(), HashMap::new());
            }
        };

        let mut results = HashMap::new();
        match build_streams_value(&stream) {
            Ok(value) => {
                results.insert("streams".to_owned(), value);
            }
            Err(err) => {
                error!(%err, "screencast: cannot encode streams result");
                return (Response::Other.code(), HashMap::new());
            }
        }

        if config.persist_mode > 0
            && selection.allow_token
            && let Ok(restore_data) = restore::encode(&selection.target)
        {
            results.insert("restore_data".to_owned(), restore_data);
            if let Some(mode) = owned(config.persist_mode) {
                results.insert("persist_mode".to_owned(), mode);
            }
        }

        (Response::Success.code(), results)
    }
}

/// A started stream's reportable properties.
struct StreamInfo {
    node_id: u32,
    size: (u32, u32),
    source_type: SourceType,
}

impl ScreenCast {
    /// Starts the PipeWire producer and stores its handle on the session.
    ///
    /// PipeWire/Wayland setup blocks (it waits for the producer thread to
    /// capture a first frame and connect), so it runs on the blocking pool
    /// rather than stalling the async D-Bus executor.
    #[cfg(feature = "pipewire")]
    async fn begin_stream(
        &self,
        session_handle: &OwnedObjectPath,
        target: &CaptureTarget,
        cursor_mode: u32,
    ) -> Result<StreamInfo, String> {
        let show_cursor = source::CursorMode::from_bits(cursor_mode).show_cursor();
        let source_type = target.source_type();
        let target = target.clone();
        let handle =
            tokio::task::spawn_blocking(move || pipewire::start_stream(target, show_cursor, DEFAULT_FPS))
                .await
                .map_err(|err| format!("screencast stream task failed: {err}"))??;
        let info = StreamInfo {
            node_id: handle.node_id,
            size: handle.size,
            source_type,
        };
        if let Ok(mut map) = self.streams.lock() {
            map.entry(session_handle.clone()).or_default().push(handle);
        }
        if let Ok(mut sizes) = self.stream_sizes.lock() {
            sizes.insert(info.node_id, info.size);
        }
        Ok(info)
    }

    /// Without the `pipewire` feature there is no producer.
    #[cfg(not(feature = "pipewire"))]
    async fn begin_stream(
        &self,
        _session_handle: &OwnedObjectPath,
        _target: &CaptureTarget,
        _cursor_mode: u32,
    ) -> Result<StreamInfo, String> {
        Err("wayle-portal built without the pipewire feature".to_owned())
    }
}

/// Builds the `a(ua{sv})` streams result from one started stream.
fn build_streams_value(stream: &StreamInfo) -> Result<OwnedValue, zbus::zvariant::Error> {
    let mut props: HashMap<String, OwnedValue> = HashMap::new();
    props.insert(
        "source_type".to_owned(),
        OwnedValue::try_from(Value::from(stream.source_type.bit()))?,
    );
    let size = (
        i32::try_from(stream.size.0).unwrap_or(i32::MAX),
        i32::try_from(stream.size.1).unwrap_or(i32::MAX),
    );
    props.insert("size".to_owned(), OwnedValue::try_from(Value::from(size))?);

    let streams: Vec<(u32, Vardict)> = vec![(stream.node_id, props)];
    OwnedValue::try_from(Value::from(streams))
}

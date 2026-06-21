//! `org.freedesktop.impl.portal.RemoteDesktop`.
//!
//! `CreateSession` tracks a session, `SelectDevices` records the requested
//! device types, `Start` grants them and spins up a [`VirtualInput`] thread,
//! and the `Notify*` methods replay pointer/keyboard events onto the virtual
//! devices.
//!
//! Pointer (relative motion, buttons, scroll) and keyboard (evdev keycodes)
//! are supported via `zwlr_virtual_pointer_v1` / `zwp_virtual_keyboard_v1`.
//! Absolute pointer motion, keysym input, and touch are accepted but not yet
//! injected (logged), pending stream-coordinate mapping and a virtual-touch
//! protocol.

mod eis;
mod input;

use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use tracing::{debug, error, warn};
use zbus::{
    Connection, interface,
    zvariant::{OwnedObjectPath, OwnedValue},
};

use self::input::{InputCommand, VirtualInput};
use crate::{
    StreamSizes,
    dbus_util::{opt_u32, owned},
    response::Response,
    session,
};

/// Device-type bitmask values (match the portal spec).
const DEVICE_KEYBOARD: u32 = 1;
const DEVICE_POINTER: u32 = 2;

/// Per-session requested device types.
#[derive(Clone, Default)]
struct RdConfig {
    device_types: u32,
}

/// RemoteDesktop portal interface.
pub struct RemoteDesktop {
    connection: Connection,
    sessions: session::SessionStore<RdConfig>,
    inputs: Arc<Mutex<HashMap<OwnedObjectPath, Arc<VirtualInput>>>>,
    /// node id -> stream size, populated by ScreenCast; maps absolute coords.
    stream_sizes: StreamSizes,
}

impl RemoteDesktop {
    /// Builds the interface over the backend's session-bus connection and the
    /// shared stream-size registry.
    #[must_use]
    pub fn new(connection: Connection, stream_sizes: StreamSizes) -> Self {
        Self {
            connection,
            sessions: session::SessionStore::default(),
            inputs: Arc::new(Mutex::new(HashMap::new())),
            stream_sizes,
        }
    }

    /// Looks up the virtual-input handle for a session.
    fn input_for(&self, session_handle: &OwnedObjectPath) -> Option<Arc<VirtualInput>> {
        self.inputs.lock().ok()?.get(session_handle).cloned()
    }

    /// Sends a command to a session's virtual input, if the session is active.
    fn send(&self, session_handle: &OwnedObjectPath, command: InputCommand) {
        if let Some(input) = self.input_for(session_handle) {
            input.send(command);
        }
    }
}

#[interface(name = "org.freedesktop.impl.portal.RemoteDesktop")]
impl RemoteDesktop {
    /// Device types we can inject: keyboard | pointer.
    #[zbus(property, name = "AvailableDeviceTypes")]
    fn available_device_types(&self) -> u32 {
        DEVICE_KEYBOARD | DEVICE_POINTER
    }

    /// Interface version.
    #[zbus(property, name = "version")]
    fn version(&self) -> u32 {
        2
    }

    /// Creates a session.
    async fn create_session(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let sessions = self.sessions.clone();
        let inputs = self.inputs.clone();
        let key = session_handle.clone();
        let on_close = move || {
            sessions.remove(&key);
            // Dropping the VirtualInput stops its thread.
            if let Ok(mut map) = inputs.lock() {
                map.remove(&key);
            }
        };
        if let Err(err) = session::mount(&self.connection, &session_handle, on_close).await {
            error!(%err, "remotedesktop: cannot mount session");
            return (Response::Other.code(), HashMap::new());
        }
        self.sessions.insert(session_handle, RdConfig::default());
        (Response::Success.code(), HashMap::new())
    }

    /// Records the requested device types.
    async fn select_devices(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let requested = opt_u32(&options, "types").unwrap_or(DEVICE_KEYBOARD | DEVICE_POINTER);
        self.sessions.update(&session_handle, |config| {
            config.device_types = requested;
        });
        (Response::Success.code(), HashMap::new())
    }

    /// Grants the session and starts the virtual-input devices.
    async fn start(
        &self,
        _handle: OwnedObjectPath,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _parent_window: String,
        _options: HashMap<String, OwnedValue>,
    ) -> (u32, HashMap<String, OwnedValue>) {
        let config = self.sessions.get(&session_handle).unwrap_or_default();
        let granted = if config.device_types == 0 {
            DEVICE_KEYBOARD | DEVICE_POINTER
        } else {
            config.device_types & (DEVICE_KEYBOARD | DEVICE_POINTER)
        };

        // Binding the virtual devices blocks on a Wayland roundtrip; keep it off
        // the async executor.
        match tokio::task::spawn_blocking(VirtualInput::spawn).await {
            Ok(Ok(input)) => {
                if let Ok(mut map) = self.inputs.lock() {
                    map.insert(session_handle, Arc::new(input));
                }
            }
            Ok(Err(err)) => {
                error!(%err, "remotedesktop: cannot start virtual input");
                return (Response::Other.code(), HashMap::new());
            }
            Err(err) => {
                error!(%err, "remotedesktop: virtual input task failed");
                return (Response::Other.code(), HashMap::new());
            }
        }

        let mut results = HashMap::new();
        if let Some(devices) = owned(granted) {
            results.insert("devices".to_owned(), devices);
        }
        (Response::Success.code(), results)
    }

    /// Relative pointer motion.
    fn notify_pointer_motion(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        dx: f64,
        dy: f64,
    ) {
        self.send(&session_handle, InputCommand::PointerMotion { dx, dy });
    }

    /// Absolute pointer motion, mapped onto the source stream's pixel extent.
    fn notify_pointer_motion_absolute(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        stream: u32,
        x: f64,
        y: f64,
    ) {
        let Some((width, height)) =
            self.stream_sizes.lock().ok().and_then(|m| m.get(&stream).copied())
        else {
            debug!(stream, "remotedesktop: unknown stream extent for absolute motion");
            return;
        };
        self.send(
            &session_handle,
            InputCommand::PointerMotionAbsolute { x, y, width, height },
        );
    }

    /// Pointer button press/release (evdev button code).
    fn notify_pointer_button(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        button: i32,
        state: u32,
    ) {
        self.send(
            &session_handle,
            InputCommand::PointerButton {
                button: button as u32,
                pressed: state != 0,
            },
        );
    }

    /// Smooth scroll.
    fn notify_pointer_axis(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        dx: f64,
        dy: f64,
    ) {
        if dy != 0.0 {
            self.send(&session_handle, InputCommand::PointerAxis { axis: 0, value: dy });
        }
        if dx != 0.0 {
            self.send(&session_handle, InputCommand::PointerAxis { axis: 1, value: dx });
        }
    }

    /// Discrete scroll steps.
    fn notify_pointer_axis_discrete(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        axis: u32,
        steps: i32,
    ) {
        self.send(
            &session_handle,
            InputCommand::PointerAxisDiscrete { axis, steps },
        );
    }

    /// Key press/release by evdev keycode.
    fn notify_keyboard_keycode(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        keycode: i32,
        state: u32,
    ) {
        self.send(
            &session_handle,
            InputCommand::Key {
                keycode: keycode as u32,
                pressed: state != 0,
            },
        );
    }

    /// Key by keysym, resolved to a keycode via the active keymap.
    fn notify_keyboard_keysym(
        &self,
        session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        keysym: i32,
        state: u32,
    ) {
        self.send(
            &session_handle,
            InputCommand::Keysym {
                keysym: keysym as u32,
                pressed: state != 0,
            },
        );
    }

    /// Touch down. Not supported (no virtual-touch protocol).
    fn notify_touch_down(
        &self,
        _session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        _stream: u32,
        _slot: u32,
        _x: f64,
        _y: f64,
    ) {
        warn!("remotedesktop: touch input not supported");
    }

    /// Touch motion. Not supported.
    fn notify_touch_motion(
        &self,
        _session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        _stream: u32,
        _slot: u32,
        _x: f64,
        _y: f64,
    ) {
    }

    /// Touch up. Not supported.
    fn notify_touch_up(
        &self,
        _session_handle: OwnedObjectPath,
        _options: HashMap<String, OwnedValue>,
        _slot: u32,
    ) {
    }

    /// Returns an EIS socket fd the app can use to stream input events instead
    /// of the `Notify*` methods (the modern libei transport).
    async fn connect_to_eis(
        &self,
        session_handle: OwnedObjectPath,
        _app_id: String,
        _options: HashMap<String, OwnedValue>,
    ) -> zbus::fdo::Result<zbus::zvariant::OwnedFd> {
        let input = self
            .input_for(&session_handle)
            .ok_or_else(|| zbus::fdo::Error::Failed("no active remote-desktop session".to_owned()))?;
        eis::connect(input)
            .map(zbus::zvariant::OwnedFd::from)
            .map_err(zbus::fdo::Error::Failed)
    }
}

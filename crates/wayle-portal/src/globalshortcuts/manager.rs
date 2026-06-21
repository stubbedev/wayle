//! Wayland side of GlobalShortcuts: registers shortcuts via
//! `hyprland-global-shortcuts-v1` and forwards press/release events.
//!
//! A dedicated thread owns the Wayland connection and dispatches events;
//! registration requests and the manager proxy are `Send`, so the async
//! interface registers shortcuts directly while the thread reports activations
//! over a Tokio channel.

use tokio::sync::mpsc;
use tracing::warn;
use wayland_client::{Connection, Dispatch, QueueHandle, protocol::wl_registry};

use crate::protocol::hyprland_global_shortcuts_v1::{
    hyprland_global_shortcut_v1::{self, HyprlandGlobalShortcutV1},
    hyprland_global_shortcuts_manager_v1::HyprlandGlobalShortcutsManagerV1,
};

/// A shortcut activation/deactivation reported by the compositor.
pub struct ShortcutEvent {
    /// Opaque key the shortcut was registered with (`app_id\x1fid`).
    pub key: String,
    /// `true` = pressed, `false` = released.
    pub pressed: bool,
    /// Event timestamp in milliseconds.
    pub timestamp: u64,
}

/// Handle to the running shortcuts manager. `Send`, so the async interface can
/// register shortcuts without touching the Wayland thread directly.
pub struct GsHandle {
    manager: HyprlandGlobalShortcutsManagerV1,
    qh: QueueHandle<GsState>,
}

impl GsHandle {
    /// Registers a global shortcut. The compositor delivers activations for it
    /// tagged with `key`.
    pub fn register(&self, key: String, id: &str, app_id: &str, description: &str, trigger: &str) {
        self.manager.register_shortcut(
            id.to_owned(),
            app_id.to_owned(),
            description.to_owned(),
            trigger.to_owned(),
            &self.qh,
            key,
        );
    }
}

/// Dispatch state for the Wayland thread: the activation channel plus the
/// manager bound during the registry roundtrip.
struct GsState {
    events: mpsc::UnboundedSender<ShortcutEvent>,
    manager: Option<HyprlandGlobalShortcutsManagerV1>,
}

/// Spawns the shortcuts manager thread.
///
/// # Errors
///
/// Returns an error if Wayland is unreachable or the compositor lacks
/// `hyprland_global_shortcuts_manager_v1`.
pub fn spawn() -> Result<(GsHandle, mpsc::UnboundedReceiver<ShortcutEvent>), String> {
    let (events_tx, events_rx) = mpsc::unbounded_channel();
    let (setup_tx, setup_rx) = std::sync::mpsc::channel::<
        Result<(HyprlandGlobalShortcutsManagerV1, QueueHandle<GsState>), String>,
    >();

    std::thread::Builder::new()
        .name("wayle-global-shortcuts".to_owned())
        .spawn(move || run(&events_tx, &setup_tx))
        .map_err(|e| format!("cannot spawn global-shortcuts thread: {e}"))?;

    let (manager, qh) = setup_rx
        .recv()
        .map_err(|_| "global-shortcuts thread exited during setup".to_owned())??;
    Ok((GsHandle { manager, qh }, events_rx))
}

/// Thread body: bind the manager, hand it back, then dispatch events forever.
fn run(
    events: &mpsc::UnboundedSender<ShortcutEvent>,
    setup: &std::sync::mpsc::Sender<
        Result<(HyprlandGlobalShortcutsManagerV1, QueueHandle<GsState>), String>,
    >,
) {
    let connection = match Connection::connect_to_env() {
        Ok(connection) => connection,
        Err(err) => {
            let _ = setup.send(Err(format!("cannot connect to wayland: {err}")));
            return;
        }
    };
    let mut queue = connection.new_event_queue();
    let handle = queue.handle();
    connection.display().get_registry(&handle, ());

    let mut state = GsState {
        events: events.clone(),
        manager: None,
    };
    if let Err(err) = queue.roundtrip(&mut state) {
        let _ = setup.send(Err(format!("wayland roundtrip failed: {err}")));
        return;
    }
    let Some(manager) = state.manager.clone() else {
        let _ = setup.send(Err(
            "compositor lacks hyprland_global_shortcuts_manager_v1".to_owned()
        ));
        return;
    };

    if setup.send(Ok((manager, handle))).is_err() {
        return;
    }

    loop {
        if let Err(err) = queue.blocking_dispatch(&mut state) {
            warn!(%err, "global-shortcuts dispatch ended");
            return;
        }
    }
}

impl Dispatch<wl_registry::WlRegistry, ()> for GsState {
    fn event(
        state: &mut Self,
        registry: &wl_registry::WlRegistry,
        event: wl_registry::Event,
        _data: &(),
        _conn: &Connection,
        handle: &QueueHandle<Self>,
    ) {
        if let wl_registry::Event::Global {
            name, interface, ..
        } = event
            && interface == "hyprland_global_shortcuts_manager_v1"
        {
            state.manager = Some(registry.bind(name, 1, handle, ()));
        }
    }
}

impl Dispatch<HyprlandGlobalShortcutsManagerV1, ()> for GsState {
    fn event(
        _state: &mut Self,
        _proxy: &HyprlandGlobalShortcutsManagerV1,
        _event: <HyprlandGlobalShortcutsManagerV1 as wayland_client::Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
    }
}

impl Dispatch<HyprlandGlobalShortcutV1, String> for GsState {
    fn event(
        state: &mut Self,
        _proxy: &HyprlandGlobalShortcutV1,
        event: hyprland_global_shortcut_v1::Event,
        key: &String,
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        let (pressed, timestamp) = match event {
            hyprland_global_shortcut_v1::Event::Pressed {
                tv_sec_hi,
                tv_sec_lo,
                tv_nsec,
            } => (true, millis(tv_sec_hi, tv_sec_lo, tv_nsec)),
            hyprland_global_shortcut_v1::Event::Released {
                tv_sec_hi,
                tv_sec_lo,
                tv_nsec,
            } => (false, millis(tv_sec_hi, tv_sec_lo, tv_nsec)),
        };
        let _ = state.events.send(ShortcutEvent {
            key: key.clone(),
            pressed,
            timestamp,
        });
    }
}

/// Combines the split-second + nanosecond Wayland timestamp into milliseconds.
fn millis(tv_sec_hi: u32, tv_sec_lo: u32, tv_nsec: u32) -> u64 {
    let secs = (u64::from(tv_sec_hi) << 32) | u64::from(tv_sec_lo);
    secs.wrapping_mul(1000) + u64::from(tv_nsec) / 1_000_000
}

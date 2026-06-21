//! Wayland side of the Clipboard portal: bridges `zwlr_data_control` to the
//! D-Bus interface.
//!
//! A dedicated thread reads selection/transfer events; the manager/device/queue
//! handle are `Send`, so the async interface issues requests (receive, set
//! selection) directly — same split as `globalshortcuts::manager`. Shared state
//! (current offer, pending transfer fds) is reachable from both sides.

use std::{
    collections::HashMap,
    os::fd::{AsFd, OwnedFd},
    sync::{
        Arc, Mutex,
        atomic::{AtomicU32, Ordering},
        mpsc,
    },
};

use tokio::sync::mpsc as tokio_mpsc;
use tracing::warn;
use wayland_client::{
    Connection, Dispatch, QueueHandle, delegate_noop, event_created_child,
    protocol::{wl_registry, wl_seat::WlSeat},
};
use wayland_protocols_wlr::data_control::v1::client::{
    zwlr_data_control_device_v1::{self, EVT_DATA_OFFER_OPCODE, ZwlrDataControlDeviceV1},
    zwlr_data_control_manager_v1::ZwlrDataControlManagerV1,
    zwlr_data_control_offer_v1::{self, ZwlrDataControlOfferV1},
    zwlr_data_control_source_v1::{self, ZwlrDataControlSourceV1},
};

/// An event from the compositor for the async side to turn into a D-Bus signal.
pub enum ClipEvent {
    /// The selection owner changed (new clipboard content available).
    OwnerChanged,
    /// The compositor requests our owned selection's data for `mime`; the app
    /// must write it to the fd returned by `SelectionWrite(serial)`.
    Transfer { mime: String, serial: u32 },
}

/// State shared between the Wayland thread and the async interface.
#[derive(Clone, Default)]
struct Shared {
    current_offer: Arc<Mutex<Option<ZwlrDataControlOfferV1>>>,
    transfer_fds: Arc<Mutex<HashMap<u32, OwnedFd>>>,
    serial: Arc<AtomicU32>,
}

/// Handle to the running clipboard bridge. `Send`.
pub struct ClipboardHandle {
    connection: Connection,
    manager: ZwlrDataControlManagerV1,
    device: ZwlrDataControlDeviceV1,
    qh: QueueHandle<ClipState>,
    shared: Shared,
    /// Owned selection sources kept alive while they hold the selection.
    sources: Mutex<Vec<ZwlrDataControlSourceV1>>,
}

impl ClipboardHandle {
    /// Reads the current selection's `mime` content, returning a readable fd the
    /// data streams into. `None` if there is no selection.
    pub fn read(&self, mime: &str) -> Option<OwnedFd> {
        let offer = self.shared.current_offer.lock().ok()?.clone()?;
        let (read_fd, write_fd) = make_pipe()?;
        offer.receive(mime.to_owned(), write_fd.as_fd());
        let _ = self.connection.flush();
        // write_fd drops here; the compositor holds its own dup via the protocol.
        Some(read_fd)
    }

    /// Becomes the selection owner, offering `mimes`. The app provides data on
    /// demand via the Transfer events.
    pub fn set_selection(&self, mimes: &[String]) {
        let source = self.manager.create_data_source(&self.qh, ());
        for mime in mimes {
            source.offer(mime.clone());
        }
        self.device.set_selection(Some(&source));
        let _ = self.connection.flush();
        if let Ok(mut sources) = self.sources.lock() {
            sources.push(source);
        }
    }

    /// Returns the compositor's write fd for a pending transfer `serial` so the
    /// app can write its clipboard data into it directly.
    pub fn take_transfer_fd(&self, serial: u32) -> Option<OwnedFd> {
        self.shared.transfer_fds.lock().ok()?.remove(&serial)
    }
}

/// Dispatch state owned by the Wayland thread.
struct ClipState {
    shared: Shared,
    events: tokio_mpsc::UnboundedSender<ClipEvent>,
    manager: Option<ZwlrDataControlManagerV1>,
    seat: Option<WlSeat>,
}

/// Spawns the clipboard bridge thread.
///
/// # Errors
///
/// Returns an error if Wayland is unreachable or the compositor lacks
/// `zwlr_data_control_manager_v1`.
pub fn spawn() -> Result<(ClipboardHandle, tokio_mpsc::UnboundedReceiver<ClipEvent>), String> {
    let (events_tx, events_rx) = tokio_mpsc::unbounded_channel();
    #[allow(clippy::type_complexity)]
    let (setup_tx, setup_rx) = mpsc::channel::<
        Result<
            (
                Connection,
                ZwlrDataControlManagerV1,
                ZwlrDataControlDeviceV1,
                QueueHandle<ClipState>,
                Shared,
            ),
            String,
        >,
    >();

    std::thread::Builder::new()
        .name("wayle-clipboard".to_owned())
        .spawn(move || run(&events_tx, &setup_tx))
        .map_err(|e| format!("cannot spawn clipboard thread: {e}"))?;

    let (connection, manager, device, qh, shared) = setup_rx
        .recv()
        .map_err(|_| "clipboard thread exited during setup".to_owned())??;
    Ok((
        ClipboardHandle {
            connection,
            manager,
            device,
            qh,
            shared,
            sources: Mutex::new(Vec::new()),
        },
        events_rx,
    ))
}

#[allow(clippy::type_complexity)]
fn run(
    events: &tokio_mpsc::UnboundedSender<ClipEvent>,
    setup: &mpsc::Sender<
        Result<
            (
                Connection,
                ZwlrDataControlManagerV1,
                ZwlrDataControlDeviceV1,
                QueueHandle<ClipState>,
                Shared,
            ),
            String,
        >,
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

    let shared = Shared::default();
    let mut state = ClipState {
        shared: shared.clone(),
        events: events.clone(),
        manager: None,
        seat: None,
    };
    if queue.roundtrip(&mut state).is_err() {
        let _ = setup.send(Err("wayland roundtrip failed".to_owned()));
        return;
    }
    let (Some(manager), Some(seat)) = (state.manager.clone(), state.seat.clone()) else {
        let _ = setup.send(Err(
            "compositor lacks zwlr_data_control_manager_v1".to_owned()
        ));
        return;
    };
    let device = manager.get_data_device(&seat, &handle, ());
    // Drain the initial selection advertisement.
    let _ = queue.roundtrip(&mut state);

    if setup
        .send(Ok((connection, manager, device, handle, shared)))
        .is_err()
    {
        return;
    }

    loop {
        if queue.blocking_dispatch(&mut state).is_err() {
            warn!("clipboard dispatch ended");
            return;
        }
    }
}

impl Dispatch<wl_registry::WlRegistry, ()> for ClipState {
    fn event(
        state: &mut Self,
        registry: &wl_registry::WlRegistry,
        event: wl_registry::Event,
        _data: &(),
        _conn: &Connection,
        handle: &QueueHandle<Self>,
    ) {
        if let wl_registry::Event::Global {
            name,
            interface,
            version,
        } = event
        {
            match interface.as_str() {
                "zwlr_data_control_manager_v1" => {
                    state.manager = Some(registry.bind(name, version.min(2), handle, ()));
                }
                "wl_seat" => {
                    state.seat = Some(registry.bind(name, version.min(7), handle, ()));
                }
                _ => {}
            }
        }
    }
}

impl Dispatch<ZwlrDataControlDeviceV1, ()> for ClipState {
    fn event(
        state: &mut Self,
        _device: &ZwlrDataControlDeviceV1,
        event: zwlr_data_control_device_v1::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        match event {
            zwlr_data_control_device_v1::Event::Selection { id } => {
                if let Ok(mut current) = state.shared.current_offer.lock() {
                    *current = id;
                }
                let _ = state.events.send(ClipEvent::OwnerChanged);
            }
            zwlr_data_control_device_v1::Event::Finished => {
                if let Ok(mut current) = state.shared.current_offer.lock() {
                    *current = None;
                }
            }
            _ => {}
        }
    }

    event_created_child!(ClipState, ZwlrDataControlDeviceV1, [
        EVT_DATA_OFFER_OPCODE => (ZwlrDataControlOfferV1, ()),
    ]);
}

impl Dispatch<ZwlrDataControlOfferV1, ()> for ClipState {
    fn event(
        _state: &mut Self,
        _offer: &ZwlrDataControlOfferV1,
        _event: zwlr_data_control_offer_v1::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        // We don't need the per-offer mime list for read/owner-change handling.
    }
}

impl Dispatch<ZwlrDataControlSourceV1, ()> for ClipState {
    fn event(
        state: &mut Self,
        _source: &ZwlrDataControlSourceV1,
        event: zwlr_data_control_source_v1::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        match event {
            zwlr_data_control_source_v1::Event::Send { mime_type, fd } => {
                let serial = state
                    .shared
                    .serial
                    .fetch_add(1, Ordering::SeqCst)
                    .wrapping_add(1);
                if let Ok(mut map) = state.shared.transfer_fds.lock() {
                    map.insert(serial, fd);
                }
                let _ = state.events.send(ClipEvent::Transfer {
                    mime: mime_type,
                    serial,
                });
            }
            zwlr_data_control_source_v1::Event::Cancelled => {}
            _ => {}
        }
    }
}

delegate_noop!(ClipState: ignore WlSeat);
delegate_noop!(ClipState: ignore ZwlrDataControlManagerV1);

/// Creates a unix pipe, returning `(read, write)`.
fn make_pipe() -> Option<(OwnedFd, OwnedFd)> {
    use std::os::fd::FromRawFd;
    let mut fds = [0i32; 2];
    // SAFETY: fds is a valid 2-element buffer; we own the returned descriptors.
    let ret = unsafe { libc::pipe(fds.as_mut_ptr()) };
    if ret != 0 {
        return None;
    }
    // SAFETY: pipe() succeeded, so both fds are valid and owned by us.
    Some(unsafe { (OwnedFd::from_raw_fd(fds[0]), OwnedFd::from_raw_fd(fds[1])) })
}

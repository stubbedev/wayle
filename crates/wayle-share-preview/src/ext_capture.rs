//! Compositor-agnostic toplevel capture.
//!
//! Combines three staging protocols that any modern wlroots-style compositor
//! (niri, sway, …) implements:
//! - `ext_foreign_toplevel_list_v1` — enumerate toplevels (title / app_id /
//!   identifier),
//! - `ext_foreign_toplevel_image_capture_source_manager_v1` — turn a toplevel
//!   handle into a capture source,
//! - `ext_image_copy_capture_v1` — copy the source into an SHM buffer.
//!
//! `ext_foreign_toplevel_list_v1` carries no focus/activation state, so the
//! caller must decide *which* toplevel to capture (e.g. by matching the
//! compositor's active window's app_id/title) and pass its handle to
//! [`ExtToplevelManager::capture_toplevel`].

use std::sync::{Arc, Mutex, Weak};

use wayland_client::{
    Connection, Dispatch, Proxy, QueueHandle, delegate_noop, event_created_child,
    protocol::{
        wl_buffer::WlBuffer,
        wl_registry,
        wl_shm::{Format, WlShm},
        wl_shm_pool::WlShmPool,
    },
};
use wayland_protocols::ext::{
    foreign_toplevel_list::v1::client::{
        ext_foreign_toplevel_handle_v1::{self, ExtForeignToplevelHandleV1},
        ext_foreign_toplevel_list_v1::{self, ExtForeignToplevelListV1},
    },
    image_capture_source::v1::client::{
        ext_foreign_toplevel_image_capture_source_manager_v1::ExtForeignToplevelImageCaptureSourceManagerV1,
        ext_image_capture_source_v1::ExtImageCaptureSourceV1,
    },
    image_copy_capture::v1::client::{
        ext_image_copy_capture_frame_v1::{self, ExtImageCopyCaptureFrameV1},
        ext_image_copy_capture_manager_v1::{self, ExtImageCopyCaptureManagerV1},
        ext_image_copy_capture_session_v1::{self, ExtImageCopyCaptureSessionV1},
    },
};

use crate::{buffer::Buffer, error::Error};

/// A toplevel reported by `ext_foreign_toplevel_list_v1`.
#[derive(Clone)]
pub struct ExtToplevel {
    pub handle: ExtForeignToplevelHandleV1,
    pub title: Option<String>,
    pub app_id: Option<String>,
    pub identifier: Option<String>,
}

#[derive(Default)]
struct SessionState {
    width: u32,
    height: u32,
    format: Option<Format>,
    done: bool,
    stopped: bool,
}

#[derive(Default)]
struct FrameState {
    ready: bool,
    failed: bool,
}

/// Enumerates and captures toplevels via the `ext-*` staging protocols.
pub struct ExtToplevelManager {
    connection: Connection,
    shm: Option<WlShm>,
    source_manager: Option<ExtForeignToplevelImageCaptureSourceManagerV1>,
    capture_manager: Option<ExtImageCopyCaptureManagerV1>,
    /// Kept alive so the toplevel handles stay valid.
    _list: Option<ExtForeignToplevelListV1>,
    toplevels: Vec<ExtToplevel>,
}

impl ExtToplevelManager {
    /// Binds the protocols and collects the current toplevel list.
    ///
    /// # Errors
    ///
    /// Returns [`Error::ProtocolNotAvailable`] when the compositor does not
    /// implement the required `ext-*` protocols.
    pub fn new(connection: &Connection) -> Result<Self, Error> {
        let mut event_queue = connection.new_event_queue();
        let handle = event_queue.handle();

        let mut manager = Self {
            connection: connection.clone(),
            shm: None,
            source_manager: None,
            capture_manager: None,
            _list: None,
            toplevels: Vec::new(),
        };

        connection.display().get_registry(&handle, ());
        event_queue
            .roundtrip(&mut manager)
            .map_err(Error::WaylandDispatch)?;

        if manager.capture_manager.is_none() {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ExtImageCopyCaptureManagerV1,
            >()))?
        }
        if manager.source_manager.is_none() {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ExtForeignToplevelImageCaptureSourceManagerV1,
            >()))?
        }
        if manager._list.is_none() {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ExtForeignToplevelListV1,
            >()))?
        }
        if manager.shm.is_none() {
            Err(Error::NoShm)?
        }

        // The list emits a burst of `toplevel` + per-handle metadata events;
        // a couple of roundtrips drains them.
        event_queue
            .roundtrip(&mut manager)
            .map_err(Error::WaylandDispatch)?;
        event_queue
            .roundtrip(&mut manager)
            .map_err(Error::WaylandDispatch)?;

        Ok(manager)
    }

    /// The toplevels currently known to the compositor.
    pub fn toplevels(&self) -> &[ExtToplevel] {
        &self.toplevels
    }

    /// Captures a single frame of `handle` into an SHM [`Buffer`].
    ///
    /// `overlay_cursor` asks the compositor to composite the pointer cursor
    /// into the frame (via the `paint_cursors` session option).
    ///
    /// # Errors
    ///
    /// Returns an error if session negotiation or the frame copy fails.
    pub fn capture_toplevel(
        &mut self,
        handle: &ExtForeignToplevelHandleV1,
        overlay_cursor: bool,
    ) -> Result<Buffer, Error> {
        let (Some(source_manager), Some(capture_manager)) =
            (self.source_manager.clone(), self.capture_manager.clone())
        else {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ExtImageCopyCaptureManagerV1,
            >()))?
        };

        let mut event_queue = self.connection.new_event_queue();
        let qh = event_queue.handle();

        let options = if overlay_cursor {
            ext_image_copy_capture_manager_v1::Options::PaintCursors
        } else {
            ext_image_copy_capture_manager_v1::Options::empty()
        };
        let source = source_manager.create_source(handle, &qh, ());
        let session_state = Arc::new(Mutex::new(SessionState::default()));
        let session = capture_manager.create_session(
            &source,
            options,
            &qh,
            Arc::downgrade(&session_state),
        );

        // Wait for the session to advertise its buffer constraints.
        let (width, height, format) = loop {
            event_queue
                .blocking_dispatch(self)
                .map_err(Error::WaylandDispatch)?;
            let state = session_state.lock().expect("lock should not be poisoned");
            if state.stopped {
                session.destroy();
                source.destroy();
                return Err(Error::Failed);
            }
            if state.done && state.format.is_some() {
                break (state.width, state.height, state.format);
            }
        };
        let Some(format) = format else {
            session.destroy();
            source.destroy();
            return Err(Error::Failed);
        };

        let buffer = Buffer::new(
            self.shm.as_ref().ok_or(Error::NoShm)?,
            width,
            height,
            width * 4,
            format,
            &qh,
            (),
        )?;

        let frame_state = Arc::new(Mutex::new(FrameState::default()));
        let frame = session.create_frame(&qh, Arc::downgrade(&frame_state));
        frame.attach_buffer(&buffer.buffer);
        frame.capture();

        let failed = loop {
            event_queue
                .blocking_dispatch(self)
                .map_err(Error::WaylandDispatch)?;
            let state = frame_state.lock().expect("lock should not be poisoned");
            if state.ready {
                break false;
            }
            if state.failed {
                break true;
            }
        };

        frame.destroy();
        session.destroy();
        source.destroy();

        if failed {
            buffer.destroy();
            return Err(Error::Failed);
        }
        Ok(buffer)
    }
}

impl Dispatch<wl_registry::WlRegistry, ()> for ExtToplevelManager {
    fn event(
        state: &mut Self,
        registry: &wl_registry::WlRegistry,
        event: <wl_registry::WlRegistry as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        handle: &QueueHandle<Self>,
    ) {
        let wl_registry::Event::Global {
            name,
            interface,
            version,
        } = event
        else {
            return;
        };
        match interface.as_str() {
            "wl_shm" => state.shm = Some(registry.bind(name, version, handle, ())),
            "ext_foreign_toplevel_list_v1" => {
                state._list = Some(registry.bind(name, version, handle, ()));
            }
            "ext_foreign_toplevel_image_capture_source_manager_v1" => {
                state.source_manager = Some(registry.bind(name, version, handle, ()));
            }
            "ext_image_copy_capture_manager_v1" => {
                state.capture_manager = Some(registry.bind(name, version, handle, ()));
            }
            _ => {}
        }
    }
}

impl Dispatch<ExtForeignToplevelListV1, ()> for ExtToplevelManager {
    fn event(
        state: &mut Self,
        _proxy: &ExtForeignToplevelListV1,
        event: <ExtForeignToplevelListV1 as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        if let ext_foreign_toplevel_list_v1::Event::Toplevel { toplevel } = event {
            state.toplevels.push(ExtToplevel {
                handle: toplevel,
                title: None,
                app_id: None,
                identifier: None,
            });
        }
    }

    event_created_child!(ExtToplevelManager, ExtForeignToplevelListV1, [
        ext_foreign_toplevel_list_v1::EVT_TOPLEVEL_OPCODE => (ExtForeignToplevelHandleV1, ()),
    ]);
}

impl Dispatch<ExtForeignToplevelHandleV1, ()> for ExtToplevelManager {
    fn event(
        state: &mut Self,
        proxy: &ExtForeignToplevelHandleV1,
        event: <ExtForeignToplevelHandleV1 as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        let Some(entry) = state.toplevels.iter_mut().find(|t| &t.handle == proxy) else {
            return;
        };
        match event {
            ext_foreign_toplevel_handle_v1::Event::Title { title } => entry.title = Some(title),
            ext_foreign_toplevel_handle_v1::Event::AppId { app_id } => entry.app_id = Some(app_id),
            ext_foreign_toplevel_handle_v1::Event::Identifier { identifier } => {
                entry.identifier = Some(identifier);
            }
            ext_foreign_toplevel_handle_v1::Event::Closed => {
                state.toplevels.retain(|t| &t.handle != proxy);
            }
            _ => {}
        }
    }
}

impl Dispatch<ExtImageCopyCaptureSessionV1, Weak<Mutex<SessionState>>> for ExtToplevelManager {
    fn event(
        _state: &mut Self,
        _proxy: &ExtImageCopyCaptureSessionV1,
        event: <ExtImageCopyCaptureSessionV1 as Proxy>::Event,
        data: &Weak<Mutex<SessionState>>,
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        let Some(data) = data.upgrade() else {
            return;
        };
        let mut session = data.lock().expect("lock should not be poisoned");
        match event {
            ext_image_copy_capture_session_v1::Event::BufferSize { width, height } => {
                session.width = width;
                session.height = height;
            }
            ext_image_copy_capture_session_v1::Event::ShmFormat { format } => {
                if session.format.is_none()
                    && let Ok(format) = format.into_result()
                {
                    session.format = Some(format);
                }
            }
            ext_image_copy_capture_session_v1::Event::Done => session.done = true,
            ext_image_copy_capture_session_v1::Event::Stopped => session.stopped = true,
            _ => {}
        }
    }
}

impl Dispatch<ExtImageCopyCaptureFrameV1, Weak<Mutex<FrameState>>> for ExtToplevelManager {
    fn event(
        _state: &mut Self,
        _proxy: &ExtImageCopyCaptureFrameV1,
        event: <ExtImageCopyCaptureFrameV1 as Proxy>::Event,
        data: &Weak<Mutex<FrameState>>,
        _conn: &Connection,
        _handle: &QueueHandle<Self>,
    ) {
        let Some(data) = data.upgrade() else {
            return;
        };
        let mut frame = data.lock().expect("lock should not be poisoned");
        match event {
            ext_image_copy_capture_frame_v1::Event::Ready => frame.ready = true,
            ext_image_copy_capture_frame_v1::Event::Failed { .. } => frame.failed = true,
            _ => {}
        }
    }
}

delegate_noop!(ExtToplevelManager: ignore WlShm);
delegate_noop!(ExtToplevelManager: ignore WlShmPool);
delegate_noop!(ExtToplevelManager: ignore WlBuffer);
delegate_noop!(ExtToplevelManager: ignore ExtImageCaptureSourceV1);
delegate_noop!(ExtToplevelManager: ignore ExtForeignToplevelImageCaptureSourceManagerV1);
delegate_noop!(ExtToplevelManager: ignore ExtImageCopyCaptureManagerV1);

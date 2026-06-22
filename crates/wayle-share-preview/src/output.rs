use std::{
    os::fd::AsFd,
    sync::{Arc, Mutex, Weak},
};

use wayland_client::{
    Connection, Dispatch, EventQueue, delegate_noop,
    protocol::{
        wl_buffer::WlBuffer,
        wl_output::{self, Mode, Subpixel, Transform, WlOutput},
        wl_registry,
        wl_shm::WlShm,
        wl_shm_pool::WlShmPool,
    },
};
use wayland_protocols::wp::linux_dmabuf::zv1::client::{
    zwp_linux_buffer_params_v1::{self, ZwpLinuxBufferParamsV1},
    zwp_linux_dmabuf_v1::ZwpLinuxDmabufV1,
};
use wayland_protocols_wlr::screencopy::v1::client::{
    zwlr_screencopy_frame_v1::{self, ZwlrScreencopyFrameV1},
    zwlr_screencopy_manager_v1::ZwlrScreencopyManagerV1,
};

use crate::{Frame, buffer::Buffer, dmabuf::DmaBuffer, error::Error};

/// State for one dmabuf `capture_into` round-trip: the compositor signals
/// `buffer_done` (we may copy), then `ready`/`failed`.
#[derive(Default)]
struct DmaCapture {
    /// The compositor finished advertising buffer constraints; safe to `copy`.
    can_copy: bool,
    /// We already issued the `copy`.
    requested: bool,
    /// The copy completed.
    ready: bool,
    /// The copy failed.
    failed: bool,
}

#[derive(Debug, Clone)]
pub struct Geometry {
    pub x: i32,
    pub y: i32,
    pub physical_width: i32,
    pub physical_height: i32,
    pub subpixel: Subpixel,
    pub make: String,
    pub model: String,
    pub transform: Transform,
}

#[derive(Debug, Clone)]
pub struct OutputMode {
    pub mode: Mode,
    pub width: i32,
    pub height: i32,
    pub refresh: i32,
}

#[derive(Default, Debug, Clone)]
pub struct Output {
    pub name: Option<String>,
    pub description: Option<String>,
    pub scale: Option<i32>,
    pub mode: Option<OutputMode>,
    pub geometry: Option<Geometry>,
}

#[derive(Clone)]
pub struct OutputManager {
    shm: Option<WlShm>,
    manager: Option<ZwlrScreencopyManagerV1>,
    /// `zwp_linux_dmabuf_v1`, when the compositor offers it — enables the
    /// zero-copy capture path.
    linux_dmabuf: Option<ZwpLinuxDmabufV1>,
    pub outputs: Vec<(WlOutput, Output)>,
    intialized_outputs: u32,
    connection: Connection,
}

impl OutputManager {
    /// setup a new output manager which can be used to capture one or more frames of outputs or of selected regions
    pub fn new(connection: &Connection) -> Result<Self, Error> {
        let display = connection.display();

        let mut event_queue = connection.new_event_queue();
        let handle = event_queue.handle();

        let mut manager = Self {
            shm: None,
            manager: None,
            linux_dmabuf: None,
            outputs: Vec::new(),
            intialized_outputs: 0,
            connection: connection.clone(),
        };

        display.get_registry(&handle, ());

        event_queue
            .roundtrip(&mut manager)
            .map_err(Error::WaylandDispatch)?;

        if manager.manager.is_none() {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ZwlrScreencopyManagerV1,
            >()))?
        }
        if manager.shm.is_none() {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<WlShm>()))?
        }

        event_queue
            .roundtrip(&mut manager)
            .map_err(Error::WaylandDispatch)?;

        Ok(manager)
    }

    /// capture a single frame buffer of an output
    ///
    /// `overlay_cursor` composites the hardware cursor into the frame.
    pub fn capture_output(
        &mut self,
        output: &WlOutput,
        overlay_cursor: bool,
    ) -> Result<Buffer, Error> {
        let Some(zwlr_manager) = &self.manager else {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ZwlrScreencopyManagerV1,
            >()))?
        };

        let frame = Arc::new(Mutex::new(Frame::default()));
        let mut event_queue = self.connection.new_event_queue();
        let handle = event_queue.handle();
        let zwlr_frame = zwlr_manager.capture_output(
            i32::from(overlay_cursor),
            output,
            &handle,
            Arc::downgrade(&frame),
        );
        self.finish_capture(frame, zwlr_frame, &mut event_queue)
    }

    /// capture a selected region of an output
    ///
    /// `overlay_cursor` composites the hardware cursor into the frame.
    pub fn capture_output_region(
        &mut self,
        output: &WlOutput,
        x: i32,
        y: i32,
        width: i32,
        height: i32,
        overlay_cursor: bool,
    ) -> Result<Buffer, Error> {
        let Some(zwlr_manager) = &self.manager else {
            Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ZwlrScreencopyManagerV1,
            >()))?
        };

        let frame = Arc::new(Mutex::new(Frame::default()));
        let mut event_queue = self.connection.new_event_queue();
        let handle = event_queue.handle();
        let zwlr_frame = zwlr_manager.capture_output_region(
            i32::from(overlay_cursor),
            output,
            x,
            y,
            width,
            height,
            &handle,
            Arc::downgrade(&frame),
        );
        self.finish_capture(frame, zwlr_frame, &mut event_queue)
    }

    /// Whether the compositor advertised `zwp_linux_dmabuf_v1`, i.e. whether
    /// the zero-copy [`capture_output_into`](Self::capture_output_into) path is
    /// usable.
    #[must_use]
    pub fn supports_dmabuf(&self) -> bool {
        self.linux_dmabuf.is_some()
    }

    /// Imports an allocated [`DmaBuffer`] as a `wl_buffer` the compositor can
    /// blit a captured frame into. The returned buffer can be reused across
    /// many captures.
    ///
    /// # Errors
    ///
    /// Returns an error if dmabuf is unavailable or the import round-trip fails.
    pub fn import_dmabuf(&mut self, dma: &DmaBuffer) -> Result<WlBuffer, Error> {
        let Some(dmabuf) = self.linux_dmabuf.clone() else {
            return Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ZwpLinuxDmabufV1,
            >()));
        };

        let mut event_queue = self.connection.new_event_queue();
        let handle = event_queue.handle();

        let modifier: u64 = dma.modifier.into();
        let params = dmabuf.create_params(&handle, ());
        params.add(
            dma.fd.as_fd(),
            0,
            dma.offset,
            dma.stride,
            (modifier >> 32) as u32,
            (modifier & 0xFFFF_FFFF) as u32,
        );
        let buffer = params.create_immed(
            dma.width as i32,
            dma.height as i32,
            dma.format as u32,
            zwp_linux_buffer_params_v1::Flags::empty(),
            &handle,
            (),
        );
        params.destroy();
        // Flush the import requests so the buffer exists server-side before the
        // first capture references it.
        event_queue
            .roundtrip(self)
            .map_err(Error::WaylandDispatch)?;
        Ok(buffer)
    }

    /// Captures `output` straight into a caller-provided dmabuf `wl_buffer`
    /// (from [`import_dmabuf`](Self::import_dmabuf)). The compositor blits into
    /// GPU memory — no pixel copy crosses the CPU.
    ///
    /// # Errors
    ///
    /// Returns [`Error::Failed`] if the compositor reports the copy failed.
    pub fn capture_output_into(
        &mut self,
        output: &WlOutput,
        buffer: &WlBuffer,
        overlay_cursor: bool,
    ) -> Result<(), Error> {
        let Some(zwlr_manager) = &self.manager else {
            return Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ZwlrScreencopyManagerV1,
            >()));
        };
        let state = Arc::new(Mutex::new(DmaCapture::default()));
        let mut event_queue = self.connection.new_event_queue();
        let handle = event_queue.handle();
        let weak: Weak<Mutex<DmaCapture>> = Arc::downgrade(&state);
        let zwlr_frame = zwlr_manager.capture_output(i32::from(overlay_cursor), output, &handle, weak);
        self.finish_capture_into(&state, buffer, zwlr_frame, &mut event_queue)
    }

    /// Region variant of [`capture_output_into`](Self::capture_output_into).
    ///
    /// # Errors
    ///
    /// Returns [`Error::Failed`] if the compositor reports the copy failed.
    #[allow(clippy::too_many_arguments)]
    pub fn capture_output_region_into(
        &mut self,
        output: &WlOutput,
        x: i32,
        y: i32,
        width: i32,
        height: i32,
        buffer: &WlBuffer,
        overlay_cursor: bool,
    ) -> Result<(), Error> {
        let Some(zwlr_manager) = &self.manager else {
            return Err(Error::ProtocolNotAvailable(std::any::type_name::<
                ZwlrScreencopyManagerV1,
            >()));
        };
        let state = Arc::new(Mutex::new(DmaCapture::default()));
        let mut event_queue = self.connection.new_event_queue();
        let handle = event_queue.handle();
        let weak: Weak<Mutex<DmaCapture>> = Arc::downgrade(&state);
        let zwlr_frame = zwlr_manager.capture_output_region(
            i32::from(overlay_cursor),
            output,
            x,
            y,
            width,
            height,
            &handle,
            weak,
        );
        self.finish_capture_into(&state, buffer, zwlr_frame, &mut event_queue)
    }

    /// Drives a `capture_*_into` frame to completion: wait for `buffer_done`,
    /// issue the `copy` into `buffer`, wait for `ready`/`failed`.
    fn finish_capture_into(
        &mut self,
        state: &Arc<Mutex<DmaCapture>>,
        buffer: &WlBuffer,
        zwlr_frame: ZwlrScreencopyFrameV1,
        event_queue: &mut EventQueue<OutputManager>,
    ) -> Result<(), Error> {
        let result = loop {
            if let Err(err) = event_queue.blocking_dispatch(self) {
                break Err(Error::WaylandDispatch(err));
            }
            let mut current = state.lock().expect("lock should not be poisoned");
            if current.failed {
                break Err(Error::Failed);
            }
            if current.ready {
                break Ok(());
            }
            if current.can_copy && !current.requested {
                zwlr_frame.copy(buffer);
                current.requested = true;
            }
        };
        zwlr_frame.destroy();
        result
    }

    fn finish_capture(
        &mut self,
        frame: Arc<Mutex<Frame>>,
        zwlr_frame: ZwlrScreencopyFrameV1,
        event_queue: &mut EventQueue<OutputManager>,
    ) -> Result<Buffer, Error> {
        loop {
            if let Err(err) = event_queue.blocking_dispatch(self) {
                Err(Error::WaylandDispatch(err))?;
            }
            let frame = frame.clone();
            let mut current = frame.lock().expect("lock should not be poisoned");
            match (
                current.ready,
                current.requested,
                &current.error,
                &current.buffer,
            ) {
                (_, _, Some(_), _) | (true, _, _, Some(_)) => {
                    zwlr_frame.destroy();
                    break;
                }
                (false, false, _, Some(buffer)) => {
                    zwlr_frame.copy(&buffer.buffer);
                    current.requested = true;
                }
                _ => continue,
            };
        }

        match Arc::into_inner(frame) {
            Some(frame) => {
                let frame = frame.into_inner().expect("lock should not be poisoned");
                if let Some(err) = frame.error {
                    return Err(err);
                }
                if let Some(buffer) = frame.buffer {
                    Ok(buffer)
                } else {
                    unreachable!("we only exit the loop when buffer or error is some")
                }
            }
            None => {
                unreachable!("we only exit the loop after waiting blockingly for all dispatchers")
            }
        }
    }
}

impl Dispatch<wl_registry::WlRegistry, ()> for OutputManager {
    fn event(
        state: &mut Self,
        registry: &wl_registry::WlRegistry,
        event: <wl_registry::WlRegistry as wayland_client::Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        handle: &wayland_client::QueueHandle<Self>,
    ) {
        if let wl_registry::Event::Global {
            name,
            interface,
            version,
        } = event
        {
            match interface.as_str() {
                "wl_shm" => {
                    let shm: WlShm = registry.bind(name, version, handle, ());
                    state.shm = Some(shm);
                }
                "zwlr_screencopy_manager_v1" => {
                    let manager: ZwlrScreencopyManagerV1 = registry.bind(name, version, handle, ());
                    state.manager = Some(manager);
                }
                "zwp_linux_dmabuf_v1" => {
                    // v3+ for `create_immed` with a modifier; cap at the
                    // protocol's max so newer compositors still bind.
                    let dmabuf: ZwpLinuxDmabufV1 =
                        registry.bind(name, version.min(4), handle, ());
                    state.linux_dmabuf = Some(dmabuf);
                }
                "wl_output" => {
                    let output: WlOutput = registry.bind(name, version, handle, ());
                    state.outputs.push((output, Output::default()));
                }
                _ => {}
            }
        }
    }
}

impl Dispatch<wl_output::WlOutput, ()> for OutputManager {
    fn event(
        state: &mut Self,
        _proxy: &wl_output::WlOutput,
        event: <wl_output::WlOutput as wayland_client::Proxy>::Event,
        _data: &(),
        _conn: &wayland_client::Connection,
        _qhandle: &wayland_client::QueueHandle<Self>,
    ) {
        let (_, output) = &mut state.outputs[state.intialized_outputs as usize];

        match event {
            wl_output::Event::Geometry {
                x,
                y,
                physical_width,
                physical_height,
                subpixel,
                make,
                model,
                transform,
            } => {
                let geometry = Geometry {
                    x,
                    y,
                    physical_width,
                    physical_height,
                    make,
                    model,
                    subpixel: subpixel.into_result().expect("should be valid subpixel"),
                    transform: transform.into_result().expect("should be valid transform"),
                };
                output.geometry = Some(geometry);
            }
            wl_output::Event::Mode {
                flags,
                width,
                height,
                refresh,
            } => {
                let mode = OutputMode {
                    mode: flags.into_result().expect("should be valid mode"),
                    width,
                    height,
                    refresh,
                };
                output.mode = Some(mode)
            }
            wl_output::Event::Scale { factor } => output.scale = Some(factor),
            wl_output::Event::Name { name } => output.name = Some(name),
            wl_output::Event::Description { description } => output.description = Some(description),
            wl_output::Event::Done => state.intialized_outputs += 1,
            _ => {}
        }
    }
}

impl Dispatch<ZwlrScreencopyFrameV1, Weak<Mutex<Frame>>> for OutputManager {
    fn event(
        state: &mut Self,
        _proxy: &ZwlrScreencopyFrameV1,
        event: <ZwlrScreencopyFrameV1 as wayland_client::Proxy>::Event,
        data: &Weak<Mutex<Frame>>,
        _conn: &wayland_client::Connection,
        qhandle: &wayland_client::QueueHandle<Self>,
    ) {
        let Some(data) = data.upgrade() else {
            log::debug!(
                "dispatcher for ZwlrScreencopyFrameV1 was called with event {event:?} but frame was already dropped"
            );
            return;
        };
        let mut frame = data.lock().expect("lock should not be poisoned");
        match event {
            zwlr_screencopy_frame_v1::Event::Buffer {
                format,
                width,
                height,
                stride,
            } => {
                let format = match format.into_result() {
                    Ok(format) => format,
                    Err(err) => return frame.error = Some(Error::ProtocolInvalidEnum(err)),
                };
                if let Some(shm) = &state.shm {
                    match Buffer::new(shm, width, height, stride, format, qhandle, ()) {
                        Ok(buffer) => frame.buffer = Some(buffer),
                        Err(err) => frame.error = Some(err),
                    }
                } else {
                    frame.error = Some(Error::ProtocolNotAvailable(std::any::type_name::<WlShm>()));
                }
            }
            zwlr_screencopy_frame_v1::Event::Flags { .. } => {}
            zwlr_screencopy_frame_v1::Event::Ready { .. } => {
                frame.ready = true;
            }
            zwlr_screencopy_frame_v1::Event::Failed => frame.error = Some(Error::Failed),
            zwlr_screencopy_frame_v1::Event::Damage { .. } => {}
            zwlr_screencopy_frame_v1::Event::LinuxDmabuf { .. } => {}
            zwlr_screencopy_frame_v1::Event::BufferDone => {}
            _ => {}
        }
    }
}

impl Dispatch<ZwlrScreencopyFrameV1, Weak<Mutex<DmaCapture>>> for OutputManager {
    fn event(
        _state: &mut Self,
        _proxy: &ZwlrScreencopyFrameV1,
        event: <ZwlrScreencopyFrameV1 as wayland_client::Proxy>::Event,
        data: &Weak<Mutex<DmaCapture>>,
        _conn: &wayland_client::Connection,
        _qhandle: &wayland_client::QueueHandle<Self>,
    ) {
        let Some(data) = data.upgrade() else {
            return;
        };
        let mut capture = data.lock().expect("lock should not be poisoned");
        match event {
            // Any of these means constraints are advertised; `buffer_done` is
            // the canonical "ready to copy" signal, but copying after the first
            // constraint event is safe and also works on pre-v3 compositors.
            zwlr_screencopy_frame_v1::Event::Buffer { .. }
            | zwlr_screencopy_frame_v1::Event::LinuxDmabuf { .. }
            | zwlr_screencopy_frame_v1::Event::BufferDone => capture.can_copy = true,
            zwlr_screencopy_frame_v1::Event::Ready { .. } => capture.ready = true,
            zwlr_screencopy_frame_v1::Event::Failed => capture.failed = true,
            zwlr_screencopy_frame_v1::Event::Flags { .. }
            | zwlr_screencopy_frame_v1::Event::Damage { .. } => {}
            _ => {}
        }
    }
}

delegate_noop!(OutputManager: ignore WlShm);
delegate_noop!(OutputManager: ignore WlShmPool);
delegate_noop!(OutputManager: ignore WlBuffer);
delegate_noop!(OutputManager: ignore ZwlrScreencopyManagerV1);
delegate_noop!(OutputManager: ignore ZwpLinuxDmabufV1);
delegate_noop!(OutputManager: ignore ZwpLinuxBufferParamsV1);

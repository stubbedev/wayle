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

use crate::{
    Frame,
    buffer::{Buffer, DmabufBacking},
    dmabuf::{self, GbmDevice},
    error::Error,
};

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
    /// `zwp_linux_dmabuf_v1`, bound when the compositor exposes it. Required for
    /// the dmabuf zero-copy path; its absence forces the SHM fallback.
    linux_dmabuf: Option<ZwpLinuxDmabufV1>,
    /// gbm device opened lazily on the first dmabuf capture attempt, on a DRM
    /// render node. Shared across captures. `None` until first use; an opened
    /// device is wrapped in `Arc` so the `Clone` impl stays cheap.
    gbm: Option<Arc<GbmDevice>>,
    /// Set once we have tried (and failed) to open a gbm device, so we don't
    /// retry every frame and keep falling back to SHM cleanly.
    gbm_failed: bool,
    /// Cached dmabuf format `(drm_fourcc, width, height)` learned from the first
    /// dmabuf probe. `Some(Some(..))` = probed and dmabuf offered; `Some(None)`
    /// = probed and no dmabuf offered (stay on SHM forever); `None` = not yet
    /// probed. The format is stable per output, so we probe once and skip the
    /// extra screencopy pass on every later frame.
    dmabuf_probed: Option<Option<(u32, u32, u32)>>,
    /// Reusable SHM capture buffer for the continuous-capture (PipeWire) path,
    /// so each frame leases a shared `wl_buffer`/memfd instead of allocating a
    /// fresh memfd + pool + `wl_buffer`. Reallocated only when the frame
    /// geometry/format changes; `None` until the first SHM capture.
    shm_cache: Option<crate::buffer::ShmSlot>,
    pub outputs: Vec<(WlOutput, Output)>,
    /// Bound `zwlr_screencopy_manager_v1` version. `buffer_done` /
    /// `linux_dmabuf` are v3+; below that we decide on the first `buffer` event
    /// (no dmabuf path exists pre-v3 anyway).
    screencopy_version: u32,
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
            gbm: None,
            gbm_failed: false,
            dmabuf_probed: None,
            shm_cache: None,
            outputs: Vec::new(),
            screencopy_version: 0,
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

    /// capture a single frame buffer of an output (cursor not composited)
    pub fn capture_output(&mut self, output: &WlOutput) -> Result<Buffer, Error> {
        self.capture_output_with_cursor(output, false)
    }

    /// capture a single frame buffer of an output, optionally compositing the
    /// cursor into the frame (`overlay_cursor`). The wlr-screencopy
    /// `overlay_cursor` argument is `1` when the cursor should be embedded.
    pub fn capture_output_with_cursor(
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

    /// capture a selected region of an output (cursor not composited)
    pub fn capture_output_region(
        &mut self,
        output: &WlOutput,
        x: i32,
        y: i32,
        width: i32,
        height: i32,
    ) -> Result<Buffer, Error> {
        self.capture_output_region_with_cursor(output, x, y, width, height, false)
    }

    /// capture a selected region of an output, optionally compositing the cursor.
    pub fn capture_output_region_with_cursor(
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

    /// Attempt a dmabuf (zero-copy) capture of `output`, falling back to SHM on
    /// any failure.
    ///
    /// This is **best-effort and unverified on hardware**: if `zwp_linux_dmabuf`
    /// is unavailable, no gbm render node can be opened, the compositor never
    /// advertises a dmabuf format for the frame, or allocation / import /
    /// copy fails at any point, this returns a plain SHM [`Buffer`] captured the
    /// same way [`capture_output_with_cursor`](Self::capture_output_with_cursor)
    /// would — so capture never regresses.
    ///
    /// The returned [`Buffer`] has `dmabuf: Some(..)` only when the zero-copy
    /// path fully succeeded; otherwise it is a normal SHM buffer.
    pub fn capture_output_dmabuf_or_shm(
        &mut self,
        output: &WlOutput,
        overlay_cursor: bool,
    ) -> Result<Buffer, Error> {
        match self.try_capture_output_dmabuf(output, overlay_cursor) {
            Ok(buffer) => Ok(buffer),
            Err(err) => {
                // SHM is the high-CPU path (full GPU readback + memcpy per frame),
                // so make the reason dmabuf was declined visible by default rather
                // than burying it at debug — it's the answer to "why is screencast
                // pegging a core?".
                log::warn!("dmabuf capture unavailable ({err}); falling back to SHM (higher CPU)");
                self.capture_output_with_cursor(output, overlay_cursor)
            }
        }
    }

    /// One dmabuf capture attempt. Errors (rather than falling back internally)
    /// so the caller can cleanly drop to SHM.
    ///
    /// Allocate a dmabuf-backed [`Buffer`] for `output` WITHOUT capturing into
    /// it — a gbm bo imported as a `wl_buffer`, ready to be the target of a
    /// later [`recapture_output_dmabuf`](Self::recapture_output_dmabuf). This is
    /// the allocation half used by the PipeWire `add_buffer` path, where one
    /// such buffer is bound to each PipeWire buffer for the stream's lifetime.
    ///
    /// The dmabuf format is learned (and cached) once via
    /// [`probe_dmabuf_format`](Self::probe_dmabuf_format).
    ///
    /// # Errors
    ///
    /// Returns an error if dmabuf is unavailable, the gbm allocation fails, or
    /// the `wl_buffer` import is rejected.
    pub fn allocate_output_dmabuf(
        &mut self,
        output: &WlOutput,
        overlay_cursor: bool,
    ) -> Result<Buffer, Error> {
        // Protocol + device preconditions; any miss -> SHM fallback.
        let zwlr_manager =
            self.manager
                .clone()
                .ok_or(Error::ProtocolNotAvailable(std::any::type_name::<
                    ZwlrScreencopyManagerV1,
                >()))?;
        let linux_dmabuf = self
            .linux_dmabuf
            .clone()
            .ok_or_else(|| Error::DmabufUnavailable("zwp_linux_dmabuf_v1 not bound".into()))?;
        let gbm = self.ensure_gbm()?;

        // Learn (and cache) the dmabuf format the compositor offers for this
        // output. The format is stable per output, so we probe once with an
        // extra screencopy pass and reuse it for every later frame.
        let (drm_fourcc, width, height) =
            match self.probe_dmabuf_format(&zwlr_manager, output, overlay_cursor)? {
                Some(fmt) => fmt,
                None => {
                    return Err(Error::DmabufUnavailable(
                        "compositor advertised no dmabuf format".into(),
                    ));
                }
            };

        // Allocate a gbm bo for the advertised format and import it as a
        // dmabuf-backed wl_buffer. The empty modifier list lets the helper use
        // its safe fallbacks (driver-chosen / linear); we have no compositor
        // modifier list from wlr-screencopy.
        let alloc = dmabuf::allocate(&gbm, drm_fourcc, width, height, &[])?;

        let mut params_queue = self.connection.new_event_queue();
        let params_handle = params_queue.handle();
        let params_state = Arc::new(Mutex::new(ParamsState::default()));
        let params = linux_dmabuf.create_params(&params_handle, Arc::downgrade(&params_state));
        let modifier = alloc.modifier;
        for (idx, fd) in alloc.owned_fds.iter().enumerate() {
            params.add(
                fd.as_fd(),
                idx as u32,
                alloc.offsets.get(idx).copied().unwrap_or(0),
                alloc.strides.get(idx).copied().unwrap_or(0),
                (modifier >> 32) as u32,
                (modifier & 0xffff_ffff) as u32,
            );
        }
        let shm_format = dmabuf::shm_format_from_fourcc(drm_fourcc).ok_or_else(|| {
            Error::DmabufUnavailable(format!("no wl_shm format for fourcc {drm_fourcc:#x}"))
        })?;
        // create_immed (since v2) builds the wl_buffer synchronously; a bad
        // import is reported with a `failed` event on the params object, which
        // we drain with one roundtrip below before trusting the buffer.
        let wl_buffer = params.create_immed(
            width as i32,
            height as i32,
            drm_fourcc,
            zwp_linux_buffer_params_v1::Flags::empty(),
            &params_handle,
            (),
        );
        params_queue
            .roundtrip(self)
            .map_err(Error::WaylandDispatch)?;
        params.destroy();
        if params_state
            .lock()
            .expect("lock should not be poisoned")
            .failed
        {
            wl_buffer.destroy();
            return Err(Error::DmabufUnavailable(
                "dmabuf import was rejected".into(),
            ));
        }

        let stride0 = alloc.strides.first().copied().unwrap_or(width * 4);
        let backing = DmabufBacking::new(
            drm_fourcc,
            alloc.modifier,
            alloc.owned_fds,
            &alloc.offsets,
            &alloc.strides,
        );
        // `alloc.bo` is deliberately left owned by `alloc` (dropped at function
        // end, after the capture below): the dmabuf fds + `wl_buffer` keep the
        // kernel buffer alive, and the bo is `!Send` so it must not enter the
        // `Buffer`/`Frame` graph used as Wayland dispatch userdata.
        let buffer = Buffer::from_dmabuf(
            wl_buffer.clone(),
            width,
            height,
            stride0,
            shm_format,
            backing,
        );
        // `alloc.bo` is deliberately left owned by `alloc` (dropped at function
        // end): the dmabuf fds + `wl_buffer` keep the kernel buffer alive, and
        // the bo is `!Send` so it must not enter the `Buffer`/`Frame` graph.
        Ok(buffer)
    }

    /// Allocate a dmabuf-backed [`Buffer`] and capture one frame into it. The
    /// per-frame fallback path ([`capture_output_dmabuf_or_shm`]); the PipeWire
    /// producer instead allocates once via
    /// [`allocate_output_dmabuf`](Self::allocate_output_dmabuf) and recaptures.
    fn try_capture_output_dmabuf(
        &mut self,
        output: &WlOutput,
        overlay_cursor: bool,
    ) -> Result<Buffer, Error> {
        let mut buffer = self.allocate_output_dmabuf(output, overlay_cursor)?;
        self.recapture_output_dmabuf(output, overlay_cursor, &mut buffer)?;
        Ok(buffer)
    }

    /// Re-capture into an already-allocated dmabuf [`Buffer`], reusing its gbm
    /// bo + `wl_buffer` instead of allocating fresh ones. Runs a single
    /// screencopy pass that copies into `buf`'s existing dmabuf `wl_buffer` and
    /// refreshes its per-frame damage; no `dmabuf::allocate` / `create_immed`
    /// occurs. This is the steady-state path for the continuous dmabuf producer,
    /// turning per-frame GPU allocation + import into a plain copy.
    ///
    /// Fencing is the caller's responsibility: only reuse a buffer the consumer
    /// has finished with (the producer recycles the oldest of `KEEPALIVE_FRAMES`
    /// buffers, the same buffer the previous design dropped at that point).
    ///
    /// # Errors
    ///
    /// Returns an error if the screencopy manager is unavailable, `buf` is not
    /// dmabuf-backed, or the copy fails.
    pub fn recapture_output_dmabuf(
        &mut self,
        output: &WlOutput,
        overlay_cursor: bool,
        buf: &mut Buffer,
    ) -> Result<(), Error> {
        if buf.dmabuf.is_none() {
            return Err(Error::DmabufUnavailable(
                "recapture target is not dmabuf-backed".into(),
            ));
        }
        let zwlr_manager =
            self.manager
                .clone()
                .ok_or(Error::ProtocolNotAvailable(std::any::type_name::<
                    ZwlrScreencopyManagerV1,
                >()))?;
        let wl_buffer = buf.buffer.clone();

        let frame = Arc::new(Mutex::new(Frame::default()));
        let mut copy_queue = self.connection.new_event_queue();
        let copy_handle = copy_queue.handle();
        let zwlr_frame = zwlr_manager.capture_output(
            i32::from(overlay_cursor),
            output,
            &copy_handle,
            Arc::downgrade(&frame),
        );

        // Copy into the caller's existing dmabuf wl_buffer (already allocated),
        // so the target is fixed regardless of the advertised frame state.
        let res = self.drive_screencopy(&frame, &zwlr_frame, &mut copy_queue, move |_| {
            Some(wl_buffer)
        });
        zwlr_frame.destroy();
        res.map_err(|e| Error::DmabufUnavailable(format!("dmabuf recapture failed: {e}")))?;
        buf.damage = std::mem::take(&mut frame.lock().expect("lock should not be poisoned").damage);
        Ok(())
    }

    /// Learns the dmabuf format the compositor offers for `output`, caching the
    /// result so the extra screencopy pass runs at most once per manager.
    ///
    /// Returns `Some((fourcc, w, h))` when a dmabuf format is offered, `None`
    /// when the compositor advertises only SHM (so the caller should stay on the
    /// SHM path for good).
    fn probe_dmabuf_format(
        &mut self,
        zwlr_manager: &ZwlrScreencopyManagerV1,
        output: &WlOutput,
        overlay_cursor: bool,
    ) -> Result<Option<(u32, u32, u32)>, Error> {
        if let Some(cached) = self.dmabuf_probed {
            return Ok(cached);
        }

        // Probe pass: capture but never copy; once the formats are advertised,
        // read whether a dmabuf format was offered, then drop the frame object.
        let probe = Arc::new(Mutex::new(Frame::default()));
        let mut probe_queue = self.connection.new_event_queue();
        let probe_handle = probe_queue.handle();
        let probe_frame = zwlr_manager.capture_output(
            i32::from(overlay_cursor),
            output,
            &probe_handle,
            Arc::downgrade(&probe),
        );
        let res = self.drive_until_advertised(&probe, &mut probe_queue);
        probe_frame.destroy();
        // A probe error is transient; do not cache it.
        res.map_err(|e| Error::DmabufUnavailable(format!("probe failed: {e}")))?;

        let result = probe
            .lock()
            .expect("lock should not be poisoned")
            .dmabuf_format;
        self.dmabuf_probed = Some(result);
        Ok(result)
    }

    /// Open the gbm device on a DRM render node lazily, caching success and
    /// failure so we try at most once.
    fn ensure_gbm(&mut self) -> Result<Arc<GbmDevice>, Error> {
        if let Some(gbm) = &self.gbm {
            return Ok(gbm.clone());
        }
        if self.gbm_failed {
            return Err(Error::DmabufUnavailable("gbm device unavailable".into()));
        }
        match dmabuf::open_gbm_device() {
            Ok(dev) => {
                let dev = Arc::new(dev);
                self.gbm = Some(dev.clone());
                Ok(dev)
            }
            Err(err) => {
                self.gbm_failed = true;
                Err(err)
            }
        }
    }

    /// Dispatch `queue` until the compositor finishes advertising buffer
    /// formats (`buffer_done` on v3+, else the `buffer` event) or reports an
    /// error. Shared prelude of every screencopy drive loop: deciding before
    /// this point races the `buffer`/`linux_dmabuf`/`buffer_done` sequence (the
    /// two format events arrive in unspecified order), which can miss the
    /// dmabuf format and silently disable the zero-copy path. On return the
    /// frame's `dmabuf_format` / `buffer` state is final.
    fn drive_until_advertised(
        &mut self,
        frame: &Arc<Mutex<Frame>>,
        queue: &mut EventQueue<OutputManager>,
    ) -> Result<(), Error> {
        let v3 = self.screencopy_version >= 3;
        loop {
            queue
                .blocking_dispatch(self)
                .map_err(Error::WaylandDispatch)?;
            let mut f = frame.lock().expect("lock should not be poisoned");
            if let Some(err) = f.error.take() {
                return Err(err);
            }
            let advertised = if v3 {
                f.buffer_done
            } else {
                f.buffer.is_some()
            };
            if advertised {
                return Ok(());
            }
        }
    }

    /// Drive a full screencopy capture: wait for format advertisement
    /// ([`drive_until_advertised`](Self::drive_until_advertised)), send one
    /// `copy_with_damage` into the buffer `target` selects from the advertised
    /// frame state, then dispatch until the frame is `Ready`.
    ///
    /// `target` picks the destination `wl_buffer` — the frame's own SHM lease
    /// or a caller-owned dmabuf buffer. `copy_with_damage` (v2+) is required for
    /// the compositor to emit `damage` events; a plain `copy` never reports any.
    fn drive_screencopy<F>(
        &mut self,
        frame: &Arc<Mutex<Frame>>,
        zwlr_frame: &ZwlrScreencopyFrameV1,
        queue: &mut EventQueue<OutputManager>,
        target: F,
    ) -> Result<(), Error>
    where
        F: FnOnce(&Frame) -> Option<WlBuffer>,
    {
        self.drive_until_advertised(frame, queue)?;

        let wl_buffer = {
            let f = frame.lock().expect("lock should not be poisoned");
            target(&f).ok_or(Error::Failed)?
        };
        zwlr_frame.copy_with_damage(&wl_buffer);

        loop {
            queue
                .blocking_dispatch(self)
                .map_err(Error::WaylandDispatch)?;
            let mut f = frame.lock().expect("lock should not be poisoned");
            if let Some(err) = f.error.take() {
                return Err(err);
            }
            if f.ready {
                return Ok(());
            }
        }
    }

    fn finish_capture(
        &mut self,
        frame: Arc<Mutex<Frame>>,
        zwlr_frame: ZwlrScreencopyFrameV1,
        event_queue: &mut EventQueue<OutputManager>,
    ) -> Result<Buffer, Error> {
        let res = self.drive_screencopy(&frame, &zwlr_frame, event_queue, |f| {
            f.buffer.as_ref().map(|b| b.buffer.clone())
        });
        zwlr_frame.destroy();
        res?;

        let mut frame = Arc::into_inner(frame)
            .expect("sole owner after capture")
            .into_inner()
            .expect("lock should not be poisoned");
        let mut buffer = frame.buffer.take().expect("Ready implies a buffer");
        // Carry the per-frame damage rects onto the returned buffer.
        buffer.damage = frame.damage;
        Ok(buffer)
    }
}

/// Tracks the outcome of a `zwp_linux_buffer_params_v1` import. With
/// `create_immed` the success path is silent; only a `failed` event is
/// meaningful (it means the wl_buffer is invalid and we must fall back).
#[derive(Default)]
struct ParamsState {
    failed: bool,
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
                    state.screencopy_version = version;
                }
                "zwp_linux_dmabuf_v1" => {
                    // Bind at most v3 (the version we rely on: create_immed is
                    // v2, modifier event is v3). Binding the advertised version
                    // is fine since we only use v2 requests.
                    let dmabuf: ZwpLinuxDmabufV1 = registry.bind(name, version.min(4), handle, ());
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
        proxy: &wl_output::WlOutput,
        event: <wl_output::WlOutput as wayland_client::Proxy>::Event,
        _data: &(),
        _conn: &wayland_client::Connection,
        _qhandle: &wayland_client::QueueHandle<Self>,
    ) {
        // Route by the event's own proxy. Indexing by a `Done`-counter assumed
        // every output's events arrive strictly grouped in bind order and that
        // no output ever emits a later event (mode/scale change, hotplug) — a
        // late event would corrupt a sibling's data or panic out of bounds.
        let Some((_, output)) = state.outputs.iter_mut().find(|(o, _)| o == proxy) else {
            return;
        };

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
            wl_output::Event::Done => {}
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
                if let Some(shm) = state.shm.clone() {
                    // Reuse the cached slot when the geometry/format is
                    // unchanged; otherwise (dis)allocate and rebuild it. The
                    // lease shares the slot's wl_buffer + memfd, so no per-frame
                    // memfd/pool/wl_buffer allocation occurs in steady state.
                    let reusable = state
                        .shm_cache
                        .as_ref()
                        .is_some_and(|s| s.matches(width, height, stride, format));
                    if !reusable {
                        if let Some(old) = state.shm_cache.take() {
                            old.destroy();
                        }
                        match crate::buffer::ShmSlot::new(
                            &shm,
                            width,
                            height,
                            stride,
                            format,
                            qhandle,
                            (),
                        ) {
                            Ok(slot) => state.shm_cache = Some(slot),
                            Err(err) => {
                                frame.error = Some(err);
                                return;
                            }
                        }
                    }
                    frame.buffer = state.shm_cache.as_ref().map(crate::buffer::ShmSlot::lease);
                } else {
                    frame.error = Some(Error::ProtocolNotAvailable(std::any::type_name::<WlShm>()));
                }
            }
            zwlr_screencopy_frame_v1::Event::Flags { .. } => {}
            zwlr_screencopy_frame_v1::Event::Ready { .. } => {
                frame.ready = true;
            }
            zwlr_screencopy_frame_v1::Event::Failed => frame.error = Some(Error::Failed),
            zwlr_screencopy_frame_v1::Event::Damage {
                x,
                y,
                width,
                height,
            } => {
                // Accumulate the damaged region; the consumer maps these to the
                // SPA_META_VideoDamage meta. Falls back to whole-frame downstream
                // when none are reported.
                frame.damage.push((x, y, width, height));
            }
            zwlr_screencopy_frame_v1::Event::LinuxDmabuf {
                format,
                width,
                height,
            } => {
                // Learn the dmabuf format the compositor offers for this frame so
                // the dmabuf path can allocate a matching gbm bo. `format` is a
                // DRM fourcc.
                frame.dmabuf_format = Some((format, width, height));
            }
            zwlr_screencopy_frame_v1::Event::BufferDone => {
                frame.buffer_done = true;
            }
            _ => {}
        }
    }
}

impl Dispatch<ZwpLinuxBufferParamsV1, Weak<Mutex<ParamsState>>> for OutputManager {
    fn event(
        _state: &mut Self,
        _proxy: &ZwpLinuxBufferParamsV1,
        event: <ZwpLinuxBufferParamsV1 as wayland_client::Proxy>::Event,
        data: &Weak<Mutex<ParamsState>>,
        _conn: &wayland_client::Connection,
        _qhandle: &wayland_client::QueueHandle<Self>,
    ) {
        let Some(data) = data.upgrade() else {
            return;
        };
        let mut state = data.lock().expect("lock should not be poisoned");
        if let zwp_linux_buffer_params_v1::Event::Failed = event {
            state.failed = true;
        }
    }
}

delegate_noop!(OutputManager: ignore WlShm);
delegate_noop!(OutputManager: ignore WlShmPool);
delegate_noop!(OutputManager: ignore WlBuffer);
delegate_noop!(OutputManager: ignore ZwlrScreencopyManagerV1);
delegate_noop!(OutputManager: ignore ZwpLinuxDmabufV1);

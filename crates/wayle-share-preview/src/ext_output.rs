//! Continuous, damage-driven whole-output capture via `ext-image-copy-capture`.
//!
//! Unlike [`crate::output`] (wlr-screencopy), which creates a fresh frame —
//! re-advertising buffer constraints with a blocking roundtrip — on *every*
//! capture, this opens a **persistent capture session** once and then only
//! creates lightweight per-frame objects. The session model has two decisive
//! advantages for a continuous screencast:
//!
//! - **No per-frame re-advertise roundtrip** → lower latency, higher sustainable
//!   frame rate.
//! - **Damage-driven**: `ext_image_copy_capture_frame_v1::capture` does not
//!   complete until the source content changes, so a *static* screen costs zero
//!   copies (and near-zero CPU) instead of a full readback every tick.
//!
//! Because `capture` may block indefinitely waiting for damage, it cannot run
//! inside the PipeWire `process` callback (that would stall the producer loop).
//! Instead this module owns a **dedicated capture thread** that loops
//! create-frame → capture → wait-ready → publish-latest; the PipeWire producer
//! reads the most recent ready frame via [`ExtOutputCapture::latest`] and copies
//! it into its own buffer.
//!
//! ## Transport: SHM, by design
//!
//! Frames are captured into a small **ring** of SHM [`Buffer`]s. The producer
//! copies (`read_into`) the latest ready slot into a PipeWire buffer. A
//! decoupled capture thread necessarily rotates which buffer holds the freshest
//! frame, and [`crate::output`] documents that rotating a *dmabuf* bo across
//! PipeWire buffers stalls the consumer — so the zero-copy dmabuf transport
//! belongs to the synchronous wlr path, while this damage-driven path uses an
//! SHM copy that only runs on *changed* frames (keeping CPU low) and gives the
//! consumer a stable per-buffer mapping (no stall).

use std::{
    os::fd::AsRawFd,
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
    thread::JoinHandle,
};

use wayland_client::{
    Connection, Dispatch, EventQueue, Proxy, QueueHandle, WEnum, delegate_noop,
    protocol::{
        wl_buffer::WlBuffer,
        wl_output::{self, Transform, WlOutput},
        wl_registry,
        wl_shm::{Format, WlShm},
        wl_shm_pool::WlShmPool,
    },
};
use wayland_protocols::ext::{
    image_capture_source::v1::client::{
        ext_image_capture_source_v1::ExtImageCaptureSourceV1,
        ext_output_image_capture_source_manager_v1::ExtOutputImageCaptureSourceManagerV1,
    },
    image_copy_capture::v1::client::{
        ext_image_copy_capture_frame_v1::{self, ExtImageCopyCaptureFrameV1},
        ext_image_copy_capture_manager_v1::{self, ExtImageCopyCaptureManagerV1},
        ext_image_copy_capture_session_v1::{self, ExtImageCopyCaptureSessionV1},
    },
};

use crate::{buffer::Buffer, error::Error};

/// Number of capture buffers in the ring. One is being captured into, the
/// just-published one is being read by the consumer, and the spares give slack
/// for a producer that copies a slot while the next capture is already running.
const RING: usize = 3;

/// How long the capture thread blocks waiting for frame events before looping
/// back to re-check the stop flag. Bounds stop latency on a static screen, where
/// `capture` would otherwise wait indefinitely for damage.
const POLL_TIMEOUT_MS: i32 = 50;

/// Cap on negotiation dispatch spins, so a misbehaving compositor cannot hang
/// [`ExtOutputCapture::start`] forever.
const MAX_NEGOTIATE_SPINS: u32 = 100;

/// Geometry/format the stream negotiated, read once from the session
/// constraints. Handed to the PipeWire producer so it can advertise the matching
/// `EnumFormat`.
#[derive(Debug, Clone)]
pub struct ExtStreamInfo {
    pub width: u32,
    pub height: u32,
    pub stride: u32,
    pub format: Format,
    pub refresh_mhz: Option<i32>,
    pub transform: Transform,
}

/// One ready frame published by the capture thread for the PipeWire producer.
///
/// `buffer` is one ring slot; read its pixels via [`Buffer::read_into`]. `seq`
/// lets the consumer skip a frame it has already sent (damage-driven: an
/// unchanged `seq` means nothing new was captured).
pub struct PublishedFrame {
    pub seq: u64,
    pub buffer: Arc<Buffer>,
    pub damage: Vec<(u32, u32, u32, u32)>,
    pub transform: Transform,
    /// Presentation time in nanoseconds (system monotonic), when the compositor
    /// reported it; else `None` and the consumer stamps its own clock.
    pub pts_nanos: Option<u64>,
}

/// Handle to a running ext-image-copy capture thread. Drop stops and joins it.
pub struct ExtOutputCapture {
    info: ExtStreamInfo,
    latest: Arc<Mutex<Option<Arc<PublishedFrame>>>>,
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
}

impl ExtOutputCapture {
    /// Start capturing the output named `output_name`, compositing the cursor
    /// when `show_cursor`. Blocks until the session has negotiated its
    /// constraints and the buffer ring is allocated (so [`info`](Self::info) is
    /// final), then returns with the capture thread running.
    ///
    /// # Errors
    ///
    /// Returns an error if the compositor lacks the `ext-image-copy-capture` /
    /// `ext-image-capture-source` protocols, the output is not found, or the
    /// session fails to negotiate — the caller should then fall back to the
    /// wlr-screencopy path.
    pub fn start(output_name: &str, show_cursor: bool) -> Result<Self, Error> {
        let stop = Arc::new(AtomicBool::new(false));
        let latest: Arc<Mutex<Option<Arc<PublishedFrame>>>> = Arc::new(Mutex::new(None));
        let (tx, rx) = std::sync::mpsc::channel::<Result<ExtStreamInfo, Error>>();

        let thread = {
            let stop = stop.clone();
            let latest = latest.clone();
            let name = output_name.to_owned();
            std::thread::Builder::new()
                .name("wayle-ext-capture".to_owned())
                .spawn(move || capture_thread(&name, show_cursor, &stop, &latest, &tx))
                .map_err(|e| Error::DmabufUnavailable(format!("cannot spawn ext capture: {e}")))?
        };

        match rx.recv() {
            Ok(Ok(info)) => Ok(Self {
                info,
                latest,
                stop,
                thread: Some(thread),
            }),
            Ok(Err(err)) => {
                let _ = thread.join();
                Err(err)
            }
            Err(_) => {
                let _ = thread.join();
                Err(Error::Failed)
            }
        }
    }

    #[must_use]
    pub fn info(&self) -> &ExtStreamInfo {
        &self.info
    }

    /// The most recently captured ready frame, or `None` before the first.
    #[must_use]
    pub fn latest(&self) -> Option<Arc<PublishedFrame>> {
        self.latest
            .lock()
            .expect("lock should not be poisoned")
            .clone()
    }

    /// A cheap, cloneable handle to just the latest-frame slot, for a consumer
    /// (e.g. a PipeWire `process` closure that must be `'static`) that needs to
    /// read frames without borrowing — or owning, and thus stopping — the
    /// capture. The capture thread keeps running as long as the owning
    /// [`ExtOutputCapture`] is alive.
    #[must_use]
    pub fn frame_handle(&self) -> FrameHandle {
        FrameHandle(self.latest.clone())
    }
}

/// Cloneable read handle to the capture thread's latest published frame. Holds
/// no ownership over the thread, so dropping it does not stop capture.
#[derive(Clone)]
pub struct FrameHandle(Arc<Mutex<Option<Arc<PublishedFrame>>>>);

impl FrameHandle {
    /// The most recently captured ready frame, or `None` before the first.
    #[must_use]
    pub fn latest(&self) -> Option<Arc<PublishedFrame>> {
        self.0.lock().expect("lock should not be poisoned").clone()
    }
}

impl Drop for ExtOutputCapture {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
        if let Some(thread) = self.thread.take() {
            let _ = thread.join();
        }
    }
}

/// Per-output metadata gathered from `wl_output`, for the refresh-rate clamp and
/// the `SPA_META_VideoTransform`.
#[derive(Clone)]
struct OutputInfo {
    name: Option<String>,
    refresh_mhz: Option<i32>,
    transform: Transform,
}

impl Default for OutputInfo {
    fn default() -> Self {
        // `wl_output::Transform` has no `Default`; identity is the safe start.
        Self {
            name: None,
            refresh_mhz: None,
            transform: Transform::Normal,
        }
    }
}

/// Buffer constraints advertised by the session, finalized on the `done` event.
#[derive(Default)]
struct SessionState {
    width: u32,
    height: u32,
    shm_format: Option<Format>,
    done: bool,
    stopped: bool,
}

/// Per-frame state filled from `ext_image_copy_capture_frame_v1` events.
#[derive(Default)]
struct FrameState {
    ready: bool,
    failed: bool,
    damage: Vec<(u32, u32, u32, u32)>,
    transform: Option<Transform>,
    pts_nanos: Option<u64>,
}

/// Single Wayland-dispatch target for the capture thread: globals, the resolved
/// outputs, and the live session/frame state.
struct CaptureState {
    shm: Option<WlShm>,
    source_manager: Option<ExtOutputImageCaptureSourceManagerV1>,
    capture_manager: Option<ExtImageCopyCaptureManagerV1>,
    outputs: Vec<(WlOutput, OutputInfo)>,
    session: SessionState,
    frame: FrameState,
}

/// Body of the capture thread. Sets everything up, reports the negotiated
/// [`ExtStreamInfo`] (or an error) over `tx`, then runs the capture loop until
/// `stop`.
fn capture_thread(
    output_name: &str,
    show_cursor: bool,
    stop: &Arc<AtomicBool>,
    latest: &Arc<Mutex<Option<Arc<PublishedFrame>>>>,
    tx: &std::sync::mpsc::Sender<Result<ExtStreamInfo, Error>>,
) {
    let (mut queue, mut state, session, source, ring, info, out_transform) =
        match setup(output_name, show_cursor) {
            Ok(parts) => parts,
            Err(err) => {
                let _ = tx.send(Err(err));
                return;
            }
        };

    if tx.send(Ok(info)).is_err() {
        cleanup(&session, &source, &ring);
        return;
    }

    run_capture_loop(
        &mut queue,
        &mut state,
        &session,
        &ring,
        out_transform,
        stop,
        latest,
    );

    cleanup(&session, &source, &ring);
}

type SetupParts = (
    EventQueue<CaptureState>,
    CaptureState,
    ExtImageCopyCaptureSessionV1,
    ExtImageCaptureSourceV1,
    Vec<Arc<Buffer>>,
    ExtStreamInfo,
    Transform,
);

/// Connect, bind globals, resolve the output, open the session, negotiate
/// constraints, and allocate the SHM buffer ring.
fn setup(output_name: &str, show_cursor: bool) -> Result<SetupParts, Error> {
    let connection = Connection::connect_to_env()
        .map_err(|e| Error::DmabufUnavailable(format!("wayland connect: {e}")))?;
    let mut queue = connection.new_event_queue();
    let qh = queue.handle();

    let mut state = CaptureState {
        shm: None,
        source_manager: None,
        capture_manager: None,
        outputs: Vec::new(),
        session: SessionState::default(),
        frame: FrameState::default(),
    };

    connection.display().get_registry(&qh, ());
    queue
        .roundtrip(&mut state)
        .map_err(Error::WaylandDispatch)?;
    // Drain the wl_output name/mode/geometry burst.
    queue
        .roundtrip(&mut state)
        .map_err(Error::WaylandDispatch)?;

    if state.capture_manager.is_none() || state.source_manager.is_none() {
        return Err(Error::ProtocolNotAvailable(std::any::type_name::<
            ExtImageCopyCaptureManagerV1,
        >()));
    }
    if state.shm.is_none() {
        return Err(Error::NoShm);
    }

    let (output, out_info) = state
        .outputs
        .iter()
        .find(|(_, info)| info.name.as_deref() == Some(output_name))
        .map(|(o, i)| (o.clone(), i.clone()))
        .ok_or_else(|| Error::DmabufUnavailable(format!("output '{output_name}' not found")))?;
    let out_transform = out_info.transform;

    let source_manager = state.source_manager.clone().expect("checked above");
    let capture_manager = state.capture_manager.clone().expect("checked above");

    let source = source_manager.create_source(&output, &qh, ());
    let options = if show_cursor {
        ext_image_copy_capture_manager_v1::Options::PaintCursors
    } else {
        ext_image_copy_capture_manager_v1::Options::empty()
    };
    let session = capture_manager.create_session(&source, options, &qh, ());

    // Negotiate constraints: dispatch until the compositor sends `done` after a
    // batch that includes an SHM format and a size.
    let mut spins = 0;
    loop {
        queue
            .blocking_dispatch(&mut state)
            .map_err(Error::WaylandDispatch)?;
        if state.session.stopped {
            session.destroy();
            source.destroy();
            return Err(Error::Failed);
        }
        let s = &state.session;
        if s.done && s.shm_format.is_some() && s.width > 0 && s.height > 0 {
            break;
        }
        spins += 1;
        if spins > MAX_NEGOTIATE_SPINS {
            session.destroy();
            source.destroy();
            return Err(Error::DmabufUnavailable(
                "ext session never advertised usable constraints".into(),
            ));
        }
    }

    let (width, height) = (state.session.width, state.session.height);
    let format = state.session.shm_format.expect("checked in the loop above");
    let shm = state.shm.clone().ok_or(Error::NoShm)?;
    let stride = width * 4;

    let mut ring = Vec::with_capacity(RING);
    for _ in 0..RING {
        ring.push(Arc::new(Buffer::new(
            &shm,
            width,
            height,
            stride,
            format,
            &qh,
            (),
        )?));
    }
    queue
        .roundtrip(&mut state)
        .map_err(Error::WaylandDispatch)?;

    let info = ExtStreamInfo {
        width,
        height,
        stride,
        format,
        refresh_mhz: out_info.refresh_mhz,
        transform: out_transform,
    };
    Ok((queue, state, session, source, ring, info, out_transform))
}

/// The capture loop: round-robin a ring slot, create one frame, capture into it,
/// wait for `ready` (re-checking `stop` every [`POLL_TIMEOUT_MS`]), then publish.
fn run_capture_loop(
    queue: &mut EventQueue<CaptureState>,
    state: &mut CaptureState,
    session: &ExtImageCopyCaptureSessionV1,
    ring: &[Arc<Buffer>],
    out_transform: Transform,
    stop: &Arc<AtomicBool>,
    latest: &Arc<Mutex<Option<Arc<PublishedFrame>>>>,
) {
    let qh = queue.handle();
    let (width, height) = (ring[0].width, ring[0].height);
    let mut seq = 0u64;
    let mut slot = 0usize;

    while !stop.load(Ordering::Relaxed) {
        state.frame = FrameState::default();
        let frame = session.create_frame(&qh, ());
        frame.attach_buffer(&ring[slot].buffer);
        // We do not track client-side damage, so damage the whole buffer; the
        // compositor still reports the actual changed region via the `damage`
        // event, forwarded as `SPA_META_VideoDamage`.
        frame.damage_buffer(0, 0, width as i32, height as i32);
        frame.capture();

        loop {
            if stop.load(Ordering::Relaxed) {
                break;
            }
            if dispatch_timeout(queue, state, POLL_TIMEOUT_MS).is_err() {
                stop.store(true, Ordering::SeqCst);
                break;
            }
            if state.frame.ready || state.frame.failed || state.session.stopped {
                break;
            }
        }

        let done = std::mem::take(&mut state.frame);
        frame.destroy();

        if state.session.stopped {
            break;
        }
        if !done.ready {
            // Failed copy or interrupted by stop; the outer loop re-checks stop.
            continue;
        }

        let published = Arc::new(PublishedFrame {
            seq,
            buffer: ring[slot].clone(),
            damage: done.damage,
            transform: done.transform.unwrap_or(out_transform),
            pts_nanos: done.pts_nanos,
        });
        *latest.lock().expect("lock should not be poisoned") = Some(published);
        seq = seq.wrapping_add(1);
        slot = (slot + 1) % ring.len();
    }
}

/// Dispatch events, blocking at most `timeout_ms` so the caller can re-check its
/// stop flag even when no frame events arrive (static screen). Uses
/// `prepare_read` + `poll` so the wait is interruptible by a timeout, which
/// `blocking_dispatch` is not.
fn dispatch_timeout(
    queue: &mut EventQueue<CaptureState>,
    state: &mut CaptureState,
    timeout_ms: i32,
) -> Result<(), Error> {
    queue
        .flush()
        .map_err(|e| Error::DmabufUnavailable(format!("wayland flush: {e}")))?;
    if queue
        .dispatch_pending(state)
        .map_err(Error::WaylandDispatch)?
        > 0
    {
        return Ok(());
    }
    let Some(guard) = queue.prepare_read() else {
        // Events arrived between flush and prepare_read; dispatch them.
        queue
            .dispatch_pending(state)
            .map_err(Error::WaylandDispatch)?;
        return Ok(());
    };
    let fd = guard.connection_fd();
    let mut pollfd = libc::pollfd {
        fd: fd.as_raw_fd(),
        events: libc::POLLIN,
        revents: 0,
    };
    // SAFETY: `pollfd` is one valid, initialized `pollfd`; `poll` reads
    // `fd`/`events` and writes `revents` only.
    let ret = unsafe { libc::poll(&mut pollfd, 1, timeout_ms) };
    if ret > 0 && (pollfd.revents & libc::POLLIN) != 0 {
        guard
            .read()
            .map_err(|e| Error::DmabufUnavailable(format!("wayland read: {e}")))?;
        queue
            .dispatch_pending(state)
            .map_err(Error::WaylandDispatch)?;
    } else {
        // Timed out (ret == 0) or poll error: release the read intent so the
        // next iteration can prepare a fresh read.
        drop(guard);
    }
    Ok(())
}

/// Destroy the session/source and the ring's `wl_buffer`s.
fn cleanup(
    session: &ExtImageCopyCaptureSessionV1,
    source: &ExtImageCaptureSourceV1,
    ring: &[Arc<Buffer>],
) {
    for buf in ring {
        buf.destroy();
    }
    session.destroy();
    source.destroy();
}

impl Dispatch<wl_registry::WlRegistry, ()> for CaptureState {
    fn event(
        state: &mut Self,
        registry: &wl_registry::WlRegistry,
        event: <wl_registry::WlRegistry as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        qh: &QueueHandle<Self>,
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
            "wl_shm" => state.shm = Some(registry.bind(name, version, qh, ())),
            "wl_output" => {
                // Name needs v4; cap the bind there.
                let output: WlOutput = registry.bind(name, version.min(4), qh, ());
                state.outputs.push((output, OutputInfo::default()));
            }
            "ext_output_image_capture_source_manager_v1" => {
                state.source_manager = Some(registry.bind(name, version, qh, ()));
            }
            "ext_image_copy_capture_manager_v1" => {
                state.capture_manager = Some(registry.bind(name, version, qh, ()));
            }
            _ => {}
        }
    }
}

impl Dispatch<WlOutput, ()> for CaptureState {
    fn event(
        state: &mut Self,
        proxy: &WlOutput,
        event: <WlOutput as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        _qh: &QueueHandle<Self>,
    ) {
        let Some((_, info)) = state.outputs.iter_mut().find(|(o, _)| o == proxy) else {
            return;
        };
        match event {
            wl_output::Event::Name { name } => info.name = Some(name),
            wl_output::Event::Geometry {
                transform: WEnum::Value(t),
                ..
            } => info.transform = t,
            wl_output::Event::Mode { flags, refresh, .. } => {
                if let WEnum::Value(f) = flags
                    && f.contains(wl_output::Mode::Current)
                {
                    info.refresh_mhz = Some(refresh);
                }
            }
            _ => {}
        }
    }
}

impl Dispatch<ExtImageCopyCaptureSessionV1, ()> for CaptureState {
    fn event(
        state: &mut Self,
        _proxy: &ExtImageCopyCaptureSessionV1,
        event: <ExtImageCopyCaptureSessionV1 as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        _qh: &QueueHandle<Self>,
    ) {
        let s = &mut state.session;
        match event {
            ext_image_copy_capture_session_v1::Event::BufferSize { width, height } => {
                s.width = width;
                s.height = height;
            }
            ext_image_copy_capture_session_v1::Event::ShmFormat { format } => {
                if s.shm_format.is_none()
                    && let WEnum::Value(f) = format
                {
                    s.shm_format = Some(f);
                }
            }
            ext_image_copy_capture_session_v1::Event::Done => s.done = true,
            ext_image_copy_capture_session_v1::Event::Stopped => s.stopped = true,
            _ => {}
        }
    }
}

impl Dispatch<ExtImageCopyCaptureFrameV1, ()> for CaptureState {
    fn event(
        state: &mut Self,
        _proxy: &ExtImageCopyCaptureFrameV1,
        event: <ExtImageCopyCaptureFrameV1 as Proxy>::Event,
        _data: &(),
        _conn: &Connection,
        _qh: &QueueHandle<Self>,
    ) {
        let f = &mut state.frame;
        match event {
            ext_image_copy_capture_frame_v1::Event::Transform {
                transform: WEnum::Value(t),
            } => f.transform = Some(t),
            ext_image_copy_capture_frame_v1::Event::Damage {
                x,
                y,
                width,
                height,
            } => {
                let (x, y) = (x.max(0) as u32, y.max(0) as u32);
                let (w, h) = (width.max(0) as u32, height.max(0) as u32);
                if w > 0 && h > 0 {
                    f.damage.push((x, y, w, h));
                }
            }
            ext_image_copy_capture_frame_v1::Event::PresentationTime {
                tv_sec_hi,
                tv_sec_lo,
                tv_nsec,
            } => {
                let secs = (u64::from(tv_sec_hi) << 32) | u64::from(tv_sec_lo);
                f.pts_nanos = Some(
                    secs.saturating_mul(1_000_000_000)
                        .saturating_add(u64::from(tv_nsec)),
                );
            }
            ext_image_copy_capture_frame_v1::Event::Ready => f.ready = true,
            ext_image_copy_capture_frame_v1::Event::Failed { .. } => f.failed = true,
            _ => {}
        }
    }
}

delegate_noop!(CaptureState: ignore WlShm);
delegate_noop!(CaptureState: ignore WlShmPool);
delegate_noop!(CaptureState: ignore WlBuffer);
delegate_noop!(CaptureState: ignore ExtImageCaptureSourceV1);
delegate_noop!(CaptureState: ignore ExtOutputImageCaptureSourceManagerV1);
delegate_noop!(CaptureState: ignore ExtImageCopyCaptureManagerV1);

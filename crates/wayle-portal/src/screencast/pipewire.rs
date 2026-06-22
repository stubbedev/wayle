//! PipeWire producer for ScreenCast streams.
//!
//! Each stream runs a dedicated thread with its own PipeWire main loop. The
//! loop owns a [`Capturer`] and a video output [`Stream`]; a timer fires at the
//! target frame rate (we are the graph DRIVER) and triggers one process cycle,
//! which captures a fresh Wayland frame into the dequeued PipeWire buffer. The
//! node id the frontend hands to the client is read back over a channel once
//! the stream reaches PAUSED.
//!
//! Two backends, picked at startup:
//! - [`run_dmabuf`] — zero-copy: the compositor blits each frame straight into
//!   a GPU buffer object whose dmabuf fd is handed to the consumer. Used for
//!   output/region targets when the compositor offers `zwp_linux_dmabuf` and a
//!   probe capture succeeds.
//! - [`run_shm`] — the fallback, correct on every compositor: PipeWire
//!   allocates mapped buffers and each frame is copied in. Used for windows and
//!   whenever the dmabuf chain is unavailable.
//!
//! Frames are `BGRx`, matching `wl_shm`/DRM `XRGB8888`.

use std::{
    cell::{Cell, RefCell},
    collections::HashMap,
    os::fd::AsRawFd,
    rc::Rc,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc,
    },
    thread::JoinHandle,
    time::{Duration, Instant},
};

use pipewire as pw;
use pw::spa::buffer::meta::MetaHeader;
use tracing::{debug, error, warn};
use wayland_client::protocol::wl_buffer::WlBuffer;
use wayle_share_preview::dmabuf::{self, DmaBuffer};

use super::{capture::Capturer, source::CaptureTarget};

/// A running PipeWire stream; stops and joins its thread on drop.
pub struct StreamHandle {
    /// Global PipeWire node id the client connects `pipewiresrc` to.
    pub node_id: u32,
    /// Negotiated stream size in pixels (from the first captured frame).
    pub size: (u32, u32),
    stop: Arc<AtomicBool>,
    join: Option<JoinHandle<()>>,
}

impl Drop for StreamHandle {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::SeqCst);
        if let Some(join) = self.join.take() {
            let _ = join.join();
        }
    }
}

/// Starts a PipeWire stream that captures `target` at `fps`.
///
/// Blocks until the stream is connected and its node id is known, then returns
/// a handle that keeps the stream alive until dropped.
///
/// # Errors
///
/// Returns an error if the capturer cannot be opened, the PipeWire loop fails
/// to start, or `connect` fails.
pub fn start_stream(
    target: CaptureTarget,
    show_cursor: bool,
    fps: u32,
) -> Result<StreamHandle, String> {
    let stop = Arc::new(AtomicBool::new(false));
    let (ready_tx, ready_rx) = mpsc::channel::<Result<(u32, u32, u32), String>>();

    let thread_stop = stop.clone();
    let join = std::thread::Builder::new()
        .name("wayle-screencast".to_owned())
        .spawn(move || run_loop(&target, show_cursor, fps.max(1), &thread_stop, &ready_tx))
        .map_err(|e| format!("cannot spawn screencast thread: {e}"))?;

    match ready_rx.recv() {
        Ok(Ok((node_id, width, height))) => Ok(StreamHandle {
            node_id,
            size: (width, height),
            stop,
            join: Some(join),
        }),
        Ok(Err(err)) => {
            let _ = join.join();
            Err(err)
        }
        Err(_) => {
            let _ = join.join();
            Err("screencast thread exited before reporting a node id".to_owned())
        }
    }
}

/// Body of the per-stream thread: sets up PipeWire and runs its loop until
/// `stop` is set.
fn run_loop(
    target: &CaptureTarget,
    show_cursor: bool,
    fps: u32,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) {
    if let Err(err) = run_loop_inner(target, show_cursor, fps, stop, ready) {
        // If we failed before reporting readiness, surface it to the caller.
        let _ = ready.send(Err(err));
    }
}

fn run_loop_inner(
    target: &CaptureTarget,
    show_cursor: bool,
    fps: u32,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) -> Result<(), String> {
    pw::init();

    // Open the capturer and grab one frame up front to learn the stream size.
    let capturer = Capturer::open(target, show_cursor)?;
    let capturer = Rc::new(RefCell::new(capturer));
    let first = capturer
        .borrow_mut()
        .capture()
        .map_err(|e| format!("initial capture failed: {e}"))?;
    let (width, height) = (first.width, first.height);
    drop(first);

    let main_loop =
        pw::main_loop::MainLoopRc::new(None).map_err(|e| format!("pipewire main loop: {e}"))?;
    let context = pw::context::ContextRc::new(&main_loop, None)
        .map_err(|e| format!("pipewire context: {e}"))?;
    let core = context
        .connect_rc(None)
        .map_err(|e| format!("pipewire connect: {e}"))?;

    // Prefer zero-copy dmabuf when the target (output/region) and compositor
    // support it; fall back to the SHM path — correct everywhere — on any
    // dmabuf setup failure.
    if capturer.borrow().supports_dmabuf()
        && let Some(gbm) = dmabuf::GbmDevice::open()
    {
        match run_dmabuf(&main_loop, &core, &capturer, gbm, width, height, fps, stop, ready) {
            Ok(()) => return Ok(()),
            Err(err) => warn!(%err, "screencast: dmabuf path unavailable; falling back to SHM"),
        }
    }

    run_shm(&main_loop, &core, &capturer, width, height, fps, stop, ready)
}

/// SHM producer: PipeWire allocates mapped buffers; each cycle captures a fresh
/// Wayland frame and copies it into the dequeued buffer. Correct on every
/// compositor.
fn run_shm(
    main_loop: &pw::main_loop::MainLoopRc,
    core: &pw::core::CoreRc,
    capturer: &Rc<RefCell<Capturer>>,
    width: u32,
    height: u32,
    fps: u32,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) -> Result<(), String> {
    let stream = new_stream(core)?;

    // Frame presentation clock and sequence counter for the buffer header meta.
    let start = Instant::now();
    let seq = Cell::new(0u64);
    // Currently negotiated frame size; renegotiated if the source resizes.
    let size = Cell::new((width, height));

    // Produce a frame each time PipeWire schedules a cycle (driven by our timer
    // via `trigger_process`).
    let produce = {
        let capturer = capturer.clone();
        move |stream: &pw::stream::Stream| {
            // Capture first: the source can change size (window resized, output
            // mode switched), and we must renegotiate the format before filling
            // a buffer that was sized for the old dimensions.
            let frame = match capturer.borrow_mut().capture() {
                Ok(frame) => frame,
                Err(err) => {
                    debug!(%err, "screencast: frame capture failed (skipped)");
                    return;
                }
            };

            if (frame.width, frame.height) != size.get() {
                renegotiate(stream, fps, &frame, &size);
                // Buffers are reallocated for the new size; fill next cycle.
                return;
            }

            let Some(mut pw_buffer) = stream.dequeue_buffer() else {
                return;
            };
            let pts = i64::try_from(start.elapsed().as_nanos()).unwrap_or(i64::MAX);
            stamp_header(&mut pw_buffer, pts, &seq);
            if let Some(data) = pw_buffer.datas_mut().first_mut() {
                write_frame(data, &frame);
            }
        }
    };

    let _listener = stream
        .add_local_listener::<()>()
        .state_changed(node_id_reporter(main_loop, ready, width, height))
        // Once the format is fixated, advertise the buffer metadata we write
        // (a header carrying pts/seq) so PipeWire allocates room for it.
        .param_changed(move |stream, _user_data, id, param| {
            if param.is_none() || id != pw::spa::param::ParamType::Format.as_raw() {
                return;
            }
            let mut params = meta_params();
            if let Err(err) = stream.update_params(&mut params) {
                warn!(%err, "screencast: cannot advertise buffer meta");
            }
        })
        .process(move |stream, _user_data| produce(stream))
        .register()
        .map_err(|e| format!("pipewire listener: {e}"))?;

    let mut params = format_params(width, height, fps);
    stream
        .connect(
            pw::spa::utils::Direction::Output,
            None,
            // DRIVER: we pace the graph ourselves via `trigger_process`. A
            // WebRTC/`pipewiresrc` consumer links as a non-driver and never
            // pulls on its own, so without this the producer is never scheduled
            // and no frames reach the browser.
            pw::stream::StreamFlags::MAP_BUFFERS | pw::stream::StreamFlags::DRIVER,
            &mut params,
        )
        .map_err(|e| format!("pipewire stream connect: {e}"))?;

    let _frame_timer = arm_frame_timer(main_loop, &stream, fps, stop)?;
    main_loop.run();
    error!("screencast loop exited");
    Ok(())
}

/// Creates the screencast output stream object.
fn new_stream(core: &pw::core::CoreRc) -> Result<pw::stream::StreamRc, String> {
    pw::stream::StreamRc::new(
        core.clone(),
        "wayle-screencast",
        pw::properties::properties! {
            *pw::keys::MEDIA_TYPE => "Video",
            *pw::keys::MEDIA_CATEGORY => "Capture",
            *pw::keys::MEDIA_ROLE => "Screen",
            *pw::keys::NODE_NAME => "wayle-screencast",
        },
    )
    .map_err(|e| format!("pipewire stream: {e}"))
}

/// State-change handler that reports the node id once the stream reaches PAUSED
/// (the server has exported the node by then; reading it earlier yields
/// `SPA_ID_INVALID` and no consumer ever links) and reports + quits on error so
/// the caller never blocks joining a thread stuck in `run()`.
fn node_id_reporter(
    main_loop: &pw::main_loop::MainLoopRc,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
    width: u32,
    height: u32,
) -> impl FnMut(&pw::stream::Stream, &mut (), pw::stream::StreamState, pw::stream::StreamState) + 'static
{
    let quit = main_loop.clone();
    let ready = ready.clone();
    let mut reported = false;
    move |stream, _ud, old, new| {
        debug!(?old, ?new, "screencast stream state changed");
        if reported {
            return;
        }
        match new {
            pw::stream::StreamState::Paused => {
                reported = true;
                let node_id = stream.node_id();
                debug!(node_id, width, height, "screencast: node id assigned");
                let _ = ready.send(Ok((node_id, width, height)));
            }
            pw::stream::StreamState::Error(ref err) => {
                reported = true;
                let _ = ready.send(Err(format!("stream error: {err}")));
                quit.quit();
            }
            _ => {}
        }
    }
}

/// Arms a repeating timer at `fps` that drives one process cycle per tick (we
/// are the graph DRIVER) and quits the loop when `stop` is set. The returned
/// guard must outlive `main_loop.run()`.
fn arm_frame_timer<'l>(
    main_loop: &'l pw::main_loop::MainLoopRc,
    stream: &pw::stream::StreamRc,
    fps: u32,
    stop: &Arc<AtomicBool>,
) -> Result<pw::loop_::TimerSource<'l>, String> {
    let quit_loop = main_loop.clone();
    let stop = stop.clone();
    let weak = stream.downgrade();
    let timer = main_loop.loop_().add_timer(move |_| {
        if stop.load(Ordering::SeqCst) {
            quit_loop.quit();
            return;
        }
        if let Some(stream) = weak.upgrade() {
            let _ = stream.trigger_process();
        }
    });
    let period = Duration::from_nanos(1_000_000_000 / u64::from(fps));
    timer
        .update_timer(Some(period), Some(period))
        .into_result()
        .map_err(|e| format!("pipewire frame-timer: {e}"))?;
    Ok(timer)
}

/// One pw-buffer's backing GPU allocation and its imported `wl_buffer`.
struct PoolEntry {
    /// Keeps the bo and its exported fd alive while PipeWire references the fd.
    _dma: DmaBuffer,
    /// The dmabuf-backed `wl_buffer` the compositor blits each frame into.
    wl_buffer: WlBuffer,
}

impl Drop for PoolEntry {
    fn drop(&mut self) {
        self.wl_buffer.destroy();
    }
}

/// dmabuf (zero-copy) producer: PipeWire schedules empty buffers we back with
/// GPU dmabufs; the compositor blits each frame straight into GPU memory and
/// the consumer imports it by fd — no pixel copy crosses the CPU. Output/region
/// only (the window path stays on SHM).
#[allow(clippy::too_many_arguments)]
fn run_dmabuf(
    main_loop: &pw::main_loop::MainLoopRc,
    core: &pw::core::CoreRc,
    capturer: &Rc<RefCell<Capturer>>,
    gbm: dmabuf::GbmDevice,
    width: u32,
    height: u32,
    fps: u32,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) -> Result<(), String> {
    // Probe the whole chain (allocate → import → compositor copy) up front so a
    // failure falls back to SHM cleanly, before any stream is connected.
    let probe = gbm.allocate_bgrx(width, height)?;
    let modifier: u64 = probe.modifier.into();
    let stride = probe.stride;
    {
        let wl_buffer = capturer.borrow_mut().import_dmabuf(&probe)?;
        let result = capturer.borrow_mut().capture_into(&wl_buffer);
        wl_buffer.destroy();
        result?;
    }
    drop(probe);
    debug!(width, height, stride, modifier, "screencast: dmabuf path validated");

    let gbm = Rc::new(gbm);
    let stream = new_stream(core)?;
    let start = Instant::now();
    let seq = Cell::new(0u64);
    // pw-buffer dmabuf pool, keyed by the dmabuf fd we stamp into each buffer.
    let pool: Rc<RefCell<HashMap<i32, PoolEntry>>> = Rc::new(RefCell::new(HashMap::new()));

    let add = {
        let pool = pool.clone();
        let capturer = capturer.clone();
        let gbm = gbm.clone();
        move |_s: &pw::stream::Stream, _ud: &mut (), pw_buffer: *mut pw::sys::pw_buffer| {
            if let Err(err) =
                attach_dmabuf(&gbm, &capturer, &pool, width, height, stride, pw_buffer)
            {
                warn!(%err, "screencast: dmabuf buffer setup failed");
            }
        }
    };
    let remove = {
        let pool = pool.clone();
        move |_s: &pw::stream::Stream, _ud: &mut (), pw_buffer: *mut pw::sys::pw_buffer| {
            if let Some(fd) = unsafe { dmabuf_fd(pw_buffer) } {
                pool.borrow_mut().remove(&fd);
            }
        }
    };
    let process = {
        let pool = pool.clone();
        let capturer = capturer.clone();
        move |stream: &pw::stream::Stream, _ud: &mut ()| {
            let Some(mut pw_buffer) = stream.dequeue_buffer() else {
                return;
            };
            let pts = i64::try_from(start.elapsed().as_nanos()).unwrap_or(i64::MAX);
            stamp_header(&mut pw_buffer, pts, &seq);
            let Some(data) = pw_buffer.datas_mut().first_mut() else {
                return;
            };
            // Correlate this buffer with its bo by the fd we stamped in
            // `attach_dmabuf` (the raw `pw_buffer` handle is not exposed here).
            let fd = data.fd();
            let captured = {
                let pool = pool.borrow();
                match pool.get(&fd) {
                    Some(entry) => capturer.borrow_mut().capture_into(&entry.wl_buffer),
                    None => Err(format!("no dmabuf bound to fd {fd}")),
                }
            };
            if let Err(err) = captured {
                debug!(%err, "screencast: dmabuf capture failed (skipped)");
                return;
            }
            let size = stride.saturating_mul(height);
            let chunk = data.chunk_mut();
            *chunk.offset_mut() = 0;
            *chunk.stride_mut() = stride as i32;
            *chunk.size_mut() = size;
        }
    };

    let _listener = stream
        .add_local_listener::<()>()
        .state_changed(node_id_reporter(main_loop, ready, width, height))
        .param_changed(move |stream, _ud, id, param| {
            if param.is_none() || id != pw::spa::param::ParamType::Format.as_raw() {
                return;
            }
            let mut params = dmabuf_buffer_params(stride, height);
            params.extend(meta_params());
            if let Err(err) = stream.update_params(&mut params) {
                warn!(%err, "screencast: cannot set dmabuf buffer params");
            }
        })
        .add_buffer(add)
        .remove_buffer(remove)
        .process(process)
        .register()
        .map_err(|e| format!("pipewire listener: {e}"))?;

    let mut params = dmabuf_format_params(width, height, fps, modifier);
    stream
        .connect(
            pw::spa::utils::Direction::Output,
            None,
            // No MAP_BUFFERS: we hand PipeWire dmabuf fds, not mapped memory.
            pw::stream::StreamFlags::DRIVER,
            &mut params,
        )
        .map_err(|e| format!("pipewire dmabuf connect: {e}"))?;

    let _frame_timer = arm_frame_timer(main_loop, &stream, fps, stop)?;
    main_loop.run();
    error!("screencast dmabuf loop exited");
    Ok(())
}

/// Allocates a GPU buffer for one pw-buffer, imports it as a `wl_buffer`, and
/// stamps the dmabuf fd + chunk geometry into the pw-buffer's data block.
fn attach_dmabuf(
    gbm: &dmabuf::GbmDevice,
    capturer: &Rc<RefCell<Capturer>>,
    pool: &Rc<RefCell<HashMap<i32, PoolEntry>>>,
    width: u32,
    height: u32,
    stride: u32,
    pw_buffer: *mut pw::sys::pw_buffer,
) -> Result<(), String> {
    let dma = gbm.allocate_bgrx(width, height)?;
    let wl_buffer = capturer.borrow_mut().import_dmabuf(&dma)?;
    let fd = dma.fd.as_raw_fd();
    let size = stride.saturating_mul(height);
    // SAFETY: PipeWire guarantees `pw_buffer` and its single data block are
    // valid for this callback; we only write our dmabuf fd and chunk geometry
    // into the block it allocated.
    unsafe {
        let spa_buffer = (*pw_buffer).buffer;
        if spa_buffer.is_null() || (*spa_buffer).n_datas < 1 || (*spa_buffer).datas.is_null() {
            return Err("pw_buffer has no data block".to_owned());
        }
        let data = &mut *(*spa_buffer).datas;
        data.type_ = pw::spa::sys::SPA_DATA_DmaBuf;
        data.flags = pw::spa::sys::SPA_DATA_FLAG_READABLE;
        data.fd = i64::from(fd);
        data.mapoffset = 0;
        data.maxsize = size;
        if !data.chunk.is_null() {
            (*data.chunk).offset = 0;
            (*data.chunk).stride = stride as i32;
            (*data.chunk).size = size;
        }
    }
    pool.borrow_mut().insert(fd, PoolEntry { _dma: dma, wl_buffer });
    Ok(())
}

/// Reads the dmabuf fd previously stamped into a pw-buffer's first data block.
///
/// # Safety
///
/// `pw_buffer` must be a valid pointer for the duration of the call.
unsafe fn dmabuf_fd(pw_buffer: *mut pw::sys::pw_buffer) -> Option<i32> {
    unsafe {
        let spa_buffer = (*pw_buffer).buffer;
        if spa_buffer.is_null() || (*spa_buffer).n_datas < 1 || (*spa_buffer).datas.is_null() {
            return None;
        }
        Some((*(*spa_buffer).datas).fd as i32)
    }
}

/// Builds the `SPA_PARAM_Buffers` pod for the dmabuf path: single-block buffers
/// the consumer must import as `SPA_DATA_DmaBuf`.
fn dmabuf_buffer_params(stride: u32, height: u32) -> Vec<&'static pw::spa::pod::Pod> {
    use pw::spa::pod::{Object, Property, Value};

    let size = stride.saturating_mul(height) as i32;
    let object = Object {
        type_: pw::spa::sys::SPA_TYPE_OBJECT_ParamBuffers,
        id: pw::spa::sys::SPA_PARAM_Buffers,
        properties: vec![
            Property::new(pw::spa::sys::SPA_PARAM_BUFFERS_buffers, Value::Int(4)),
            Property::new(pw::spa::sys::SPA_PARAM_BUFFERS_blocks, Value::Int(1)),
            Property::new(pw::spa::sys::SPA_PARAM_BUFFERS_size, Value::Int(size)),
            Property::new(pw::spa::sys::SPA_PARAM_BUFFERS_stride, Value::Int(stride as i32)),
            Property::new(
                pw::spa::sys::SPA_PARAM_BUFFERS_dataType,
                Value::Int(1 << pw::spa::sys::SPA_DATA_DmaBuf),
            ),
        ],
    };
    leak_pod(&Value::Object(object))
}

/// Builds the dmabuf `EnumFormat` pod, advertising the DRM modifier so the
/// consumer can import the GPU buffer.
fn dmabuf_format_params(
    width: u32,
    height: u32,
    fps: u32,
    modifier: u64,
) -> Vec<&'static pw::spa::pod::Pod> {
    use pw::spa::param::format::{FormatProperties, MediaSubtype, MediaType};
    use pw::spa::param::video::VideoFormat;
    use pw::spa::pod::{Object, Property, Value};
    use pw::spa::utils::{Fraction, Id, Rectangle};

    let object = Object {
        type_: pw::spa::utils::SpaTypes::ObjectParamFormat.as_raw(),
        id: pw::spa::param::ParamType::EnumFormat.as_raw(),
        properties: vec![
            Property::new(
                FormatProperties::MediaType.as_raw(),
                Value::Id(Id(MediaType::Video.as_raw())),
            ),
            Property::new(
                FormatProperties::MediaSubtype.as_raw(),
                Value::Id(Id(MediaSubtype::Raw.as_raw())),
            ),
            Property::new(
                FormatProperties::VideoFormat.as_raw(),
                Value::Id(Id(VideoFormat::BGRx.as_raw())),
            ),
            Property::new(
                pw::spa::sys::SPA_FORMAT_VIDEO_modifier,
                Value::Long(modifier as i64),
            ),
            Property::new(
                FormatProperties::VideoSize.as_raw(),
                Value::Rectangle(Rectangle { width, height }),
            ),
            Property::new(
                FormatProperties::VideoFramerate.as_raw(),
                Value::Fraction(Fraction { num: fps, denom: 1 }),
            ),
        ],
    };
    leak_pod(&Value::Object(object))
}

/// Builds the `SPA_PARAM_Meta` pod advertising a per-buffer header meta, so the
/// server reserves space for the pts/seq we stamp in `process`.
fn meta_params() -> Vec<&'static pw::spa::pod::Pod> {
    use pw::spa::pod::{Object, Property, Value};

    let object = Object {
        type_: pw::spa::sys::SPA_TYPE_OBJECT_ParamMeta,
        id: pw::spa::sys::SPA_PARAM_Meta,
        properties: vec![
            Property::new(
                pw::spa::sys::SPA_PARAM_META_type,
                Value::Id(pw::spa::utils::Id(pw::spa::sys::SPA_META_Header)),
            ),
            Property::new(
                pw::spa::sys::SPA_PARAM_META_size,
                Value::Int(std::mem::size_of::<pw::spa::sys::spa_meta_header>() as i32),
            ),
        ],
    };

    leak_pod(&Value::Object(object))
}

/// Requests a format renegotiation after the source changed size, updating the
/// tracked `size` on success.
fn renegotiate(
    stream: &pw::stream::Stream,
    fps: u32,
    frame: &wayle_share_preview::buffer::Buffer,
    size: &Cell<(u32, u32)>,
) {
    let mut params = format_params(frame.width, frame.height, fps);
    match stream.update_params(&mut params) {
        Ok(()) => {
            debug!(
                width = frame.width,
                height = frame.height,
                "screencast: source resized, renegotiating"
            );
            size.set((frame.width, frame.height));
        }
        Err(err) => warn!(%err, "screencast: cannot renegotiate size"),
    }
}

/// Stamps the buffer's header meta with a presentation timestamp and an
/// incrementing sequence number, so consumers (Chrome/WebRTC) get monotonic
/// timing. No-op if the buffer carries no header meta.
fn stamp_header(pw_buffer: &mut pw::buffer::Buffer, pts_ns: i64, seq: &Cell<u64>) {
    let Some(header) = pw_buffer.find_meta::<MetaHeader>() else {
        return;
    };
    let n = seq.get();
    seq.set(n.wrapping_add(1));
    // `find_meta` hands back a shared reference; the underlying meta lives in
    // our own mapped buffer and nothing else touches it during `process`, so
    // writing through it is sound.
    let raw = std::ptr::from_ref(header.as_raw()).cast_mut();
    unsafe {
        (*raw).pts = pts_ns;
        (*raw).seq = n;
        (*raw).flags = 0;
        (*raw).offset = 0;
        (*raw).dts_offset = 0;
    }
}

/// Reads the captured frame straight into the mapped PipeWire buffer — no
/// per-frame heap allocation, one copy — and records the chunk geometry.
fn write_frame(data: &mut pw::spa::buffer::Data, frame: &wayle_share_preview::buffer::Buffer) {
    let Some(dst) = data.data() else {
        return;
    };
    match frame.read_into(dst) {
        Ok(written) => {
            let chunk = data.chunk_mut();
            *chunk.offset_mut() = 0;
            *chunk.stride_mut() = frame.stride as i32;
            *chunk.size_mut() = written as u32;
        }
        Err(err) => warn!(%err, "screencast: reading frame bytes failed"),
    }
}

/// Builds the `EnumFormat` parameter pod for a fixed-size BGRx video stream.
fn format_params(width: u32, height: u32, fps: u32) -> Vec<&'static pw::spa::pod::Pod> {
    // Leaked once per stream; freed when the process exits. Streams are
    // long-lived and few, so this is acceptable and avoids lifetime gymnastics
    // across the FFI boundary.
    let object = pw::spa::pod::object!(
        pw::spa::utils::SpaTypes::ObjectParamFormat,
        pw::spa::param::ParamType::EnumFormat,
        pw::spa::pod::property!(
            pw::spa::param::format::FormatProperties::MediaType,
            Id,
            pw::spa::param::format::MediaType::Video
        ),
        pw::spa::pod::property!(
            pw::spa::param::format::FormatProperties::MediaSubtype,
            Id,
            pw::spa::param::format::MediaSubtype::Raw
        ),
        pw::spa::pod::property!(
            pw::spa::param::format::FormatProperties::VideoFormat,
            Id,
            pw::spa::param::video::VideoFormat::BGRx
        ),
        pw::spa::pod::property!(
            pw::spa::param::format::FormatProperties::VideoSize,
            Rectangle,
            pw::spa::utils::Rectangle { width, height }
        ),
        // Advertise the framerate as a range so a consumer that prefers a lower
        // rate can fixate one, defaulting to our target.
        pw::spa::pod::property!(
            pw::spa::param::format::FormatProperties::VideoFramerate,
            Choice,
            Range,
            Fraction,
            pw::spa::utils::Fraction { num: fps, denom: 1 },
            pw::spa::utils::Fraction { num: 1, denom: 1 },
            pw::spa::utils::Fraction { num: fps, denom: 1 }
        ),
    );

    leak_pod(&pw::spa::pod::Value::Object(object))
}

/// Serializes `value` into a POD and leaks the backing bytes so the resulting
/// `&'static Pod` outlives the FFI call that consumes it. Streams are few and
/// long-lived, so the leak is bounded and avoids lifetime gymnastics across the
/// C boundary.
fn leak_pod(value: &pw::spa::pod::Value) -> Vec<&'static pw::spa::pod::Pod> {
    let values: Vec<u8> = pw::spa::pod::serialize::PodSerializer::serialize(
        std::io::Cursor::new(Vec::new()),
        value,
    )
    .map(|(cursor, _)| cursor.into_inner())
    .unwrap_or_default();

    let leaked: &'static [u8] = Box::leak(values.into_boxed_slice());
    match pw::spa::pod::Pod::from_bytes(leaked) {
        Some(pod) => vec![pod],
        None => Vec::new(),
    }
}

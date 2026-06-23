//! PipeWire producer for ScreenCast streams.
//!
//! Each stream runs a dedicated thread with its own PipeWire main loop. The
//! loop owns a [`Capturer`] and a video output [`Stream`]; a timer fires at the
//! target frame rate, captures a fresh Wayland frame, and copies it into a
//! dequeued PipeWire buffer. The node id the frontend hands to the client is
//! read back over a channel once `connect` succeeds.
//!
//! The SPA video format is mapped from the `wl_shm` format the compositor
//! actually returns (see [`PixelFormat`]), and each frame is stamped with a
//! `SPA_META_Header` timestamp/sequence, a `SPA_META_VideoTransform` (the
//! output's rotation/flip, constant per stream) and a `SPA_META_VideoDamage`
//! (the regions that changed this frame).
//!
//! ## dmabuf vs SHM
//!
//! Capture is SHM by default and works everywhere. When the captured frame
//! carries dmabuf import parameters (the wlr-screencopy dmabuf path succeeded —
//! see [`wayle_share_preview`]), the stream additionally offers the dmabuf
//! format and attaches the dmabuf fd to the PipeWire buffer instead of copying
//! mapped pixels. This is **best-effort and unverified on hardware**: if the
//! first frame is SHM, or anything in the dmabuf path is missing, the stream
//! behaves exactly as the SHM-only path did and never regresses.

use std::{
    cell::{Cell, RefCell},
    collections::HashMap,
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
use pw::spa::buffer::meta::{MetaHeader, MetaVideoDamage, MetaVideoTransform};
use tracing::{debug, error, warn};

use super::{
    capture::Capturer,
    source::{CaptureTarget, PixelFormat, clamp_damage_into, effective_fps, spa_video_transform},
};

/// Maximum number of damage regions we declare room for in the
/// `SPA_META_VideoDamage` meta (matches xdph's `MAX_DAMAGE` sizing of 4
/// `spa_meta_region` slots). More damage than this collapses to a full-frame
/// rect (see [`clamp_damage`]).
const MAX_DAMAGE_REGIONS: usize = 4;

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

/// Acquire one capture frame for the process callback.
///
/// dmabuf: reuse the buffer bound to THIS pw_buffer (`dmabuf_key`), recapturing
/// into its existing bo (zero allocation); the first time a pw_buffer is seen,
/// allocate one. SHM: the capturer leases the pooled `ShmSlot` (also zero
/// allocation in steady state). Returns `None` when the frame should be skipped.
fn acquire_frame(
    prefer_dmabuf: bool,
    dmabuf_key: Option<usize>,
    capturer: &RefCell<Capturer>,
    dmabuf_pool: &RefCell<HashMap<usize, wayle_share_preview::buffer::Buffer>>,
) -> Option<wayle_share_preview::buffer::Buffer> {
    if prefer_dmabuf {
        acquire_dmabuf_frame(capturer, dmabuf_pool, dmabuf_key)
    } else {
        capture_frame(capturer, false)
    }
}

/// Capture a fresh frame, logging and swallowing the error as a skip.
fn capture_frame(
    capturer: &RefCell<Capturer>,
    prefer_dmabuf: bool,
) -> Option<wayle_share_preview::buffer::Buffer> {
    match capturer.borrow_mut().capture(prefer_dmabuf) {
        Ok(frame) => Some(frame),
        Err(err) => {
            debug!(%err, "screencast: frame capture failed (skipped)");
            None
        }
    }
}

/// dmabuf acquisition with a **stable per-pw_buffer** binding.
///
/// Each PipeWire buffer keeps its own dmabuf bo for the stream's life, keyed by
/// the pw_buffer's `spa_data` address (`dmabuf_key`). We take that bound buffer
/// out of the pool, recapture into its existing bo (zero allocation), and the
/// caller reinserts it under the same key. The first time a pw_buffer is seen
/// there is no entry, so we allocate one.
///
/// The binding must be stable: a bo that moves between pw_buffers stalls the
/// consumer (it caches the dmabuf import per pw_buffer and can't follow a moved
/// bo) — that was the freeze in the FIFO-recycle version. This is the same 1:1
/// invariant the canonical `PW_STREAM_FLAG_ALLOC_BUFFERS` + `add_buffer` model
/// enforces (xdg-desktop-portal-wlr / -hyprland), achieved here under
/// MAP_BUFFERS by keying on the buffer rather than allocating producer buffers.
fn acquire_dmabuf_frame(
    capturer: &RefCell<Capturer>,
    dmabuf_pool: &RefCell<HashMap<usize, wayle_share_preview::buffer::Buffer>>,
    key: Option<usize>,
) -> Option<wayle_share_preview::buffer::Buffer> {
    let existing = key.and_then(|k| dmabuf_pool.borrow_mut().remove(&k));
    let Some(mut buf) = existing else {
        // First time we see this pw_buffer: allocate its dmabuf bo.
        return capture_frame(capturer, true);
    };
    match capturer.borrow_mut().recapture_dmabuf(&mut buf) {
        Ok(()) => Some(buf),
        Err(err) => {
            debug!(%err, "screencast: dmabuf recapture failed (skipped)");
            // Keep the binding so a transient failure doesn't drop the buffer.
            if let Some(k) = key {
                dmabuf_pool.borrow_mut().insert(k, buf);
            }
            None
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

    // Open the capturer and grab one frame up front to learn the stream size,
    // pixel format, and (for the refresh-rate clamp) the output mode.
    let capturer = Capturer::open(target, show_cursor)?;
    let capturer = Rc::new(RefCell::new(capturer));
    // Probe with dmabuf preferred so we learn whether the dmabuf path is
    // available for this target; the per-frame decision below is gated on the
    // negotiated buffer type so we never hand the consumer the wrong kind.
    let first = capturer
        .borrow_mut()
        .capture(true)
        .map_err(|e| format!("initial capture failed: {e}"))?;
    let (width, height) = (first.width, first.height);
    // Keep the stride to size the PipeWire buffers; wlr-screencopy may pad it
    // past width*4, and the consumer maps buffers of exactly the size we
    // advertise, so it must match the bytes we copy in.
    let stride = first.stride;
    // Map the captured wl_shm format to the matching SPA format instead of
    // assuming BGRx; fall back to BGRx for any exotic format we can't name.
    let pixel_format = PixelFormat::from_wl(first.format).unwrap_or(PixelFormat::Bgrx);
    // Whether the very first frame came back dmabuf-backed. The dmabuf path is
    // all-or-nothing per stream: if the first frame is SHM we never offer
    // dmabuf, guaranteeing the SHM behaviour is unchanged. If it is dmabuf we
    // offer the dmabuf format too, but every individual frame still falls back
    // to SHM if its own dmabuf backing is absent (see the process closure).
    let dmabuf_modifier = first.dmabuf.as_ref().map(|d| d.modifier);
    // Release the probe frame's compositor-side buffer (Buffer has no Drop).
    first.destroy();
    drop(first);

    // The output's rotation/flip, mapped to the SPA videotransform value. This
    // is constant for the lifetime of the stream, so compute it once and stamp
    // every buffer with it. Identity (0) for window capture.
    let transform = spa_video_transform(capturer.borrow().transform());

    // Clamp the requested rate to the output's actual refresh (no-op for window
    // capture, where the refresh is unknown).
    let fps = effective_fps(fps, capturer.borrow().refresh_mhz());

    // When the compositor offers dmabuf, take the zero-copy / zero-allocation
    // path: PW_STREAM_FLAG_ALLOC_BUFFERS + add_buffer/remove_buffer binds one
    // dmabuf bo to each PipeWire buffer for the stream's life and screencopy
    // captures straight into it (the model xdg-desktop-portal-wlr / -hyprland
    // use). The SHM path below is unchanged and serves compositors without
    // dmabuf.
    // One line that pins down both common screencast complaints:
    // - path (`dmabuf` = zero-copy/low-CPU vs `SHM` = full readback+memcpy/high-CPU)
    // - `cursor` (whether the client asked for an embedded cursor at all).
    // If CPU is high this should read `path=SHM`; the preceding `warn!` from
    // `capture_output_dmabuf_or_shm` then says why dmabuf was declined.
    let path = if dmabuf_modifier.is_some() {
        "dmabuf"
    } else {
        "SHM"
    };
    tracing::info!(
        path,
        show_cursor,
        fps,
        width,
        height,
        "screencast stream starting"
    );

    if dmabuf_modifier.is_some() {
        return run_loop_dmabuf(
            &capturer,
            width,
            height,
            stride,
            fps,
            transform,
            pixel_format,
            dmabuf_modifier,
            stop,
            ready,
        );
    }

    let main_loop =
        pw::main_loop::MainLoopRc::new(None).map_err(|e| format!("pipewire main loop: {e}"))?;
    let context = pw::context::ContextRc::new(&main_loop, None)
        .map_err(|e| format!("pipewire context: {e}"))?;
    let core = context
        .connect_rc(None)
        .map_err(|e| format!("pipewire connect: {e}"))?;

    let stream = pw::stream::StreamBox::new(
        &core,
        "wayle-screencast",
        pw::properties::properties! {
            *pw::keys::MEDIA_TYPE => "Video",
            *pw::keys::MEDIA_CATEGORY => "Capture",
            *pw::keys::MEDIA_ROLE => "Screen",
            *pw::keys::NODE_NAME => "wayle-screencast",
        },
    )
    .map_err(|e| format!("pipewire stream: {e}"))?;

    // Produce a frame on each timer tick.
    let produce = {
        let capturer = capturer.clone();
        // Frame presentation clock + sequence counter, written into the
        // SPA_META_Header so consumers (recorders, WebRTC) can pace/sync.
        let start = Instant::now();
        let seq = Cell::new(0u64);
        // Reused per-frame damage scratch (avoids a small alloc every frame).
        let damage_scratch = RefCell::new(Vec::<(u32, u32, u32, u32)>::new());
        // Per-pw_buffer dmabuf reuse pool, keyed by the buffer's spa_data
        // address (stable for that pw_buffer's lifetime). Each PipeWire buffer
        // keeps its own dmabuf bo and we recapture into it every frame — no
        // per-frame `dmabuf::allocate` / `create_immed`. The binding is 1:1 and
        // stable, so a bo never moves between pw_buffers (which stalled the
        // consumer). SHM frames lease the pooled ShmSlot, so they don't allocate
        // either. The map is bounded by the negotiated buffer count (~4–8).
        let dmabuf_pool =
            RefCell::new(HashMap::<usize, wayle_share_preview::buffer::Buffer>::new());
        move |stream: &pw::stream::Stream| {
            let Some(mut pw_buffer) = stream.dequeue_buffer() else {
                return;
            };

            // Decide SHM vs dmabuf from the *negotiated* buffer the consumer gave
            // us: only request a dmabuf capture when this buffer's data slot is a
            // dmabuf slot. A consumer that negotiated SHM thus always gets a
            // readable SHM buffer — the dmabuf path never regresses SHM.
            let prefer_dmabuf = pw_buffer
                .datas_mut()
                .first()
                .map(|d| d.type_().as_raw() == pw::spa::sys::SPA_DATA_DmaBuf)
                .unwrap_or(false);

            // Stable key for THIS pw_buffer's dmabuf binding: the address of its
            // first spa_data, fixed for the pw_buffer's lifetime. Only needed on
            // the dmabuf path.
            let dmabuf_key = if prefer_dmabuf {
                pw_buffer
                    .datas_mut()
                    .first()
                    .map(|d| std::ptr::from_ref(d.as_raw()) as usize)
            } else {
                None
            };

            // Acquire the frame. dmabuf: recapture into this pw_buffer's bound bo
            // (zero allocation; first sighting allocates). SHM: the capturer
            // leases the pooled ShmSlot (also zero allocation in steady state).
            let Some(frame) = acquire_frame(prefer_dmabuf, dmabuf_key, &capturer, &dmabuf_pool)
            else {
                return;
            };

            // Move the captured pixels into the PipeWire buffer. Scoped so the
            // mutable buffer borrow ends before we touch the metadata below.
            //
            // dmabuf path: attach the dmabuf fd to the buffer data instead of
            // copying. Best-effort — only taken when this frame is dmabuf-backed
            // AND the buffer's data slot accepts a DmaBuf type; otherwise the SHM
            // copy below runs, so a single SHM frame mid-stream still works.
            let frame_stride = frame.stride as i32;
            let dmabuf_attached = {
                let datas = pw_buffer.datas_mut();
                let Some(data) = datas.first_mut() else {
                    return;
                };

                let attached = if let Some(backing) = frame.dmabuf.as_ref() {
                    attach_dmabuf(data, backing, frame_stride)
                } else {
                    false
                };

                if !attached {
                    let Some(dst) = data.data() else {
                        return;
                    };
                    match frame.read_into(dst) {
                        Ok(written) => {
                            let chunk = data.chunk_mut();
                            *chunk.offset_mut() = 0;
                            *chunk.stride_mut() = frame_stride;
                            *chunk.size_mut() = written as u32;
                        }
                        Err(err) => warn!(%err, "screencast: reading frame bytes failed"),
                    }
                }
                attached
            };

            // Stamp the header meta if the consumer negotiated it.
            let pts = i64::try_from(start.elapsed().as_nanos()).unwrap_or(i64::MAX);
            let seq_val = seq.get();
            seq.set(seq_val.wrapping_add(1));
            if let Some(header) = pw_buffer.find_meta::<MetaHeader>() {
                let raw = std::ptr::from_ref(header.as_raw()).cast_mut();
                // SAFETY: `raw` points at the SPA_META_Header region PipeWire
                // allocated for this buffer (we declared it in param_changed) and
                // `pw_buffer` is uniquely owned here, so no aliasing write exists.
                unsafe {
                    (*raw).pts = pts;
                    (*raw).seq = seq_val;
                    (*raw).dts_offset = 0;
                    (*raw).flags = 0;
                }
            }

            // Stamp the constant output transform if the consumer negotiated it.
            if let Some(vt) = pw_buffer.find_meta::<MetaVideoTransform>() {
                let raw = std::ptr::from_ref(vt.as_raw()).cast_mut();
                // SAFETY: `raw` points at the SPA_META_VideoTransform region
                // PipeWire allocated for this buffer (declared in param_changed);
                // `pw_buffer` is uniquely owned here so there is no aliasing write.
                unsafe {
                    (*raw).transform = transform;
                }
            }

            // Stamp the damage regions if the consumer negotiated them. The meta
            // is a fixed array of spa_meta_region slots; write each clamped rect
            // then terminate with a zero-size region (matching xdph).
            if let Some(damage) = pw_buffer.find_meta::<MetaVideoDamage>() {
                let mut rects = damage_scratch.borrow_mut();
                clamp_damage_into(&mut rects, &frame.damage, width, height, MAX_DAMAGE_REGIONS);
                write_damage(damage, &rects);
            }

            // Re-bind the dmabuf frame to its pw_buffer so the next dequeue of
            // the same pw_buffer recaptures into it (stable 1:1). The fd/bo thus
            // outlives the consumer's use of the buffer, and steady state does no
            // allocation. SHM frames are leases and just drop.
            if dmabuf_attached {
                match dmabuf_key {
                    Some(k) => {
                        dmabuf_pool.borrow_mut().insert(k, frame);
                    }
                    // No key (shouldn't happen on the dmabuf path); destroy
                    // rather than leak the wl_buffer.
                    None => frame.destroy(),
                }
            } else {
                // SHM frame: a lightweight lease over the pooled ShmSlot. The
                // pixels are copied and damage is read, so just drop it —
                // `destroy()` is a no-op for a lease (the slot owns and reuses
                // the wl_buffer; the SHM pool fix lives in the capturer now).
                frame.destroy();
            }
        }
    };

    // Report the node id to the caller only once the stream has reached PAUSED.
    // `pw_stream_get_node_id` returns SPA_ID_INVALID until the server has
    // exported the node, which happens on a loop roundtrip after `connect` —
    // reading it eagerly hands the client a bogus id and no consumer ever links.
    let ready_state = ready.clone();
    let mut reported = false;
    let _listener = stream
        .add_local_listener::<()>()
        .state_changed(move |stream, _, old, new| {
            debug!(?old, ?new, "screencast stream state changed");
            if reported {
                return;
            }
            match new {
                pw::stream::StreamState::Paused => {
                    reported = true;
                    let node_id = stream.node_id();
                    debug!(node_id, width, height, "screencast: node id assigned");
                    let _ = ready_state.send(Ok((node_id, width, height)));
                }
                pw::stream::StreamState::Error(ref err) => {
                    reported = true;
                    let _ = ready_state.send(Err(format!("stream error: {err}")));
                }
                _ => {}
            }
        })
        // Once a concrete format is negotiated, declare the buffer layout. Without
        // this the server maps zero-size buffers and the consumer (e.g. Firefox's
        // WebRTC sink) shows nothing even though the stream links and runs.
        .param_changed(move |stream, _user_data, id, param| {
            if param.is_none() || id != pw::spa::param::ParamType::Format.as_raw() {
                return;
            }
            // Buffers (so non-empty frames are mapped) + Header / VideoTransform /
            // VideoDamage metas. Declared together in one update_params call.
            //
            // When the first frame was dmabuf-backed we additionally allow the
            // DmaBuf data type so the server can hand us dmabuf buffers; the SHM
            // types stay allowed so an SHM frame mid-stream still works.
            // Build the pod bytes locally and borrow `&Pod` views for the call;
            // both are dropped when this callback returns (no `'static` leak).
            // param_changed fires only on (re)negotiation, so rebuilding is cheap.
            let blobs = param_blobs(stride, height, dmabuf_modifier.is_some());
            let mut pods: Vec<&pw::spa::pod::Pod> = blobs
                .iter()
                .filter_map(|bytes| pw::spa::pod::Pod::from_bytes(bytes))
                .collect();
            if !pods.is_empty()
                && let Err(err) = stream.update_params(&mut pods)
            {
                warn!(%err, "screencast: update_params failed");
            }
        })
        // The consumer drives the cycle; we fill a buffer whenever PipeWire asks.
        .process(move |stream, _user_data| produce(stream))
        .register()
        .map_err(|e| format!("pipewire listener: {e}"))?;

    // EnumFormat: always offer the SHM format. When the first frame was
    // dmabuf-backed, also offer the same format carrying the bo's modifier so a
    // dmabuf-capable consumer can pick the zero-copy path; consumers that ignore
    // it just use the SHM format.
    // Own the format pod bytes for the duration of `connect`, borrowing `&Pod`
    // views into them (no `'static` leak).
    let format_bytes = format_blobs(width, height, fps, pixel_format, dmabuf_modifier);
    let mut params: Vec<&pw::spa::pod::Pod> = format_bytes
        .iter()
        .filter_map(|bytes| pw::spa::pod::Pod::from_bytes(bytes))
        .collect();
    // MAP_BUFFERS lets PipeWire map the SHM buffers we copy into. It is harmless
    // for the dmabuf path (we attach external fds in `process` and do not rely on
    // the mapping there), and keeping it guarantees the SHM fallback is byte-for-
    // byte the previous behaviour.
    stream
        .connect(
            pw::spa::utils::Direction::Output,
            None,
            pw::stream::StreamFlags::MAP_BUFFERS,
            &mut params,
        )
        .map_err(|e| format!("pipewire stream connect: {e}"))?;

    // The node id is reported from the state_changed callback once the stream
    // reaches PAUSED (the server has exported the node by then); see above.

    // Quit the loop when asked to stop. The timer borrows `main_loop` (via
    // loop_()), which lives to the end of the function; the closure owns a
    // separate clone for `.quit()`.
    let quit = {
        let quit_loop = main_loop.clone();
        let stop = stop.clone();
        main_loop.loop_().add_timer(move |_| {
            if stop.load(Ordering::SeqCst) {
                quit_loop.quit();
            }
        })
    };
    let poll = Duration::from_millis(100);
    quit.update_timer(Some(poll), Some(poll))
        .into_result()
        .map_err(|e| format!("pipewire stop-timer: {e}"))?;

    main_loop.run();
    error!("screencast loop exited");
    Ok(())
}

/// dmabuf zero-copy / zero-allocation producer.
///
/// Connects with `PW_STREAM_FLAG_ALLOC_BUFFERS`: `add_buffer` allocates one
/// dmabuf bo per PipeWire buffer and points the buffer's `spa_data` at its fd;
/// `process` recaptures (screencopy) straight into that bo; `remove_buffer`
/// frees it. One bo bound to each PipeWire buffer for the stream's life — no
/// per-frame allocation and no copy (the canonical model used by
/// xdg-desktop-portal-wlr / -hyprland).
#[allow(clippy::too_many_arguments)]
fn run_loop_dmabuf(
    capturer: &Rc<RefCell<Capturer>>,
    width: u32,
    height: u32,
    stride: u32,
    fps: u32,
    transform: u32,
    pixel_format: PixelFormat,
    dmabuf_modifier: Option<u64>,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) -> Result<(), String> {
    let main_loop =
        pw::main_loop::MainLoopRc::new(None).map_err(|e| format!("pipewire main loop: {e}"))?;
    let context = pw::context::ContextRc::new(&main_loop, None)
        .map_err(|e| format!("pipewire context: {e}"))?;
    let core = context
        .connect_rc(None)
        .map_err(|e| format!("pipewire connect: {e}"))?;

    let stream = pw::stream::StreamBox::new(
        &core,
        "wayle-screencast",
        pw::properties::properties! {
            *pw::keys::MEDIA_TYPE => "Video",
            *pw::keys::MEDIA_CATEGORY => "Capture",
            *pw::keys::MEDIA_ROLE => "Screen",
            *pw::keys::NODE_NAME => "wayle-screencast",
        },
    )
    .map_err(|e| format!("pipewire stream: {e}"))?;

    // pw_buffer first-spa_data address -> the dmabuf Buffer bound to it for the
    // stream's life. Shared by add_buffer / remove_buffer / process; all run on
    // this single thread, so Rc<RefCell> is sound.
    let pool: Rc<RefCell<HashMap<usize, wayle_share_preview::buffer::Buffer>>> =
        Rc::new(RefCell::new(HashMap::new()));

    let ready_state = ready.clone();
    let mut reported = false;

    let cap_add = capturer.clone();
    let pool_add = pool.clone();
    let pool_rm = pool.clone();
    let cap_proc = capturer.clone();
    let pool_proc = pool.clone();
    let start = Instant::now();
    let seq = Cell::new(0u64);
    // Pace capture to ~the target fps. If PipeWire already drives `process` at
    // the negotiated rate this never trips (33ms spacing > the 28ms floor at
    // 30fps); it only bites when the callback free-runs (e.g. a consumer pulling
    // at the monitor's refresh), which is what pegs a core. Floor = 1/(1.2·fps).
    let min_interval = Duration::from_nanos(1_000_000_000 * 10 / (u64::from(fps.max(1)) * 12));
    let last_capture = Cell::new(start - min_interval);

    let _listener = stream
        .add_local_listener::<()>()
        .state_changed(move |stream, _, old, new| {
            debug!(?old, ?new, "screencast stream state changed");
            if reported {
                return;
            }
            match new {
                pw::stream::StreamState::Paused => {
                    reported = true;
                    let node_id = stream.node_id();
                    debug!(node_id, width, height, "screencast: node id assigned");
                    let _ = ready_state.send(Ok((node_id, width, height)));
                }
                pw::stream::StreamState::Error(ref err) => {
                    reported = true;
                    let _ = ready_state.send(Err(format!("stream error: {err}")));
                }
                _ => {}
            }
        })
        .param_changed(move |stream, _user_data, id, param| {
            if param.is_none() || id != pw::spa::param::ParamType::Format.as_raw() {
                return;
            }
            let blobs = param_blobs_dmabuf(stride, height);
            let mut pods: Vec<&pw::spa::pod::Pod> = blobs
                .iter()
                .filter_map(|bytes| pw::spa::pod::Pod::from_bytes(bytes))
                .collect();
            if !pods.is_empty()
                && let Err(err) = stream.update_params(&mut pods)
            {
                warn!(%err, "screencast: update_params failed");
            }
        })
        // Allocate one dmabuf bo for this PipeWire buffer and point its data slot
        // at the bo's fd. The bo lives (in `pool`) until remove_buffer.
        .add_buffer(move |_stream, _user_data, pwb: *mut pw::sys::pw_buffer| {
            // SAFETY: `pwb` is the buffer PipeWire just added; its spa_buffer and
            // datas[0] are allocated (we asked for blocks=1). We are the sole
            // writer on this thread.
            unsafe {
                let spa_buf = (*pwb).buffer;
                if spa_buf.is_null() || (*spa_buf).n_datas < 1 {
                    return;
                }
                let data = (*spa_buf).datas; // first spa_data
                let buf = match cap_add.borrow_mut().allocate_dmabuf() {
                    Ok(b) => b,
                    Err(err) => {
                        warn!(%err, "screencast: add_buffer dmabuf alloc failed");
                        return;
                    }
                };
                let Some(plane) = buf.dmabuf.as_ref().and_then(|d| d.planes.first()) else {
                    warn!("screencast: allocated dmabuf has no plane");
                    buf.destroy();
                    return;
                };
                (*data).type_ = pw::spa::sys::SPA_DATA_DmaBuf;
                (*data).flags = 0;
                (*data).fd = i64::from(plane.fd);
                (*data).mapoffset = 0;
                (*data).maxsize = 0;
                let chunk = (*data).chunk;
                if !chunk.is_null() {
                    (*chunk).offset = plane.offset;
                    (*chunk).stride = buf.stride as i32;
                    (*chunk).size = buf.stride.saturating_mul(buf.height);
                    (*chunk).flags = 0;
                }
                pool_add.borrow_mut().insert(data as usize, buf);
            }
        })
        .remove_buffer(move |_stream, _user_data, pwb: *mut pw::sys::pw_buffer| {
            // SAFETY: as add_buffer; we only read the data pointer to key the map.
            unsafe {
                let spa_buf = (*pwb).buffer;
                if spa_buf.is_null() || (*spa_buf).n_datas < 1 {
                    return;
                }
                let key = (*spa_buf).datas as usize;
                if let Some(buf) = pool_rm.borrow_mut().remove(&key) {
                    buf.destroy();
                }
            }
        })
        .process(move |stream, _user_data| {
            // Rate gate: drop this cycle if we captured too recently, so a
            // free-running consumer can't drive captures past ~the target fps.
            let now = Instant::now();
            if now.duration_since(last_capture.get()) < min_interval {
                return;
            }
            let Some(mut pw_buffer) = stream.dequeue_buffer() else {
                return;
            };
            let Some(key) = pw_buffer
                .datas_mut()
                .first()
                .map(|d| std::ptr::from_ref(d.as_raw()) as usize)
            else {
                return;
            };
            last_capture.set(now);
            let mut pool = pool_proc.borrow_mut();
            let Some(buf) = pool.get_mut(&key) else {
                debug!("screencast: process with no bound dmabuf buffer");
                return;
            };
            // Screencopy straight into the bound bo (no allocation, no copy).
            if let Err(err) = cap_proc.borrow_mut().recapture_dmabuf(buf) {
                debug!(%err, "screencast: dmabuf recapture failed (skipped)");
                return;
            }
            // Refresh the chunk descriptor (the fd was set once in add_buffer).
            if let Some(data) = pw_buffer.datas_mut().first_mut() {
                let chunk = data.chunk_mut();
                *chunk.offset_mut() = 0;
                *chunk.stride_mut() = buf.stride as i32;
                *chunk.size_mut() = buf.stride.saturating_mul(buf.height);
            }
            let pts = i64::try_from(start.elapsed().as_nanos()).unwrap_or(i64::MAX);
            let seq_val = seq.get();
            seq.set(seq_val.wrapping_add(1));
            stamp_metadata(
                &mut pw_buffer,
                transform,
                pts,
                seq_val,
                &buf.damage,
                width,
                height,
            );
        })
        .register()
        .map_err(|e| format!("pipewire listener: {e}"))?;

    let format_bytes = format_blobs(width, height, fps, pixel_format, dmabuf_modifier);
    let mut params: Vec<&pw::spa::pod::Pod> = format_bytes
        .iter()
        .filter_map(|bytes| pw::spa::pod::Pod::from_bytes(bytes))
        .collect();
    stream
        .connect(
            pw::spa::utils::Direction::Output,
            None,
            pw::stream::StreamFlags::ALLOC_BUFFERS,
            &mut params,
        )
        .map_err(|e| format!("pipewire stream connect: {e}"))?;

    let quit = {
        let quit_loop = main_loop.clone();
        let stop = stop.clone();
        main_loop.loop_().add_timer(move |_| {
            if stop.load(Ordering::SeqCst) {
                quit_loop.quit();
            }
        })
    };
    let poll = Duration::from_millis(100);
    quit.update_timer(Some(poll), Some(poll))
        .into_result()
        .map_err(|e| format!("pipewire stop-timer: {e}"))?;

    main_loop.run();
    error!("screencast loop exited");

    // Free any bos still bound (the connection teardown also reaps them).
    for (_, buf) in pool.borrow_mut().drain() {
        buf.destroy();
    }
    Ok(())
}

/// Stamp the per-frame metas (`Header` timestamp/seq, `VideoTransform`,
/// `VideoDamage`) onto a dequeued PipeWire buffer, when the consumer negotiated
/// them. Shared by the SHM and dmabuf producers.
fn stamp_metadata(
    pw_buffer: &mut pw::buffer::Buffer,
    transform: u32,
    pts: i64,
    seq: u64,
    frame_damage: &[(u32, u32, u32, u32)],
    width: u32,
    height: u32,
) {
    if let Some(header) = pw_buffer.find_meta::<MetaHeader>() {
        let raw = std::ptr::from_ref(header.as_raw()).cast_mut();
        // SAFETY: `raw` is this buffer's SPA_META_Header region (declared in
        // param_changed); the buffer is uniquely held here.
        unsafe {
            (*raw).pts = pts;
            (*raw).seq = seq;
            (*raw).dts_offset = 0;
            (*raw).flags = 0;
        }
    }
    if let Some(vt) = pw_buffer.find_meta::<MetaVideoTransform>() {
        let raw = std::ptr::from_ref(vt.as_raw()).cast_mut();
        // SAFETY: as above for the SPA_META_VideoTransform region.
        unsafe {
            (*raw).transform = transform;
        }
    }
    if let Some(damage) = pw_buffer.find_meta::<MetaVideoDamage>() {
        let mut rects: Vec<(u32, u32, u32, u32)> = Vec::new();
        clamp_damage_into(&mut rects, frame_damage, width, height, MAX_DAMAGE_REGIONS);
        write_damage(damage, &rects);
    }
}

/// dmabuf-only buffer-layout pod (+ the same metas) for the ALLOC_BUFFERS path:
/// the producer hands the server dmabuf buffers, so only `SPA_DATA_DmaBuf` is
/// advertised.
fn param_blobs_dmabuf(stride: u32, height: u32) -> Vec<Vec<u8>> {
    use pw::spa::pod::Value;

    let header = std::mem::size_of::<pw::spa::sys::spa_meta_header>() as i32;
    let transform = std::mem::size_of::<pw::spa::sys::spa_meta_videotransform>() as i32;
    let region = std::mem::size_of::<pw::spa::sys::spa_meta_region>() as i32;
    let max = MAX_DAMAGE_REGIONS as i32;

    [
        buffer_blob_dmabuf(stride, height),
        meta_blob(pw::spa::sys::SPA_META_Header, Value::Int(header)),
        meta_blob(pw::spa::sys::SPA_META_VideoTransform, Value::Int(transform)),
        meta_blob(
            pw::spa::sys::SPA_META_VideoDamage,
            damage_size_choice(region, max),
        ),
    ]
    .into_iter()
    .flatten()
    .collect()
}

/// Buffer-layout pod advertising `SPA_DATA_DmaBuf` only (producer-allocated).
fn buffer_blob_dmabuf(stride: u32, height: u32) -> Option<Vec<u8>> {
    use pw::spa::{
        pod::{ChoiceValue, Object, Property, PropertyFlags, Value},
        utils::{Choice, ChoiceEnum, ChoiceFlags},
    };

    let size = stride.saturating_mul(height);
    let int_prop = |key, value| Property {
        key,
        flags: PropertyFlags::empty(),
        value: Value::Int(value),
    };

    let object = Object {
        type_: pw::spa::sys::SPA_TYPE_OBJECT_ParamBuffers,
        id: pw::spa::sys::SPA_PARAM_Buffers,
        properties: vec![
            Property {
                key: pw::spa::sys::SPA_PARAM_BUFFERS_buffers,
                flags: PropertyFlags::empty(),
                value: Value::Choice(ChoiceValue::Int(Choice(
                    ChoiceFlags::empty(),
                    ChoiceEnum::Range {
                        default: 4,
                        min: 2,
                        max: 8,
                    },
                ))),
            },
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_blocks, 1),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_size, size as i32),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_stride, stride as i32),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_align, 16),
            int_prop(
                pw::spa::sys::SPA_PARAM_BUFFERS_dataType,
                1i32 << pw::spa::sys::SPA_DATA_DmaBuf,
            ),
        ],
    };

    serialize_object(object)
}

/// Serializes a pod `Object` to owned bytes. The bytes must outlive any `&Pod`
/// built from them via [`pw::spa::pod::Pod::from_bytes`]; the caller keeps them
/// in a scope spanning the `connect`/`update_params` use, so unlike the previous
/// `Box::leak` approach nothing leaks per stream start.
fn serialize_object(object: pw::spa::pod::Object) -> Option<Vec<u8>> {
    pw::spa::pod::serialize::PodSerializer::serialize(
        std::io::Cursor::new(Vec::new()),
        &pw::spa::pod::Value::Object(object),
    )
    .map(|(cursor, _)| cursor.into_inner())
    .ok()
}

/// Bytes for a `SPA_PARAM_Meta` object declaring one meta type of the given
/// per-buffer `size` (a fixed `Int` for Header/VideoTransform, a `Choice` range
/// for the variable-length VideoDamage array).
fn meta_blob(meta_type: u32, size: pw::spa::pod::Value) -> Option<Vec<u8>> {
    use pw::spa::pod::{Object, Property, PropertyFlags, Value};
    let object = Object {
        type_: pw::spa::sys::SPA_TYPE_OBJECT_ParamMeta,
        id: pw::spa::sys::SPA_PARAM_Meta,
        properties: vec![
            Property {
                key: pw::spa::sys::SPA_PARAM_META_type,
                flags: PropertyFlags::empty(),
                value: Value::Id(pw::spa::utils::Id(meta_type)),
            },
            Property {
                key: pw::spa::sys::SPA_PARAM_META_size,
                flags: PropertyFlags::empty(),
                value: size,
            },
        ],
    };
    serialize_object(object)
}

/// The `Choice` range value sizing the VideoDamage meta: 1..=`MAX_DAMAGE_REGIONS`
/// `spa_meta_region` slots.
fn damage_size_choice(region: i32, max: i32) -> pw::spa::pod::Value {
    use pw::spa::{
        pod::{ChoiceValue, Value},
        utils::{Choice, ChoiceEnum, ChoiceFlags},
    };
    Value::Choice(ChoiceValue::Int(Choice(
        ChoiceFlags::empty(),
        ChoiceEnum::Range {
            default: region * max,
            min: region,
            max: region * max,
        },
    )))
}

/// All `param_changed` pods (buffer layout + Header / VideoTransform /
/// VideoDamage metas) as owned byte blobs. The caller keeps the returned `Vec`
/// alive for the stream's lifetime and builds `&Pod` views from it per call.
fn param_blobs(stride: u32, height: u32, allow_dmabuf: bool) -> Vec<Vec<u8>> {
    use pw::spa::pod::Value;

    let header = std::mem::size_of::<pw::spa::sys::spa_meta_header>() as i32;
    let transform = std::mem::size_of::<pw::spa::sys::spa_meta_videotransform>() as i32;
    let region = std::mem::size_of::<pw::spa::sys::spa_meta_region>() as i32;
    let max = MAX_DAMAGE_REGIONS as i32;

    [
        buffer_blob(stride, height, allow_dmabuf),
        meta_blob(pw::spa::sys::SPA_META_Header, Value::Int(header)),
        meta_blob(pw::spa::sys::SPA_META_VideoTransform, Value::Int(transform)),
        meta_blob(
            pw::spa::sys::SPA_META_VideoDamage,
            damage_size_choice(region, max),
        ),
    ]
    .into_iter()
    .flatten()
    .collect()
}

/// Buffer-layout pod bytes (count/blocks/size/stride/align/dataType).
///
/// When `allow_dmabuf` is set the `SPA_DATA_DmaBuf` type is added to the allowed
/// data types so the server may hand us dmabuf buffers; the SHM types remain
/// allowed so an SHM frame mid-stream still works.
fn buffer_blob(stride: u32, height: u32, allow_dmabuf: bool) -> Option<Vec<u8>> {
    use pw::spa::{
        pod::{ChoiceValue, Object, Property, PropertyFlags, Value},
        utils::{Choice, ChoiceEnum, ChoiceFlags},
    };

    let size = stride.saturating_mul(height);
    let mut data_type =
        (1i32 << pw::spa::sys::SPA_DATA_MemFd) | (1i32 << pw::spa::sys::SPA_DATA_MemPtr);
    if allow_dmabuf {
        data_type |= 1i32 << pw::spa::sys::SPA_DATA_DmaBuf;
    }

    let int_prop = |key, value| Property {
        key,
        flags: PropertyFlags::empty(),
        value: Value::Int(value),
    };

    let object = Object {
        type_: pw::spa::sys::SPA_TYPE_OBJECT_ParamBuffers,
        id: pw::spa::sys::SPA_PARAM_Buffers,
        properties: vec![
            Property {
                key: pw::spa::sys::SPA_PARAM_BUFFERS_buffers,
                flags: PropertyFlags::empty(),
                value: Value::Choice(ChoiceValue::Int(Choice(
                    ChoiceFlags::empty(),
                    ChoiceEnum::Range {
                        default: 4,
                        min: 2,
                        max: 16,
                    },
                ))),
            },
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_blocks, 1),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_size, size as i32),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_stride, stride as i32),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_align, 16),
            int_prop(pw::spa::sys::SPA_PARAM_BUFFERS_dataType, data_type),
        ],
    };

    serialize_object(object)
}

/// The SPA video format matching a captured [`PixelFormat`].
fn spa_video_format(format: PixelFormat) -> pw::spa::param::video::VideoFormat {
    use pw::spa::param::video::VideoFormat;
    match format {
        PixelFormat::Bgrx => VideoFormat::BGRx,
        PixelFormat::Bgra => VideoFormat::BGRA,
        PixelFormat::Rgbx => VideoFormat::RGBx,
        PixelFormat::Rgba => VideoFormat::RGBA,
    }
}

/// `EnumFormat` pod bytes for each offered format: the dmabuf format (carrying
/// the bo's modifier, mandatory) first when `dmabuf_modifier` is `Some` so a
/// dmabuf-capable consumer prefers zero-copy, then the SHM format as the
/// guaranteed fallback. The caller keeps the returned bytes alive across
/// `connect`.
fn format_blobs(
    width: u32,
    height: u32,
    fps: u32,
    format: PixelFormat,
    dmabuf_modifier: Option<u64>,
) -> Vec<Vec<u8>> {
    let mut blobs = Vec::new();
    if let Some(modifier) = dmabuf_modifier
        && let Some(blob) = format_blob(width, height, fps, format, Some(modifier))
    {
        blobs.push(blob);
    }
    if let Some(blob) = format_blob(width, height, fps, format, None) {
        blobs.push(blob);
    }
    blobs
}

/// One `EnumFormat` object's bytes. With `modifier` set, adds a mandatory
/// `VideoModifier` property marking it a dmabuf format; without it, the plain
/// SHM format.
fn format_blob(
    width: u32,
    height: u32,
    fps: u32,
    format: PixelFormat,
    modifier: Option<u64>,
) -> Option<Vec<u8>> {
    use pw::spa::pod::{Object, Property, PropertyFlags, Value};

    let id_prop = |key, id: u32| Property {
        key,
        flags: PropertyFlags::empty(),
        value: Value::Id(pw::spa::utils::Id(id)),
    };

    let mut properties = vec![
        id_prop(
            pw::spa::sys::SPA_FORMAT_mediaType,
            pw::spa::sys::SPA_MEDIA_TYPE_video,
        ),
        id_prop(
            pw::spa::sys::SPA_FORMAT_mediaSubtype,
            pw::spa::sys::SPA_MEDIA_SUBTYPE_raw,
        ),
        id_prop(
            pw::spa::sys::SPA_FORMAT_VIDEO_format,
            spa_video_format(format).as_raw(),
        ),
    ];

    // Modifier (mandatory) marks this as a dmabuf format. A fixed single value
    // (not a choice): we offer exactly the modifier the bo was allocated with.
    if let Some(modifier) = modifier {
        properties.push(Property {
            key: pw::spa::sys::SPA_FORMAT_VIDEO_modifier,
            flags: PropertyFlags::MANDATORY,
            value: Value::Long(modifier as i64),
        });
    }

    properties.push(Property {
        key: pw::spa::sys::SPA_FORMAT_VIDEO_size,
        flags: PropertyFlags::empty(),
        value: Value::Rectangle(pw::spa::utils::Rectangle { width, height }),
    });
    properties.push(Property {
        key: pw::spa::sys::SPA_FORMAT_VIDEO_framerate,
        flags: PropertyFlags::empty(),
        value: Value::Fraction(pw::spa::utils::Fraction { num: fps, denom: 1 }),
    });

    let object = Object {
        type_: pw::spa::sys::SPA_TYPE_OBJECT_Format,
        id: pw::spa::sys::SPA_PARAM_EnumFormat,
        properties,
    };

    serialize_object(object)
}

/// Attaches a captured frame's dmabuf fd to a PipeWire buffer data slot,
/// returning `true` on success.
///
/// Returns `false` (so the caller falls back to the SHM copy) if the buffer's
/// data slot was not negotiated for `SPA_DATA_DmaBuf` — e.g. the consumer chose
/// the SHM format, or this is an SHM-only stream. Single-plane only: the packed
/// formats screencopy hands out are single-plane, and a multi-plane bo would
/// need one data block per plane (more than we declare), so we decline and copy.
///
/// dmabuf transport is best-effort and unverified on hardware; declining here
/// always degrades to the working SHM path.
fn attach_dmabuf(
    data: &mut pw::spa::buffer::Data,
    backing: &wayle_share_preview::buffer::DmabufBacking,
    frame_stride: i32,
) -> bool {
    // Only attach when the slot accepts DmaBuf. `type_` here is the negotiated
    // type (or, before negotiation, a bitmask of allowed types).
    let allowed = data.type_().as_raw();
    let accepts_dmabuf = allowed == pw::spa::sys::SPA_DATA_DmaBuf
        || (allowed & (1 << pw::spa::sys::SPA_DATA_DmaBuf)) != 0;
    if !accepts_dmabuf {
        return false;
    }
    if backing.planes.len() != 1 {
        return false;
    }
    let Some(plane) = backing.planes.first() else {
        return false;
    };

    let raw = std::ptr::from_ref(data.as_raw()).cast_mut();
    // SAFETY: `raw` points at the spa_data PipeWire allocated for this buffer
    // slot; `data` is uniquely borrowed here, so there is no aliasing write. The
    // dmabuf fd stays valid because the owning `Buffer`/`DmabufBacking` (and the
    // gbm bo) outlives this enqueue. We mirror xdph's `pwStreamAddBuffer` /
    // `enqueue` field writes for a dmabuf plane.
    unsafe {
        (*raw).type_ = pw::spa::sys::SPA_DATA_DmaBuf;
        (*raw).fd = i64::from(plane.fd);
        (*raw).mapoffset = 0;
        // dmabuf memory is not host-mapped here, so maxsize stays 0 (as xdph
        // does); the chunk advertises stride/offset and a non-zero size for
        // consumers that gate validity on chunk->size.
        (*raw).maxsize = 0;
        let chunk = (*raw).chunk;
        if !chunk.is_null() {
            (*chunk).offset = plane.offset;
            (*chunk).stride = if frame_stride > 0 {
                frame_stride
            } else {
                plane.stride as i32
            };
            // Non-zero so consumers that check chunk->size treat the buffer as
            // valid (matches xdph's "chosen by a fair d20" fallback of 9).
            (*chunk).size = if plane.stride == 0 { 9 } else { plane.stride };
        }
    }
    true
}

/// Writes the clamped damage rects into a `SPA_META_VideoDamage` meta block,
/// terminating with a zero-size region (matching xdph), without overrunning the
/// slots the consumer allocated.
fn write_damage(damage: &MetaVideoDamage, rects: &[(u32, u32, u32, u32)]) {
    // The meta wraps a `spa_meta`; its `.data` points at the region array and
    // `.size` is the byte capacity. Walk region-sized strides within `.size`.
    let meta = damage.as_raw();
    let base = meta.data.cast::<pw::spa::sys::spa_meta_region>();
    if base.is_null() {
        return;
    }
    let region_size = std::mem::size_of::<pw::spa::sys::spa_meta_region>();
    if region_size == 0 {
        return;
    }
    let capacity = meta.size as usize / region_size;
    if capacity == 0 {
        return;
    }

    // SAFETY: `base..base+capacity` is the region array PipeWire allocated for
    // this meta (declared in param_changed); `damage` is uniquely borrowed from
    // the owned buffer, so there is no aliasing write. We never write past
    // `capacity` slots — the loop leaves room for the terminator.
    unsafe {
        let mut i = 0;
        while i < rects.len() && i + 1 < capacity {
            let (x, y, w, h) = rects[i];
            let slot = base.add(i);
            (*slot).region = pw::spa::sys::spa_region {
                position: pw::spa::sys::spa_point {
                    x: x as i32,
                    y: y as i32,
                },
                size: pw::spa::sys::spa_rectangle {
                    width: w,
                    height: h,
                },
            };
            i += 1;
        }
        // Zero-size terminator in the next slot (always within capacity).
        let term = base.add(i);
        (*term).region = pw::spa::sys::spa_region {
            position: pw::spa::sys::spa_point { x: 0, y: 0 },
            size: pw::spa::sys::spa_rectangle {
                width: 0,
                height: 0,
            },
        };
    }
}

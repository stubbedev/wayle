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
    collections::VecDeque,
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

/// How many recent dmabuf-backed frames to keep alive (>= the max buffer count
/// we let the server negotiate in `buffer_blob`), so an attached dmabuf fd/bo
/// outlives the consumer's use of the PipeWire buffer it was handed to.
const KEEPALIVE_FRAMES: usize = 16;

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
    drop(first);

    // The output's rotation/flip, mapped to the SPA videotransform value. This
    // is constant for the lifetime of the stream, so compute it once and stamp
    // every buffer with it. Identity (0) for window capture.
    let transform = spa_video_transform(capturer.borrow().transform());

    // Clamp the requested rate to the output's actual refresh (no-op for window
    // capture, where the refresh is unknown).
    let fps = effective_fps(fps, capturer.borrow().refresh_mhz());

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
        // Recently-enqueued dmabuf-backed frames, kept alive: the dmabuf fd/bo we
        // attach to a PipeWire buffer must outlive the consumer's use of that
        // buffer, but `frame` would otherwise drop (closing the fd) at the end of
        // this callback. Bounded to the max negotiated buffer count so memory
        // stays flat. (A true reuse pool keyed by the pw buffer is the remaining
        // perf optimization; this just makes the dmabuf path memory-safe.)
        let dmabuf_keepalive =
            RefCell::new(VecDeque::<wayle_share_preview::buffer::Buffer>::new());
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

            let frame = match capturer.borrow_mut().capture(prefer_dmabuf) {
                Ok(frame) => frame,
                Err(err) => {
                    debug!(%err, "screencast: frame capture failed (skipped)");
                    return;
                }
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

            // Keep dmabuf-backed frames alive past this callback so the fd/bo we
            // attached outlives the consumer's use of the buffer; SHM frames have
            // been fully copied and can drop now.
            if dmabuf_attached {
                let mut ring = dmabuf_keepalive.borrow_mut();
                ring.push_back(frame);
                while ring.len() > KEEPALIVE_FRAMES {
                    ring.pop_front();
                }
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

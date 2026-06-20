//! PipeWire producer for ScreenCast streams.
//!
//! Each stream runs a dedicated thread with its own PipeWire main loop. The
//! loop owns a [`Capturer`] and a video output [`Stream`]; a timer fires at the
//! target frame rate, captures a fresh Wayland frame, and copies it into a
//! dequeued PipeWire buffer. The node id the frontend hands to the client is
//! read back over a channel once `connect` succeeds.
//!
//! SHM only for now (correct everywhere; dmabuf zero-copy is a later
//! optimization). Frames are `BGRx`, matching `wl_shm` `Xrgb8888`.

use std::{
    cell::RefCell,
    rc::Rc,
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
        mpsc,
    },
    thread::JoinHandle,
    time::Duration,
};

use pipewire as pw;
use tracing::{debug, error, warn};

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
    _show_cursor: bool,
    fps: u32,
) -> Result<StreamHandle, String> {
    let stop = Arc::new(AtomicBool::new(false));
    let (ready_tx, ready_rx) = mpsc::channel::<Result<(u32, u32, u32), String>>();

    let thread_stop = stop.clone();
    let join = std::thread::Builder::new()
        .name("wayle-screencast".to_owned())
        .spawn(move || run_loop(&target, fps.max(1), &thread_stop, &ready_tx))
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
    fps: u32,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) {
    if let Err(err) = run_loop_inner(target, fps, stop, ready) {
        // If we failed before reporting readiness, surface it to the caller.
        let _ = ready.send(Err(err));
    }
}

fn run_loop_inner(
    target: &CaptureTarget,
    fps: u32,
    stop: &Arc<AtomicBool>,
    ready: &mpsc::Sender<Result<(u32, u32, u32), String>>,
) -> Result<(), String> {
    pw::init();

    // Open the capturer and grab one frame up front to learn the stream size.
    let capturer = Capturer::open(target)?;
    let capturer = Rc::new(RefCell::new(capturer));
    let first = capturer
        .borrow_mut()
        .capture()
        .map_err(|e| format!("initial capture failed: {e}"))?;
    let (width, height) = (first.width, first.height);
    drop(first);

    let main_loop = pw::main_loop::MainLoop::new(None)
        .map_err(|e| format!("pipewire main loop: {e}"))?;
    let context =
        pw::context::Context::new(&main_loop).map_err(|e| format!("pipewire context: {e}"))?;
    let core = context
        .connect(None)
        .map_err(|e| format!("pipewire connect: {e}"))?;

    let stream = pw::stream::Stream::new(
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
        move |stream: &pw::stream::StreamRef| {
            let Some(mut pw_buffer) = stream.dequeue_buffer() else {
                return;
            };
            let datas = pw_buffer.datas_mut();
            let Some(data) = datas.first_mut() else {
                return;
            };
            match capturer.borrow_mut().capture() {
                Ok(frame) => {
                    let stride = frame.stride as usize;
                    let Some(dst) = data.data() else {
                        return;
                    };
                    // Read the captured frame straight into the mapped PipeWire
                    // buffer — no per-frame heap allocation, one copy.
                    match frame.read_into(dst) {
                        Ok(written) => {
                            let chunk = data.chunk_mut();
                            *chunk.offset_mut() = 0;
                            *chunk.stride_mut() = stride as i32;
                            *chunk.size_mut() = written as u32;
                        }
                        Err(err) => warn!(%err, "screencast: reading frame bytes failed"),
                    }
                }
                Err(err) => debug!(%err, "screencast: frame capture failed (skipped)"),
            }
        }
    };

    let _listener = stream
        .add_local_listener::<()>()
        .state_changed(|_, _, old, new| {
            debug!(?old, ?new, "screencast stream state changed");
        })
        .process(move |stream, _user_data| produce(stream))
        .register()
        .map_err(|e| format!("pipewire listener: {e}"))?;

    let mut params = format_params(width, height, fps);
    stream
        .connect(
            pw::spa::utils::Direction::Output,
            None,
            pw::stream::StreamFlags::DRIVER | pw::stream::StreamFlags::MAP_BUFFERS,
            &mut params,
        )
        .map_err(|e| format!("pipewire stream connect: {e}"))?;

    // The node id is assigned during connect.
    let node_id = stream.node_id();
    ready
        .send(Ok((node_id, width, height)))
        .map_err(|_| "caller dropped before node id delivery".to_owned())?;

    // Pace frame production with a loop timer at the target rate.
    let interval = Duration::from_nanos(1_000_000_000 / u64::from(fps));
    let timer = main_loop.loop_().add_timer({
        let stream = stream.clone();
        move |_| stream.trigger_process().unwrap_or(())
    });
    timer
        .update_timer(Some(interval), Some(interval))
        .into_result()
        .map_err(|e| format!("pipewire timer: {e}"))?;

    // Quit the loop when asked to stop.
    let quit = {
        let main_loop = main_loop.clone();
        let stop = stop.clone();
        main_loop.loop_().add_timer(move |_| {
            if stop.load(Ordering::SeqCst) {
                main_loop.quit();
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
        pw::spa::pod::property!(
            pw::spa::param::format::FormatProperties::VideoFramerate,
            Fraction,
            pw::spa::utils::Fraction { num: fps, denom: 1 }
        ),
    );

    let values: Vec<u8> = pw::spa::pod::serialize::PodSerializer::serialize(
        std::io::Cursor::new(Vec::new()),
        &pw::spa::pod::Value::Object(object),
    )
    .map(|(cursor, _)| cursor.into_inner())
    .unwrap_or_default();

    let leaked: &'static [u8] = Box::leak(values.into_boxed_slice());
    match pw::spa::pod::Pod::from_bytes(leaked) {
        Some(pod) => vec![pod],
        None => Vec::new(),
    }
}

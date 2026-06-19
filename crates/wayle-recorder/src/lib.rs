//! GStreamer-backed screen recorder with optional webcam picture-in-picture.
//!
//! [`Recorder`] owns a GStreamer pipeline. [`Recorder::start`] negotiates the
//! xdg-desktop-portal ScreenCast session, builds a pipeline from
//! [`RecordOptions`], and plays it; [`Recorder::stop`] finalizes the file.

mod options;
mod pipeline;
mod portal;

use std::{
    os::fd::OwnedFd,
    sync::{
        Mutex, PoisonError,
        atomic::{AtomicBool, Ordering},
    },
    time::Duration,
};

use futures::StreamExt;
use gst::prelude::*;
use gstreamer as gst;
pub use options::{
    AudioOptions, OutputFormat, RecordOptions, WebcamOptions, WebcamPosition,
};
use tokio::sync::mpsc::UnboundedSender;
use tracing::{info, warn};

/// How long to wait for the muxer to finalize on stop.
const EOS_TIMEOUT: Duration = Duration::from_secs(5);
/// How long to wait for the pipeline to reach `Playing` before treating the
/// start as failed. A live screencast prerolls quickly; anything slower than
/// this is a stuck negotiation, not a slow start.
const STARTUP_TIMEOUT: Duration = Duration::from_secs(10);

/// Errors from the recorder engine.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// GStreamer failed to initialize.
    #[error("gstreamer init failed: {0}")]
    Init(String),
    /// The ScreenCast portal failed or was denied.
    #[error("screencast portal failed: {0}")]
    Portal(String),
    /// The pipeline description failed to parse / build.
    #[error("pipeline build failed: {0}")]
    Pipeline(String),
    /// A pipeline state change failed.
    #[error("pipeline state change failed: {0}")]
    State(String),
    /// The pipeline reported a runtime error while starting (e.g. a missing
    /// encoder, a busy capture device, or an invalid output path).
    #[error("recording failed to start: {0}")]
    Capture(String),
    /// A recording is already in progress.
    #[error("recorder is already running")]
    AlreadyRunning,
    /// No recording is in progress.
    #[error("recorder is not running")]
    NotRunning,
}

/// An active recording: the running pipeline plus the portal fd that must
/// outlive it.
struct Active {
    pipeline: gst::Pipeline,
    _fd: OwnedFd,
    /// Set true by [`Recorder::stop`] so the bus monitor knows the EOS/teardown
    /// it is about to observe was requested, not a failure.
    stopping: std::sync::Arc<AtomicBool>,
    /// Bus-watch task; aborted on stop so it stops emitting events.
    monitor: tokio::task::JoinHandle<()>,
}

/// GStreamer screen-recorder engine.
///
/// Cheap to clone the handle by wrapping in `Arc`; methods take `&self` and
/// guard the pipeline behind a mutex.
pub struct Recorder {
    active: Mutex<Option<Active>>,
}

impl Recorder {
    /// Creates the engine, initializing GStreamer once.
    ///
    /// # Errors
    ///
    /// Returns [`Error::Init`] if GStreamer fails to initialize.
    pub fn new() -> Result<Self, Error> {
        gst::init().map_err(|e| Error::Init(e.to_string()))?;
        Ok(Self {
            active: Mutex::new(None),
        })
    }

    /// Whether a recording pipeline is currently running.
    #[must_use]
    pub fn is_running(&self) -> bool {
        self.active
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .is_some()
    }

    /// Starts recording with the given options.
    ///
    /// Negotiates the ScreenCast portal, builds the pipeline, sets it playing,
    /// and confirms it actually reached `Playing` before reporting success —
    /// so a missing encoder, busy device, or unwritable path surfaces as an
    /// error here instead of a silently dead recording. The portal file
    /// descriptor is held for the pipeline's lifetime.
    ///
    /// If the pipeline later dies on its own (source disconnect, disk full,
    /// encoder fault), a human-readable reason is sent on `term_tx` so the
    /// caller can tear down its UI state. Normal [`Recorder::stop`] does not
    /// emit on `term_tx`.
    ///
    /// # Errors
    ///
    /// Returns [`Error::AlreadyRunning`] if a recording is in progress, or a
    /// portal / pipeline / state / capture error otherwise.
    pub async fn start(
        &self,
        opts: &RecordOptions,
        term_tx: UnboundedSender<String>,
    ) -> Result<(), Error> {
        if self.is_running() {
            return Err(Error::AlreadyRunning);
        }

        let cast = portal::open_screencast(opts.show_cursor).await?;

        // Prefer a hardware encoder, but never let a flaky GPU encode path block
        // a recording: if the detected hardware encoder fails to launch or reach
        // Playing, rebuild on the always-available software path and try once
        // more before giving up. Reuses the same portal session, so the user is
        // never re-prompted.
        let built = pipeline::build(opts, &cast);
        info!(path = %opts.output_path, hardware = built.hardware, "starting recording pipeline");
        let pipeline = match launch_pipeline(&built.description) {
            Ok(pipeline) => pipeline,
            Err(reason) if built.hardware => {
                warn!(reason, "hardware encoder failed; retrying with software");
                let software = pipeline::build_software(opts, &cast);
                launch_pipeline(&software.description).map_err(Error::Capture)?
            }
            Err(reason) => return Err(Error::Capture(reason)),
        };

        let stopping = std::sync::Arc::new(AtomicBool::new(false));
        let monitor = spawn_monitor(&pipeline, stopping.clone(), term_tx);

        let mut guard = self.active.lock().unwrap_or_else(PoisonError::into_inner);
        *guard = Some(Active {
            pipeline,
            _fd: cast.fd,
            stopping,
            monitor,
        });
        Ok(())
    }

    /// Stops recording, sending EOS so the muxer finalizes the output file.
    ///
    /// # Errors
    ///
    /// Returns [`Error::NotRunning`] if no recording is in progress.
    pub fn stop(&self) -> Result<(), Error> {
        let active = self
            .active
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .take();
        let Some(active) = active else {
            return Err(Error::NotRunning);
        };

        // Tell the monitor the upcoming EOS/teardown is intentional, then stop
        // it watching so it doesn't report this as an unexpected failure.
        active.stopping.store(true, Ordering::SeqCst);
        active.monitor.abort();

        // EOS lets the muxer write its trailer; without it the file is corrupt.
        if !active.pipeline.send_event(gst::event::Eos::new()) {
            warn!("failed to send EOS to recording pipeline");
        }
        if let Some(bus) = active.pipeline.bus() {
            let _ = bus.timed_pop_filtered(
                gst::ClockTime::from_mseconds(EOS_TIMEOUT.as_millis() as u64),
                &[gst::MessageType::Eos, gst::MessageType::Error],
            );
        }
        if let Err(err) = active.pipeline.set_state(gst::State::Null) {
            warn!(error = %err, "failed to reset recording pipeline");
        }
        info!("recording stopped");
        Ok(())
    }

    /// Pauses or resumes the recording.
    ///
    /// # Errors
    ///
    /// Returns [`Error::NotRunning`] if no recording is in progress, or
    /// [`Error::State`] if the state change fails.
    pub fn set_paused(&self, paused: bool) -> Result<(), Error> {
        let guard = self.active.lock().unwrap_or_else(PoisonError::into_inner);
        let Some(active) = guard.as_ref() else {
            return Err(Error::NotRunning);
        };
        let state = if paused {
            gst::State::Paused
        } else {
            gst::State::Playing
        };
        active
            .pipeline
            .set_state(state)
            .map_err(|e| Error::State(e.to_string()))?;
        Ok(())
    }
}

/// Parses, plays, and confirms a single pipeline attempt.
///
/// Returns the running pipeline, or a human-readable reason on any failure
/// (parse, downcast, state change, or a failed/timed-out transition to
/// `Playing`). A failed pipeline is reset to `Null` before returning so it
/// releases its resources; the caller can then retry with another description.
fn launch_pipeline(description: &str) -> Result<gst::Pipeline, String> {
    let element = gst::parse::launch(description).map_err(|e| e.to_string())?;
    let pipeline = element
        .downcast::<gst::Pipeline>()
        .map_err(|_| String::from("parsed element is not a pipeline"))?;

    if let Err(e) = pipeline.set_state(gst::State::Playing) {
        let _ = pipeline.set_state(gst::State::Null);
        return Err(e.to_string());
    }

    // A live pipeline reaches Playing asynchronously, so set_state returning Ok
    // means nothing. Block until it actually settles; if it failed, scrape the
    // bus for the real reason and tear down.
    if let Err(reason) = confirm_playing(&pipeline) {
        let _ = pipeline.set_state(gst::State::Null);
        return Err(reason);
    }

    Ok(pipeline)
}

/// Blocks until the pipeline finishes its transition to `Playing`.
///
/// Returns `Err` with a human-readable reason if the transition failed or did
/// not complete within [`STARTUP_TIMEOUT`]; the reason prefers the actual
/// GStreamer error posted on the bus over the opaque state-change failure.
fn confirm_playing(pipeline: &gst::Pipeline) -> Result<(), String> {
    let timeout = gst::ClockTime::from_mseconds(STARTUP_TIMEOUT.as_millis() as u64);
    let (result, _current, _pending) = pipeline.state(timeout);
    match result {
        Ok(gst::StateChangeSuccess::Success | gst::StateChangeSuccess::NoPreroll) => Ok(()),
        // Still negotiating after the timeout: the portal/source never produced
        // a frame. Treat as failure rather than reporting a stalled recording.
        Ok(gst::StateChangeSuccess::Async) => Err(bus_error(pipeline)
            .unwrap_or_else(|| String::from("pipeline did not start within timeout"))),
        Err(err) => Err(bus_error(pipeline).unwrap_or_else(|| err.to_string())),
    }
}

/// Drains the pipeline bus and returns the first error message, if any.
fn bus_error(pipeline: &gst::Pipeline) -> Option<String> {
    let bus = pipeline.bus()?;
    while let Some(msg) = bus.pop() {
        if let gst::MessageView::Error(err) = msg.view() {
            let src = msg
                .src()
                .map_or_else(|| String::from("pipeline"), |s| s.name().to_string());
            return Some(format!("{src}: {}", err.error()));
        }
    }
    None
}

/// Spawns a task that watches the pipeline bus and reports the first
/// unexpected `Error` / `Eos` on `term_tx`. Does nothing once `stopping` is
/// set (a stop was requested, so the teardown is expected).
fn spawn_monitor(
    pipeline: &gst::Pipeline,
    stopping: std::sync::Arc<AtomicBool>,
    term_tx: UnboundedSender<String>,
) -> tokio::task::JoinHandle<()> {
    let Some(bus) = pipeline.bus() else {
        warn!("recording pipeline has no bus; cannot monitor for failures");
        return tokio::spawn(async {});
    };
    tokio::spawn(async move {
        let mut stream = bus.stream();
        while let Some(msg) = stream.next().await {
            if stopping.load(Ordering::SeqCst) {
                break;
            }
            let reason = match msg.view() {
                gst::MessageView::Error(err) => {
                    let src = msg
                        .src()
                        .map_or_else(|| String::from("pipeline"), |s| s.name().to_string());
                    Some(format!("{src}: {}", err.error()))
                }
                gst::MessageView::Eos(_) => Some(String::from("capture source ended unexpectedly")),
                _ => None,
            };
            if let Some(reason) = reason {
                warn!(reason, "recording pipeline terminated unexpectedly");
                let _ = term_tx.send(reason);
                break;
            }
        }
    })
}

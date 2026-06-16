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
    sync::{Mutex, PoisonError},
    time::Duration,
};

use gstreamer as gst;
use gst::prelude::*;
pub use options::{
    AudioOptions, EncoderPreset, OutputFormat, RecordOptions, WebcamOptions, WebcamPosition,
};
use tracing::{info, warn};

/// How long to wait for the muxer to finalize on stop.
const EOS_TIMEOUT: Duration = Duration::from_secs(5);

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
    /// Negotiates the ScreenCast portal, builds the pipeline, and sets it
    /// playing. The portal file descriptor is held for the pipeline's lifetime.
    ///
    /// # Errors
    ///
    /// Returns [`Error::AlreadyRunning`] if a recording is in progress, or a
    /// portal / pipeline / state error otherwise.
    pub async fn start(&self, opts: &RecordOptions) -> Result<(), Error> {
        if self.is_running() {
            return Err(Error::AlreadyRunning);
        }

        let cast = portal::open_screencast(opts.show_cursor).await?;
        let description = pipeline::build(opts, &cast);
        info!(path = %opts.output_path, "starting recording pipeline");

        let element =
            gst::parse::launch(&description).map_err(|e| Error::Pipeline(e.to_string()))?;
        let pipeline = element
            .downcast::<gst::Pipeline>()
            .map_err(|_| Error::Pipeline(String::from("parsed element is not a pipeline")))?;

        pipeline
            .set_state(gst::State::Playing)
            .map_err(|e| Error::State(e.to_string()))?;

        let mut guard = self.active.lock().unwrap_or_else(PoisonError::into_inner);
        *guard = Some(Active {
            pipeline,
            _fd: cast.fd,
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

//! Shared reactive state for the screen recorder.

use std::{
    path::PathBuf,
    sync::{Arc, Mutex, PoisonError},
    time::Duration,
};

use chrono::Local;
use tokio::time::interval;
use tokio_util::sync::CancellationToken;
use tracing::warn;
use wayle_config::{
    ConfigService,
    schemas::modules::{EncoderPreset, RecorderFormat, WebcamPosition},
};
use wayle_core::Property;
use wayle_recorder::{
    AudioOptions, EncoderPreset as EngineEncoderPreset, OutputFormat, RecordOptions, Recorder,
    WebcamOptions, WebcamPosition as EngineWebcamPosition,
};

/// Reactive recorder state shared between the D-Bus daemon and the bar module.
///
/// The bar module watches these properties to update its icon/label; the daemon
/// mutates them in response to CLI / RPC calls.
#[derive(Clone)]
pub struct RecorderState {
    /// Whether a recording is in progress.
    pub active: Property<bool>,
    /// Whether the active recording is paused.
    pub paused: Property<bool>,
    /// Elapsed recording time in seconds.
    pub elapsed_secs: Property<u32>,
    /// Path of the current/last output file.
    pub file_path: Property<String>,
    recorder: Arc<Recorder>,
    config: Arc<ConfigService>,
    timer_token: Arc<Mutex<CancellationToken>>,
}

impl RecorderState {
    /// Creates recorder state wrapping the given engine and config.
    pub fn new(recorder: Arc<Recorder>, config: Arc<ConfigService>) -> Self {
        Self {
            active: Property::new(false),
            paused: Property::new(false),
            elapsed_secs: Property::new(0),
            file_path: Property::new(String::new()),
            recorder,
            config,
            timer_token: Arc::new(Mutex::new(CancellationToken::new())),
        }
    }

    /// Starts a recording using the current config, if not already recording.
    pub async fn start(&self) {
        if self.active.get() {
            return;
        }

        let opts = self.build_options();
        let path = opts.output_path.clone();
        match self.recorder.start(&opts).await {
            Ok(()) => {
                self.file_path.set(path);
                self.elapsed_secs.set(0);
                self.paused.set(false);
                self.active.set(true);
                self.start_timer();
            }
            Err(err) => warn!(error = %err, "failed to start recording"),
        }
    }

    /// Stops the active recording.
    pub fn stop(&self) {
        if !self.active.get() {
            return;
        }
        self.cancel_timer();
        if let Err(err) = self.recorder.stop() {
            warn!(error = %err, "failed to stop recording");
        }
        self.active.set(false);
        self.paused.set(false);
        self.elapsed_secs.set(0);
    }

    /// Toggles recording on/off.
    pub async fn toggle(&self) {
        if self.active.get() {
            self.stop();
        } else {
            self.start().await;
        }
    }

    /// Pauses or resumes the active recording.
    pub fn set_paused(&self, paused: bool) {
        if !self.active.get() {
            return;
        }
        if let Err(err) = self.recorder.set_paused(paused) {
            warn!(error = %err, "failed to set recording pause state");
            return;
        }
        self.paused.set(paused);
    }

    fn build_options(&self) -> RecordOptions {
        let config = self.config.config();
        let rec = &config.modules.recorder;

        let format = match rec.output_format.get() {
            RecorderFormat::Mp4 => OutputFormat::Mp4,
            RecorderFormat::Mkv => OutputFormat::Mkv,
            RecorderFormat::Webm => OutputFormat::Webm,
        };

        let webcam = rec.webcam_enabled.get().then(|| WebcamOptions {
            device: rec.webcam_device.get(),
            position: map_position(rec.webcam_position.get()),
            size_percent: u32::from(rec.webcam_size.get().value()),
        });

        RecordOptions {
            output_path: output_path(&rec.output_directory.get(), format),
            format,
            framerate: rec.framerate.get(),
            bitrate_kbps: rec.bitrate_kbps.get(),
            preset: map_preset(rec.encoder_preset.get()),
            show_cursor: rec.show_cursor.get(),
            audio: AudioOptions {
                microphone: rec.microphone.get(),
                microphone_device: rec.microphone_device.get(),
                system_audio: rec.system_audio.get(),
                bitrate_kbps: rec.audio_bitrate_kbps.get(),
                separate_tracks: rec.separate_audio_tracks.get(),
            },
            webcam,
        }
    }

    fn start_timer(&self) {
        let token = CancellationToken::new();
        {
            let mut guard = self.timer_token.lock().unwrap_or_else(PoisonError::into_inner);
            *guard = token.clone();
        }

        let state = self.clone();
        tokio::spawn(async move {
            let mut tick = interval(Duration::from_secs(1));
            tick.tick().await;
            loop {
                tokio::select! {
                    () = token.cancelled() => break,
                    _ = tick.tick() => {
                        if !state.active.get() {
                            break;
                        }
                        if state.paused.get() {
                            continue;
                        }
                        state.elapsed_secs.set(state.elapsed_secs.get().saturating_add(1));
                    }
                }
            }
        });
    }

    fn cancel_timer(&self) {
        let guard = self.timer_token.lock().unwrap_or_else(PoisonError::into_inner);
        guard.cancel();
    }
}

fn map_preset(preset: EncoderPreset) -> EngineEncoderPreset {
    match preset {
        EncoderPreset::Speed => EngineEncoderPreset::Speed,
        EncoderPreset::Balanced => EngineEncoderPreset::Balanced,
        EncoderPreset::Quality => EngineEncoderPreset::Quality,
    }
}

fn map_position(position: WebcamPosition) -> EngineWebcamPosition {
    match position {
        WebcamPosition::TopLeft => EngineWebcamPosition::TopLeft,
        WebcamPosition::TopRight => EngineWebcamPosition::TopRight,
        WebcamPosition::BottomLeft => EngineWebcamPosition::BottomLeft,
        WebcamPosition::BottomRight => EngineWebcamPosition::BottomRight,
    }
}

/// Builds a timestamped output path in the configured (or default) directory.
fn output_path(configured_dir: &str, format: OutputFormat) -> String {
    let dir = if configured_dir.is_empty() {
        videos_dir()
    } else {
        PathBuf::from(configured_dir)
    };
    let name = format!(
        "wayle-{}.{}",
        Local::now().format("%Y%m%d-%H%M%S"),
        format.extension()
    );
    dir.join(name).to_string_lossy().into_owned()
}

/// Resolves the default recordings directory: `$XDG_VIDEOS_DIR` or `$HOME/Videos`.
fn videos_dir() -> PathBuf {
    if let Some(dir) = std::env::var_os("XDG_VIDEOS_DIR") {
        return PathBuf::from(dir);
    }
    std::env::var_os("HOME")
        .map(|home| PathBuf::from(home).join("Videos"))
        .unwrap_or_else(|| PathBuf::from("."))
}

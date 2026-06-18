//! Shared reactive state for the screen recorder.

use std::{
    path::PathBuf,
    process::Stdio,
    sync::{Arc, Mutex, PoisonError},
    time::Duration,
};

use chrono::Local;
use tokio::{process::Command, sync::mpsc, time::interval};
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

use crate::{
    i18n::t,
    services::widget_ipc::{ToastBus, ToastRequest},
};

/// Icon shown on the recorder toasts.
const TOAST_ICON: &str = "ld-circle-dot-symbolic";
/// Icon shown on recorder failure toasts/notifications.
const ERROR_ICON: &str = "ld-alert-triangle-symbolic";
/// How long the "starting" toast stays on screen, in milliseconds.
const START_TOAST_MS: u32 = 1000;
/// Delay between the start toast and the actual capture. Kept longer than
/// [`START_TOAST_MS`] (plus the OSD's exit animation) so the toast has cleared
/// the screen before recording begins — otherwise it ends up in the capture.
const START_CAPTURE_DELAY_MS: u64 = 1400;
/// How long the "stopped" toast stays on screen, in milliseconds.
const STOP_TOAST_MS: u32 = 1500;

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
    /// Whether a start has been requested and is in its pre-capture delay
    /// (the bar pulses its icon during this window).
    pub preparing: Property<bool>,
    /// Elapsed recording time in seconds.
    pub elapsed_secs: Property<u32>,
    /// Path of the current/last output file.
    pub file_path: Property<String>,
    recorder: Arc<Recorder>,
    config: Arc<ConfigService>,
    toast_bus: ToastBus,
    timer_token: Arc<Mutex<CancellationToken>>,
}

impl RecorderState {
    /// Creates recorder state wrapping the given engine and config.
    pub fn new(recorder: Arc<Recorder>, config: Arc<ConfigService>, toast_bus: ToastBus) -> Self {
        Self {
            active: Property::new(false),
            paused: Property::new(false),
            preparing: Property::new(false),
            elapsed_secs: Property::new(0),
            file_path: Property::new(String::new()),
            recorder,
            config,
            toast_bus,
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

        // filesink won't create missing directories; do it ourselves so a
        // first-ever recording into ~/Videos (or a custom dir) doesn't fail.
        if let Some(parent) = PathBuf::from(&path).parent()
            && let Err(err) = std::fs::create_dir_all(parent)
        {
            warn!(error = %err, dir = %parent.display(), "cannot create recording directory");
            self.show_error(&format!("{}: {err}", t!("recorder-toast-failed")));
            return;
        }

        // Announce the start and pulse the bar icon, then wait for the toast
        // to clear the screen before capture begins — otherwise the toast is
        // in the recording.
        self.show_toast(&t!("recorder-toast-starting"), START_TOAST_MS);
        self.preparing.set(true);
        tokio::time::sleep(Duration::from_millis(START_CAPTURE_DELAY_MS)).await;

        let (term_tx, term_rx) = mpsc::unbounded_channel();
        match self.recorder.start(&opts, term_tx).await {
            Ok(()) => {
                self.file_path.set(path);
                self.elapsed_secs.set(0);
                self.paused.set(false);
                // Flip active before clearing preparing so the icon goes
                // straight from pulsing to solid-recording, with no idle flash.
                self.active.set(true);
                self.preparing.set(false);
                self.start_timer();
                self.watch_termination(term_rx);
            }
            Err(err) => {
                self.preparing.set(false);
                warn!(error = %err, "failed to start recording");
                self.show_error(&format!("{}: {err}", t!("recorder-toast-failed")));
            }
        }
    }

    /// Watches for an unexpected pipeline death (source disconnect, disk full,
    /// encoder fault) reported by the engine, and resets UI state + notifies
    /// the user if one arrives.
    fn watch_termination(&self, mut term_rx: mpsc::UnboundedReceiver<String>) {
        let state = self.clone();
        tokio::spawn(async move {
            if let Some(reason) = term_rx.recv().await {
                state.handle_unexpected_stop(&reason);
            }
        });
    }

    /// Tears down a recording that died on its own and tells the user why.
    fn handle_unexpected_stop(&self, reason: &str) {
        if !self.active.get() {
            return;
        }
        warn!(reason, "recording terminated unexpectedly");
        self.cancel_timer();
        // Best-effort teardown of the (already failed) pipeline.
        let _ = self.recorder.stop();
        self.active.set(false);
        self.paused.set(false);
        self.elapsed_secs.set(0);
        self.show_error(&format!("{}: {reason}", t!("recorder-toast-failed")));
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

        // Only claim success if the muxer actually wrote a non-empty file;
        // otherwise the capture died and "saved" would be a lie.
        let path = self.file_path.get();
        let saved = std::fs::metadata(&path).is_ok_and(|m| m.len() > 0);
        if saved {
            self.show_toast(&t!("recorder-toast-stopped"), STOP_TOAST_MS);
            self.notify_saved(&path);
        } else {
            warn!(path = %path, "recording produced no output file");
            self.show_error(&t!("recorder-toast-failed"));
        }
    }

    /// Publishes a recorder toast to the OSD.
    fn show_toast(&self, label: &str, duration_ms: u32) {
        self.toast_bus.publish(ToastRequest {
            label: Some(label.to_owned()),
            icon: Some(TOAST_ICON.to_owned()),
            percentage: None,
            duration_ms: Some(duration_ms),
            preset: None,
            class: None,
        });
    }

    /// Shows a failure toast and fires a desktop notification so the user is
    /// never left guessing why a recording silently stopped.
    fn show_error(&self, message: &str) {
        self.toast_bus.publish(ToastRequest {
            label: Some(message.to_owned()),
            icon: Some(ERROR_ICON.to_owned()),
            percentage: None,
            duration_ms: Some(STOP_TOAST_MS),
            preset: None,
            class: None,
        });
        self.notify(&t!("recorder-notification-failed"), message, ERROR_ICON);
    }

    /// Spawns a fire-and-forget `notify-send` with the given summary/body/icon.
    fn notify(&self, summary: &str, body: &str, icon: &str) {
        let mut command = Command::new("notify-send");
        command
            .arg("--app-name=Wayle")
            .arg(format!("--icon={icon}"))
            .arg(summary)
            .arg(body)
            .stdout(Stdio::null())
            .stderr(Stdio::null());

        match command.spawn() {
            Ok(child) => {
                tokio::spawn(async move {
                    let _ = child.wait_with_output().await;
                });
            }
            Err(err) => warn!(error = %err, "cannot spawn notify-send"),
        }
    }

    /// Fires a desktop notification (via `notify-send`) reporting where the
    /// recording was saved. No-op when the path is empty.
    fn notify_saved(&self, path: &str) {
        if path.is_empty() {
            return;
        }
        self.notify(
            &t!("recorder-notification-saved"),
            path,
            "ld-video-symbolic",
        );
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
            let mut guard = self
                .timer_token
                .lock()
                .unwrap_or_else(PoisonError::into_inner);
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
        let guard = self
            .timer_token
            .lock()
            .unwrap_or_else(PoisonError::into_inner);
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

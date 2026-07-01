//! Shared reactive state for the screen recorder.

use std::{
    path::PathBuf,
    sync::{Arc, Mutex, PoisonError},
    time::Duration,
};

use chrono::Local;
use tokio::{sync::mpsc, time::interval};
use tokio_util::sync::CancellationToken;
use tracing::warn;
use wayle_config::{ConfigService, schemas::modules::RecorderFormat};
use wayle_core::Property;
use wayle_recorder::{AudioOptions, OutputFormat, RecordOptions, Recorder, WebcamOptions};

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
/// How long the "stopped" toast stays on screen, in milliseconds.
const STOP_TOAST_MS: u32 = 1500;

/// Lifecycle of a recording. This is the single source of truth that gates
/// start/stop, updated synchronously so it never has the gap the public
/// `active` property does — `active` only flips true *after* the pre-capture
/// delay and portal negotiation, which is too late to dedupe rapid clicks.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
enum Status {
    /// Nothing running; a `start` is allowed.
    Idle,
    /// A start was requested and is in its pre-capture delay / portal
    /// negotiation. No pipeline is recording yet, and `active` is still false.
    Starting,
    /// A pipeline is actively recording.
    Recording,
    /// A stop is in progress, or has cancelled an in-flight start. Blocks
    /// further starts/stops until it settles back to `Idle`.
    Stopping,
}

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
    /// Authoritative lifecycle gate; see [`Status`].
    status: Arc<Mutex<Status>>,
    /// Cancels the current in-flight `start` (its pre-capture delay / portal
    /// negotiation) so a stop can abort a recording before it begins.
    start_cancel: Arc<Mutex<CancellationToken>>,
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
            status: Arc::new(Mutex::new(Status::Idle)),
            start_cancel: Arc::new(Mutex::new(CancellationToken::new())),
        }
    }

    fn lock_status(&self) -> std::sync::MutexGuard<'_, Status> {
        self.status.lock().unwrap_or_else(PoisonError::into_inner)
    }

    /// Replaces the in-flight start's cancellation token with a fresh one for a
    /// new attempt, returning the new token.
    fn arm_start_cancel(&self) -> CancellationToken {
        let token = CancellationToken::new();
        *self
            .start_cancel
            .lock()
            .unwrap_or_else(PoisonError::into_inner) = token.clone();
        token
    }

    /// Cancels any in-flight start so it tears itself down.
    fn cancel_start(&self) {
        self.start_cancel
            .lock()
            .unwrap_or_else(PoisonError::into_inner)
            .cancel();
    }

    /// Returns to the idle state: stops the timer and clears every UI property.
    fn reset_idle(&self) {
        self.cancel_timer();
        *self.lock_status() = Status::Idle;
        self.preparing.set(false);
        self.active.set(false);
        self.paused.set(false);
        self.elapsed_secs.set(0);
    }

    /// Starts a recording using the current config, if not already recording.
    ///
    /// Idempotent against rapid clicks: the `Idle -> Starting` transition is
    /// claimed synchronously under [`Self::status`], so a second call while a
    /// start is still negotiating (or while recording) is a no-op rather than a
    /// duplicate portal session / pipeline.
    pub async fn start(&self) {
        // Claim the start atomically. Anything other than Idle means a start,
        // recording, or stop is already in flight — bail.
        {
            let mut status = self.lock_status();
            if *status != Status::Idle {
                return;
            }
            *status = Status::Starting;
        }

        // Fresh cancellation token for this attempt; `stop` cancels it to abort
        // a start that is still in its delay / portal negotiation.
        let cancel = self.arm_start_cancel();
        self.preparing.set(true);

        // Drive the rest (toast delay, portal negotiation, pipeline launch) on a
        // tokio task so the caller returns immediately: the heavy/blocking work
        // never runs on the GTK main thread (dropdown path) or stalls the D-Bus
        // handler (bar/CLI path). The synchronous `preparing` pulse above is the
        // immediate feedback.
        let state = self.clone();
        tokio::spawn(async move { state.run_start(cancel).await });
    }

    /// Body of a start attempt: announce it, wait out the pre-capture delay,
    /// negotiate the portal, launch the pipeline, and commit to Recording —
    /// unless a `stop` cancels us first (by cancelling [`Self::start_cancel`]).
    async fn run_start(&self, cancel: CancellationToken) {
        let opts = self.build_options();
        let path = opts.output_path.clone();

        // filesink won't create missing directories; do it ourselves so a
        // first-ever recording into ~/Videos (or a custom dir) doesn't fail.
        if let Some(parent) = PathBuf::from(&path).parent()
            && let Err(err) = std::fs::create_dir_all(parent)
        {
            warn!(error = %err, dir = %parent.display(), "cannot create recording directory");
            self.reset_idle();
            self.show_error(&format!("{}: {err}", t!("recorder-toast-failed")));
            return;
        }

        // Open the portal session first so the source picker appears
        // immediately on the user's action, not after the delay below.
        // Cancellable: a stop while the picker is up drops the open_session
        // future (cancelling the D-Bus call) so the lifecycle doesn't wedge in
        // Stopping until the user dismisses the picker.
        let cast = tokio::select! {
            () = cancel.cancelled() => {
                self.reset_idle();
                return;
            }
            result = self.recorder.open_session(opts.show_cursor) => match result {
                Ok(cast) => cast,
                Err(err) => {
                    self.reset_idle();
                    warn!(error = %err, "failed to start recording");
                    self.show_error(&format!("{}: {err}", t!("recorder-toast-failed")));
                    return;
                }
            },
        };

        // With the source chosen, announce the start and pulse the bar icon,
        // then wait for the toast to clear the screen before capture begins —
        // otherwise the toast is in the recording. Cancellable: a stop during
        // this window aborts cleanly without ever launching the pipeline.
        let delay_ms = u64::from(self.config.config().modules.recorder.start_delay_ms.get());
        self.show_toast(&t!("recorder-toast-starting"), START_TOAST_MS);
        tokio::select! {
            () = cancel.cancelled() => {
                self.reset_idle();
                return;
            }
            () = tokio::time::sleep(Duration::from_millis(delay_ms)) => {}
        }

        let (term_tx, term_rx) = mpsc::unbounded_channel();
        match self.recorder.start(cast, &opts, term_tx) {
            Ok(()) => {
                // A stop may have arrived while the portal/pipeline negotiated.
                // Commit to Recording only if nothing cancelled us meanwhile;
                // decide under the status lock so it can't race `stop`.
                let mut status = self.lock_status();
                if *status != Status::Starting || cancel.is_cancelled() {
                    drop(status);
                    let _ = self.recorder.stop();
                    self.reset_idle();
                    return;
                }
                *status = Status::Recording;
                drop(status);

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
                self.reset_idle();
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
        // Only act on a live recording; ignore if a normal stop already ran.
        {
            let mut status = self.lock_status();
            if *status != Status::Recording {
                return;
            }
            *status = Status::Stopping;
        }
        warn!(reason, "recording terminated unexpectedly");
        // Best-effort teardown of the (already failed) pipeline.
        let _ = self.recorder.stop();
        self.reset_idle();
        self.show_error(&format!("{}: {reason}", t!("recorder-toast-failed")));
    }

    /// Stops the active recording (or cancels one that is still starting).
    ///
    /// Idempotent: only the first call from `Recording`/`Starting` does work;
    /// repeats while already `Stopping`/`Idle` are no-ops, so a double-press
    /// can't double-stop or accidentally start a new recording.
    pub fn stop(&self) {
        // Claim the stop. From Recording we own teardown here; from Starting we
        // only flag Stopping + cancel, and let the in-flight `start` (which owns
        // the half-built pipeline) tear itself down and return to Idle.
        let prev = {
            let mut status = self.lock_status();
            match *status {
                Status::Idle | Status::Stopping => return,
                prev => {
                    *status = Status::Stopping;
                    prev
                }
            }
        };

        // Abort any start still in its delay / portal negotiation.
        self.cancel_start();

        if prev == Status::Starting {
            // The in-flight `start` observes the cancellation and resets to
            // Idle itself. Nothing was recorded, so no "saved" toast.
            return;
        }

        // prev == Recording. Flip the UI to stopped *now*, before the blocking
        // muxer finalize below. `recorder.stop()` sends EOS and blocks until the
        // muxer writes its trailer (up to EOS_TIMEOUT, ~5s); if we left `active`
        // true across it the bar icon would keep showing "recording" the whole
        // time, and the user — seeing no feedback — presses again, which no-ops
        // while we are Stopping. Status stays Stopping until the finalize
        // completes, so no new recording can start mid-teardown.
        self.cancel_timer();
        self.active.set(false);
        self.paused.set(false);
        self.elapsed_secs.set(0);
        let path = self.file_path.get();

        // Run the blocking finalize off the async executor so it never stalls
        // the D-Bus handler / tokio worker, then settle to Idle and report
        // where the file landed.
        let state = self.clone();
        tokio::task::spawn_blocking(move || {
            if let Err(err) = state.recorder.stop() {
                warn!(error = %err, "failed to stop recording");
            }
            *state.lock_status() = Status::Idle;

            // Only claim success if the muxer actually wrote a non-empty file;
            // otherwise the capture died and "saved" would be a lie.
            let saved = std::fs::metadata(&path).is_ok_and(|m| m.len() > 0);
            if saved {
                state.show_toast(&t!("recorder-toast-stopped"), STOP_TOAST_MS);
                state.notify_saved(&path);
            } else {
                warn!(path = %path, "recording produced no output file");
                state.show_error(&t!("recorder-toast-failed"));
            }
        });
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

    /// Fires a fire-and-forget desktop notification.
    fn notify(&self, summary: &str, body: &str, icon: &str) {
        crate::notify::notify("Wayle", summary, body, icon);
    }

    /// Fires a desktop notification reporting where the recording was saved.
    /// No-op when the path is empty.
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
    ///
    /// Keys off the lifecycle status rather than `active`: during the
    /// pre-capture delay `active` is still false, but a toggle should cancel
    /// the pending start instead of kicking off a second one.
    pub async fn toggle(&self) {
        let idle = *self.lock_status() == Status::Idle;
        if idle {
            self.start().await;
        } else {
            self.stop();
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
            x_percent: u32::from(rec.webcam_x.get().value()),
            y_percent: u32::from(rec.webcam_y.get().value()),
            size_percent: u32::from(rec.webcam_size.get().value()),
        });

        RecordOptions {
            output_path: output_path(&rec.output_directory.get(), format),
            format,
            framerate: rec.framerate.get(),
            show_cursor: rec.show_cursor.get(),
            audio: AudioOptions {
                microphone: rec.microphone.get(),
                microphone_device: rec.microphone_device.get(),
                system_audio: rec.system_audio.get(),
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

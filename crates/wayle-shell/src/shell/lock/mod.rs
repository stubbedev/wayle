//! Secure session lock screen backed by `ext-session-lock-v1`.
//!
//! Unlike the layer-shell overlays in this crate, the lock surface is created
//! through [`gtk4_session_lock`]: the compositor blanks every output, routes
//! input only to our surfaces, and—critically—keeps the session locked with a
//! solid color if this process dies. We never expose an unlock path until the
//! surfaces are mapped, and password verification (via [`wayle_auth`]) runs on
//! a worker thread so the GTK loop cannot be stalled into skipping the grab.
//!
//! One [`gtk::Window`] is created per monitor and handed to the lock instance
//! via [`Instance::assign_window_to_monitor`]. The instance, the windows, and
//! the per-window widgets are all owned by the model and torn down on unlock.

mod logind;

use std::time::{Duration, Instant};

use gtk4_session_lock::Instance;
use relm4::{
    gtk,
    gtk::{EventControllerKey, gdk, glib, prelude::*},
    prelude::*,
};
use tracing::{info, warn};
use wayle_auth::{AuthEvent, AuthHandle, AuthPrompt, PamAuth};
use wayle_config::{
    ConfigService,
    schemas::{animations::AnimSurface, lock::LockBackground},
};
use wayle_widgets::components::credential_box::{CredentialBox, CredentialOpts};

use crate::shell::helpers::{animation::revealer_transition, monitors::current_monitors};

/// Per-monitor widgets we keep live to drive the clock, password entry, and
/// blank scrim while the screen is locked.
struct Surface {
    window: gtk::Window,
    entry: gtk::PasswordEntry,
    clock: gtk::Label,
    date: gtk::Label,
    error: gtk::Label,
    /// Reveals the credential box via the shared `[animations]` framework.
    reveal: gtk::Revealer,
    /// Opaque black overlay shown after `blank-timeout-ms` of inactivity.
    scrim: gtk::Box,
}

/// Resolved background configuration for a lock session.
struct BgConfig {
    mode: LockBackground,
    color: wayle_config::schemas::styling::HexColor,
    /// Image path for `Image`/`Wallpaper` modes; empty for `Color`.
    image: String,
    /// Gaussian blur radius for image/wallpaper backgrounds (0 = none).
    blur: u32,
}

/// Lock screen component. Owns the session-lock instance and surfaces.
pub(crate) struct Lock {
    config: std::sync::Arc<ConfigService>,
    /// `None` until first lock; reused across lock/unlock cycles.
    instance: Option<Instance>,
    surfaces: Vec<Surface>,
    /// Failed password attempts in the current lock session.
    attempts: u32,
    /// When the current lock began; drives the password-free grace window.
    locked_at: Option<Instant>,
    /// Handle to the in-flight auth conversation; `None` when idle.
    auth: Option<AuthHandle>,
    /// A prompt is on screen waiting for the user's next submit.
    awaiting: bool,
    /// A submitted value waiting to answer the conversation's first prompt
    /// (the common "type password, hit enter" case where the value exists
    /// before the backend asks for it).
    pending: Option<String>,
    /// 1s clock refresh source, removed on unlock.
    clock_source: Option<glib::SourceId>,
    /// One-shot blank-screen source, re-armed on activity.
    blank_source: Option<glib::SourceId>,
}

#[derive(Debug)]
pub(crate) enum LockInput {
    /// Acquire the lock and show surfaces (idempotent while locked).
    Lock,
    /// External unlock request (logind `Unlock` signal). Tears down surfaces
    /// without a password — only honored when something else (logind/PAM
    /// agent) has authorized it.
    ForceUnlock,
    /// The password entry was activated; verify `0`.
    Submit(String),
    /// The auth conversation produced an event (prompt / success / failure).
    Auth(AuthEvent),
    /// Refresh the clock/date labels.
    Tick,
    /// User input observed; unblank and re-arm the blank timer.
    Activity,
    /// Blank timer fired; show the black scrim.
    Blank,
}

#[relm4::component(pub(crate))]
impl Component for Lock {
    type Init = std::sync::Arc<ConfigService>;
    type Input = LockInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        // Placeholder root; the real per-monitor lock surfaces are created in
        // `acquire()` and never share this window. It is never mapped.
        #[root]
        gtk::Window {
            set_decorated: false,
            set_visible: false,
        }
    }

    fn init(
        config: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        // Bridge for the CLI/IPC `lock` command and the logind signal listener.
        crate::services::lock::register_sender(sender.input_sender().clone());

        // Listen for logind Lock/Unlock so `loginctl lock-session`, idle
        // daemons, and `wayle lock` all drive this component.
        let input = sender.input_sender().clone();
        relm4::spawn(async move {
            logind::listen(input).await;
        });

        let model = Lock {
            config,
            instance: None,
            surfaces: Vec::new(),
            attempts: 0,
            locked_at: None,
            auth: None,
            awaiting: false,
            pending: None,
            clock_source: None,
            blank_source: None,
        };
        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: LockInput, sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            LockInput::Lock => self.acquire(&sender),
            LockInput::ForceUnlock => self.release(),
            LockInput::Submit(password) => self.submit(password, &sender),
            LockInput::Auth(event) => self.on_auth_event(event),
            LockInput::Tick => self.refresh_clock(),
            LockInput::Activity => self.on_activity(&sender),
            LockInput::Blank => self.set_blanked(true),
        }
    }
}

impl Lock {
    /// Whether Wayle should handle locking (config `lock.enabled`).
    fn enabled(&self) -> bool {
        self.config.config().lock.enabled.get()
    }

    /// Resolves the configured background for a new lock session.
    fn background_config(&self) -> BgConfig {
        let cfg = self.config.config();
        let mode = cfg.lock.background_mode.get();
        let image = match mode {
            LockBackground::Image => cfg.lock.background_image.get(),
            LockBackground::Wallpaper => cfg.wallpaper.wallpaper.get(),
            LockBackground::Color => String::new(),
        };
        BgConfig {
            mode,
            color: cfg.lock.background_color.get(),
            image,
            blur: cfg.lock.blur.get(),
        }
    }

    /// Resolves the credential-box reveal animation `(transition, duration_ms)`
    /// from the shared `[animations]` config (`AnimSurface::Lock`, entering).
    fn reveal_anim(&self) -> (gtk::RevealerTransitionType, u32) {
        let anim = &self.config.config().animations;
        (
            revealer_transition(anim.transition_for(AnimSurface::Lock, false)),
            anim.duration_for(AnimSurface::Lock, false),
        )
    }

    /// Whether a lock can be acquired now (enabled, not already locked, and the
    /// compositor supports `ext-session-lock-v1`). Logs the reason when not.
    fn can_acquire(&self) -> bool {
        if !self.enabled() {
            info!("lock: disabled in config; ignoring lock request");
            return false;
        }
        if self.instance.as_ref().is_some_and(Instance::is_locked) {
            return false; // already locked
        }
        if !gtk4_session_lock::is_supported() {
            warn!("lock: compositor does not support ext-session-lock-v1; cannot lock");
            return false;
        }
        true
    }

    /// Acquires the session lock and builds one surface per monitor.
    fn acquire(&mut self, sender: &ComponentSender<Self>) {
        if !self.can_acquire() {
            return;
        }

        // Read all config before borrowing the instance mutably, so building
        // surfaces (which borrows the instance) doesn't conflict.
        let bg = self.background_config();
        let show_clock = self.config.config().lock.show_clock.get();
        let reveal = self.reveal_anim();

        let instance = self.instance.get_or_insert_with(Instance::new);
        if !instance.lock() {
            warn!("lock: failed to acquire session lock");
            return;
        }

        self.attempts = 0;
        // Reset conversation state by field (a `reset_auth()` call here would
        // re-borrow `self` while the `instance` borrow below is still live).
        self.auth = None;
        self.awaiting = false;
        self.pending = None;
        self.locked_at = Some(Instant::now());
        self.surfaces = build_surfaces(instance, &bg, show_clock, reveal, sender);

        if let Some(first) = self.surfaces.first() {
            first.entry.grab_focus();
        }

        self.start_clock(sender);
        self.arm_blank(sender);
        self.set_locked_hint(true);
        info!(monitors = self.surfaces.len(), "lock: session locked");
    }

    /// Handles a submitted entry value (or honors the grace window).
    ///
    /// The first submit of a lock session starts a PAM conversation; the typed
    /// value is stashed in `pending` to answer that conversation's first
    /// prompt. A submit made while a later prompt is on screen (`awaiting` — a
    /// re-prompt such as an expired-password change) answers it directly.
    fn submit(&mut self, value: String, sender: &ComponentSender<Self>) {
        if self.attempts_exhausted() {
            return;
        }

        // A prompt is waiting for input: answer it directly.
        if self.awaiting {
            self.awaiting = false;
            if let Some(handle) = self.auth.as_ref() {
                handle.answer(Some(value));
            }
            self.set_entries_sensitive(false);
            return;
        }

        // Conversation already running but no prompt is pending: ignore the
        // stray submit rather than racing the worker.
        if self.auth.is_some() {
            return;
        }

        // Grace window: unlock without a password if still within it.
        let grace = self.config.config().lock.grace_period_ms.get();
        if grace > 0
            && self
                .locked_at
                .is_some_and(|t| t.elapsed() < Duration::from_millis(u64::from(grace)))
        {
            info!("lock: unlocked within grace window");
            self.release();
            return;
        }

        if value.is_empty() {
            return;
        }

        self.set_entries_sensitive(false);
        self.start_conversation(value, sender);
    }

    /// Spawns a PAM conversation, stashing `first_answer` for its first prompt.
    fn start_conversation(&mut self, first_answer: String, sender: &ComponentSender<Self>) {
        let service = self.config.config().lock.pam_service.get();
        let input = sender.input_sender().clone();
        self.pending = Some(first_answer);
        self.auth = Some(wayle_auth::spawn(
            PamAuth::new(service),
            Some(wayle_auth::current_username()),
            move |event| input.emit(LockInput::Auth(event)),
        ));
    }

    /// Applies an event from the running auth conversation.
    fn on_auth_event(&mut self, event: AuthEvent) {
        match event {
            AuthEvent::Prompt(prompt) => self.on_auth_prompt(prompt),
            AuthEvent::Success => {
                info!("lock: authentication succeeded");
                self.release();
            }
            AuthEvent::Failure(reason) => self.on_auth_failure(&reason),
        }
    }

    /// Routes a conversation prompt: answer input prompts from `pending` if a
    /// value is already queued, otherwise surface the prompt and wait for the
    /// next submit. Info/error prompts only update the on-screen text.
    fn on_auth_prompt(&mut self, prompt: AuthPrompt) {
        match prompt {
            AuthPrompt::Secret(_) | AuthPrompt::Visible(_) => {
                if let Some(answer) = self.pending.take() {
                    if let Some(handle) = self.auth.as_ref() {
                        handle.answer(Some(answer));
                    }
                } else {
                    // A re-prompt with no queued value: re-enable input and let
                    // the user respond. The next Submit answers it.
                    self.awaiting = true;
                    self.set_entries_sensitive(true);
                    for s in &self.surfaces {
                        s.entry.set_text("");
                    }
                    if let Some(first) = self.surfaces.first() {
                        first.entry.grab_focus();
                    }
                }
            }
            AuthPrompt::Info(text) | AuthPrompt::Error(text) => {
                for s in &self.surfaces {
                    s.error.set_text(&text);
                    s.error.set_visible(true);
                }
            }
        }
    }

    /// Applies a failed/aborted conversation: counts the attempt and re-arms
    /// the entry unless the cap is reached.
    fn on_auth_failure(&mut self, reason: &str) {
        self.reset_auth();
        self.attempts = self.attempts.saturating_add(1);
        warn!(attempts = self.attempts, %reason, "lock: authentication failed");
        self.set_entries_sensitive(!self.attempts_exhausted());
        self.show_error();
        for s in &self.surfaces {
            s.entry.set_text("");
        }
        if let Some(first) = self.surfaces.first()
            && !self.attempts_exhausted()
        {
            first.entry.grab_focus();
        }
    }

    /// Drops any in-flight conversation and clears its transient state.
    fn reset_auth(&mut self) {
        self.auth = None;
        self.awaiting = false;
        self.pending = None;
    }

    /// Tears down all surfaces and releases the lock.
    fn release(&mut self) {
        if let Some(id) = self.clock_source.take() {
            id.remove();
        }
        if let Some(id) = self.blank_source.take() {
            id.remove();
        }
        for surface in self.surfaces.drain(..) {
            surface.window.destroy();
        }
        if let Some(instance) = self.instance.as_ref()
            && instance.is_locked()
        {
            instance.unlock();
        }
        self.locked_at = None;
        self.attempts = 0;
        self.reset_auth();
        self.set_locked_hint(false);
        info!("lock: session unlocked");
    }

    /// `true` when the configured attempt cap has been reached (`0` = no cap).
    fn attempts_exhausted(&self) -> bool {
        let max = self.config.config().lock.max_attempts.get();
        max > 0 && self.attempts >= max
    }

    fn set_entries_sensitive(&self, sensitive: bool) {
        for s in &self.surfaces {
            s.entry.set_sensitive(sensitive);
        }
    }

    fn show_error(&self) {
        let show = self.config.config().lock.show_failed_attempts.get();
        let text = if self.attempts_exhausted() {
            crate::i18n::t!("lock-locked-out")
        } else if show {
            crate::i18n::t!("lock-failed-attempts", count = self.attempts.to_string())
        } else {
            crate::i18n::t!("lock-incorrect")
        };
        for s in &self.surfaces {
            s.error.set_text(&text);
            s.error.set_visible(true);
        }
    }

    fn refresh_clock(&self) {
        let cfg = self.config.config();
        let now = chrono::Local::now();
        let time = now.format(&cfg.lock.clock_format.get()).to_string();
        let date = now.format(&cfg.lock.date_format.get()).to_string();
        for s in &self.surfaces {
            s.clock.set_text(&time);
            s.date.set_text(&date);
        }
    }

    fn start_clock(&mut self, sender: &ComponentSender<Self>) {
        self.refresh_clock();
        let input = sender.input_sender().clone();
        let id = glib::timeout_add_seconds_local(1, move || {
            input.emit(LockInput::Tick);
            glib::ControlFlow::Continue
        });
        self.clock_source = Some(id);
    }

    /// Re-arms the one-shot blank timer (and removes any pending one).
    fn arm_blank(&mut self, sender: &ComponentSender<Self>) {
        if let Some(id) = self.blank_source.take() {
            id.remove();
        }
        let ms = self.config.config().lock.blank_timeout_ms.get();
        if ms == 0 {
            return;
        }
        let input = sender.input_sender().clone();
        let id = glib::timeout_add_local(Duration::from_millis(u64::from(ms)), move || {
            input.emit(LockInput::Blank);
            glib::ControlFlow::Break
        });
        self.blank_source = Some(id);
    }

    fn on_activity(&mut self, sender: &ComponentSender<Self>) {
        self.set_blanked(false);
        self.arm_blank(sender);
    }

    fn set_blanked(&self, blanked: bool) {
        for s in &self.surfaces {
            s.scrim.set_visible(blanked);
        }
    }

    /// Best-effort logind `SetLockedHint` so other session tooling agrees about
    /// the lock state. Failures are non-fatal.
    fn set_locked_hint(&self, locked: bool) {
        relm4::spawn(async move {
            logind::set_locked_hint(locked).await;
        });
    }
}

/// Builds and presents one lock surface per monitor, assigning each to its
/// output on the session-lock `instance`.
fn build_surfaces(
    instance: &Instance,
    bg: &BgConfig,
    show_clock: bool,
    reveal: (gtk::RevealerTransitionType, u32),
    sender: &ComponentSender<Lock>,
) -> Vec<Surface> {
    let mut surfaces = Vec::new();
    for (connector, monitor) in current_monitors() {
        let surface = build_surface(bg, show_clock, reveal, sender);
        instance.assign_window_to_monitor(&surface.window, &monitor);
        surface.window.present();
        // Reveal on the next tick so the transition actually runs (a same-tick
        // false→true does not animate), mirroring the other overlays.
        let revealer = surface.reveal.clone();
        glib::idle_add_local_once(move || revealer.set_reveal_child(true));
        tracing::debug!(%connector, "lock: surface presented");
        surfaces.push(surface);
    }
    surfaces
}

/// Builds one lock surface (window + widgets) for a monitor.
fn build_surface(
    bg: &BgConfig,
    show_clock: bool,
    reveal: (gtk::RevealerTransitionType, u32),
    sender: &ComponentSender<Lock>,
) -> Surface {
    let window = gtk::Window::builder().decorated(false).build();
    window.add_css_class("lock-window");

    let overlay = gtk::Overlay::new();
    overlay.set_child(Some(&build_background(bg)));

    // Shared credential box (clock, date, entry, error) wrapped in the reveal
    // animation. The background stays put so the screen is opaque the instant
    // it locks.
    let (transition, duration) = reveal;
    let input = sender.input_sender().clone();
    let cred = CredentialBox::build(
        &CredentialOpts {
            show_clock,
            transition,
            duration_ms: duration,
        },
        move |text| input.emit(LockInput::Submit(text)),
    );
    overlay.add_overlay(&cred.root);

    // Blank scrim: opaque black layer on top, hidden until the blank timer.
    let scrim = gtk::Box::new(gtk::Orientation::Vertical, 0);
    scrim.add_css_class("lock-scrim");
    scrim.set_visible(false);
    overlay.add_overlay(&scrim);

    window.set_child(Some(&overlay));

    // Any keypress counts as activity (unblank + reset blank timer).
    {
        let input = sender.input_sender().clone();
        let key = EventControllerKey::new();
        key.set_propagation_phase(gtk::PropagationPhase::Capture);
        key.connect_key_pressed(move |_, _, _, _| {
            input.emit(LockInput::Activity);
            glib::Propagation::Proceed
        });
        window.add_controller(key);
    }

    Surface {
        window,
        entry: cred.entry,
        clock: cred.clock,
        date: cred.date,
        error: cred.error,
        reveal: cred.root,
        scrim,
    }
}

/// Builds the background widget for a surface.
///
/// `Color` paints a solid fill; `Image`/`Wallpaper` show the file scaled to
/// cover, with a dark scrim for legibility. A non-zero `blur` applies a real
/// gaussian blur to the image up front (see [`blurred_texture`]).
fn build_background(bg: &BgConfig) -> gtk::Widget {
    match bg.mode {
        LockBackground::Color => solid_fill(gdk::RGBA::parse(bg.color.as_str()).ok()),
        LockBackground::Image | LockBackground::Wallpaper if !bg.image.is_empty() => {
            let overlay = gtk::Overlay::new();
            let picture = match (bg.blur > 0).then(|| blurred_texture(&bg.image, bg.blur)) {
                Some(Some(texture)) => gtk::Picture::for_paintable(&texture),
                // blur disabled, or load/blur failed: GPU-scale the file directly.
                _ => gtk::Picture::for_filename(&bg.image),
            };
            picture.set_content_fit(gtk::ContentFit::Cover);
            overlay.set_child(Some(&picture));
            let scrim = gtk::Box::new(gtk::Orientation::Vertical, 0);
            scrim.add_css_class("lock-bg-scrim");
            overlay.add_overlay(&scrim);
            overlay.upcast()
        }
        // Image/Wallpaper mode with no path falls back to a black fill.
        _ => solid_fill(None),
    }
}

/// A `DrawingArea` that fills with `rgba` (black when `None`).
fn solid_fill(rgba: Option<gdk::RGBA>) -> gtk::Widget {
    let area = gtk::DrawingArea::new();
    area.add_css_class("lock-bg");
    let rgba = rgba.unwrap_or(gdk::RGBA::BLACK);
    area.set_draw_func(move |_, cr, _, _| {
        cr.set_source_rgba(
            f64::from(rgba.red()),
            f64::from(rgba.green()),
            f64::from(rgba.blue()),
            f64::from(rgba.alpha()),
        );
        let _ = cr.paint();
    });
    area.upcast()
}

/// Loads `path` and returns a gaussian-blurred texture, or `None` if the file
/// can't be read/decoded. Runs synchronously on the GTK thread during lock
/// acquisition (a one-off cost, like the screenshot freeze capture).
fn blurred_texture(path: &str, radius: u32) -> Option<gdk::Texture> {
    let rgba = image::open(path).ok()?.blur(radius as f32).to_rgba8();
    let (width, height) = rgba.dimensions();
    let stride = width as usize * 4;
    let bytes = gtk::glib::Bytes::from_owned(rgba.into_raw());
    let texture = gdk::MemoryTexture::new(
        width as i32,
        height as i32,
        gdk::MemoryFormat::R8g8b8a8,
        &bytes,
        stride,
    );
    Some(texture.upcast())
}

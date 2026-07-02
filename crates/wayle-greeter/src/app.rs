//! The greeter UI: a single fullscreen window driving a greetd login.
//!
//! Mirrors the lock screen's credential flow but for pre-login: it reuses the
//! shared [`CredentialBox`] and the `lock-*` theme, and drives authentication
//! through [`wayle_auth`] with the [`GreetdAuth`] backend instead of PAM. The
//! greeter runs as the single client of a kiosk compositor (e.g. `cage`)
//! launched by greetd, so it needs no layer-shell or session-lock protocol.
//!
//! Known limitation: the shared credential box uses a secret entry, so the
//! greetd "username" prompt is echoed hidden. The prompt text is surfaced in
//! the label so the user knows what is being asked. A visible username field is
//! left for a later iteration.
//!
//! A [`gtk::DropDown`] below the entry lets the user pick which Wayland session
//! to start; the choice is remembered across restarts (see [`crate::session`]).
//! The session command is shared with the greetd backend through an
//! `Arc<Mutex<_>>` so changing the dropdown mid-login is picked up when greetd
//! actually starts the session.

use std::{
    path::PathBuf,
    sync::{Arc, Mutex},
};

use gdk4::Display;
use relm4::{
    gtk,
    gtk::{gdk, glib, prelude::*},
    prelude::*,
};
use tracing::{info, warn};
use wayle_auth::{AuthEvent, AuthHandle, AuthPrompt, GreetdAuth};
use wayle_config::{
    Config,
    schemas::{animations::AnimationType, lock::LockBackground},
};
use wayle_styling::{STATIC_CSS, theme_css};
use wayle_widgets::components::credential_box::{CredentialBox, CredentialOpts};

use crate::session::{self, Session};

/// Initialization payload for the greeter component.
pub struct GreeterInit {
    /// Theme/background/clock config (shared with the desktop + lock screen).
    pub config: Config,
    /// Selectable Wayland sessions (non-empty; enforced in `main`).
    pub sessions: Vec<Session>,
    /// Id of the last-used session to pre-select, if any is remembered.
    pub last_session: Option<String>,
    /// File the selected session id is persisted to on success.
    pub state_path: PathBuf,
    /// Extra `KEY=value` environment entries for the session.
    pub session_env: Vec<String>,
}

/// Greeter component state.
pub struct Greeter {
    config: Config,
    /// Selectable sessions, indexed in lockstep with the dropdown rows.
    sessions: Vec<Session>,
    /// Session picker; its selected row drives `selected_cmd` and is persisted.
    dropdown: gtk::DropDown,
    /// Argv of the currently selected session, shared with the greetd backend
    /// (read only when greetd starts the session, so mid-login changes apply).
    selected_cmd: Arc<Mutex<Vec<String>>>,
    /// Where the selected session id is remembered.
    state_path: PathBuf,
    session_env: Vec<String>,
    /// Live credential-box handles (entry, clock, date, error).
    cred: CredentialBox,
    /// Handle to the in-flight greetd conversation; `None` between attempts.
    auth: Option<AuthHandle>,
    /// A prompt is on screen waiting for the user's next submit.
    awaiting: bool,
    /// 1s clock refresh source.
    clock_source: Option<glib::SourceId>,
}

/// Greeter input messages.
#[derive(Debug)]
pub enum GreeterInput {
    /// The entry was activated with this value.
    Submit(String),
    /// The greetd conversation produced an event.
    Auth(AuthEvent),
    /// Refresh the clock/date labels.
    Tick,
}

#[relm4::component(pub)]
impl Component for Greeter {
    type Init = GreeterInit;
    type Input = GreeterInput;
    type Output = ();
    type CommandOutput = ();

    view! {
        #[root]
        gtk::Window {
            set_decorated: false,
            set_fullscreened: true,
            add_css_class: "lock-window",
        }
    }

    fn init(
        init: Self::Init,
        root: Self::Root,
        sender: ComponentSender<Self>,
    ) -> ComponentParts<Self> {
        install_css(&init.config);

        let overlay = gtk::Overlay::new();
        overlay.set_child(Some(&build_background(&init.config)));

        // Session picker: one row per session, pre-selecting the remembered one.
        let dropdown = build_session_dropdown(&init.sessions, init.last_session.as_deref());
        let selected = dropdown.selected() as usize;
        let selected_cmd = Arc::new(Mutex::new(init.sessions[selected].exec.clone()));

        let show_clock = init.config.lock.show_clock.get();
        let input = sender.input_sender().clone();
        let dropdown_widget: gtk::Widget = dropdown.clone().upcast();
        let cred = CredentialBox::build(
            &CredentialOpts {
                show_clock,
                transition: AnimationType::Fade,
                duration_ms: 300,
            },
            Some(&dropdown_widget),
            move |text| input.emit(GreeterInput::Submit(text)),
        );
        overlay.add_overlay(&cred.root);
        root.set_child(Some(&overlay));
        cred.reveal();

        // Keep the shared session command in step with the dropdown.
        {
            let sessions = init.sessions.clone();
            let selected_cmd = Arc::clone(&selected_cmd);
            dropdown.connect_selected_notify(move |dd| {
                if let Some(session) = sessions.get(dd.selected() as usize)
                    && let Ok(mut cmd) = selected_cmd.lock()
                {
                    *cmd = session.exec.clone();
                }
            });
        }

        let mut model = Greeter {
            config: init.config,
            sessions: init.sessions,
            dropdown,
            selected_cmd,
            state_path: init.state_path,
            session_env: init.session_env,
            cred,
            auth: None,
            awaiting: false,
            clock_source: None,
        };

        model.refresh_clock();
        model.start_clock(&sender);
        model.start_conversation(&sender);

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: GreeterInput, sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            GreeterInput::Submit(value) => self.submit(value),
            GreeterInput::Auth(event) => self.on_auth_event(event, &sender),
            GreeterInput::Tick => self.refresh_clock(),
        }
    }
}

impl Greeter {
    /// Spawns a fresh greetd conversation. greetd prompts for the username
    /// first (we pass `None`), then for the password.
    fn start_conversation(&mut self, sender: &ComponentSender<Self>) {
        let selected_cmd = Arc::clone(&self.selected_cmd);
        let cmd = move || selected_cmd.lock().map(|c| c.clone()).unwrap_or_default();
        let backend = match GreetdAuth::from_env(cmd, self.session_env.clone()) {
            Ok(backend) => backend,
            Err(err) => {
                warn!(error = %err, "greeter: cannot connect to greetd");
                self.show_message(&format!("greetd unavailable: {err}"));
                return;
            }
        };
        let input = sender.input_sender().clone();
        self.cred.entry.set_sensitive(true);
        self.auth = Some(wayle_auth::spawn(backend, None, move |event| {
            input.emit(GreeterInput::Auth(event));
        }));
    }

    /// Persists the currently selected session so it pre-selects next time.
    fn remember_session(&self) {
        if let Some(session) = self.sessions.get(self.dropdown.selected() as usize) {
            session::save_last(&self.state_path, &session.id);
        }
    }

    /// Answers the pending prompt with the submitted value.
    fn submit(&mut self, value: String) {
        if !self.awaiting {
            return;
        }
        self.awaiting = false;
        if let Some(handle) = self.auth.as_ref() {
            handle.answer(Some(value));
        }
        self.cred.entry.set_text("");
        self.cred.entry.set_sensitive(false);
    }

    /// Applies a greetd conversation event.
    fn on_auth_event(&mut self, event: AuthEvent, sender: &ComponentSender<Self>) {
        match event {
            AuthEvent::Prompt(prompt) => self.on_prompt(prompt),
            AuthEvent::Success => {
                info!("greeter: authentication succeeded; greetd is starting the session");
                self.remember_session();
                relm4::main_application().quit();
            }
            AuthEvent::Failure(reason) => {
                warn!(%reason, "greeter: authentication failed");
                self.auth = None;
                self.awaiting = false;
                self.show_message(&reason);
                // greetd cancels the session on failure; start a fresh one.
                self.start_conversation(sender);
            }
        }
    }

    /// Surfaces a prompt: input prompts re-enable the entry and wait for the
    /// next submit; info/error prompts only update the label.
    fn on_prompt(&mut self, prompt: AuthPrompt) {
        match prompt {
            AuthPrompt::Secret(label) | AuthPrompt::Visible(label) => {
                self.awaiting = true;
                self.show_message(&label);
                self.cred.entry.set_sensitive(true);
                self.cred.entry.set_text("");
                self.cred.entry.grab_focus();
            }
            AuthPrompt::Info(text) | AuthPrompt::Error(text) => self.show_message(&text),
        }
    }

    /// Shows `text` in the error/info label.
    fn show_message(&self, text: &str) {
        self.cred.error.set_text(text);
        self.cred.error.set_visible(!text.is_empty());
    }

    fn refresh_clock(&self) {
        let now = chrono::Local::now();
        let time = now.format(&self.config.lock.clock_format.get()).to_string();
        let date = now.format(&self.config.lock.date_format.get()).to_string();
        self.cred.clock.set_text(&time);
        self.cred.date.set_text(&date);
    }

    fn start_clock(&mut self, sender: &ComponentSender<Self>) {
        let input = sender.input_sender().clone();
        let id = glib::timeout_add_seconds_local(1, move || {
            input.emit(GreeterInput::Tick);
            glib::ControlFlow::Continue
        });
        self.clock_source = Some(id);
    }
}

/// Builds the session picker: one row per session, pre-selecting `last` (by id)
/// when it is still present. The dropdown is hidden when there is only one
/// session, since there is then nothing to choose.
fn build_session_dropdown(sessions: &[Session], last: Option<&str>) -> gtk::DropDown {
    let names: Vec<&str> = sessions.iter().map(|s| s.name.as_str()).collect();
    let dropdown = gtk::DropDown::from_strings(&names);
    dropdown.add_css_class("lock-session");
    dropdown.set_halign(gtk::Align::Center);

    if let Some(last) = last
        && let Some(idx) = sessions.iter().position(|s| s.id == last)
    {
        dropdown.set_selected(idx as u32);
    }
    dropdown.set_visible(sessions.len() > 1);
    dropdown
}

/// Installs the wayle theme (static CSS + palette) on the default display.
fn install_css(config: &Config) {
    let Some(display) = Display::default() else {
        warn!("greeter: no default display; skipping CSS");
        return;
    };
    let provider = gtk::CssProvider::new();
    let palette = config.styling.palette();
    let theme = theme_css(&palette, &config.general, &config.bar, &config.styling);
    provider.load_from_string(&format!("{STATIC_CSS}\n{theme}"));
    gtk::style_context_add_provider_for_display(
        &display,
        &provider,
        gtk::STYLE_PROVIDER_PRIORITY_USER + 100,
    );
}

/// Builds the greeter background from the shared lock background config.
fn build_background(config: &Config) -> gtk::Widget {
    let lock = &config.lock;
    let color = lock.background_color.get();
    match lock.background_mode.get() {
        LockBackground::Color => solid_fill(color.as_str()),
        LockBackground::Image => image_or_fill(&lock.background_image.get(), color.as_str()),
        LockBackground::Wallpaper => {
            image_or_fill(&config.wallpaper.wallpaper.get(), color.as_str())
        }
    }
}

/// An image scaled to cover with a legibility scrim, or a solid fill if the
/// path is empty. Blur is intentionally omitted in the greeter for now.
fn image_or_fill(path: &str, color: &str) -> gtk::Widget {
    if path.is_empty() {
        return solid_fill(color);
    }
    let overlay = gtk::Overlay::new();
    let picture = gtk::Picture::for_filename(path);
    picture.set_content_fit(gtk::ContentFit::Cover);
    overlay.set_child(Some(&picture));
    let scrim = gtk::Box::new(gtk::Orientation::Vertical, 0);
    scrim.add_css_class("lock-bg-scrim");
    overlay.add_overlay(&scrim);
    overlay.upcast()
}

/// A `DrawingArea` filled with `color` (black if it cannot be parsed).
fn solid_fill(color: &str) -> gtk::Widget {
    let area = gtk::DrawingArea::new();
    area.add_css_class("lock-bg");
    let rgba = gdk::RGBA::parse(color).unwrap_or(gdk::RGBA::BLACK);
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

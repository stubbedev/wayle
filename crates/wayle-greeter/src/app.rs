//! The greeter UI: a single fullscreen window driving a greetd login.
//!
//! Mirrors the lock screen's credential flow but for pre-login: it reuses the
//! shared [`CredentialBox`] and the `lock-*` theme, and drives authentication
//! through [`wayle_auth`] with the [`GreetdAuth`] backend instead of PAM. The
//! greeter runs as the single client of a kiosk compositor (e.g. `cage`)
//! launched by greetd, so it needs no layer-shell or session-lock protocol.
//!
//! The login is form-driven, like sddm: a visible username entry and a secret
//! password entry are both on screen, and submitting starts a fresh greetd
//! conversation with the username passed up front (so greetd only prompts for
//! the password, answered from the stashed value). Extra prompts (OTP, expired
//! password) fall back to interactive mode on the secret entry. The last
//! successful username is remembered and pre-filled, alongside the session.
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
    /// Last successfully logged-in username to pre-fill, if remembered.
    pub last_user: Option<String>,
    /// File the selected session id is persisted to on success (the username
    /// is persisted to a `last-user` sibling of this path).
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
    /// Live credential-box handles (username, entry, clock, date, error).
    cred: CredentialBox,
    /// Handle to the in-flight greetd conversation; `None` between attempts.
    auth: Option<AuthHandle>,
    /// A prompt is on screen waiting for the user's next submit.
    awaiting: bool,
    /// The submitted password, waiting to answer the conversation's first
    /// secret prompt (the username is passed to greetd up front, so the
    /// password prompt is the first thing it asks).
    pending: Option<String>,
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
                with_username: true,
                transition: AnimationType::Fade,
                duration_ms: 300,
            },
            Some(&dropdown_widget),
            move |text| input.emit(GreeterInput::Submit(text)),
        );

        // Pre-fill the remembered username and focus whichever field the user
        // needs next (password when the username is known, username otherwise).
        if let Some(user_entry) = &cred.username {
            match init.last_user.as_deref() {
                Some(last) if !last.is_empty() => {
                    user_entry.set_text(last);
                    cred.entry.grab_focus();
                }
                _ => {
                    user_entry.grab_focus();
                }
            }
        }

        overlay.add_overlay(&cred.root);
        overlay.add_overlay(&build_power_box());
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
            pending: None,
            clock_source: None,
        };

        model.refresh_clock();
        model.start_clock(&sender);

        let widgets = view_output!();
        ComponentParts { model, widgets }
    }

    fn update(&mut self, msg: GreeterInput, sender: ComponentSender<Self>, _root: &Self::Root) {
        match msg {
            GreeterInput::Submit(value) => self.submit(value, &sender),
            GreeterInput::Auth(event) => self.on_auth_event(event),
            GreeterInput::Tick => self.refresh_clock(),
        }
    }
}

impl Greeter {
    /// Spawns a fresh greetd conversation for `username`, stashing `password`
    /// to answer the conversation's first secret prompt.
    fn start_conversation(
        &mut self,
        username: String,
        password: String,
        sender: &ComponentSender<Self>,
    ) {
        let selected_cmd = Arc::clone(&self.selected_cmd);
        let cmd = move || selected_cmd.lock().map(|c| c.clone()).unwrap_or_default();
        let backend = match GreetdAuth::from_env(cmd, self.session_env.clone()) {
            Ok(backend) => backend,
            Err(err) => {
                warn!(error = %err, "greeter: cannot connect to greetd");
                self.show_message(&format!("greetd unavailable: {err}"));
                self.set_form_sensitive(true);
                return;
            }
        };
        let input = sender.input_sender().clone();
        self.pending = Some(password);
        self.auth = Some(wayle_auth::spawn(backend, Some(username), move |event| {
            input.emit(GreeterInput::Auth(event));
        }));
    }

    /// Persists the selected session and username so they pre-fill next time.
    fn remember_login(&self) {
        if let Some(session) = self.sessions.get(self.dropdown.selected() as usize) {
            session::save_last(&self.state_path, &session.id);
        }
        if let Some(user_entry) = &self.cred.username {
            let user = user_entry.text();
            let user = user.trim();
            if !user.is_empty() {
                session::save_last(&self.state_path.with_file_name("last-user"), user);
            }
        }
    }

    /// Handles a submitted password: answers the pending prompt when one is on
    /// screen, otherwise starts a fresh login with the entered username.
    fn submit(&mut self, value: String, sender: &ComponentSender<Self>) {
        // A mid-conversation re-prompt (OTP, expired password) is waiting.
        if self.awaiting {
            self.awaiting = false;
            if let Some(handle) = self.auth.as_ref() {
                handle.answer(Some(value));
            }
            self.cred.entry.set_text("");
            self.cred.entry.set_sensitive(false);
            return;
        }
        // Conversation already running with no prompt pending: ignore the
        // stray submit rather than racing the worker.
        if self.auth.is_some() {
            return;
        }

        let username = self
            .cred
            .username
            .as_ref()
            .map(|u| u.text().trim().to_owned())
            .unwrap_or_default();
        if username.is_empty() {
            self.show_message("Enter a username");
            if let Some(user_entry) = &self.cred.username {
                user_entry.grab_focus();
            }
            return;
        }

        self.show_message("");
        self.set_form_sensitive(false);
        self.start_conversation(username, value, sender);
    }

    /// Applies a greetd conversation event.
    fn on_auth_event(&mut self, event: AuthEvent) {
        match event {
            AuthEvent::Prompt(prompt) => self.on_prompt(prompt),
            AuthEvent::Success => {
                info!("greeter: authentication succeeded; greetd is starting the session");
                self.remember_login();
                relm4::main_application().quit();
            }
            AuthEvent::Failure(reason) => {
                warn!(%reason, "greeter: authentication failed");
                self.auth = None;
                self.awaiting = false;
                self.pending = None;
                self.show_message(&reason);
                // Re-arm the form for another attempt: keep the username,
                // clear the password. The next submit starts a fresh
                // conversation (greetd cancelled this one).
                self.set_form_sensitive(true);
                self.cred.entry.set_text("");
                self.cred.entry.grab_focus();
            }
        }
    }

    /// Routes a conversation prompt: the stashed password answers the first
    /// input prompt; later prompts (OTP, expired password) re-enable the secret
    /// entry and wait for the next submit. Info/error prompts update the label.
    fn on_prompt(&mut self, prompt: AuthPrompt) {
        match prompt {
            AuthPrompt::Secret(label) | AuthPrompt::Visible(label) => {
                if let Some(answer) = self.pending.take() {
                    if let Some(handle) = self.auth.as_ref() {
                        handle.answer(Some(answer));
                    }
                } else {
                    self.awaiting = true;
                    self.show_message(&label);
                    self.cred.entry.set_sensitive(true);
                    self.cred.entry.set_text("");
                    self.cred.entry.grab_focus();
                }
            }
            AuthPrompt::Info(text) | AuthPrompt::Error(text) => self.show_message(&text),
        }
    }

    /// Enables/disables the whole login form (username, password, session).
    fn set_form_sensitive(&self, sensitive: bool) {
        if let Some(user_entry) = &self.cred.username {
            user_entry.set_sensitive(sensitive);
        }
        self.cred.entry.set_sensitive(sensitive);
        self.dropdown.set_sensitive(sensitive);
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

/// Builds the power controls shown at the bottom of the screen: shutdown and
/// reboot, via `systemctl` (logind allows both for the active local session
/// without extra polkit rules).
fn build_power_box() -> gtk::Widget {
    let row = gtk::Box::new(gtk::Orientation::Horizontal, 12);
    row.add_css_class("lock-power");
    row.set_halign(gtk::Align::Center);
    row.set_valign(gtk::Align::End);

    for (icon, tooltip, verb) in [
        ("system-shutdown-symbolic", "Shut down", "poweroff"),
        ("system-reboot-symbolic", "Restart", "reboot"),
    ] {
        let button = gtk::Button::from_icon_name(icon);
        button.add_css_class("lock-power-button");
        button.add_css_class("flat");
        button.set_tooltip_text(Some(tooltip));
        button.connect_clicked(move |_| {
            if let Err(err) = std::process::Command::new("systemctl").arg(verb).spawn() {
                warn!(%verb, error = %err, "greeter: power action failed");
            }
        });
        row.append(&button);
    }
    row.upcast()
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

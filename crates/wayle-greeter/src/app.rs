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

use crate::{
    i18n::t,
    session::{self, Session},
    users::User,
};

/// Most users shown as avatar buttons; beyond this the row is dropped and the
/// free-text username entry carries the load (a row of 30 avatars on a
/// corporate box helps nobody).
const MAX_LISTED_USERS: usize = 8;

/// Power icons baked into the binary: the greeter runs pre-login, so it cannot
/// rely on `wayle icons install` (per-user) or a system icon theme being
/// present in the kiosk environment.
const EMBEDDED_ICONS: [(&str, &str); 4] = [
    // GTK's DropDown checkmark; shipped under GTK's own icon name so it
    // resolves without a system icon theme.
    (
        "object-select-symbolic.svg",
        include_str!("../assets/object-select-symbolic.svg"),
    ),
    (
        "ld-power-symbolic.svg",
        include_str!("../assets/ld-power-symbolic.svg"),
    ),
    (
        "ld-rotate-ccw-symbolic.svg",
        include_str!("../assets/ld-rotate-ccw-symbolic.svg"),
    ),
    (
        "ld-chevron-down-symbolic.svg",
        include_str!("../assets/ld-chevron-down-symbolic.svg"),
    ),
];

/// Initialization payload for the greeter component.
pub struct GreeterInit {
    /// Theme/background/clock config (shared with the desktop + lock screen).
    pub config: Config,
    /// Selectable Wayland sessions (non-empty; enforced in `main`).
    pub sessions: Vec<Session>,
    /// Login accounts offered as clickable avatars (may be empty).
    pub users: Vec<User>,
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
        install_icons();
        install_cursor(&init.config);

        let overlay = gtk::Overlay::new();
        overlay.set_child(Some(&build_background(&init.config)));

        // Session picker: one row per session, pre-selecting the remembered one.
        let dropdown = build_session_dropdown(&init.sessions, init.last_session.as_deref());
        let selected = dropdown.selected() as usize;
        let selected_cmd = Arc::new(Mutex::new(init.sessions[selected].exec.clone()));

        // Below-entry column: caps-lock warning + session picker.
        let caps_label = gtk::Label::new(Some(&t!("greeter-caps-lock")));
        caps_label.add_css_class("lock-caps");
        caps_label.set_visible(false);
        let below = gtk::Box::new(gtk::Orientation::Vertical, 0);
        below.append(&caps_label);
        below.append(&dropdown);

        // User list container; populated after the credential box exists (the
        // buttons need the entry handles).
        let user_row = gtk::Box::new(gtk::Orientation::Horizontal, 12);
        user_row.add_css_class("lock-users");
        user_row.set_halign(gtk::Align::Center);

        let show_clock = init.config.greeter.show_clock.get();
        let input = sender.input_sender().clone();
        let below_widget: gtk::Widget = below.clone().upcast();
        let user_row_widget: gtk::Widget = user_row.clone().upcast();
        let cred = CredentialBox::build(
            &CredentialOpts {
                show_clock,
                with_username: true,
                transition: AnimationType::Fade,
                duration_ms: 300,
            },
            Some(&user_row_widget),
            Some(&below_widget),
            move |text| input.emit(GreeterInput::Submit(text)),
        );
        if let Some(user_entry) = &cred.username {
            user_entry.set_placeholder_text(Some(&t!("greeter-username")));
        }

        if init.config.greeter.show_user_list.get() {
            populate_user_row(&user_row, &init.users, init.last_user.as_deref(), &cred);
        } else {
            user_row.set_visible(false);
        }

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
        if init.config.greeter.show_power_buttons.get() {
            overlay.add_overlay(&build_power_box());
        }
        root.set_child(Some(&overlay));
        cred.reveal();
        watch_caps_lock(&caps_label);

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
        apply_debug_ops(&model, &sender);

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
                self.show_message(&t!("greeter-greetd-unavailable", error = err.to_string()));
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
            self.show_message(&t!("greeter-enter-username"));
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
        let time = now
            .format(&self.config.greeter.clock_format.get())
            .to_string();
        let date = now
            .format(&self.config.greeter.date_format.get())
            .to_string();
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

/// Dev-harness hook: `WAYLE_GREETER_DEBUG` drives interactions that synthetic
/// input cannot reach under a nested headless compositor. Comma-separated ops:
/// `popup` opens the session dropdown; `login=<user>:<pass>` fills the form
/// and submits. Never set in production (greetd does not forward it).
fn apply_debug_ops(model: &Greeter, sender: &ComponentSender<Greeter>) {
    let Ok(spec) = std::env::var("WAYLE_GREETER_DEBUG") else {
        return;
    };
    for op in spec.split(',') {
        if op == "popup" {
            let dropdown = model.dropdown.clone();
            glib::timeout_add_seconds_local_once(1, move || {
                dropdown.emit_by_name::<()>("activate", &[]);
            });
        } else if let Some(login) = op.strip_prefix("login=")
            && let Some((user, pass)) = login.split_once(':')
        {
            let username = model.cred.username.clone();
            let input = sender.input_sender().clone();
            let (user, pass) = (user.to_owned(), pass.to_owned());
            glib::timeout_add_seconds_local_once(1, move || {
                if let Some(entry) = &username {
                    entry.set_text(&user);
                }
                input.emit(GreeterInput::Submit(pass));
            });
        }
    }
}

/// Fills the user row with one avatar button per discovered login user.
/// Clicking one fills the username entry and focuses the password. The row is
/// left empty (invisible) when there are no users or too many to be useful.
fn populate_user_row(row: &gtk::Box, users: &[User], last: Option<&str>, cred: &CredentialBox) {
    if users.is_empty() || users.len() > MAX_LISTED_USERS {
        row.set_visible(false);
        return;
    }

    let mut buttons = Vec::with_capacity(users.len());
    for user in users {
        let button = build_user_button(user);
        if last == Some(user.name.as_str()) {
            button.add_css_class("selected");
        }
        row.append(&button);
        buttons.push(button);
    }

    for (button, user) in buttons.iter().zip(users) {
        let name = user.name.clone();
        let username_entry = cred.username.clone();
        let password_entry = cred.entry.clone();
        let all = buttons.clone();
        let this = button.clone();
        button.connect_clicked(move |_| {
            for b in &all {
                b.remove_css_class("selected");
            }
            this.add_css_class("selected");
            if let Some(entry) = &username_entry {
                entry.set_text(&name);
            }
            password_entry.grab_focus();
        });
    }
}

/// Builds one user button: avatar (image or initial-letter fallback) above the
/// display name.
fn build_user_button(user: &User) -> gtk::Button {
    let avatar: gtk::Widget = match &user.icon {
        Some(path) => {
            let picture = gtk::Picture::for_filename(path);
            picture.set_content_fit(gtk::ContentFit::Cover);
            picture.add_css_class("lock-avatar");
            picture.upcast()
        }
        None => {
            let initial = user
                .display_name
                .chars()
                .next()
                .map(|c| c.to_uppercase().to_string())
                .unwrap_or_default();
            let label = gtk::Label::new(Some(&initial));
            label.add_css_class("lock-avatar");
            label.add_css_class("lock-avatar-fallback");
            label.upcast()
        }
    };
    avatar.set_size_request(64, 64);

    let name = gtk::Label::new(Some(&user.display_name));
    name.add_css_class("lock-user-name");
    name.set_ellipsize(gtk::pango::EllipsizeMode::End);
    name.set_max_width_chars(14);

    let column = gtk::Box::new(gtk::Orientation::Vertical, 6);
    column.set_halign(gtk::Align::Center);
    column.append(&avatar);
    column.append(&name);

    let button = gtk::Button::new();
    button.add_css_class("lock-user-button");
    button.add_css_class("flat");
    button.set_child(Some(&column));
    button
}

/// Shows `label` whenever Caps Lock is on, tracking the keyboard device's
/// caps-lock-state property (set at startup, updated on every change).
fn watch_caps_lock(label: &gtk::Label) {
    let Some(keyboard) = Display::default()
        .and_then(|display| display.default_seat())
        .and_then(|seat| seat.keyboard())
    else {
        return; // no keyboard device; leave the warning hidden
    };
    label.set_visible(keyboard.is_caps_locked());
    let label = label.clone();
    keyboard.connect_caps_lock_state_notify(move |device| {
        label.set_visible(device.is_caps_locked());
    });
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
        ("ld-power-symbolic", t!("greeter-shutdown"), "poweroff"),
        ("ld-rotate-ccw-symbolic", t!("greeter-restart"), "reboot"),
    ] {
        let button = gtk::Button::from_icon_name(icon);
        button.add_css_class("lock-power-button");
        button.add_css_class("flat");
        button.set_tooltip_text(Some(&tooltip));
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

/// Materializes the embedded power icons into a runtime directory and adds it
/// to GTK's icon search path, so `from_icon_name` resolves them without any
/// installed icon theme.
fn install_icons() {
    let dir = std::env::var_os("XDG_RUNTIME_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(std::env::temp_dir)
        .join("wayle-greeter-icons");
    if let Err(err) = std::fs::create_dir_all(&dir) {
        warn!(dir = %dir.display(), error = %err, "greeter: cannot create icon dir");
        return;
    }
    for (name, svg) in EMBEDDED_ICONS {
        if let Err(err) = std::fs::write(dir.join(name), svg) {
            warn!(%name, error = %err, "greeter: cannot write icon");
        }
    }
    let Some(display) = Display::default() else {
        return;
    };
    gtk::IconTheme::for_display(&display).add_search_path(&dir);
}

/// Applies the configured cursor theme/size. The size is logical: GTK loads a
/// scaled cursor per output, so HiDPI displays get the right resolution. The
/// settings are re-applied on monitor hotplug to force that re-resolution.
fn install_cursor(config: &Config) {
    let Some(settings) = gtk::Settings::default() else {
        return;
    };
    let theme = config.greeter.cursor_theme.get();
    let theme = if theme.is_empty() {
        std::env::var("XCURSOR_THEME").unwrap_or_default()
    } else {
        theme
    };
    let size = config.greeter.cursor_size.get().max(1) as i32;
    info!(%theme, size, "greeter: applying cursor settings");
    let apply = move |settings: &gtk::Settings| {
        if !theme.is_empty() {
            settings.set_gtk_cursor_theme_name(Some(&theme));
        }
        settings.set_gtk_cursor_theme_size(size);
    };
    apply(&settings);

    if let Some(display) = Display::default() {
        display.monitors().connect_items_changed(move |_, _, _, _| {
            if let Some(settings) = gtk::Settings::default() {
                apply(&settings);
            }
        });
    }
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

/// Builds the greeter background from the greeter background config.
fn build_background(config: &Config) -> gtk::Widget {
    let greeter = &config.greeter;
    let color = greeter.background_color.get();
    match greeter.background_mode.get() {
        LockBackground::Color => solid_fill(color.as_str()),
        LockBackground::Image => image_or_fill(&greeter.background_image.get(), color.as_str()),
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

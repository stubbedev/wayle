//! Shared credential box: clock, date, secret entry, and error label wrapped in
//! a reveal animation.
//!
//! This is the inner widget tree common to the session lock screen and the
//! pre-login greeter, extracted so both render an identical credential prompt.
//! It carries the `lock-*` CSS classes on purpose: the greeter deliberately
//! shares the lock screen's theme, so reusing the classes keeps the two pixel
//! identical without duplicating styling.
//!
//! It is a plain builder (not a relm4 template) because both callers create
//! their surfaces imperatively and keep the returned widget handles live to
//! drive the clock and respond to authentication events.

use relm4::gtk::{self, prelude::*};
use wayle_config::schemas::animations::AnimationType;

use crate::primitives::revealer::WayleRevealer;

/// Build-time options for a [`CredentialBox`].
pub struct CredentialOpts {
    /// Whether the clock and date labels are shown.
    pub show_clock: bool,
    /// Reveal animation for the box.
    pub transition: AnimationType,
    /// Reveal animation duration, in milliseconds.
    pub duration_ms: u32,
}

/// A built credential box plus the handles needed to drive it.
pub struct CredentialBox {
    /// Root widget to place into the surface; overlay it and call
    /// [`CredentialBox::reveal`] once it is mapped.
    pub root: WayleRevealer,
    /// Secret entry; activates fire the `on_submit` callback passed to
    /// [`CredentialBox::build`].
    pub entry: gtk::PasswordEntry,
    /// Large clock label (hidden when `show_clock` is false).
    pub clock: gtk::Label,
    /// Date label (hidden when `show_clock` is false).
    pub date: gtk::Label,
    /// Error/info label, hidden until there is something to show.
    pub error: gtk::Label,
}

impl CredentialBox {
    /// Builds the credential box. `on_submit` fires with the entry's text each
    /// time the user activates it (presses Enter).
    ///
    /// `extra`, if given, is appended below the entry (above the error label) —
    /// the greeter uses this slot for its Wayland-session picker; the lock
    /// screen passes `None`.
    pub fn build(
        opts: &CredentialOpts,
        extra: Option<&gtk::Widget>,
        on_submit: impl Fn(String) + 'static,
    ) -> Self {
        let center = gtk::Box::new(gtk::Orientation::Vertical, 12);
        center.add_css_class("lock-center");
        center.set_halign(gtk::Align::Center);
        center.set_valign(gtk::Align::Center);

        let clock = gtk::Label::new(None);
        clock.add_css_class("lock-clock");
        clock.set_visible(opts.show_clock);
        let date = gtk::Label::new(None);
        date.add_css_class("lock-date");
        date.set_visible(opts.show_clock);

        let entry = gtk::PasswordEntry::new();
        entry.add_css_class("lock-entry");
        entry.set_show_peek_icon(true);
        entry.set_width_chars(24);

        let error = gtk::Label::new(None);
        error.add_css_class("lock-error");
        error.set_visible(false);

        center.append(&clock);
        center.append(&date);
        center.append(&entry);
        if let Some(extra) = extra {
            center.append(extra);
        }
        center.append(&error);

        {
            let entry_ref = entry.clone();
            entry.connect_activate(move |_| on_submit(entry_ref.text().to_string()));
        }

        let root = WayleRevealer::new();
        root.set_halign(gtk::Align::Center);
        root.set_valign(gtk::Align::Center);
        root.set_transition(opts.transition);
        root.set_transition_duration(opts.duration_ms);
        root.set_reveal_child(false);
        root.set_child(Some(&center));

        Self {
            root,
            entry,
            clock,
            date,
            error,
        }
    }

    /// Reveals the box on the next idle tick.
    ///
    /// Deferring the reveal lets the transition actually run: a same-tick
    /// `false`→`true` toggle does not animate.
    pub fn reveal(&self) {
        let revealer = self.root.clone();
        relm4::gtk::glib::idle_add_local_once(move || revealer.set_reveal_child(true));
    }
}

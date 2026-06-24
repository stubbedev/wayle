//! Backend-agnostic authentication conversation.
//!
//! Authentication on Unix is a *conversation*: the backend (PAM, or a greetd
//! daemon) asks one or more questions — password, OTP, "your password expired,
//! enter a new one" — and the UI answers each in turn. This crate models that
//! exchange so the same UI can drive either backend.
//!
//! A backend implements [`AuthConversation`] and drives the whole exchange on a
//! dedicated blocking thread, calling the supplied `ask` closure whenever it
//! needs input from the user. [`spawn`] wires `ask` to the GTK loop: each
//! prompt is marshalled out via an `on_event` callback, and the UI sends its
//! reply back through the returned [`AuthHandle`].
//!
//! The crate is deliberately free of any GUI dependency — it only speaks in
//! [`AuthPrompt`]/[`AuthEvent`] values and channels, so it can be reused by the
//! lock screen (inside a running session, [`PamAuth`]) and, later, by a
//! pre-login greeter talking to greetd.

use std::sync::mpsc;

use tracing::warn;

mod pam_backend;

pub use pam_backend::{PamAuth, current_username};

/// A single thing a backend wants from the user.
#[derive(Debug, Clone)]
pub enum AuthPrompt {
    /// Secret input, echo off (password, PIN). Expects a response.
    Secret(String),
    /// Visible input, echo on (username, OTP). Expects a response.
    Visible(String),
    /// Informational text to display. No response expected.
    Info(String),
    /// Error text to display. No response expected.
    Error(String),
}

impl AuthPrompt {
    /// Whether this prompt expects the user to type a response.
    #[must_use]
    pub fn wants_input(&self) -> bool {
        matches!(self, AuthPrompt::Secret(_) | AuthPrompt::Visible(_))
    }
}

/// An event surfaced to the UI as a conversation progresses.
#[derive(Debug, Clone)]
pub enum AuthEvent {
    /// The backend is asking for something (see [`AuthPrompt::wants_input`]).
    Prompt(AuthPrompt),
    /// Authentication completed successfully; the conversation is over.
    Success,
    /// Authentication failed or was aborted; the conversation is over. Start a
    /// fresh conversation to retry.
    Failure(String),
}

/// A backend that can drive an authentication conversation to completion.
///
/// Implementors run synchronously (blocking I/O is fine — they execute on a
/// worker thread spawned by [`spawn`]) and must never touch the GUI directly.
pub trait AuthConversation: Send {
    /// Drive the conversation for `username` (`None` to let the backend prompt
    /// for it) to completion.
    ///
    /// Call `ask` for each prompt; for prompts where
    /// [`AuthPrompt::wants_input`] is true it blocks until the UI replies and
    /// returns that reply (`None` if the user cancelled), and for info/error
    /// prompts it returns `None` immediately without blocking.
    ///
    /// # Errors
    /// Returns the failure reason on authentication failure, backend error, or
    /// user cancellation.
    fn run(
        &mut self,
        username: Option<String>,
        ask: &mut dyn FnMut(AuthPrompt) -> Option<String>,
    ) -> Result<(), String>;
}

/// Handle the UI uses to answer the currently pending prompt.
///
/// Dropping the handle closes the response channel; a backend blocked in `ask`
/// then observes a cancellation (`ask` returns `None`).
pub struct AuthHandle {
    tx: mpsc::Sender<Option<String>>,
}

impl AuthHandle {
    /// Answer the pending prompt. `None` signals cancellation.
    pub fn answer(&self, response: Option<String>) {
        // The worker may already have finished (e.g. backend errored); a closed
        // channel is not an error from the UI's perspective.
        let _ = self.tx.send(response);
    }
}

/// Runs `conv` on a dedicated thread, marshalling prompts to `on_event` and
/// reading the UI's replies from the returned [`AuthHandle`].
///
/// `on_event` is invoked from the worker thread, so it must be `Send`; in
/// practice it wraps a UI message sender (e.g. a relm4 input sender) whose
/// `emit`/`send` is safe to call from any thread. Exactly one terminal
/// [`AuthEvent::Success`] or [`AuthEvent::Failure`] is emitted before the
/// thread exits.
pub fn spawn(
    mut conv: impl AuthConversation + 'static,
    username: Option<String>,
    on_event: impl Fn(AuthEvent) + Send + 'static,
) -> AuthHandle {
    let (tx, rx) = mpsc::channel::<Option<String>>();
    std::thread::spawn(move || {
        let mut ask = |prompt: AuthPrompt| -> Option<String> {
            let wants_input = prompt.wants_input();
            on_event(AuthEvent::Prompt(prompt));
            if wants_input {
                // Block until the UI answers; a closed channel = cancellation.
                rx.recv().unwrap_or(None)
            } else {
                None
            }
        };
        let event = match conv.run(username, &mut ask) {
            Ok(()) => AuthEvent::Success,
            Err(reason) => {
                warn!(%reason, "auth: conversation failed");
                AuthEvent::Failure(reason)
            }
        };
        on_event(event);
    });
    AuthHandle { tx }
}

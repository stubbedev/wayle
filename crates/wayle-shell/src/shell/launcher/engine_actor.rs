//! Per-session engine task.
//!
//! The engine ([`Session`] + nucleo) does blocking-ish work (mode loads,
//! script exec, regex scans), so it lives on a tokio task and talks to the
//! GTK component over channels: commands in, [`EngineEvent`]s out. One actor
//! per launcher session; it exits on `Stop` or channel close.

use std::sync::Arc;

use relm4::Sender;
use tokio::sync::mpsc;
use wayle_launcher::{Action, ActivateKind, Item, MatcherOptions, Mode, Session};

use super::LauncherInput;

/// Commands from the surface into the engine.
#[derive(Debug)]
pub(crate) enum EngineCmd {
    /// Query text changed.
    SetQuery(String),
    /// Drive nucleo (sent by the notify callback and the tick chain).
    Tick,
    /// Accept a row (`Some(item_index)`) or custom input (`None`).
    Activate(Option<u32>, ActivateKind),
    /// Accept a multi-select set (item indices, input order).
    ActivateMulti(Vec<u32>),
    /// Shift-delete on a row.
    Delete(u32),
    /// Switch to the next mode.
    ModeNext,
    /// Switch to the previous mode.
    ModePrevious,
    /// Switch to the mode at this tab index.
    ModeTo(usize),
    /// Session over; drop the actor.
    Stop,
}

/// Events from the engine to the surface (wrapped in
/// [`LauncherInput::Engine`]).
#[derive(Debug)]
pub(crate) enum EngineEvent {
    /// The active mode (re)loaded: full state for the surface.
    State {
        /// Prompt (mode-supplied; the UI may override with `-p`).
        prompt: String,
        /// Message row.
        message: Option<String>,
        /// Active mode tab index.
        active_mode: usize,
        /// Display names of all loaded modes.
        mode_names: Vec<String>,
        /// Multi-select is on for this mode.
        multi_select: bool,
        /// This state came from an activation reload (script/dmenu), not a
        /// mode switch — governs filter clearing.
        after_activate: bool,
        /// Keep the typed filter (script `keep-filter`).
        keep_filter: bool,
        /// Keep the selection position (script `keep-selection`).
        keep_selection: bool,
        /// Absolute selection to apply (script `new-selection`).
        new_selection: Option<u32>,
    },
    /// Fresh match results.
    Matches {
        /// Full item vec of the active mode.
        items: Arc<Vec<Item>>,
        /// Matched indices, ranked.
        matched: Vec<u32>,
    },
    /// The mode asked the surface to act (Close / Exit / SetInput).
    Action(Action),
}

/// Spawn the actor for one session. Returns the command channel.
pub(super) fn spawn(
    modes: Vec<Box<dyn Mode>>,
    initial_mode: usize,
    options: MatcherOptions,
    ui: Sender<LauncherInput>,
) -> mpsc::UnboundedSender<EngineCmd> {
    let (tx, rx) = mpsc::unbounded_channel();
    let notify_tx = tx.clone();
    let notify: Arc<dyn Fn() + Send + Sync> = Arc::new(move || {
        let _ = notify_tx.send(EngineCmd::Tick);
    });
    let session = Session::new(modes, options, notify);
    relm4::spawn(run(session, initial_mode, rx, tx.clone(), ui));
    tx
}

async fn run(
    mut session: Session,
    initial_mode: usize,
    mut rx: mpsc::UnboundedReceiver<EngineCmd>,
    tx: mpsc::UnboundedSender<EngineCmd>,
    ui: Sender<LauncherInput>,
) {
    session.switch_to(initial_mode).await;
    send_state(&session, &ui, false);
    push_matches(&mut session, &ui);

    while let Some(cmd) = rx.recv().await {
        match cmd {
            EngineCmd::SetQuery(query) => {
                session.set_query(&query);
                // Regex/glob scans complete synchronously; nucleo paths
                // deliver via notify → Tick.
                push_matches(&mut session, &ui);
            }
            EngineCmd::Tick => {
                let status = session.engine.tick();
                if status.changed {
                    push_matches(&mut session, &ui);
                }
                if status.running {
                    // Keep driving nucleo until it settles.
                    let tx = tx.clone();
                    relm4::spawn(async move {
                        tokio::time::sleep(std::time::Duration::from_millis(10)).await;
                        let _ = tx.send(EngineCmd::Tick);
                    });
                }
            }
            EngineCmd::Activate(index, kind) => {
                let action = session.activate(index, kind).await;
                dispatch(action, &mut session, &ui);
            }
            EngineCmd::ActivateMulti(indices) => {
                let action = session.activate_many(&indices).await;
                dispatch(action, &mut session, &ui);
            }
            EngineCmd::Delete(index) => {
                let action = session.delete(index).await;
                dispatch(action, &mut session, &ui);
            }
            EngineCmd::ModeNext => {
                session.switch_next().await;
                send_state(&session, &ui, false);
                push_matches(&mut session, &ui);
            }
            EngineCmd::ModePrevious => {
                session.switch_previous().await;
                send_state(&session, &ui, false);
                push_matches(&mut session, &ui);
            }
            EngineCmd::ModeTo(index) => {
                session.switch_to(index).await;
                send_state(&session, &ui, false);
                push_matches(&mut session, &ui);
            }
            EngineCmd::Stop => break,
        }
    }
}

/// Forward a resolved mode action. `Session` already consumed
/// Reload/SwitchMode internally (returning `Nothing`), but a reload changes
/// state — refresh the surface on anything non-terminal.
fn dispatch(action: Action, session: &mut Session, ui: &Sender<LauncherInput>) {
    match action {
        Action::Nothing => {
            send_state(session, ui, true);
            push_matches(session, ui);
        }
        other => {
            let _ = ui.send(LauncherInput::Engine(EngineEvent::Action(other)));
        }
    }
}

fn send_state(session: &Session, ui: &Sender<LauncherInput>, after_activate: bool) {
    let state = session.state();
    let _ = ui.send(LauncherInput::Engine(EngineEvent::State {
        prompt: state.prompt.clone(),
        message: state.message.clone(),
        active_mode: session.active_index(),
        mode_names: session
            .mode_display_names()
            .into_iter()
            .map(ToOwned::to_owned)
            .collect(),
        multi_select: state.multi_select,
        after_activate,
        keep_filter: state.keep_filter,
        keep_selection: state.keep_selection,
        new_selection: state.new_selection,
    }));
}

fn push_matches(session: &mut Session, ui: &Sender<LauncherInput>) {
    let matched = session.matched();
    let items = session.engine.items().clone();
    let _ = ui.send(LauncherInput::Engine(EngineEvent::Matches { items, matched }));
}

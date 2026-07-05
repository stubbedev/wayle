//! Reactive treeman worktree-health service.
//!
//! Exposes the aggregated worktree status as a [`Property`] that stays live via
//! the daemon's `event_subscribe` push: on connect and on each subsequent event
//! (debounced) it runs `treeman status --format json` and republishes. When the
//! daemon socket is unavailable it degrades to a slow poll on the same fetch,
//! which still works because `treeman status` reads the store directly.

use std::{io::ErrorKind, time::Duration};

use tokio::{process::Command, time::sleep};
use tokio_util::sync::CancellationToken;
use tracing::{debug, warn};
use wayle_core::Property;

use crate::{
    error::{Error, Result},
    model::TreemanStatus,
    socket,
};

/// A worktree mutation, dispatched through the `treeman` CLI. The CLI resolves
/// the repo, autostarts the daemon, and queues the work; the resulting
/// lifecycle events flow back through the subscription and refresh the status,
/// so callers do not poll for completion.
#[derive(Debug, Clone, Copy)]
pub enum Action {
    /// Re-run the prepare pipeline for the worktree.
    Prepare,
    /// Drop the worktree's branch-scoped databases and re-seed them.
    Reset,
    /// Tear the worktree down entirely (teardown hooks + DB drop + git remove).
    Teardown,
}

impl Action {
    fn args(self, worktree_path: &str) -> Vec<&str> {
        match self {
            Self::Prepare => vec!["prepare", "--worktree", worktree_path],
            Self::Reset => vec!["db", "reset", worktree_path],
            Self::Teardown => vec!["worktree", "delete", worktree_path, "--yes"],
        }
    }
}

/// Default `treeman` binary name (resolved on `$PATH`).
const DEFAULT_BINARY: &str = "treeman";
/// Delay between reconnect attempts; doubles as the poll interval while the
/// daemon is down.
const RECONNECT_DELAY: Duration = Duration::from_secs(5);
/// Quiet window after the last event before refetching, so a burst of events
/// during one operation collapses into a single status refresh.
const DEBOUNCE: Duration = Duration::from_millis(200);

/// Reactive treeman status service.
///
/// The [`status`](Self::status) field is `None` until the first successful
/// fetch and whenever treeman has never produced readable data.
#[derive(Debug)]
pub struct TreemanService {
    cancellation_token: CancellationToken,
    binary: String,

    /// Latest aggregated worktree health. `None` before the first fetch.
    pub status: Property<Option<TreemanStatus>>,
}

impl TreemanService {
    /// Returns a builder for configuring the service.
    #[must_use]
    pub fn builder() -> TreemanServiceBuilder {
        TreemanServiceBuilder::new()
    }

    /// Dispatches a worktree [`Action`] via the `treeman` CLI.
    ///
    /// Fire-and-forget from the UI's perspective: the CLI queues the work with
    /// the daemon and returns, and the status subscription reflects progress.
    ///
    /// # Errors
    ///
    /// Returns [`Error::Io`] if `treeman` cannot be spawned, or
    /// [`Error::Command`] (carrying stderr) if it exits non-zero.
    pub async fn run_action(&self, action: Action, worktree_path: &str) -> Result<()> {
        let output = Command::new(&self.binary)
            .args(action.args(worktree_path))
            .output()
            .await?;

        if output.status.success() {
            Ok(())
        } else {
            Err(Error::Command(
                String::from_utf8_lossy(&output.stderr).trim().to_owned(),
            ))
        }
    }
}

impl Drop for TreemanService {
    fn drop(&mut self) {
        self.cancellation_token.cancel();
    }
}

/// Builder for [`TreemanService`].
pub struct TreemanServiceBuilder {
    binary: String,
}

impl TreemanServiceBuilder {
    /// Creates a builder with defaults (the `treeman` binary on `$PATH`).
    #[must_use]
    pub fn new() -> Self {
        Self {
            binary: DEFAULT_BINARY.to_owned(),
        }
    }

    /// Overrides the `treeman` binary path.
    #[must_use]
    pub fn binary(mut self, binary: impl Into<String>) -> Self {
        self.binary = binary.into();
        self
    }

    /// Builds the service and starts the background subscription task.
    #[must_use]
    pub fn build(self) -> TreemanService {
        let cancellation_token = CancellationToken::new();
        let status = Property::new(None);
        let binary = self.binary;

        tokio::spawn(run(
            cancellation_token.child_token(),
            status.clone(),
            binary.clone(),
        ));

        TreemanService {
            cancellation_token,
            binary,
            status,
        }
    }
}

impl Default for TreemanServiceBuilder {
    fn default() -> Self {
        Self::new()
    }
}

/// Outcome of one `treeman status` invocation.
enum Fetch {
    /// Parsed status.
    Data(TreemanStatus),
    /// Ran but produced no usable data (non-zero exit, parse failure).
    Empty,
    /// The `treeman` binary is not installed (spawn failed with `NotFound`).
    Unavailable,
}

/// Runs `treeman status --format json` and classifies the outcome.
async fn fetch(binary: &str) -> Fetch {
    match Command::new(binary)
        .args(["status", "--format", "json"])
        .output()
        .await
    {
        Ok(output) => classify(&output),
        Err(err) if err.kind() == ErrorKind::NotFound => Fetch::Unavailable,
        Err(err) => {
            debug!(error = %err, "treeman status command not runnable");
            Fetch::Empty
        }
    }
}

/// Classifies a completed `treeman status` invocation into a [`Fetch`].
fn classify(output: &std::process::Output) -> Fetch {
    if !output.status.success() {
        debug!(
            code = ?output.status.code(),
            stderr = %String::from_utf8_lossy(&output.stderr).trim(),
            "treeman status exited non-zero"
        );
        return Fetch::Empty;
    }

    match serde_json::from_slice::<TreemanStatus>(&output.stdout) {
        Ok(status) => Fetch::Data(status),
        Err(err) => {
            warn!(error = %err, "cannot parse treeman status JSON");
            Fetch::Empty
        }
    }
}

/// Refetches and republishes when changed. Returns `false` when treeman is not
/// installed, signalling the caller to stop the loop instead of spinning.
async fn refresh(binary: &str, status: &Property<Option<TreemanStatus>>) -> bool {
    match fetch(binary).await {
        Fetch::Data(next) => {
            if status.get().as_ref() != Some(&next) {
                status.set(Some(next));
            }
            true
        }
        Fetch::Empty => true,
        Fetch::Unavailable => false,
    }
}

/// Background loop: fetch, subscribe, refetch on events; reconnect/repoll on drop.
async fn run(token: CancellationToken, status: Property<Option<TreemanStatus>>, binary: String) {
    loop {
        // Snapshot current state first — this works even when the daemon is
        // down, so the widget shows data before (or without) a subscription.
        // A missing binary means treeman isn't installed: idle instead of
        // re-spawning a nonexistent command every RECONNECT_DELAY.
        if !refresh(&binary, &status).await {
            debug!("treeman binary not found; status service idle");
            return;
        }

        match socket::connect_subscribe().await {
            Ok(mut events) => consume_events(&token, &mut events, &status, &binary).await,
            Err(err) => debug!(error = %err, "treeman daemon socket unavailable"),
        }

        tokio::select! {
            () = token.cancelled() => return,
            () = sleep(RECONNECT_DELAY) => {}
        }
    }
}

/// Drains the event stream, refreshing status on each debounced burst. Returns
/// when the stream closes/errors (triggering a reconnect) or on cancellation.
async fn consume_events(
    token: &CancellationToken,
    events: &mut socket::EventStream,
    status: &Property<Option<TreemanStatus>>,
    binary: &str,
) {
    let mut dirty = false;
    loop {
        tokio::select! {
            () = token.cancelled() => return,
            line = events.next_line() => match line {
                Ok(Some(_)) => dirty = true,
                Ok(None) => {
                    debug!("treeman event stream closed");
                    return;
                }
                Err(err) => {
                    warn!(error = %err, "treeman event stream read error");
                    return;
                }
            },
            () = sleep(DEBOUNCE), if dirty => {
                refresh(binary, status).await;
                dirty = false;
            }
        }
    }
}

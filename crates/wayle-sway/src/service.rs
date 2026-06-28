//! The [`SwayService`] type: reactive compositor state plus every public
//! method for reading, watching, and commanding sway.

use std::{collections::HashMap, sync::Arc};

use derive_more::Debug;
use tokio::sync::broadcast;
use tokio_util::sync::CancellationToken;
use tracing::instrument;
use wayle_core::Property;
use wayle_traits::ServiceMonitoring;

use crate::{
    constants::EVENT_CHANNEL_CAPACITY,
    core::{Window, Workspace},
    error::Result,
    ipc::{SwayCommandClient, SwayEvent},
};

/// How to address a workspace when focusing it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum WorkspaceRef {
    /// The stable sway container id; resolved against the current snapshot.
    Id(u64),
    /// A leading workspace number (`workspace number N`).
    Number(i32),
    /// A workspace name (`workspace "name"`).
    Name(String),
}

/// Reactive bindings to the sway compositor. See [crate-level docs](crate).
///
/// All public fields are [`Property`] values that update as sway emits events.
#[derive(Debug)]
pub struct SwayService {
    #[debug(skip)]
    pub(crate) cancellation_token: CancellationToken,
    #[debug(skip)]
    pub(crate) command_client: Arc<SwayCommandClient>,
    #[debug(skip)]
    pub(crate) inbound_event_tx: broadcast::Sender<SwayEvent>,

    /// All workspaces keyed by stable id. Iteration order is undefined; sort by
    /// `(output, num)` at the call site when ordered display is needed.
    pub workspaces: Property<HashMap<u64, Arc<Workspace>>>,

    /// All open leaf windows keyed by stable id.
    pub windows: Property<HashMap<u64, Arc<Window>>>,

    /// Name of the active XKB keyboard layout.
    ///
    /// `None` until sway reports an input device with a layout, or when no
    /// keyboard is connected.
    pub keyboard_layout: Property<Option<String>>,
}

impl SwayService {
    /// Connects to sway, subscribes to the event stream, and returns a ready
    /// service.
    ///
    /// # Errors
    ///
    /// - [`Error::SwayNotRunning`](crate::Error::SwayNotRunning) if `$SWAYSOCK`
    ///   is unset.
    /// - [`Error::IpcConnectionFailed`](crate::Error::IpcConnectionFailed) if
    ///   either the command or event-stream socket fails to connect.
    #[instrument(err)]
    pub async fn new() -> Result<Arc<Self>> {
        let cancellation_token = CancellationToken::new();
        let command_client = Arc::new(SwayCommandClient::connect().await?);
        let (inbound_event_tx, _) = broadcast::channel(EVENT_CHANNEL_CAPACITY);

        let service = Arc::new(Self {
            cancellation_token,
            command_client,
            inbound_event_tx,
            workspaces: Property::new(HashMap::new()),
            windows: Property::new(HashMap::new()),
            keyboard_layout: Property::new(None),
        });

        service.start_monitoring().await?;
        Ok(service)
    }

    /// Looks up a workspace by id in the current snapshot.
    pub fn workspace(&self, id: u64) -> Option<Arc<Workspace>> {
        self.workspaces.get().get(&id).cloned()
    }

    /// Returns the currently focused window from the current snapshot, or
    /// `None` when focus is held by a layer-shell surface or nothing is
    /// focused.
    pub fn focused_window(&self) -> Option<Arc<Window>> {
        self.windows
            .get()
            .into_values()
            .find(|window| window.is_focused.get())
    }

    /// Returns the sway version string reported over IPC.
    ///
    /// # Errors
    ///
    /// Surfaces any transport or parse error.
    #[instrument(skip(self), err)]
    pub async fn version(&self) -> Result<String> {
        self.command_client.query_version().await
    }

    /// Runs one or more sway commands over IPC (e.g. `workspace number 3`).
    ///
    /// # Errors
    ///
    /// - [`Error::CommandRejected`](crate::Error::CommandRejected) if sway
    ///   rejected any sub-command.
    /// - transport/parse errors.
    #[instrument(skip(self), fields(command = %command), err)]
    pub async fn run_command(&self, command: &str) -> Result<()> {
        self.command_client.run_command(command).await
    }

    /// Focuses a workspace by id, number, or name.
    ///
    /// # Errors
    /// See [`SwayService::run_command`].
    pub async fn focus_workspace(&self, reference: WorkspaceRef) -> Result<()> {
        let command = self.focus_command(reference);
        self.run_command(&command).await
    }

    /// Focuses the next workspace on the current output.
    ///
    /// # Errors
    /// See [`SwayService::run_command`].
    pub async fn focus_next_on_output(&self) -> Result<()> {
        self.run_command("workspace next_on_output").await
    }

    /// Focuses the previous workspace on the current output.
    ///
    /// # Errors
    /// See [`SwayService::run_command`].
    pub async fn focus_prev_on_output(&self) -> Result<()> {
        self.run_command("workspace prev_on_output").await
    }

    /// Focuses the most recently focused workspace (toggle).
    ///
    /// # Errors
    /// See [`SwayService::run_command`].
    pub async fn focus_back_and_forth(&self) -> Result<()> {
        self.run_command("workspace back_and_forth").await
    }

    /// Builds the `workspace` command for a [`WorkspaceRef`], resolving an id
    /// against the current snapshot (preferring the number, falling back to
    /// the name).
    fn focus_command(&self, reference: WorkspaceRef) -> String {
        match reference {
            WorkspaceRef::Number(num) => format!("workspace --no-auto-back-and-forth number {num}"),
            WorkspaceRef::Name(name) => {
                format!("workspace --no-auto-back-and-forth {}", quote(&name))
            }
            WorkspaceRef::Id(id) => match self.workspace(id) {
                Some(workspace) => {
                    let num = workspace.num.get();
                    if num >= 0 {
                        format!("workspace --no-auto-back-and-forth number {num}")
                    } else {
                        format!(
                            "workspace --no-auto-back-and-forth {}",
                            quote(&workspace.name.get())
                        )
                    }
                }
                None => format!("[con_id={id}] focus"),
            },
        }
    }
}

/// Double-quotes a workspace name for use in a sway command, escaping any
/// embedded quotes and backslashes.
fn quote(name: &str) -> String {
    let escaped = name.replace('\\', "\\\\").replace('"', "\\\"");
    format!("\"{escaped}\"")
}

impl Drop for SwayService {
    fn drop(&mut self) {
        self.cancellation_token.cancel();
    }
}

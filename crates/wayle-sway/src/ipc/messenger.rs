//! Persistent command-socket client that issues requests and returns replies.

use tokio::{io::BufReader, net::UnixStream, sync::Mutex};
use tracing::instrument;

use super::{
    protocol::{self, MessageType},
    sway_socket_path,
};
use crate::{
    error::{Error, Result, SocketKind},
    types::{CommandResult, InputReply, VersionReply, WorkspaceReply},
};

/// Owns the long-lived command socket for request/reply traffic with sway.
pub(crate) struct SwayCommandClient {
    stream: Mutex<BufReader<UnixStream>>,
}

impl SwayCommandClient {
    /// Opens the command socket using the path from `$SWAYSOCK`.
    ///
    /// # Errors
    ///
    /// - [`Error::SwayNotRunning`] if the environment variable is unset.
    /// - [`Error::IpcConnectionFailed`] with [`SocketKind::Command`] if the
    ///   socket path is set but the connection fails.
    pub(crate) async fn connect() -> Result<Self> {
        let socket_path = sway_socket_path()?;
        let stream = UnixStream::connect(&socket_path).await.map_err(|source| {
            Error::IpcConnectionFailed {
                kind: SocketKind::Command,
                source,
            }
        })?;

        Ok(Self {
            stream: Mutex::new(BufReader::new(stream)),
        })
    }

    /// Sends one request and returns the raw JSON payload of the matching
    /// reply. sway answers requests in order, so the mutex is held only for
    /// the duration of one write + read pair.
    async fn request(&self, message_type: MessageType, payload: &[u8]) -> Result<Vec<u8>> {
        let mut guard = self.stream.lock().await;
        protocol::write_message(guard.get_mut(), message_type, payload).await?;

        loop {
            let message = protocol::read_message(&mut *guard, SocketKind::Command).await?;
            // The command socket should never carry events, but a stray event
            // would desync the reply stream; skip any that slip through.
            if message.event_kind().is_none() {
                return Ok(message.payload);
            }
        }
    }

    /// Runs sway commands (e.g. `workspace number 3`) and surfaces the first
    /// failure reported by sway.
    ///
    /// # Errors
    ///
    /// - [`Error::CommandRejected`] if any sub-command failed.
    /// - transport/parse errors from [`request`](Self::request).
    #[instrument(skip(self), fields(command = %command), err)]
    pub(crate) async fn run_command(&self, command: &str) -> Result<()> {
        let payload = self
            .request(MessageType::RunCommand, command.as_bytes())
            .await?;
        let results: Vec<CommandResult> = serde_json::from_slice(&payload)?;

        for result in results {
            if !result.success {
                return Err(Error::CommandRejected(
                    result.error.unwrap_or_else(|| command.to_owned()),
                ));
            }
        }
        Ok(())
    }

    /// Fetches the current workspace list.
    pub(crate) async fn get_workspaces(&self) -> Result<Vec<WorkspaceReply>> {
        let payload = self.request(MessageType::GetWorkspaces, b"").await?;
        Ok(serde_json::from_slice(&payload)?)
    }

    /// Fetches the full container tree as the raw JSON root node.
    pub(crate) async fn get_tree(&self) -> Result<crate::types::TreeNode> {
        let payload = self.request(MessageType::GetTree, b"").await?;
        Ok(serde_json::from_slice(&payload)?)
    }

    /// Fetches the input device list.
    pub(crate) async fn get_inputs(&self) -> Result<Vec<InputReply>> {
        let payload = self.request(MessageType::GetInputs, b"").await?;
        Ok(serde_json::from_slice(&payload)?)
    }

    /// Sends `GET_VERSION` and returns the human-readable version string.
    #[instrument(skip(self), err)]
    pub(crate) async fn query_version(&self) -> Result<String> {
        let payload = self.request(MessageType::GetVersion, b"").await?;
        let reply: VersionReply = serde_json::from_slice(&payload)?;
        Ok(reply.human_readable)
    }
}

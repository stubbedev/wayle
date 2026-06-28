//! Opens a dedicated socket, subscribes to sway's `workspace` and `window`
//! events, and pumps a decoded [`SwayEvent`] into the supplied broadcast
//! channel on each one.

use tokio::{io::BufReader, net::UnixStream, sync::broadcast};
use tokio_util::sync::CancellationToken;
use tracing::{instrument, warn};

use super::{
    protocol::{self, EventKind, MessageType},
    sway_socket_path,
};
use crate::error::{Error, Result, SocketKind};

/// A coarse signal that some workspace-, window-, or input-level state changed
/// and the reactive snapshot should be re-queried.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[allow(clippy::enum_variant_names)]
pub(crate) enum SwayEvent {
    /// A `workspace` event arrived (focus, init, empty, rename, urgent, …).
    WorkspaceChanged,
    /// A `window` event arrived (new, close, focus, title, urgent, …).
    WindowChanged,
    /// An `input` event arrived (keyboard layout change, device add/remove, …).
    InputChanged,
}

/// Connects the event-stream socket, sends `SUBSCRIBE ["workspace","window"]`,
/// and spawns the read loop.
///
/// The spawned task pushes a [`SwayEvent`] into `inbound_event_tx` for each
/// event and exits on cancellation or socket EOF.
///
/// # Errors
///
/// Surfaces any error that happens before the read loop starts (connect,
/// subscribe handshake).
#[instrument(skip(inbound_event_tx, cancellation_token), err)]
pub(crate) async fn subscribe_events(
    inbound_event_tx: broadcast::Sender<SwayEvent>,
    cancellation_token: CancellationToken,
) -> Result<()> {
    let socket_path = sway_socket_path()?;
    let stream =
        UnixStream::connect(&socket_path)
            .await
            .map_err(|source| Error::IpcConnectionFailed {
                kind: SocketKind::EventStream,
                source,
            })?;
    let mut reader = BufReader::new(stream);

    protocol::write_message(
        reader.get_mut(),
        MessageType::Subscribe,
        br#"["workspace","window","input"]"#,
    )
    .await?;
    read_subscribe_ack(&mut reader).await?;

    tokio::spawn(pump_events(reader, inbound_event_tx, cancellation_token));
    Ok(())
}

async fn read_subscribe_ack(reader: &mut BufReader<UnixStream>) -> Result<()> {
    // The subscribe reply is an ordinary (non-event) message; loop past any
    // event that races ahead of the ack.
    loop {
        let message = protocol::read_message(reader, SocketKind::EventStream).await?;
        if message.event_kind().is_none() {
            return Ok(());
        }
    }
}

async fn pump_events(
    mut reader: BufReader<UnixStream>,
    inbound_event_tx: broadcast::Sender<SwayEvent>,
    cancellation_token: CancellationToken,
) {
    loop {
        tokio::select! {
            _ = cancellation_token.cancelled() => return,
            message = protocol::read_message(&mut reader, SocketKind::EventStream) => {
                match message {
                    Ok(message) => {
                        if let Some(event) = classify(&message) {
                            let _ = inbound_event_tx.send(event);
                        }
                    }
                    Err(Error::SocketClosed { .. }) => {
                        warn!("sway event stream closed");
                        return;
                    }
                    Err(err) => {
                        warn!(error = %err, "sway event stream read error");
                        return;
                    }
                }
            }
        }
    }
}

fn classify(message: &protocol::RawMessage) -> Option<SwayEvent> {
    match message.event_kind()? {
        EventKind::Workspace => Some(SwayEvent::WorkspaceChanged),
        EventKind::Window => Some(SwayEvent::WindowChanged),
        EventKind::Input => Some(SwayEvent::InputChanged),
        EventKind::Other => None,
    }
}

//! The i3/sway IPC wire protocol: a fixed magic header, a little-endian
//! payload length and message type, then a JSON payload.
//!
//! Reference: <https://i3wm.org/docs/ipc.html>. sway speaks the same protocol.

use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};

use crate::error::{Error, Result};

/// Magic string that prefixes every IPC message in both directions.
const MAGIC: &[u8; 6] = b"i3-ipc";

/// High bit set on the `type` field of a message that is an event rather than
/// a reply.
const EVENT_BIT: u32 = 1 << 31;

/// Outgoing request message types used by this crate.
#[derive(Debug, Clone, Copy)]
pub(crate) enum MessageType {
    /// `RUN_COMMAND`: execute one or more sway commands.
    RunCommand = 0,
    /// `GET_WORKSPACES`: list all workspaces.
    GetWorkspaces = 1,
    /// `SUBSCRIBE`: subscribe to an event list.
    Subscribe = 2,
    /// `GET_TREE`: dump the full container tree.
    GetTree = 4,
    /// `GET_VERSION`: report the running sway version.
    GetVersion = 7,
    /// `GET_INPUTS`: list input devices (keyboards carry the active layout).
    GetInputs = 100,
}

/// Event categories this crate cares about, decoded from a reply's type field.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum EventKind {
    /// A `workspace` event (focus, init, empty, rename, urgent, …).
    Workspace,
    /// A `window` event (new, close, focus, title, urgent, …).
    Window,
    /// An `input` event (keyboard layout changes, device add/remove, …).
    Input,
    /// Any other event we subscribed to incidentally; ignored.
    Other,
}

impl EventKind {
    /// Decodes the event category from a raw message type with the event bit
    /// set. The low bits select the event: workspace = 0, window = 3,
    /// input = 21.
    fn from_raw(raw_type: u32) -> Self {
        match raw_type & !EVENT_BIT {
            0 => Self::Workspace,
            3 => Self::Window,
            21 => Self::Input,
            _ => Self::Other,
        }
    }
}

/// Writes a single IPC message with the given type and JSON payload.
pub(crate) async fn write_message<W: AsyncWrite + Unpin>(
    writer: &mut W,
    message_type: MessageType,
    payload: &[u8],
) -> Result<()> {
    let mut frame = Vec::with_capacity(MAGIC.len() + 8 + payload.len());
    frame.extend_from_slice(MAGIC);
    frame.extend_from_slice(&(payload.len() as u32).to_ne_bytes());
    frame.extend_from_slice(&(message_type as u32).to_ne_bytes());
    frame.extend_from_slice(payload);
    writer.write_all(&frame).await?;
    writer.flush().await?;
    Ok(())
}

/// One decoded IPC message: its raw type field and JSON payload bytes.
pub(crate) struct RawMessage {
    pub raw_type: u32,
    pub payload: Vec<u8>,
}

impl RawMessage {
    /// Returns the event category when this message is an event, or `None`
    /// when it is an ordinary reply.
    pub(crate) fn event_kind(&self) -> Option<EventKind> {
        (self.raw_type & EVENT_BIT != 0).then(|| EventKind::from_raw(self.raw_type))
    }
}

/// Reads one IPC message: validates the magic header, then reads the length
/// and type and the payload of that length.
///
/// # Errors
///
/// - [`Error::SocketClosed`] on EOF before a full header.
/// - [`Error::InvalidMagic`] if the magic header does not match.
/// - [`Error::Io`] on any other read failure.
pub(crate) async fn read_message<R: AsyncRead + Unpin>(
    reader: &mut R,
    kind: crate::error::SocketKind,
) -> Result<RawMessage> {
    let mut header = [0_u8; 14];
    if let Err(err) = reader.read_exact(&mut header).await {
        if err.kind() == std::io::ErrorKind::UnexpectedEof {
            return Err(Error::SocketClosed { kind });
        }
        return Err(Error::Io(err));
    }

    if &header[..6] != MAGIC {
        return Err(Error::InvalidMagic);
    }

    let payload_len = u32::from_ne_bytes([header[6], header[7], header[8], header[9]]) as usize;
    let raw_type = u32::from_ne_bytes([header[10], header[11], header[12], header[13]]);

    let mut payload = vec![0_u8; payload_len];
    reader.read_exact(&mut payload).await.map_err(|err| {
        if err.kind() == std::io::ErrorKind::UnexpectedEof {
            Error::SocketClosed { kind }
        } else {
            Error::Io(err)
        }
    })?;

    Ok(RawMessage { raw_type, payload })
}

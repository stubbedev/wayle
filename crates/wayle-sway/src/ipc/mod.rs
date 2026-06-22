//! IPC layer: the i3 wire protocol, the command-socket client, and the
//! event-stream subscription.

mod events;
mod messenger;
mod protocol;

use std::{env, ffi::OsString, path::PathBuf};

pub(crate) use events::{SwayEvent, subscribe_events};
pub(crate) use messenger::SwayCommandClient;

use crate::{
    constants::SOCKET_PATH_ENV,
    error::{Error, Result},
};

/// Reads `$SWAYSOCK` and turns it into a path. Both the command socket and the
/// event-stream socket connect to this same path.
pub(super) fn sway_socket_path() -> Result<PathBuf> {
    let raw: OsString = env::var_os(SOCKET_PATH_ENV).ok_or(Error::SwayNotRunning)?;
    Ok(PathBuf::from(raw))
}

//! Unix-socket protocol between the `wayle launcher` CLI (rofi shim) and
//! the launcher surface hosted in the shell.
//!
//! One persistent connection per launcher session, newline-delimited JSON
//! frames. The connection doubles as the session's lifetime: when the CLI
//! dies (ctrl-C), the daemon sees EOF and closes the surface; when the
//! daemon replies with a result, the CLI prints it and exits with the
//! rofi-compatible exit code.

use std::{collections::BTreeMap, path::PathBuf};

use serde::{Deserialize, Serialize};
use tokio::{
    io::{AsyncBufReadExt, AsyncWriteExt, BufReader},
    net::{
        UnixStream,
        unix::{OwnedReadHalf, OwnedWriteHalf},
    },
};

/// Rows per `rows` frame when streaming stdin (dmenu mode).
pub const ROW_CHUNK: usize = 2000;

/// Resolves the launcher socket path: `$XDG_RUNTIME_DIR/wayle/launcher.sock`,
/// falling back to `/tmp/wayle-launcher.sock` when the runtime dir is unset.
#[must_use]
pub fn socket_path() -> PathBuf {
    match std::env::var_os("XDG_RUNTIME_DIR") {
        Some(dir) => PathBuf::from(dir).join("wayle").join("launcher.sock"),
        None => PathBuf::from("/tmp/wayle-launcher.sock"),
    }
}

/// Per-invocation options carried by the `open` frame: the rofi CLI flag
/// surface. Every field is optional — the daemon merges `Some` values over
/// the `[launcher]` config.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default, rename_all = "kebab-case")]
pub struct SessionOptions {
    /// `-show <mode>`: mode to open in.
    pub mode: Option<String>,
    /// `-modes`/`-modi` list, including `name:script` entries.
    pub modes: Option<Vec<String>>,
    /// `-dmenu`: rows come from the CLI over `rows` frames.
    pub dmenu: bool,
    /// `-e <message>`: message-dialog mode (no input/list).
    pub error_message: Option<String>,

    /// `-p`: prompt text.
    pub prompt: Option<String>,
    /// `-l`: visible lines.
    pub lines: Option<u32>,
    /// `-mesg`: message row (Pango markup allowed).
    pub mesg: Option<String>,
    /// `-filter`: pre-filled query text.
    pub filter: Option<String>,
    /// `-select`: pre-select the first entry matching this string.
    pub select: Option<String>,
    /// `-selected-row`: pre-select row by index.
    pub selected_row: Option<u32>,
    /// `-window-title`: surface title suffix.
    pub window_title: Option<String>,

    /// `-multi-select`.
    pub multi_select: bool,
    /// `-only-match`: input restricted to matching rows.
    pub only_match: bool,
    /// `-no-custom`: reject custom (non-row) accepts.
    pub no_custom: bool,
    /// `-password`: hide typed input.
    pub password: bool,
    /// `-markup-rows`: rows are Pango markup.
    pub markup_rows: bool,
    /// `-sync`: wait for all rows before showing the surface.
    pub sync: bool,
    /// `-dump`: print the filtered list and exit, no UI.
    pub dump: bool,
    /// `-u`: urgent row indices (CLI expands ranges).
    pub urgent: Option<Vec<u32>>,
    /// `-a`: active row indices (CLI expands ranges).
    pub active: Option<Vec<u32>>,
    /// `-ballot-selected-str`.
    pub ballot_selected: Option<String>,
    /// `-ballot-unselected-str`.
    pub ballot_unselected: Option<String>,
    /// `-display-columns`: 1-based columns of each row to show.
    pub display_columns: Option<Vec<u32>>,
    /// `-display-column-separator` (regex, default `\t`).
    pub display_column_separator: Option<String>,
    /// `-ellipsize-mode`: start | middle | end.
    pub ellipsize_mode: Option<String>,
    /// `-keep-right`: ellipsize at the start.
    pub keep_right: bool,

    /// `-matching`: normal | regex | glob | fuzzy | prefix.
    pub matching: Option<String>,
    /// `-tokenize`/-no-tokenize.
    pub tokenize: Option<bool>,
    /// `-matching-negate-char`.
    pub negate_char: Option<char>,
    /// `-normalize-match`.
    pub normalize_match: Option<bool>,
    /// `-sort`/-no-sort.
    pub sort: Option<bool>,
    /// `-sorting-method`: levenshtein | fzf.
    pub sorting_method: Option<String>,
    /// `-case-sensitive`.
    pub case_sensitive: Option<bool>,
    /// `-case-smart`.
    pub case_smart: Option<bool>,
    /// `-i` (dmenu): force case-insensitive.
    pub case_insensitive: Option<bool>,

    /// `-location`: rofi 0-8 grid position.
    pub location: Option<u8>,
    /// `-monitor`: output connector name or rofi numeric.
    pub monitor: Option<String>,
    /// `-no-fixed-num-lines`.
    pub no_fixed_num_lines: bool,
    /// `-sidebar-mode`: mode tabs.
    pub sidebar_mode: Option<bool>,
    /// `-cycle`.
    pub cycle: Option<bool>,
    /// `-auto-select`.
    pub auto_select: Option<bool>,
    /// `-hover-select`.
    pub hover_select: Option<bool>,
    /// `-show-icons`/-no-show-icons.
    pub show_icons: Option<bool>,
    /// `-icon-theme`.
    pub icon_theme: Option<String>,

    /// `-terminal`.
    pub terminal: Option<String>,
    /// `-run-command`.
    pub run_command: Option<String>,
    /// `-run-shell-command`.
    pub run_shell_command: Option<String>,
    /// `-run-list-command`.
    pub run_list_command: Option<String>,
    /// `-ssh-client`.
    pub ssh_client: Option<String>,
    /// `-ssh-command`.
    pub ssh_command: Option<String>,
    /// `-parse-hosts`.
    pub parse_hosts: Option<bool>,
    /// `-parse-known-hosts`.
    pub parse_known_hosts: Option<bool>,
    /// `-window-format`.
    pub window_format: Option<String>,
    /// `-window-command`.
    pub window_command: Option<String>,
    /// `-window-match-fields`.
    pub window_match_fields: Option<Vec<String>>,
    /// `-window-hide-active-window`.
    pub hide_active_window: Option<bool>,
    /// `-drun-categories`.
    pub drun_categories: Option<Vec<String>>,
    /// `-drun-exclude-categories`.
    pub drun_exclude_categories: Option<Vec<String>>,
    /// `-drun-match-fields`.
    pub drun_match_fields: Option<Vec<String>>,
    /// `-drun-display-format`.
    pub drun_display_format: Option<String>,
    /// `-drun-show-actions`.
    pub drun_show_actions: Option<bool>,
    /// `-drun-url-launcher`.
    pub drun_url_launcher: Option<String>,
    /// `-combi-modes`.
    pub combi_modes: Option<Vec<String>>,
    /// `-combi-display-format`.
    pub combi_display_format: Option<String>,

    /// `-kb-*` overrides: action name (without `kb-`) → key list.
    pub kb_overrides: BTreeMap<String, String>,
}

/// Frames sent CLI → daemon.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "kebab-case")]
pub enum ClientFrame {
    /// Start a session.
    Open {
        /// Merged rofi flags.
        options: Box<SessionOptions>,
        /// rofi `-replace`: displace a live session instead of failing busy.
        replace: bool,
    },
    /// A chunk of dmenu rows (streamed while stdin is read).
    Rows {
        /// Row texts, in input order.
        items: Vec<String>,
    },
    /// stdin reached EOF; no more rows.
    RowsDone,
}

/// One accepted row.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Selected {
    /// Input index of the row; `-1` for accepted custom input.
    pub index: i64,
    /// Row text (or the custom input).
    pub text: String,
}

/// Frames sent daemon → CLI.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "kebab-case")]
pub enum ServerFrame {
    /// Session accepted; surface is up (or deferred if `sync`).
    Opened,
    /// Another session is active and `replace` was not set.
    Busy,
    /// Session finished with a selection (or custom accept).
    Result {
        /// rofi exit code: 0 accept, 10..=28 kb-custom-N.
        code: i32,
        /// Accepted rows (multiple with multi-select).
        selected: Vec<Selected>,
        /// Query text at accept time (rofi `-format f/F`).
        filter: String,
    },
    /// Session cancelled (Escape, or displaced by `-replace`).
    Cancelled {
        /// rofi exit code (1).
        code: i32,
    },
    /// Reply to `-dump`: the filtered list.
    Dump {
        /// Matching row texts in display order.
        items: Vec<String>,
    },
}

/// Errors raised by the launcher socket client.
#[derive(Debug, thiserror::Error)]
pub enum ClientError {
    /// The shell is not running or the socket is unavailable.
    #[error("cannot connect to wayle launcher socket at {path}: {source}")]
    Connect {
        /// Attempted socket path.
        path: String,
        /// Underlying I/O error.
        source: std::io::Error,
    },

    /// I/O failure while talking to the socket.
    #[error("launcher socket I/O failed: {0}")]
    Io(#[from] std::io::Error),

    /// Serialization or deserialization failure.
    #[error("launcher socket protocol error: {0}")]
    Protocol(#[from] serde_json::Error),

    /// The daemon closed the connection without a result.
    #[error("launcher session ended unexpectedly")]
    Disconnected,
}

/// CLI-side connection to the launcher daemon.
pub struct LauncherClient {
    reader: BufReader<OwnedReadHalf>,
    writer: OwnedWriteHalf,
}

impl LauncherClient {
    /// Connect and send the `open` frame.
    ///
    /// # Errors
    ///
    /// Fails when the socket is unreachable or I/O fails.
    pub async fn open(options: SessionOptions, replace: bool) -> Result<Self, ClientError> {
        let path = socket_path();
        let stream = UnixStream::connect(&path)
            .await
            .map_err(|source| ClientError::Connect {
                path: path.display().to_string(),
                source,
            })?;
        let (read_half, write_half) = stream.into_split();
        let mut client = Self {
            reader: BufReader::new(read_half),
            writer: write_half,
        };
        client
            .send(&ClientFrame::Open {
                options: Box::new(options),
                replace,
            })
            .await?;
        Ok(client)
    }

    /// Send a chunk of dmenu rows.
    ///
    /// # Errors
    ///
    /// Fails on I/O or serialization errors.
    pub async fn send_rows(&mut self, items: Vec<String>) -> Result<(), ClientError> {
        self.send(&ClientFrame::Rows { items }).await
    }

    /// Signal stdin EOF.
    ///
    /// # Errors
    ///
    /// Fails on I/O or serialization errors.
    pub async fn finish_rows(&mut self) -> Result<(), ClientError> {
        self.send(&ClientFrame::RowsDone).await
    }

    /// Read the next daemon frame.
    ///
    /// # Errors
    ///
    /// Fails on I/O errors, protocol errors, or daemon disconnect.
    pub async fn next_frame(&mut self) -> Result<ServerFrame, ClientError> {
        let mut line = String::new();
        let read = self.reader.read_line(&mut line).await?;
        if read == 0 {
            return Err(ClientError::Disconnected);
        }
        Ok(serde_json::from_str(line.trim())?)
    }

    async fn send(&mut self, frame: &ClientFrame) -> Result<(), ClientError> {
        let mut line = serde_json::to_string(frame)?;
        line.push('\n');
        self.writer.write_all(line.as_bytes()).await?;
        self.writer.flush().await?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn open_frame_round_trips() {
        let options = SessionOptions {
            mode: Some("drun".into()),
            prompt: Some("apps".into()),
            multi_select: true,
            urgent: Some(vec![0, 3]),
            ..SessionOptions::default()
        };
        let frame = ClientFrame::Open {
            options: Box::new(options.clone()),
            replace: true,
        };
        let encoded = serde_json::to_string(&frame).unwrap();
        assert!(encoded.contains("\"type\":\"open\""));
        let decoded: ClientFrame = serde_json::from_str(&encoded).unwrap();
        let ClientFrame::Open {
            options: decoded_options,
            replace,
        } = decoded
        else {
            unreachable!("wrong frame");
        };
        assert_eq!(*decoded_options, options);
        assert!(replace);
    }

    #[test]
    fn default_options_serialize_minimal_and_load_back() {
        // Unknown/missing fields must not break older/newer peers.
        let decoded: SessionOptions = serde_json::from_str("{}").unwrap();
        assert_eq!(decoded, SessionOptions::default());
        let with_extra: SessionOptions =
            serde_json::from_str("{\"mode\":\"run\",\"future-field\":true}").unwrap_or_default();
        // serde rejects unknown fields only with deny_unknown_fields; default is lenient.
        assert_eq!(with_extra.mode.as_deref(), Some("run"));
    }

    #[test]
    fn result_frame_round_trips() {
        let frame = ServerFrame::Result {
            code: 10,
            selected: vec![Selected {
                index: -1,
                text: "custom text".into(),
            }],
            filter: "quer".into(),
        };
        let encoded = serde_json::to_string(&frame).unwrap();
        let decoded: ServerFrame = serde_json::from_str(&encoded).unwrap();
        let ServerFrame::Result {
            code,
            selected,
            filter,
        } = decoded
        else {
            unreachable!("wrong frame");
        };
        assert_eq!(code, 10);
        assert_eq!(selected[0].index, -1);
        assert_eq!(filter, "quer");
    }
}

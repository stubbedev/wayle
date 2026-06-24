//! greetd-backed [`AuthConversation`] for pre-login authentication (greeter).
//!
//! Unlike [`crate::PamAuth`], a greeter does not talk to PAM directly — it runs
//! as an unprivileged user and delegates authentication to the `greetd` daemon
//! over a unix socket (path in `$GREETD_SOCK`). The wire format is a native-
//! endian `u32` length prefix followed by a JSON body (the greetd IPC protocol).
//!
//! The whole login flow lives in [`GreetdAuth::run`]: create the session,
//! answer each auth message, and — on success — start the session with the
//! configured command/environment. greetd then replaces the greeter, so a
//! successful `run` is the greeter's exit cue.

use std::{
    io::{self, Read, Write},
    os::unix::net::UnixStream,
};

use serde::{Deserialize, Serialize};
use tracing::warn;

use crate::{AuthConversation, AuthPrompt};

/// Environment variable greetd sets to the IPC socket path.
const GREETD_SOCK: &str = "GREETD_SOCK";

/// greetd authentication: drives a full create→auth→start session flow.
pub struct GreetdAuth {
    stream: UnixStream,
    /// Command (argv) to launch as the user's session on success.
    cmd: Vec<String>,
    /// Extra `KEY=value` environment entries for the session.
    env: Vec<String>,
}

impl GreetdAuth {
    /// Connects to the greetd socket named by `$GREETD_SOCK`.
    ///
    /// `cmd` is the session argv started on successful authentication; `env`
    /// holds extra `KEY=value` entries.
    ///
    /// # Errors
    /// Returns an error if `$GREETD_SOCK` is unset or the socket cannot be
    /// connected.
    pub fn from_env(cmd: Vec<String>, env: Vec<String>) -> io::Result<Self> {
        let path = std::env::var_os(GREETD_SOCK).ok_or_else(|| {
            io::Error::new(
                io::ErrorKind::NotFound,
                format!("{GREETD_SOCK} is not set; not running under greetd"),
            )
        })?;
        let stream = UnixStream::connect(path)?;
        Ok(Self { stream, cmd, env })
    }

    /// Builds a backend over an already-connected stream (for tests).
    #[must_use]
    pub fn with_stream(stream: UnixStream, cmd: Vec<String>, env: Vec<String>) -> Self {
        Self { stream, cmd, env }
    }

    /// Writes one length-prefixed JSON request.
    fn send(&mut self, request: &Request) -> Result<(), String> {
        let payload = serde_json::to_vec(request).map_err(|e| e.to_string())?;
        let len = u32::try_from(payload.len()).map_err(|_| "request too large".to_string())?;
        self.stream
            .write_all(&len.to_ne_bytes())
            .and_then(|()| self.stream.write_all(&payload))
            .and_then(|()| self.stream.flush())
            .map_err(|e| e.to_string())
    }

    /// Reads one length-prefixed JSON response.
    fn recv(&mut self) -> Result<Response, String> {
        let mut len_buf = [0u8; 4];
        self.stream
            .read_exact(&mut len_buf)
            .map_err(|e| e.to_string())?;
        let len = u32::from_ne_bytes(len_buf) as usize;
        let mut buf = vec![0u8; len];
        self.stream
            .read_exact(&mut buf)
            .map_err(|e| e.to_string())?;
        serde_json::from_slice(&buf).map_err(|e| e.to_string())
    }
}

impl AuthConversation for GreetdAuth {
    fn run(
        &mut self,
        username: Option<String>,
        ask: &mut dyn FnMut(AuthPrompt) -> Option<String>,
    ) -> Result<(), String> {
        // greetd needs a username up front; prompt for it if not supplied.
        let username = match username {
            Some(user) => user,
            None => ask(AuthPrompt::Visible("Username".to_owned()))
                .ok_or_else(|| "cancelled".to_owned())?,
        };
        self.send(&Request::CreateSession { username })?;

        // Answer auth messages until greetd reports success or error.
        loop {
            match self.recv()? {
                Response::Success => break,
                Response::Error { description, .. } => {
                    let _ = self.send(&Request::CancelSession);
                    return Err(description);
                }
                Response::AuthMessage {
                    auth_message_type,
                    auth_message,
                } => {
                    let prompt = auth_message_type.into_prompt(auth_message);
                    let wants_input = prompt.wants_input();
                    let response = ask(prompt);
                    // A cancelled input prompt aborts the whole session.
                    if wants_input && response.is_none() {
                        let _ = self.send(&Request::CancelSession);
                        return Err("cancelled".to_owned());
                    }
                    self.send(&Request::PostAuthMessageResponse { response })?;
                }
            }
        }

        // Authenticated: hand off to the session. greetd replaces us on success.
        self.send(&Request::StartSession {
            cmd: self.cmd.clone(),
            env: self.env.clone(),
        })?;
        match self.recv()? {
            Response::Success => Ok(()),
            Response::Error { description, .. } => Err(description),
            other => {
                warn!(?other, "greetd: unexpected response to start_session");
                Err("unexpected response to start_session".to_owned())
            }
        }
    }
}

/// Requests the greeter sends to greetd.
#[derive(Debug, Serialize)]
#[serde(tag = "type", rename_all = "snake_case")]
enum Request {
    CreateSession { username: String },
    PostAuthMessageResponse { response: Option<String> },
    StartSession { cmd: Vec<String>, env: Vec<String> },
    CancelSession,
}

/// Responses greetd sends back.
#[derive(Debug, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
enum Response {
    Success,
    Error {
        #[allow(dead_code)]
        error_type: ErrorType,
        description: String,
    },
    AuthMessage {
        auth_message_type: AuthMessageType,
        auth_message: String,
    },
}

/// Kind of error greetd reports.
#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
enum ErrorType {
    AuthError,
    Error,
}

/// Kind of auth message greetd sends.
#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
enum AuthMessageType {
    Visible,
    Secret,
    Info,
    Error,
}

impl AuthMessageType {
    /// Maps a greetd auth message to the UI-facing [`AuthPrompt`].
    fn into_prompt(self, message: String) -> AuthPrompt {
        match self {
            AuthMessageType::Visible => AuthPrompt::Visible(message),
            AuthMessageType::Secret => AuthPrompt::Secret(message),
            AuthMessageType::Info => AuthPrompt::Info(message),
            AuthMessageType::Error => AuthPrompt::Error(message),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_serializes_to_tagged_json() {
        let json = serde_json::to_string(&Request::CreateSession {
            username: "alice".to_owned(),
        })
        .expect("serialize");
        assert_eq!(json, r#"{"type":"create_session","username":"alice"}"#);

        let json = serde_json::to_string(&Request::PostAuthMessageResponse {
            response: Some("secret".to_owned()),
        })
        .expect("serialize");
        assert_eq!(
            json,
            r#"{"type":"post_auth_message_response","response":"secret"}"#
        );
    }

    #[test]
    fn response_deserializes_from_tagged_json() {
        let resp: Response = serde_json::from_str(
            r#"{"type":"auth_message","auth_message_type":"secret","auth_message":"Password:"}"#,
        )
        .expect("deserialize");
        assert!(matches!(
            &resp,
            Response::AuthMessage {
                auth_message_type: AuthMessageType::Secret,
                ..
            }
        ));
        if let Response::AuthMessage { auth_message, .. } = resp {
            assert_eq!(auth_message, "Password:");
        }

        let resp: Response = serde_json::from_str(r#"{"type":"success"}"#).expect("deserialize");
        assert!(matches!(resp, Response::Success));
    }
}

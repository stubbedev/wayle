//! Portal backend error type.

/// Errors raised while starting or running the portal backend.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// Could not connect to the session bus.
    #[error("cannot connect to session bus: {0}")]
    Connection(String),

    /// Could not register a D-Bus object on the portal root path.
    #[error("cannot register D-Bus object: {0}")]
    Registration(String),

    /// Could not claim the backend's well-known D-Bus name.
    #[error("cannot request D-Bus name: {0}")]
    NameRequest(String),

    /// The Wayle configuration could not be loaded.
    #[error("cannot load configuration: {0}")]
    Config(String),
}

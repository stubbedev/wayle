use std::fmt;

#[derive(Debug)]
pub(crate) struct ResponderDropped;

impl fmt::Display for ResponderDropped {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "pairing responder receiver was dropped")
    }
}

impl std::error::Error for ResponderDropped {}

/// Bluetooth service errors.
#[derive(thiserror::Error, Debug)]
pub enum Error {
    /// D-Bus communication error.
    #[error("dbus error: {0}")]
    Dbus(#[from] zbus::Error),

    /// Service initialization failed.
    #[error("cannot initialize bluetooth service")]
    ServiceInitialization(#[source] Box<dyn std::error::Error + Send + Sync>),

    /// Agent registration failed.
    #[error("cannot register bluetooth agent")]
    AgentRegistration(#[source] Box<dyn std::error::Error + Send + Sync>),

    /// Adapter operation failed.
    #[error("cannot {operation} on adapter")]
    AdapterOperation {
        /// The operation that failed.
        operation: &'static str,
        /// The underlying D-Bus error.
        #[source]
        source: Box<zbus::Error>,
    },

    /// No primary adapter available for the requested operation.
    #[error("cannot {operation}: no primary adapter available")]
    NoPrimaryAdapter {
        /// The operation that requires an adapter.
        operation: &'static str,
    },

    /// Object discovery failed.
    #[error("cannot discover bluetooth objects")]
    Discovery(#[source] zbus::fdo::Error),

    /// Monitoring requires a cancellation token but none was provided.
    #[error("cannot start monitoring: no cancellation token configured")]
    NoCancellationToken,

    /// Pairing request type mismatch.
    #[error("cannot provide {request_type}: no {request_type} request is pending")]
    NoPendingRequest {
        /// The type of pairing request expected.
        request_type: &'static str,
    },

    /// Pairing responder unavailable.
    #[error("cannot provide {request_type}: no responder available")]
    NoResponder {
        /// The type of responder expected.
        request_type: &'static str,
    },

    /// Pairing response channel send failed.
    #[error("cannot send {request_type} response")]
    ResponderSend {
        /// The type of response being sent.
        request_type: &'static str,
        /// The underlying send error.
        #[source]
        source: Box<dyn std::error::Error + Send + Sync>,
    },
}

#[cfg(test)]
mod tests {
    use std::error::Error as _;

    use super::*;

    #[test]
    fn boxed_zbus_source_keeps_display_and_source_chain() {
        let err = Error::AdapterOperation {
            operation: "set alias",
            source: Box::new(zbus::Error::Failure("bus gone".into())),
        };
        assert_eq!(err.to_string(), "cannot set alias on adapter");
        let source = err.source().expect("the zbus error stays the source");
        assert!(source.to_string().contains("bus gone"), "{source}");
    }

    #[test]
    fn plain_variant_has_no_source() {
        assert!(Error::NoCancellationToken.source().is_none());
    }

    /// zbus >= 5.14 grew `zbus::Error`; carried unboxed next to context
    /// fields it pushed `Error` past clippy's `result_large_err` threshold
    /// (128 bytes), which every `Result<_, Error>` in the crate tripped.
    #[test]
    fn error_stays_within_result_large_err_threshold() {
        assert!(
            std::mem::size_of::<Error>() <= 128,
            "{}",
            std::mem::size_of::<Error>()
        );
    }
}

/// System tray service errors.
#[derive(thiserror::Error, Debug)]
pub enum Error {
    /// D-Bus communication error.
    #[error("dbus operation failed")]
    Dbus(#[from] zbus::Error),

    /// Service initialization failed.
    #[error("cannot initialize system tray service: {0}")]
    ServiceInitialization(String),

    /// Cannot register as StatusNotifierWatcher.
    #[error("cannot register as StatusNotifierWatcher: {0}")]
    WatcherRegistration(String),

    /// StatusNotifierItem not found.
    #[error("cannot find StatusNotifierItem: {service}")]
    ItemNotFound {
        /// D-Bus service name of the missing item.
        service: String,
    },

    /// Cannot connect to StatusNotifierItem.
    #[error("cannot connect to tray item {service}")]
    ItemConnection {
        /// D-Bus service name of the item.
        service: String,
        /// Underlying D-Bus error.
        #[source]
        source: Box<zbus::Error>,
    },

    /// Menu operation failed.
    #[error("cannot perform menu operation for item {service}")]
    MenuOperation {
        /// D-Bus service name of the item.
        service: String,
        /// Underlying D-Bus error.
        #[source]
        source: Box<zbus::Error>,
    },

    /// Icon data parsing failed.
    #[error("cannot parse icon data for {service}: {details}")]
    IconParsing {
        /// D-Bus service name of the item.
        service: String,
        /// Description of the parsing failure.
        details: String,
    },

    /// Property conversion failed.
    #[error("cannot convert property {property} for {service}: expected {expected}")]
    PropertyConversion {
        /// D-Bus service name.
        service: String,
        /// Property name that failed to convert.
        property: String,
        /// Expected type.
        expected: String,
    },

    /// System tray operation failed.
    #[error("cannot perform tray operation '{operation}'")]
    Operation {
        /// The operation that failed.
        operation: &'static str,
        /// Underlying D-Bus error.
        #[source]
        source: Box<zbus::Error>,
    },

    /// Tray item does not support the requested activation method.
    #[error("tray item does not support '{operation}', use its menu instead")]
    OperationNotSupported {
        /// The operation that is not supported.
        operation: &'static str,
    },

    /// Invalid service name format.
    #[error("invalid bus name format: {0}")]
    InvalidBusName(String),

    /// ZVariant conversion error.
    #[error("zvariant conversion failed")]
    ZVariant(#[from] zbus::zvariant::Error),
}

#[cfg(test)]
mod tests {
    use std::error::Error as _;

    use super::*;

    #[test]
    fn boxed_zbus_source_keeps_display_and_source_chain() {
        let err = Error::Operation {
            operation: "activate",
            source: Box::new(zbus::Error::Failure("bus gone".into())),
        };
        assert_eq!(err.to_string(), "cannot perform tray operation 'activate'");
        let source = err.source().expect("the zbus error stays the source");
        assert!(source.to_string().contains("bus gone"), "{source}");
    }

    #[test]
    fn plain_variant_has_no_source() {
        assert!(
            Error::OperationNotSupported {
                operation: "activate"
            }
            .source()
            .is_none()
        );
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

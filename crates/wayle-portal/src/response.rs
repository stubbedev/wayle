//! Portal method response codes.
//!
//! Every `org.freedesktop.impl.portal.*` method that involves user interaction
//! returns `(response: u32, results: a{sv})`. The numeric code is fixed by the
//! spec.

/// Result code returned by interactive portal methods.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Response {
    /// The request succeeded.
    Success,
    /// The user cancelled the interaction.
    Cancelled,
    /// The request failed for any other reason (ended early, error, …).
    Other,
}

impl Response {
    /// The wire code for this response.
    #[must_use]
    pub fn code(self) -> u32 {
        match self {
            Self::Success => 0,
            Self::Cancelled => 1,
            Self::Other => 2,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn codes_match_spec() {
        assert_eq!(Response::Success.code(), 0);
        assert_eq!(Response::Cancelled.code(), 1);
        assert_eq!(Response::Other.code(), 2);
    }
}

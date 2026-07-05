//! Bar modules: hardware.
//!
//! Moved verbatim out of wayle-shell in the bar-crate split; the shims at the
//! bottom keep the original `crate::…` paths working so the diff stays a move.
// Internal API for the shell crates only, not a documented public surface.
#![allow(missing_docs)]
#![allow(clippy::missing_errors_doc, clippy::missing_panics_doc)]

pub mod dropdowns;
pub mod modules;

// Path-compat shims mirroring the module tree this code lived in inside
// wayle-shell.
#[allow(unused_imports)]
pub(crate) use wayle_shell_core::{glob, i18n, notify, process, services, template};

#[allow(unused_imports)]
pub(crate) mod shell {
    pub(crate) use wayle_shell_core::{helpers, shell_services as services};

    pub(crate) mod notification_popup {
        #[allow(unused_imports)]
        pub(crate) use wayle_shell_core::notification_icons as helpers;
    }

    pub(crate) mod bar {
        #[allow(unused_imports)]
        pub(crate) use wayle_shell_core::bar::icons;

        #[allow(unused_imports)]
        pub(crate) use crate::{dropdowns, modules};
    }
}

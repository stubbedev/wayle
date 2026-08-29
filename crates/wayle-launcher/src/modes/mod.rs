//! Launch mode implementations.

pub mod calc;
pub mod combi;
pub mod dmenu;
pub mod drun;
pub mod filebrowser;
pub mod keys;
pub mod run;
pub mod script;
pub mod ssh;
pub mod window;

pub use calc::CalcMode;
pub use combi::CombiMode;
pub use dmenu::{DmenuConfig, DmenuMode};
pub use drun::{DrunConfig, DrunField, DrunMode};
pub use filebrowser::{FileBrowserConfig, FileBrowserMode, FileSort};
pub use keys::KeysMode;
pub use run::{RunConfig, RunMode};
pub use script::ScriptMode;
pub use ssh::{SshConfig, SshMode};
pub use window::{WindowConfig, WindowField, WindowMode};

/// Whether an entry is one the user asked to keep out of history (rofi
/// `-ignored-prefixes`).
///
/// Matched against the command — the executable for `run`, the desktop id for
/// `drun` — because that is what history stores and what the flag names.
#[must_use]
pub fn is_ignored(entry: &str, prefixes: &[String]) -> bool {
    prefixes
        .iter()
        .any(|prefix| !prefix.is_empty() && entry.starts_with(prefix.as_str()))
}

#[cfg(test)]
mod tests {
    use super::is_ignored;

    #[test]
    fn a_prefixed_entry_is_kept_out_of_history() {
        let prefixes = vec![String::from("sudo "), String::from("tmp-")];
        assert!(is_ignored("sudo vim", &prefixes));
        assert!(is_ignored("tmp-script", &prefixes));
    }

    #[test]
    fn everything_else_is_still_recorded() {
        let prefixes = vec![String::from("sudo ")];
        assert!(!is_ignored("vim", &prefixes));
        // A prefix in the middle is not a prefix.
        assert!(!is_ignored("run sudo vim", &prefixes));
        // No prefixes configured must not swallow the whole history.
        assert!(!is_ignored("vim", &[]));
        // Nor an empty one, which would match everything.
        assert!(!is_ignored("vim", &[String::new()]));
    }
}

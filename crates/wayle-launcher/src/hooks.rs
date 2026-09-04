//! Commands run when something happens in the menu (rofi `-on-*`).
//!
//! rofi's own contract, pinned against rofi 2.0.0 rather than guessed:
//!
//! - the command string is **shell-parsed and exec'd directly**, not handed
//!   to `sh -c`. A `$HOME` in it stays literal and a `;` is one more
//!   argument, which also means a row whose text holds shell metacharacters
//!   cannot turn a hook into a second command;
//! - `{input}`, `{entry}`, `{mode}` and `{error}` are substituted where the
//!   event has one;
//! - nothing is passed on stdin, and no `ROFI_*` variables are exported;
//! - the child is detached — the menu does not wait for it.
//!
//! Where rofi is inconsistent, wayle is not: rofi substitutes for
//! `-on-selection-changed` and `-on-entry-accepted` but hands
//! `-on-mode-changed` and `-on-menu-canceled` their placeholders verbatim.
//! Substituting in all of them is a superset of what a script written
//! against rofi can rely on, since nothing can usefully depend on receiving
//! the literal string `{mode}`.

use crate::{spawn, template};

/// The `-on-*` commands for one session. Absent means nothing to run.
#[derive(Debug, Default, Clone)]
pub struct Hooks {
    /// `-on-selection-changed`: the highlighted row changed.
    pub selection_changed: Option<String>,
    /// `-on-entry-accepted`: a row (or custom input) was accepted.
    pub entry_accepted: Option<String>,
    /// `-on-mode-changed`: the active mode changed.
    pub mode_changed: Option<String>,
    /// `-on-menu-canceled`: the menu was dismissed without accepting.
    pub menu_canceled: Option<String>,
    /// `-on-menu-error`: the menu could not do what was asked.
    pub menu_error: Option<String>,
}

impl Hooks {
    /// Whether any hook is set — lets the surface skip the work of
    /// collecting row text for a session that asked for none.
    #[must_use]
    pub fn any(&self) -> bool {
        self.selection_changed.is_some()
            || self.entry_accepted.is_some()
            || self.mode_changed.is_some()
            || self.menu_canceled.is_some()
            || self.menu_error.is_some()
    }
}

/// What the event knows, for the placeholders to be filled from.
#[derive(Debug, Default, Clone)]
pub struct Context {
    /// `{input}`: the query text at the time of the event.
    pub input: String,
    /// `{entry}`: the row the event is about.
    pub entry: String,
    /// `{mode}`: the active mode's name.
    pub mode: String,
    /// `{error}`: what went wrong, for `-on-menu-error`.
    pub error: String,
}

/// Substitutes `ctx` into `command` and returns the argv to exec.
///
/// Empty when the command is blank or does not shell-parse — an unbalanced
/// quote is a broken hook, and running half of it would be worse than
/// running none of it.
///
/// One placeholder is always exactly one argument: see
/// [`template::render_argv`] for why that is not rofi's order.
#[must_use]
pub fn argv(command: &str, ctx: &Context) -> Vec<String> {
    let argv = template::render_argv(command, |key| match key {
        "input" => Some(ctx.input.clone()),
        "entry" => Some(ctx.entry.clone()),
        "mode" => Some(ctx.mode.clone()),
        "error" => Some(ctx.error.clone()),
        // An unknown placeholder renders empty, which is what
        // `template::render` does everywhere else in the launcher.
        _ => None,
    });
    // A command that renders to nothing but empty placeholders is nothing.
    if argv.iter().all(|argument| argument.trim().is_empty()) {
        return Vec::new();
    }
    argv
}

/// Runs `command` with `ctx` substituted, detached. A blank or unparseable
/// command does nothing.
pub fn fire(command: Option<&String>, ctx: &Context) {
    let Some(command) = command else {
        return;
    };
    let argv = argv(command, ctx);
    if argv.is_empty() {
        if !command.trim().is_empty() {
            tracing::warn!(%command, "launcher hook does not parse as a command");
        }
        return;
    }
    spawn::run_argv(&argv);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn ctx() -> Context {
        Context {
            input: String::from("fire"),
            entry: String::from("Firefox"),
            mode: String::from("drun"),
            error: String::from("no modes"),
        }
    }

    #[test]
    fn placeholders_are_filled_from_the_event() {
        assert_eq!(
            argv("notify-send {mode} {entry} {input}", &ctx()),
            ["notify-send", "drun", "Firefox", "fire"]
        );
        assert_eq!(argv("say {error}", &ctx()), ["say", "no modes"]);
    }

    #[test]
    fn a_row_cannot_smuggle_a_second_command_through_the_hook() {
        // The reason the argv is exec'd rather than handed to `sh -c`: a
        // dmenu feeding rofi arbitrary text must not be able to run it.
        let hostile = Context {
            entry: String::from("x; rm -rf ~"),
            ..ctx()
        };
        assert_eq!(
            argv("preview {entry}", &hostile),
            ["preview", "x; rm -rf ~"],
            "the whole row is one argument, semicolon and spaces included"
        );
    }

    #[test]
    fn a_shell_variable_stays_literal() {
        assert_eq!(argv("echo $HOME", &ctx()), ["echo", "$HOME"]);
    }

    #[test]
    fn an_unknown_placeholder_renders_empty_rather_than_verbatim() {
        assert_eq!(argv("echo a{nope}b", &ctx()), ["echo", "ab"]);
    }

    #[test]
    fn nothing_runs_for_a_blank_or_broken_command() {
        assert!(argv("", &ctx()).is_empty());
        assert!(argv("   ", &ctx()).is_empty());
        assert!(
            argv("echo 'unbalanced", &ctx()).is_empty(),
            "half a command is worse than none"
        );
        // A command that renders to nothing but placeholders is also nothing.
        assert!(argv("{nope}", &ctx()).is_empty());
    }

    #[test]
    fn a_session_with_no_hooks_asks_for_no_work() {
        assert!(!Hooks::default().any());
        assert!(
            Hooks {
                menu_canceled: Some(String::from("true")),
                ..Hooks::default()
            }
            .any()
        );
    }
}

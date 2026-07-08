//! rofi-compatible flag parsing.
//!
//! rofi uses single-dash long flags (`-show drun`, `-dmenu`) which clap
//! cannot represent, so the launcher subcommand takes the raw args and this
//! hand-rolled table parser maps them: session options travel to the daemon,
//! unsupported flags are accepted with a warning (friendlier than rofi's
//! hard error for a shim), and a few are handled locally in the CLI.

use wayle_ipc::launcher_socket::SessionOptions;

/// Commands resolved entirely in the CLI, no daemon involved.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LocalCmd {
    /// `-help`.
    Help,
    /// `-version`.
    Version,
    /// `-dump-config`: print the `[launcher]` TOML.
    DumpConfig,
    /// `-dump-theme`: no rasi themes; prints a pointer to wayle theming.
    DumpTheme,
    /// `-list-keybindings`.
    ListKeybindings,
}

/// A parsed `wayle launcher` invocation.
#[derive(Debug, Default)]
pub struct Invocation {
    /// Options forwarded to the daemon.
    pub options: SessionOptions,
    /// `-replace`: displace a live session.
    pub replace: bool,
    /// `-format` (dmenu output format, applied CLI-side).
    pub format: String,
    /// `-input <file>`: read rows from a file instead of stdin.
    pub input_file: Option<String>,
    /// `-sep`: row separator for stdin (rows are split CLI-side).
    pub row_separator: Option<String>,
    /// Local command that short-circuits the session.
    pub local: Option<LocalCmd>,
}

/// Value-taking flags accepted and ignored (X11-only, rasi theming, or
/// daemon-irrelevant). Each produces one stderr warning.
const IGNORED_WITH_VALUE: &[&str] = &[
    "theme",
    "theme-str",
    "config",
    "pid",
    "dpi",
    "plugin-path",
    "w",
    "scroll-method",
    "eh",
    "refilter-timeout-limit",
    "threads",
    "cache-dir",
    "max-history-size",
    "wayland-layer",
    "display",
    "async-pre-read",
];

/// Bare flags accepted and ignored.
const IGNORED_BARE: &[&str] = &[
    "no-lazy-grab",
    "normal-window",
    "transient-window",
    "no-plugins",
    "plugins",
    "steal-focus",
    "no-steal-focus",
    "click-to-exit",
    "no-click-to-exit",
    "xserver-i300-workaround",
    "no-config",
    "drun-use-desktop-cache",
    "drun-reload-desktop-cache",
];

/// Parse raw rofi-style args.
///
/// # Errors
///
/// Returns a usage error message when a value flag is missing its value.
#[allow(clippy::too_many_lines)] // one arm per rofi flag; splitting hurts scanability
pub fn parse(args: &[String]) -> Result<Invocation, String> {
    let mut inv = Invocation {
        format: String::from("s"),
        ..Invocation::default()
    };
    let mut iter = args.iter().peekable();

    while let Some(raw) = iter.next() {
        let flag = raw.trim_start_matches('-');
        if flag.is_empty() {
            continue;
        }
        let mut value = |name: &str| -> Result<String, String> {
            iter.next()
                .cloned()
                .ok_or_else(|| format!("-{name} requires a value"))
        };
        let opts = &mut inv.options;
        match flag {
            // ---- local ----
            "help" | "h" => inv.local = Some(LocalCmd::Help),
            "version" | "v" => inv.local = Some(LocalCmd::Version),
            "dump-config" => inv.local = Some(LocalCmd::DumpConfig),
            "dump-theme" => inv.local = Some(LocalCmd::DumpTheme),
            "list-keybindings" => inv.local = Some(LocalCmd::ListKeybindings),
            "replace" => inv.replace = true,
            "format" => inv.format = value("format")?,
            "input" => inv.input_file = Some(value("input")?),

            // ---- session: mode selection ----
            "show" => opts.mode = Some(value("show")?),
            "modes" | "modi" => opts.modes = Some(split_list(&value("modes")?)),
            "dmenu" => opts.dmenu = true,
            "e" => opts.error_message = Some(value("e")?),

            // ---- session: display ----
            "p" => opts.prompt = Some(value("p")?),
            "l" => opts.lines = value("l")?.parse().ok(),
            "mesg" => opts.mesg = Some(value("mesg")?),
            "filter" => opts.filter = Some(value("filter")?),
            "select" => opts.select = Some(value("select")?),
            "selected-row" => opts.selected_row = value("selected-row")?.parse().ok(),
            "window-title" => opts.window_title = Some(value("window-title")?),
            "location" => opts.location = value("location")?.parse().ok(),
            "monitor" | "m" => opts.monitor = Some(value("monitor")?),
            "fixed-num-lines" => opts.no_fixed_num_lines = false,
            "no-fixed-num-lines" => opts.no_fixed_num_lines = true,
            "sidebar-mode" => opts.sidebar_mode = Some(true),
            "no-sidebar-mode" => opts.sidebar_mode = Some(false),
            "cycle" => opts.cycle = Some(true),
            "no-cycle" => opts.cycle = Some(false),
            "auto-select" => opts.auto_select = Some(true),
            "no-auto-select" => opts.auto_select = Some(false),
            "hover-select" => opts.hover_select = Some(true),
            "no-hover-select" => opts.hover_select = Some(false),
            "show-icons" => opts.show_icons = Some(true),
            "no-show-icons" => opts.show_icons = Some(false),
            "icon-theme" => opts.icon_theme = Some(value("icon-theme")?),

            // ---- session: dmenu ----
            "i" => opts.case_insensitive = Some(true),
            "sep" => inv.row_separator = Some(value("sep")?),
            "multi-select" => opts.multi_select = true,
            "only-match" => opts.only_match = true,
            "no-custom" => opts.no_custom = true,
            "password" => opts.password = true,
            "markup-rows" | "markup" => opts.markup_rows = true,
            "sync" => opts.sync = true,
            "dump" => opts.dump = true,
            "u" => opts.urgent = Some(parse_ranges(&value("u")?)),
            "a" => opts.active = Some(parse_ranges(&value("a")?)),
            "ballot-selected-str" => opts.ballot_selected = Some(value("ballot-selected-str")?),
            "ballot-unselected-str" => {
                opts.ballot_unselected = Some(value("ballot-unselected-str")?);
            }
            "display-columns" => {
                opts.display_columns = Some(
                    value("display-columns")?
                        .split(',')
                        .filter_map(|part| part.trim().parse().ok())
                        .collect(),
                );
            }
            "display-column-separator" => {
                opts.display_column_separator = Some(value("display-column-separator")?);
            }
            "ellipsize-mode" => opts.ellipsize_mode = Some(value("ellipsize-mode")?),
            "keep-right" => opts.keep_right = true,

            // ---- session: matching ----
            "matching" => opts.matching = Some(value("matching")?),
            "tokenize" => opts.tokenize = Some(true),
            "no-tokenize" => opts.tokenize = Some(false),
            "matching-negate-char" => {
                opts.negate_char = value("matching-negate-char")?.chars().next();
            }
            "normalize-match" => opts.normalize_match = Some(true),
            "no-normalize-match" => opts.normalize_match = Some(false),
            "sort" => opts.sort = Some(true),
            "no-sort" => opts.sort = Some(false),
            "sorting-method" => opts.sorting_method = Some(value("sorting-method")?),
            "case-sensitive" => opts.case_sensitive = Some(true),
            "no-case-sensitive" => opts.case_sensitive = Some(false),
            "case-smart" => opts.case_smart = Some(true),
            "no-case-smart" => opts.case_smart = Some(false),

            // ---- session: per-mode ----
            "terminal" => opts.terminal = Some(value("terminal")?),
            "run-command" => opts.run_command = Some(value("run-command")?),
            "run-shell-command" => opts.run_shell_command = Some(value("run-shell-command")?),
            "run-list-command" => opts.run_list_command = Some(value("run-list-command")?),
            "ssh-client" => opts.ssh_client = Some(value("ssh-client")?),
            "ssh-command" => opts.ssh_command = Some(value("ssh-command")?),
            "parse-hosts" => opts.parse_hosts = Some(true),
            "no-parse-hosts" => opts.parse_hosts = Some(false),
            "parse-known-hosts" => opts.parse_known_hosts = Some(true),
            "no-parse-known-hosts" => opts.parse_known_hosts = Some(false),
            "window-format" => opts.window_format = Some(value("window-format")?),
            "window-command" => opts.window_command = Some(value("window-command")?),
            "window-match-fields" => {
                opts.window_match_fields = Some(split_list(&value("window-match-fields")?));
            }
            "window-hide-active-window" | "hide-active-window" => {
                opts.hide_active_window = Some(true);
            }
            "drun-categories" => {
                opts.drun_categories = Some(split_list(&value("drun-categories")?))
            }
            "drun-exclude-categories" => {
                opts.drun_exclude_categories = Some(split_list(&value("drun-exclude-categories")?));
            }
            "drun-match-fields" => {
                opts.drun_match_fields = Some(split_list(&value("drun-match-fields")?));
            }
            "drun-display-format" => {
                opts.drun_display_format = Some(value("drun-display-format")?);
            }
            "drun-show-actions" => opts.drun_show_actions = Some(true),
            "no-drun-show-actions" => opts.drun_show_actions = Some(false),
            "drun-url-launcher" => opts.drun_url_launcher = Some(value("drun-url-launcher")?),
            "combi-modes" | "combi-modi" => {
                opts.combi_modes = Some(split_list(&value("combi-modes")?));
            }
            "combi-display-format" => {
                opts.combi_display_format = Some(value("combi-display-format")?);
            }

            // ---- prefixed families ----
            _ if flag.starts_with("kb-") => {
                let action = flag.trim_start_matches("kb-").to_owned();
                opts.kb_overrides.insert(action, value(flag)?);
            }
            _ if flag.starts_with("display-") => {
                let mode = flag.trim_start_matches("display-").to_owned();
                opts.display_names.insert(mode, value(flag)?);
            }

            // ---- accepted + ignored ----
            _ if IGNORED_WITH_VALUE.contains(&flag) => {
                let _ = value(flag);
                warn_ignored(flag);
            }
            _ if IGNORED_BARE.contains(&flag) => warn_ignored(flag),
            _ => eprintln!("wayle launcher: unknown option -{flag} (skipped)"),
        }
    }
    Ok(inv)
}

fn warn_ignored(flag: &str) {
    eprintln!("wayle launcher: -{flag} is not supported by wayle and was ignored");
}

/// rofi list flags are comma-separated (modes also accept `#`).
fn split_list(raw: &str) -> Vec<String> {
    raw.split([',', '#'])
        .map(str::trim)
        .filter(|part| !part.is_empty())
        .map(ToOwned::to_owned)
        .collect()
}

/// rofi `-u`/`-a` index lists: `1,3-5,8`.
fn parse_ranges(raw: &str) -> Vec<u32> {
    let mut out = Vec::new();
    for part in raw.split(',') {
        let part = part.trim();
        if let Some((start, end)) = part.split_once('-') {
            if let (Ok(start), Ok(end)) = (start.trim().parse::<u32>(), end.trim().parse::<u32>()) {
                out.extend(start..=end);
            }
        } else if let Ok(index) = part.parse::<u32>() {
            out.push(index);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn parse_ok(args: &[&str]) -> Invocation {
        parse(&args.iter().map(ToString::to_string).collect::<Vec<_>>()).unwrap()
    }

    #[test]
    fn show_mode_and_prompt() {
        let inv = parse_ok(&["-show", "drun", "-p", "apps"]);
        assert_eq!(inv.options.mode.as_deref(), Some("drun"));
        assert_eq!(inv.options.prompt.as_deref(), Some("apps"));
    }

    #[test]
    fn dmenu_flags() {
        let inv = parse_ok(&[
            "-dmenu",
            "-i",
            "-multi-select",
            "-password",
            "-mesg",
            "hello",
            "-format",
            "i",
        ]);
        assert!(inv.options.dmenu);
        assert!(inv.options.multi_select);
        assert!(inv.options.password);
        assert_eq!(inv.options.case_insensitive, Some(true));
        assert_eq!(inv.options.mesg.as_deref(), Some("hello"));
        assert_eq!(inv.format, "i");
    }

    #[test]
    fn ranges_expand() {
        assert_eq!(parse_ranges("1,3-5,8"), vec![1, 3, 4, 5, 8]);
        assert_eq!(parse_ranges("0"), vec![0]);
    }

    #[test]
    fn kb_and_display_prefixes() {
        let inv = parse_ok(&["-kb-accept-entry", "Return", "-display-drun", "apps"]);
        assert_eq!(
            inv.options
                .kb_overrides
                .get("accept-entry")
                .map(String::as_str),
            Some("Return")
        );
        assert_eq!(
            inv.options.display_names.get("drun").map(String::as_str),
            Some("apps")
        );
    }

    #[test]
    fn modes_split_on_comma_and_hash() {
        let inv = parse_ok(&["-modi", "drun,run#clip:~/bin/clip.sh"]);
        assert_eq!(
            inv.options.modes,
            Some(vec![
                "drun".to_owned(),
                "run".to_owned(),
                "clip:~/bin/clip.sh".to_owned()
            ])
        );
    }

    #[test]
    fn ignored_flags_do_not_error() {
        let inv = parse_ok(&["-theme", "gruvbox", "-no-lazy-grab", "-show", "run"]);
        assert_eq!(inv.options.mode.as_deref(), Some("run"));
    }

    #[test]
    fn missing_value_errors() {
        let err = parse(&["-show".to_owned()]).unwrap_err();
        assert!(err.contains("-show"));
    }

    #[test]
    fn double_dash_tolerated() {
        let inv = parse_ok(&["--show", "drun"]);
        assert_eq!(inv.options.mode.as_deref(), Some("drun"));
    }

    #[test]
    fn local_commands() {
        assert_eq!(
            parse_ok(&["-dump-config"]).local,
            Some(LocalCmd::DumpConfig)
        );
        assert_eq!(
            parse_ok(&["-list-keybindings"]).local,
            Some(LocalCmd::ListKeybindings)
        );
    }
}

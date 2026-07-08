//! Default keybindings (rofi's defaults) and override merging.
//!
//! Action names are rofi's `kb-` names without the prefix. Key strings are
//! comma-separated GTK accelerator names.

/// Default binding table: `(action, keys)`.
pub const DEFAULTS: &[(&str, &str)] = &[
    ("accept-entry", "Return,KP_Enter"),
    ("accept-alt", "Shift+Return"),
    ("accept-custom", "Control+Return"),
    ("accept-custom-alt", "Control+Shift+Return"),
    ("cancel", "Escape,Control+g,Control+bracketleft"),
    ("delete-entry", "Shift+Delete"),
    ("mode-next", "Shift+Right,Control+Tab"),
    ("mode-previous", "Shift+Left,Control+ISO_Left_Tab"),
    ("mode-complete", "Control+l"),
    ("row-up", "Up,Control+p"),
    ("row-down", "Down,Control+n"),
    ("row-left", "Control+Page_Up"),
    ("row-right", "Control+Page_Down"),
    ("row-first", "Home,KP_Home"),
    ("row-last", "End,KP_End"),
    ("row-select", "Control+space"),
    ("page-prev", "Page_Up"),
    ("page-next", "Page_Down"),
    ("element-next", "Tab"),
    ("element-prev", "ISO_Left_Tab"),
    ("toggle-case-sensitivity", "grave,dead_grave"),
    ("toggle-sort", "Alt+grave"),
    ("clear-line", "Control+w"),
    ("move-front", "Control+a"),
    ("move-end", "Control+e"),
    ("move-word-back", "Alt+b,Control+Left"),
    ("move-word-forward", "Alt+f,Control+Right"),
    ("move-char-back", "Left,Control+b"),
    ("move-char-forward", "Right,Control+f"),
    ("remove-word-back", "Control+Alt+h,Control+BackSpace"),
    ("remove-word-forward", "Control+Alt+d"),
    ("remove-char-back", "BackSpace,Shift+BackSpace,Control+h"),
    ("remove-char-forward", "Delete,Control+d"),
    ("remove-to-eol", "Control+k"),
    ("remove-to-sol", "Control+u"),
    ("paste-primary", "Shift+Insert"),
    ("paste-secondary", "Control+v,Insert"),
    ("select-element", "space"),
    ("custom-1", "Alt+1"),
    ("custom-2", "Alt+2"),
    ("custom-3", "Alt+3"),
    ("custom-4", "Alt+4"),
    ("custom-5", "Alt+5"),
    ("custom-6", "Alt+6"),
    ("custom-7", "Alt+7"),
    ("custom-8", "Alt+8"),
    ("custom-9", "Alt+9"),
    ("custom-10", "Alt+0"),
    ("custom-11", "Alt+exclam"),
    ("custom-12", "Alt+at"),
    ("custom-13", "Alt+numbersign"),
    ("custom-14", "Alt+dollar"),
    ("custom-15", "Alt+percent"),
    ("custom-16", "Alt+dead_circumflex"),
    ("custom-17", "Alt+ampersand"),
    ("custom-18", "Alt+asterisk"),
    ("custom-19", "Alt+parenleft"),
];

/// Effective bindings: rofi defaults with per-action `overrides` applied
/// (config `[launcher.keybindings]`, then per-session `-kb-*`).
pub fn effective(overrides: &std::collections::BTreeMap<String, String>) -> Vec<(String, String)> {
    DEFAULTS
        .iter()
        .map(|(action, keys)| {
            let keys = overrides
                .get(*action)
                .cloned()
                .unwrap_or_else(|| (*keys).to_owned());
            ((*action).to_owned(), keys)
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn overrides_replace_defaults() {
        let mut overrides = std::collections::BTreeMap::new();
        overrides.insert("cancel".to_owned(), "Control+q".to_owned());
        let bindings = effective(&overrides);
        let cancel = bindings.iter().find(|(action, _)| action == "cancel");
        assert_eq!(cancel.map(|(_, keys)| keys.as_str()), Some("Control+q"));
        let accept = bindings.iter().find(|(action, _)| action == "accept-entry");
        assert_eq!(
            accept.map(|(_, keys)| keys.as_str()),
            Some("Return,KP_Enter")
        );
    }
}

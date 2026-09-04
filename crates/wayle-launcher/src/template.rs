//! rofi-style `{placeholder}` templates with `[optional]` blocks.
//!
//! `{key}` is replaced by its value. A `[...]` block is emitted only if
//! every `{key}` inside it resolved non-empty (rofi PATTERN semantics,
//! used by drun-display-format, window-format, ssh-command, combi).

/// Render `template` using `lookup` (returns None/empty for absent keys).
pub fn render(template: &str, lookup: impl Fn(&str) -> Option<String>) -> String {
    let mut out = String::with_capacity(template.len());
    let mut chars = template.chars().peekable();
    while let Some(ch) = chars.next() {
        match ch {
            '[' => {
                let mut block = String::new();
                let mut depth = 1;
                for inner in chars.by_ref() {
                    match inner {
                        '[' => depth += 1,
                        ']' => {
                            depth -= 1;
                            if depth == 0 {
                                break;
                            }
                        }
                        _ => {}
                    }
                    block.push(inner);
                }
                let (rendered, all_filled) = render_block(&block, &lookup);
                if all_filled {
                    out.push_str(&rendered);
                }
            }
            '{' => {
                let key: String = collect_key(&mut chars);
                if let Some(value) = lookup(&key) {
                    out.push_str(&value);
                }
            }
            _ => out.push(ch),
        }
    }
    out
}

/// Renders `template` as an argv: shell-split **first**, then each argument
/// rendered separately.
///
/// The order matters and is deliberately not rofi's. rofi substitutes into
/// the command string and shell-parses the result, so a value containing a
/// space becomes several arguments and a value containing a quote breaks the
/// parse — verified against rofi 2.0.0, where a row named `it's here` makes
/// `-on-selection-changed` run nothing at all, and one named `my song.mp3`
/// arrives as two arguments.
///
/// Splitting first means one placeholder is always exactly one argument,
/// whatever is in it, so quoting in the template is optional rather than
/// load-bearing, and a value cannot smuggle in extra arguments.
///
/// Empty when the template does not shell-parse: half a command is worse
/// than no command.
pub fn render_argv(template: &str, lookup: impl Fn(&str) -> Option<String>) -> Vec<String> {
    shlex::split(template)
        .unwrap_or_default()
        .iter()
        .map(|argument| render(argument, &lookup))
        .collect()
}

fn render_block(block: &str, lookup: &impl Fn(&str) -> Option<String>) -> (String, bool) {
    let mut out = String::with_capacity(block.len());
    let mut all_filled = true;
    let mut chars = block.chars().peekable();
    while let Some(ch) = chars.next() {
        if ch == '{' {
            let key = collect_key(&mut chars);
            match lookup(&key).filter(|value| !value.is_empty()) {
                Some(value) => out.push_str(&value),
                None => all_filled = false,
            }
        } else {
            out.push(ch);
        }
    }
    (out, all_filled)
}

fn collect_key(chars: &mut std::iter::Peekable<std::str::Chars<'_>>) -> String {
    let mut key = String::new();
    for ch in chars.by_ref() {
        if ch == '}' {
            break;
        }
        key.push(ch);
    }
    key
}

#[cfg(test)]
mod tests {
    use super::render;

    fn lookup(key: &str) -> Option<String> {
        match key {
            "name" => Some("Firefox".into()),
            "generic" => Some("Web Browser".into()),
            "empty" => Some(String::new()),
            _ => None,
        }
    }

    #[test]
    fn replaces_placeholders() {
        assert_eq!(render("{name}!", lookup), "Firefox!");
    }

    #[test]
    fn optional_block_kept_when_filled() {
        assert_eq!(
            render("{name} [({generic})]", lookup),
            "Firefox (Web Browser)"
        );
    }

    #[test]
    fn optional_block_dropped_when_empty() {
        assert_eq!(render("{name} [({empty})]", lookup), "Firefox ");
        assert_eq!(render("{name} [({missing})]", lookup), "Firefox ");
    }

    #[test]
    fn unknown_placeholder_renders_empty_outside_blocks() {
        assert_eq!(render("a{missing}b", lookup), "ab");
    }

    #[test]
    fn an_argv_placeholder_is_one_argument_whatever_is_in_it() {
        use super::render_argv;

        let awkward = |key: &str| match key {
            "entry" => Some(String::from("my song.mp3")),
            _ => None,
        };
        assert_eq!(
            render_argv("play {entry} --loop", awkward),
            ["play", "my song.mp3", "--loop"],
            "a space in the value must not become an argument boundary"
        );
        // Quoting in the template is optional, not load-bearing.
        assert_eq!(
            render_argv("play \"{entry}\"", awkward),
            ["play", "my song.mp3"]
        );
    }

    #[test]
    fn a_quote_in_a_value_does_not_break_the_command() {
        use super::render_argv;

        // rofi 2.0.0 runs *nothing* for a row named `it's here`: it
        // substitutes first and then fails to shell-parse the result.
        let quoted = |key: &str| match key {
            "entry" => Some(String::from("it's here")),
            _ => None,
        };
        assert_eq!(
            render_argv("preview {entry}", quoted),
            ["preview", "it's here"]
        );
    }

    #[test]
    fn an_unparseable_template_yields_no_argv() {
        use super::render_argv;

        assert!(render_argv("preview '{entry}", lookup).is_empty());
        assert!(render_argv("", lookup).is_empty());
    }
}

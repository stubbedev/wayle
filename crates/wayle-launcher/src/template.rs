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
}

//! Glob pattern matching utilities.

use wildcard::Wildcard;

/// Returns true if the text matches the glob pattern.
pub fn matches(pattern: &str, text: &str) -> bool {
    Wildcard::new(pattern.as_bytes()).is_ok_and(|w| w.is_match(text.as_bytes()))
}
